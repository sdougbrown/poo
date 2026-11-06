package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The delivery's message is its commit point: once the message record
// lands, the input has reached the model, and the input.delivered record
// that follows it only states that. A settle that begins between the two
// records must not undo the delivery.

// deliveryHoldSink holds the first message record that delivers an input
// until the test releases it.
//
// The held write accepts its record after the release even when the
// write's context has ended, which is the shape of a log write that had
// already started when the run settled.
type deliveryHoldSink struct {
	*recordingSink
	// entered is closed when the held write reaches the sink.
	entered chan struct{}
	// release lets the held write finish.
	release chan struct{}
	// once guards the hold.
	once sync.Once
}

// newDeliveryHoldSink builds a sink that holds one delivery's message.
func newDeliveryHoldSink() *deliveryHoldSink {
	return &deliveryHoldSink{
		recordingSink: &recordingSink{},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
}

// Append holds the first delivery message.
func (s *deliveryHoldSink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.InputID != "" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.recordingSink.Append(ctx, rec)
}

// deliveredIDs returns the input ids the log's delivered records name.
func deliveredIDs(sink *recordingSink) []string {
	var out []string
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDeliveredRecord); ok {
			out = append(out, d.InputID)
		}
	}
	return out
}

// deliveryMessages returns the log's message records that name an input.
func deliveryMessages(sink *recordingSink) []*poo.MessageRecord {
	var out []*poo.MessageRecord
	for _, m := range sink.messages() {
		if m.InputID != "" {
			out = append(out, m)
		}
	}
	return out
}

// deliveryEventIDs returns the input ids the session reported delivered.
func deliveryEventIDs(s *poo.Session) []string {
	var out []string
	for _, ev := range s.Events() {
		if ev.Name != poo.EventInputDelivered {
			continue
		}
		if id, ok := ev.Payload["input_id"].(string); ok {
			out = append(out, id)
		}
	}
	return out
}

// waitingJobSession starts a run whose background job keeps it waiting.
func waitingJobSession(t *testing.T, tr poo.ChatTransport, opts poo.Options) (*poo.Session, *pootest.JobTool) {
	t.Helper()
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	opts.Transport = tr
	opts.Tools = map[string]poo.Tool{"job": tool}
	opts.ToolOrder = []string{"job"}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start the job"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool { return s.State() == poo.StateWaiting })
	return s, tool
}

// deliveryTestOptions builds the options for a waiting run with a sink.
func deliveryTestOptions(t *testing.T, sink poo.RecordSink, observers ...poo.Observer) poo.Options {
	t.Helper()
	opts := poo.Options{
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
		Observers:    observers,
	}
	return opts
}

// TestASteerWhoseMessageLandsDuringSettleIsDelivered covers the settle
// that begins between a steer's message and its delivered record. The
// message landed, so the input reached the model: it is delivered, not
// dropped, and the live view holds its message.
func TestASteerWhoseMessageLandsDuringSettleIsDelivered(t *testing.T) {
	sink := newDeliveryHoldSink()
	settling := make(chan struct{})
	obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink, obs))

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the steer token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the steer's message never reached the sink")
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
	close(sink.release)

	id := <-sent
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-cancelled

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input: the message landed",
			res.Detail.UndeliveredInput)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 1 || got[0] != id {
		t.Errorf("the log's delivered inputs are %v, want [%s]", got, id)
	}
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	if got := deliveryEventIDs(s); len(got) != 1 || got[0] != id {
		t.Errorf("the session reported %v delivered, want [%s]", got, id)
	}
	found := false
	for _, m := range poo.Messages(s) {
		if m.Text() == "the steer token" {
			found = true
		}
	}
	if !found {
		t.Error("the live view does not hold the steer's message")
	}
}

// TestAFollowUpWhoseMessageLandsDuringSettleIsDelivered covers the same
// settle for a follow-up, whose delivery the resumed drain writes after
// the natural finalization was abandoned for it.
func TestAFollowUpWhoseMessageLandsDuringSettleIsDelivered(t *testing.T) {
	sink := newDeliveryHoldSink()
	settling := make(chan struct{})
	obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
	hook := &blockingBeforeSettle{started: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	})
	opts := deliveryTestOptions(t, sink, obs)
	opts.Transport = tr
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hook.started
	id, err := s.Send("the follow-up token", poo.FollowUp)
	if err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	// The natural finalization is abandoned for the follow-up, and the
	// resumed drain writes its message.
	close(hook.release)
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the follow-up's message never reached the sink")
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
	close(sink.release)

	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-cancelled

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input: the message landed",
			res.Detail.UndeliveredInput)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 1 || got[0] != id {
		t.Errorf("the log's delivered inputs are %v, want [%s]", got, id)
	}
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	found := false
	for _, m := range poo.Messages(s) {
		if m.Text() == "the follow-up token" {
			found = true
		}
	}
	if !found {
		t.Error("the live view does not hold the follow-up's message")
	}
	if got := deliveryEventIDs(s); len(got) != 1 || got[0] != id {
		t.Errorf("the session reported %v delivered, want [%s]", got, id)
	}
}

// TestAJobOrFeedWhoseMessageLandsDuringSettleIsNotRequeued covers the
// same settle for the session's own work coming back. The result reached
// the model, so it is not requeued and not delivered twice.
func TestAJobOrFeedWhoseMessageLandsDuringSettleIsNotRequeued(t *testing.T) {
	for _, tc := range []struct {
		name string
		// token is the text the delivered message carries.
		token string
		// held is the job or feed name a held record would carry if the
		// result were requeued.
		held string
		// queue puts the result on the session's queue. It may block
		// while the delivery's message is held.
		queue func(s *poo.Session, tool *pootest.JobTool) error
	}{
		{
			name:  "job",
			token: "the job's output",
			held:  "j1",
			queue: func(s *poo.Session, tool *pootest.JobTool) error {
				tool.Release()
				return nil
			},
		},
		{
			name:  "feed",
			token: "the feed token",
			held:  "feed:inbox",
			queue: func(s *poo.Session, tool *pootest.JobTool) error {
				return poo.QueueFeedInput(s, "inbox", "", "the feed token")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := newDeliveryHoldSink()
			settling := make(chan struct{})
			obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{jobCall("one", true)}},
				{text: "the answer"},
			})
			opts := deliveryTestOptions(t, sink, obs)
			opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
			s, tool := waitingJobSession(t, tr, opts)

			queued := make(chan error, 1)
			go func() { queued <- tc.queue(s, tool) }()
			select {
			case <-sink.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the result's message never reached the sink")
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
			close(sink.release)

			if err := <-queued; err != nil {
				t.Fatalf("queueing the result: %v", err)
			}
			res, err := s.Wait(context.Background())
			if err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			<-cancelled
			// The close is the end of the log: a result that was
			// requeued is held now.
			if err := s.Close(); err != nil {
				t.Fatalf("closing the session: %v", err)
			}

			if res.Reason != poo.ReasonCancelled {
				t.Fatalf("the run settled %q, want cancelled", res.Reason)
			}
			if len(res.Detail.UndeliveredInput) != 0 {
				t.Errorf("the settle detail lists %+v, want no undelivered input",
					res.Detail.UndeliveredInput)
			}
			if got := deliveredIDs(sink.recordingSink); len(got) != 1 {
				t.Errorf("the log's delivered inputs are %v, want one", got)
			}
			if n := sink.count("input.dropped"); n != 0 {
				t.Errorf("the log holds %d input.dropped records, want none", n)
			}
			for _, rec := range sink.allRecords() {
				held, ok := rec.(*poo.InputHeldRecord)
				if !ok {
					continue
				}
				for _, job := range held.Jobs {
					if job == tc.held {
						t.Errorf("the log held %q, want it delivered", job)
					}
				}
			}
			msgs := deliveryMessages(sink.recordingSink)
			if len(msgs) != 1 {
				t.Fatalf("the log holds %d delivery messages, want one: %+v", len(msgs), msgs)
			}
			if !strings.Contains(msgs[0].Content[0].Text, tc.token) {
				t.Errorf("the delivered message holds %q, want %q", msgs[0].Content[0].Text, tc.token)
			}
			if got := deliveryEventIDs(s); len(got) != 1 || got[0] != msgs[0].InputID {
				t.Errorf("the session reported %v delivered, want [%s]", got, msgs[0].InputID)
			}
		})
	}
}

// refusingDeliveredSink refuses the input.delivered record and accepts
// everything before it.
type refusingDeliveredSink struct {
	*recordingSink
}

// Append refuses the delivered record.
func (s *refusingDeliveredSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "input.delivered" {
		return errors.New("the store refused the delivered record")
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestARefusedDeliveredRecordDoesNotUndoTheDelivery covers the store's
// refusal of the record that follows a landed message. The message is
// the delivery, so the input is not dropped and the restore reads its
// message once.
func TestARefusedDeliveredRecordDoesNotUndoTheDelivery(t *testing.T) {
	sink := &refusingDeliveredSink{recordingSink: &recordingSink{}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink))

	id, err := s.Send("the refused steer token", poo.Steer)
	if err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonStoreError {
		t.Fatalf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input: the message landed",
			res.Detail.UndeliveredInput)
	}
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 0 {
		t.Errorf("the log holds delivered inputs %v, want none: the store refused the record", got)
	}
	if got := deliveryEventIDs(s); len(got) != 1 || got[0] != id {
		t.Errorf("the session reported %v delivered, want [%s] once", got, id)
	}
	msgs := deliveryMessages(sink.recordingSink)
	if len(msgs) != 1 || msgs[0].InputID != id {
		t.Fatalf("the log's delivery messages are %+v, want one for %s", msgs, id)
	}
	found := false
	for _, m := range poo.Messages(s) {
		if m.Text() == "the refused steer token" {
			found = true
		}
	}
	if !found {
		t.Error("the live view does not hold the delivered message")
	}

	// A restore reads the message that landed once, and the missing
	// delivered record is the recovery's to complete.
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	restored, _, err := poo.Restore(restoreOptions(t, scripted(nil), &recordingSink{}), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	defer restored.Close()
	count := 0
	for _, m := range poo.Messages(restored) {
		count += strings.Count(m.Text(), "the refused steer token")
	}
	if count != 1 {
		t.Errorf("the restored view holds the steer %d times, want once", count)
	}
}

// TestACloseLetsADeliveryWhoseMessageLandedFinish covers the close that
// begins between a message and its delivered record: the message is
// before the close's fence, so the delivery finishes and the input is
// not dropped.
func TestACloseLetsADeliveryWhoseMessageLandedFinish(t *testing.T) {
	sink := newDeliveryHoldSink()
	settling := make(chan struct{})
	obs := &eventObserver{event: poo.EventRunSettling, seen: settling}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s, _ := waitingJobSession(t, tr, deliveryTestOptions(t, sink, obs))

	sent := make(chan string, 1)
	go func() {
		id, err := s.Send("the closing steer token", poo.Steer)
		if err != nil {
			t.Errorf("sending the steer: %v", err)
		}
		sent <- id
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the steer's message never reached the sink")
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	// The close finalizes the run before it fences the writer, and the
	// finalization waits for the held message write.
	select {
	case <-settling:
	case <-time.After(5 * time.Second):
		t.Fatal("the close never began settling the run")
	}
	close(sink.release)

	id := <-sent
	if err := <-closed; err != nil {
		t.Fatalf("the close failed: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 0 {
		t.Errorf("the settle detail lists %+v, want no undelivered input",
			res.Detail.UndeliveredInput)
	}
	if got := deliveredIDs(sink.recordingSink); len(got) != 1 || got[0] != id {
		t.Errorf("the log's delivered inputs are %v, want [%s]", got, id)
	}
	if n := sink.count("input.dropped"); n != 0 {
		t.Errorf("the log holds %d input.dropped records, want none", n)
	}
}
