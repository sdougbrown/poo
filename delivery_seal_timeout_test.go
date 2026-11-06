package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The seal-timeout boundary: the finalization waits for in-flight
// delivery work only for the record budget. A delivery write that
// outlives that wait is a stalled store. The run settles through the
// store failure path, the session breaks, no input is reported dropped,
// and a load resolves the input from the records that did land.

// messageThenFateSink holds the delivery message, then holds the fate
// write, so a test can place the fate write after the finalization has
// begun.
type messageThenFateSink struct {
	*recordingSink
	messageEntered chan struct{}
	messageRelease chan struct{}
	fateEntered    chan struct{}
	fateRelease    chan struct{}
	messageOnce    sync.Once
	fateOnce       sync.Once
	// ignoreFateContext makes the fate write land after the seal, as a
	// non-cooperating sink does.
	ignoreFateContext bool
}

// Append holds the first delivery message, then the first fate write.
func (s *messageThenFateSink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
		s.messageOnce.Do(func() {
			close(s.messageEntered)
			<-s.messageRelease
		})
	}
	if rec.Meta().Record == "input.delivered" {
		s.fateOnce.Do(func() {
			close(s.fateEntered)
			<-s.fateRelease
		})
		if !s.ignoreFateContext {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	return s.recordingSink.Append(ctx, rec)
}

// stalledStoreSink holds its mutex for the first record named hold, so
// every other append and the flush serialize behind it. It ignores the
// write's context, which is the non-cooperating store the bounded close
// has to survive.
type stalledStoreSink struct {
	mu      sync.Mutex
	records []poo.Record
	seq     int64
	hold    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	flushes int
}

// Append holds the first record with the held name, then records it.
func (s *stalledStoreSink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := rec.Meta().Record == s.hold
	if m, ok := rec.(*poo.MessageRecord); ok && s.hold == "message" {
		// Every other message is a system prompt or the model's own;
		// the delivery is the one that names an input.
		held = m.InputID != ""
	}
	if held {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	s.seq++
	poo.Stamp(rec, s.seq, time.Now())
	s.records = append(s.records, rec)
	return nil
}

// Flush serializes behind a held append, and counts the call.
func (s *stalledStoreSink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushes++
	return nil
}

// Records returns the records the sink accepted.
func (s *stalledStoreSink) Records() []poo.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]poo.Record(nil), s.records...)
}

// count returns how many records carry a name.
func (s *stalledStoreSink) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// deliveryMessages returns the message records that name an input.
func (s *stalledStoreSink) deliveryMessages() []*poo.MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*poo.MessageRecord
	for _, rec := range s.records {
		if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
			out = append(out, m)
		}
	}
	return out
}

// bufferedDeliverySink holds records until Flush, so a record is only
// durable once the store has flushed it. It can hold the delivery message
// without holding the lock Flush needs, which is the buffering store the
// finalization's bounded flush is for.
type bufferedDeliverySink struct {
	mu        sync.Mutex
	pending   []poo.Record
	committed []poo.Record
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

// Append holds the first delivery message, then buffers the record.
func (s *bufferedDeliverySink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	s.mu.Lock()
	s.pending = append(s.pending, rec)
	s.mu.Unlock()
	return nil
}

// Flush moves the buffered records to the durable set.
func (s *bufferedDeliverySink) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.committed = append(s.committed, s.pending...)
	s.pending = nil
	return nil
}

// Committed returns the durable records.
func (s *bufferedDeliverySink) Committed() []poo.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]poo.Record(nil), s.committed...)
}

// committedCount returns how many durable records carry a name.
func (s *bufferedDeliverySink) committedCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rec := range s.committed {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// committedDeliveryMessages returns the durable message records that
// name an input.
func (s *bufferedDeliverySink) committedDeliveryMessages() []*poo.MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*poo.MessageRecord
	for _, rec := range s.committed {
		if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
			out = append(out, m)
		}
	}
	return out
}

// TestABufferingSinkIsFlushedAtTheStallAndAtClose covers a store that
// only makes records durable on Flush. The finalization's bounded flush
// pushes what it accepted before the stall, and the close's flush pushes
// the late message once the writer ends before the close.
func TestABufferingSinkIsFlushedAtTheStallAndAtClose(t *testing.T) {
	sink := &bufferedDeliverySink{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink))
	poo.SetRecordWriteBudget(s, 100*time.Millisecond)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the buffered token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the message write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-cancelled
	wantStalledRun(t, s, res)

	// The finalization flushed what the store had accepted before the
	// stalled message. The settle record is not written; the store is
	// broken.
	if got := sink.committedCount("input.queued"); got != 1 {
		t.Errorf("the durable log holds %d input.queued records, want 1", got)
	}
	if got := sink.committedCount("run.settled"); got != 0 {
		t.Errorf("the durable log holds %d run.settled records, want none", got)
	}
	if got := len(sink.committedDeliveryMessages()); got != 0 {
		t.Errorf("the durable log holds %d delivery messages before the writer ended, want none", got)
	}

	// The writer ends before the close, so the close's flush can make
	// the message durable.
	close(sink.release)
	id := <-sent
	if err := s.Close(); err != nil {
		t.Fatalf("the close failed after the writer ended: %v", err)
	}
	msgs := sink.committedDeliveryMessages()
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the durable log's delivery messages are %+v, want one for %s", msgs, id)
	}
	if n := sink.committedCount("input.dropped"); n != 0 {
		t.Errorf("the durable log holds %d input.dropped records, want none", n)
	}
	assertRestoredOnce(t, sink.Committed(), "the buffered token")
}

// gatedDeliverySink holds the delivery message and then the fate write
// until the test releases each. It checks its context after the release,
// as a sink that honours the write's budget does.
type gatedDeliverySink struct {
	*recordingSink
	messageEntered chan struct{}
	messageRelease chan struct{}
	fateEntered    chan struct{}
	fateRelease    chan struct{}
	messageOnce    sync.Once
	fateOnce       sync.Once
}

// Append gates the delivery message, then the fate write.
func (s *gatedDeliverySink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
		s.messageOnce.Do(func() {
			close(s.messageEntered)
			<-s.messageRelease
		})
	}
	if rec.Meta().Record == "input.delivered" {
		s.fateOnce.Do(func() {
			close(s.fateEntered)
			<-s.fateRelease
		})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestASlowDeliveryWithinItsRecordBudgetsIsNotStalled covers the healthy
// slow delivery: the message and the fate each finish inside their own
// record budget, but the fate is still in flight when the seal's first
// record budget expires. The bounded delivery grace lets both finish, so
// the input is delivered and the store is not broken.
func TestASlowDeliveryWithinItsRecordBudgetsIsNotStalled(t *testing.T) {
	const budget = 800 * time.Millisecond
	sink := &gatedDeliverySink{
		recordingSink:  &recordingSink{},
		messageEntered: make(chan struct{}),
		messageRelease: make(chan struct{}),
		fateEntered:    make(chan struct{}),
		fateRelease:    make(chan struct{}),
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink))
	poo.SetRecordWriteBudget(s, budget)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the slow-delivery token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.messageEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the message write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	// The message finishes halfway through its own budget. The fate then
	// starts and is held past the seal's first deadline but well inside
	// its own budget and the delivery grace.
	time.Sleep(budget / 2)
	close(sink.messageRelease)
	select {
	case <-sink.fateEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the fate write never reached the sink")
	}
	time.Sleep(3 * budget / 4)
	close(sink.fateRelease)

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer waitCancel()
	res, err := s.Wait(waitCtx)
	if err != nil {
		t.Fatalf("the run did not settle while the slow delivery finished: %v", err)
	}
	id := <-sent
	<-cancelled

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if s.Broken() {
		t.Error("a delivery within its record budgets broke the store")
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input", res.Detail.UndeliveredInput)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 1 || got[0] != id {
		t.Errorf("the log's delivered inputs are %v, want [%s]", got, id)
	}
	if got := deliveryEventIDs(s); len(got) != 1 || got[0] != id {
		t.Errorf("the session reported %v delivered, want [%s]", got, id)
	}
	found := false
	for _, m := range poo.Messages(s) {
		if strings.Contains(m.Text(), "the slow-delivery token") {
			found = true
		}
	}
	if !found {
		t.Error("the live view does not hold the slow delivery's message")
	}
}

// TestACancelledRunListsStoreErrorLosingOnce covers the losing list when
// a store-error candidate is already present before the commit point and
// the settle write fails afterwards. The cancelled reason wins, and
// store_error is listed once, not once per failure.
func TestACancelledRunListsStoreErrorLosingOnce(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	tool := &echoTool{result: "ignored"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		close(done)
		return poo.ToolResult{Text: "the late tool result"}, nil
	}
	sink := &failingSink{failRecord: "tool.result"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	if _, err := s.Prompt(context.Background(), "start the tool"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool never started")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	res, err := s.Wait(waitCtx)
	if err != nil {
		t.Fatalf("the run did not settle: %v", err)
	}
	<-cancelled
	close(release)
	<-done

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	storeErrors := 0
	for _, losing := range res.Detail.Losing {
		if losing == string(poo.ReasonStoreError) {
			storeErrors++
		}
	}
	if storeErrors != 1 {
		t.Errorf("the losing list is %v, want store_error exactly once", res.Detail.Losing)
	}
}

// TestASlowToolDoesNotBreakTheStoreForAQueuedInput covers the reachable
// shape of the shared-deadline concern: a started synchronous call can
// consume the seal's wait, but a client input sent during it is queued,
// not a delivery claim. The seal reports the input as a known drop and
// leaves the store healthy; it does not classify the store as stalled.
func TestASlowToolDoesNotBreakTheStoreForAQueuedInput(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	tool := &echoTool{result: "ignored"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		close(done)
		return poo.ToolResult{Text: "the late tool result"}, nil
	}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	opts := testOptions(t, tr, tool, sink)
	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	if _, err := s.Prompt(context.Background(), "start the tool"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool never started")
	}
	if _, err := s.Send("the queued steer", poo.Steer); err != nil {
		t.Fatalf("queueing the steer: %v", err)
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	res, err := s.Wait(waitCtx)
	if err != nil {
		t.Fatalf("the run did not settle while the tool ignored its context: %v", err)
	}
	<-cancelled
	close(release)
	<-done

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if s.Broken() {
		t.Error("a slow tool broke the store")
	}
	if len(res.Detail.UndeliveredInput) != 1 {
		t.Errorf("the settle detail lists %+v, want the queued steer", res.Detail.UndeliveredInput)
	}
	if n := sink.count("input.dropped"); n != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", n)
	}
	if res.Detail.Source == "store" {
		t.Error("the slow tool was reported as a store failure")
	}
}

// TestASealTimeoutOnAFateWriteDoesNotDropTheInput covers a message that
// landed before the seal's wait began and a fate write that held the
// delivery past it. The run settles as a store failure, the input is not
// reported dropped or delivered live, and a load reads the message.
func TestASealTimeoutOnAFateWriteDoesNotDropTheInput(t *testing.T) {
	sink := &messageThenFateSink{
		recordingSink:  &recordingSink{},
		messageEntered: make(chan struct{}),
		messageRelease: make(chan struct{}),
		fateEntered:    make(chan struct{}),
		fateRelease:    make(chan struct{}),
	}
	settling := make(chan struct{})
	obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink, obs))
	poo.SetRecordWriteBudget(s, 500*time.Millisecond)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the fate-timeout token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.messageEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the message write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	select {
	case <-settling:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never began settling")
	}
	// The message lands inside the seal's wait; the fate write starts
	// after it and holds the delivery past the wait.
	close(sink.messageRelease)
	select {
	case <-sink.fateEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fate write never reached the sink")
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(sink.fateRelease)
	id := <-sent
	<-cancelled

	wantStalledRun(t, s, res)
	assertNoSettledRecord(t, sink.count("run.settled"))
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 0 {
		t.Errorf("the log holds delivered inputs %v, want none: the fate write was canceled", got)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	assertNoLiveDelivery(t, s, "the fate-timeout token")

	if err := s.Close(); err != nil {
		t.Fatalf("the close failed after the writer ended: %v", err)
	}
	assertRestoredOnce(t, sink.allRecords(), "the fate-timeout token")
}

// TestALateFateWriteAfterTheSealLeavesAConsistentDelivery covers a sink
// that ignores the canceled fate context and writes input.delivered
// after the seal. The late record states the delivery the message already
// committed: it is not a drop, it does not mutate the settled run, and a
// load reads the input once.
func TestALateFateWriteAfterTheSealLeavesAConsistentDelivery(t *testing.T) {
	sink := &messageThenFateSink{
		recordingSink:     &recordingSink{},
		messageEntered:    make(chan struct{}),
		messageRelease:    make(chan struct{}),
		fateEntered:       make(chan struct{}),
		fateRelease:       make(chan struct{}),
		ignoreFateContext: true,
	}
	settling := make(chan struct{})
	obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink, obs))
	poo.SetRecordWriteBudget(s, 500*time.Millisecond)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the late-fate token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.messageEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the message write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	select {
	case <-settling:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never began settling")
	}
	close(sink.messageRelease)
	select {
	case <-sink.fateEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fate write never reached the sink")
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(sink.fateRelease)
	id := <-sent
	<-cancelled

	wantStalledRun(t, s, res)
	assertNoSettledRecord(t, sink.count("run.settled"))
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 1 || got[0] != id {
		t.Errorf("the log's delivered inputs are %v, want [%s]", got, id)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	assertNoLiveDelivery(t, s, "the late-fate token")
	if err := s.Close(); err != nil {
		t.Fatalf("the close failed after the writer ended: %v", err)
	}
	assertRestoredOnce(t, sink.allRecords(), "the late-fate token")
}

// TestASealTimeoutOnAMessageWriteDoesNotDropTheInput covers the message
// write itself outliving the seal. The message lands after the run's
// result, but no drop is written for it, and a load reads it once.
func TestASealTimeoutOnAMessageWriteDoesNotDropTheInput(t *testing.T) {
	sink := newDeliveryHoldSink()
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink))
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the message-timeout token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the message write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(sink.release)
	id := <-sent
	<-cancelled

	wantStalledRun(t, s, res)
	assertNoSettledRecord(t, sink.count("run.settled"))
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 0 {
		t.Errorf("the log holds delivered inputs %v, want none", got)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	assertNoLiveDelivery(t, s, "the message-timeout token")

	if err := s.Close(); err != nil {
		t.Fatalf("the close failed after the writer ended: %v", err)
	}
	assertRestoredOnce(t, sink.allRecords(), "the message-timeout token")
}

// TestAJobMessageAfterTheSealDoesNotMutateTheSettledRun covers the job
// result whose message lands after the seal. The message is in the log,
// so a load reads it, but the settled run's view and events stay as the
// run reported them, and the result is not requeued.
func TestAJobMessageAfterTheSealDoesNotMutateTheSettledRun(t *testing.T) {
	sink := newDeliveryHoldSink()
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	opts := deliveryTestOptions(t, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s, tool := waitingJobSession(t, tr, opts)
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	queued := make(chan struct{})
	go func() {
		tool.Release()
		close(queued)
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the result's message never reached the sink")
	}
	<-queued

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	before := len(poo.Messages(s))
	close(sink.release)
	<-cancelled

	wantStalledRun(t, s, res)
	assertNoSettledRecord(t, sink.count("run.settled"))
	waitFor(t, "the late message to land", func() bool {
		return len(deliveryMessages(sink.recordingSink)) == 1
	})
	if got := len(poo.Messages(s)); got != before {
		t.Errorf("the settled run's view grew from %d to %d messages", before, got)
	}
	if got := deliveryEventIDs(s); len(got) != 0 {
		t.Errorf("the session reported %v delivered after the seal, want none", got)
	}
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none: a result is never dropped", n)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 0 {
		t.Errorf("the log holds delivered inputs %v, want none: the fate write was canceled", got)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content[0].Text, "the job's output") {
		t.Fatalf("the log's delivery messages are %+v, want the job result", msgs)
	}
	for _, rec := range sink.allRecords() {
		if held, ok := rec.(*poo.InputHeldRecord); ok {
			for _, job := range held.Jobs {
				if job == "j1" {
					t.Error("the late result was held, want it delivered")
				}
			}
		}
	}
	// The message counts as the result's delivery, so a load does not
	// queue it again.
	if got := poo.PendingJobResults(sink.allRecords()); len(got) != 0 {
		t.Errorf("the records queue %+v, want no requeued result", got)
	}
}

// lateFailureSink holds the first delivery message and then refuses it.
type lateFailureSink struct {
	*recordingSink
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

// Append holds a delivery message and refuses it after the release.
func (s *lateFailureSink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
		return s.err
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestAJobMessageFailureAfterTheSealRequeuesOnLoad covers the other side
// of a late job delivery: the message did not land, the session is
// broken, and the queued result is for a load to deliver. No live wake
// or held record follows the settled run.
func TestAJobMessageFailureAfterTheSealRequeuesOnLoad(t *testing.T) {
	sink := &lateFailureSink{
		recordingSink: &recordingSink{},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
		err:           errors.New("the store refused the late message"),
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	opts := deliveryTestOptions(t, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s, tool := waitingJobSession(t, tr, opts)
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	queued := make(chan struct{})
	go func() {
		tool.Release()
		close(queued)
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the result's message never reached the sink")
	}
	<-queued

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(sink.release)
	<-cancelled

	wantStalledRun(t, s, res)
	assertNoSettledRecord(t, sink.count("run.settled"))
	waitFor(t, "the failed result to be queued", func() bool {
		return s.QueuedJobResults() == 1
	})
	if n := sink.count("input.held"); n != 0 {
		t.Errorf("the log holds %d input.held records, want none: the broken session does not run", n)
	}
	if len(deliveryMessages(sink.recordingSink)) != 0 {
		t.Error("the refused message was recorded")
	}
	if got := poo.PendingJobResults(sink.allRecords()); len(got) != 1 {
		t.Errorf("the records queue %d results, want the failed one", len(got))
	}
}

// TestAStalledDeliveryWriteBreaksTheSessionAndClosesBounded covers the
// non-cooperating store: the delivery write holds the sink's lock, so
// every later write and the flush serialize behind it. The run still
// settles bounded, the close still returns a bounded ErrCloseStalled,
// and the late message does not become a live delivery or a drop.
func TestAStalledDeliveryWriteBreaksTheSessionAndClosesBounded(t *testing.T) {
	sink := &stalledStoreSink{
		hold:    "message",
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink))
	poo.SetRecordWriteBudget(s, 100*time.Millisecond)

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the stalled token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the delivery write never reached the sink")
	}

	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	res, err := s.Wait(waitCtx)
	if err != nil {
		t.Fatalf("the run did not settle while the store was stalled: %v", err)
	}
	<-cancelled
	wantStalledRun(t, s, res)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if !errors.Is(err, poo.ErrCloseStalled) {
			t.Fatalf("the close returned %v, want ErrCloseStalled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the close did not finish while the store was stalled")
	}

	// The writer ends. The message lands, but the settled run and the
	// session stay as they were.
	close(sink.release)
	id := <-sent
	assertNoSettledRecord(t, sink.count("run.settled"))
	waitFor(t, "the late message to land", func() bool {
		return len(sink.deliveryMessages()) == 1
	})
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	if n := sink.count("input.delivered"); n != 0 {
		t.Errorf("the log holds %d input.delivered records, want none", n)
	}
	msgs := sink.deliveryMessages()
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	assertNoLiveDelivery(t, s, "the stalled token")
	if err := s.Close(); err != nil {
		t.Fatalf("the second close failed: %v", err)
	}
	assertRestoredOnce(t, sink.Records(), "the stalled token")
}

// assertNoSettledRecord asserts the broken store did not accept a settle
// record: a load writes the interrupted one from the log's last state.
func assertNoSettledRecord(t *testing.T, count int) {
	t.Helper()
	if count != 0 {
		t.Errorf("the log holds %d run.settled records, want none: the store is broken", count)
	}
}

// wantStalledRun asserts the settled-store outcome a delivery write that
// outlives the seal produces.
func wantStalledRun(t *testing.T, s *poo.Session, res poo.Result) {
	t.Helper()
	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	losing := false
	for _, reason := range res.Detail.Losing {
		if reason == string(poo.ReasonStoreError) {
			losing = true
		}
	}
	if !losing {
		t.Errorf("the settle detail's losing list is %v, want store_error", res.Detail.Losing)
	}
	if !s.Broken() {
		t.Error("the session is not broken after a delivery write outlived the seal")
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input", res.Detail.UndeliveredInput)
	}
	if n := len(deliveryEventIDs(s)); n != 0 {
		t.Errorf("the session reported %d deliveries, want none", n)
	}
	settled := 0
	for _, ev := range s.Events() {
		switch ev.Name {
		case poo.EventRunSettled:
			settled++
			if got := ev.Payload["reason"]; got != string(res.Reason) {
				t.Errorf("the settled event carries reason %v, want %q", got, res.Reason)
			}
		case poo.EventInputDropped:
			t.Error("the session reported an input dropped for a stalled delivery")
		}
	}
	if settled != 1 {
		t.Errorf("the session emitted %d run.settled events, want 1", settled)
	}
}

// assertNoLiveDelivery asserts the settled run's view holds no message
// with token.
func assertNoLiveDelivery(t *testing.T, s *poo.Session, token string) {
	t.Helper()
	for _, m := range poo.Messages(s) {
		if strings.Contains(m.Text(), token) {
			t.Errorf("the settled run's view holds the late message %q", token)
		}
	}
}

// assertRestoredOnce asserts a restore reads token once and queues no
// result for it.
func assertRestoredOnce(t *testing.T, records []poo.Record, token string) {
	t.Helper()
	all := append([]poo.Record{header("s1")}, records...)
	restored, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), all)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer restored.Close()
	count := 0
	for _, m := range poo.Messages(restored) {
		count += strings.Count(m.Text(), token)
	}
	if count != 1 {
		t.Errorf("the restored view holds %q %d times, want once", token, count)
	}
	if got := poo.PendingJobResults(records); len(got) != 0 {
		t.Errorf("the records queue %+v, want none for the landed message", got)
	}
}
