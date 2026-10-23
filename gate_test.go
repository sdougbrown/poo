package poo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The event bus: every event carries a session-monotonic sequence
// number, and the events arrive in the order they happened.

// TestTheEventsCarrySessionMonotonicNumbers covers the bus: the
// sequence numbers start at one and never go backwards.
func TestTheEventsCarrySessionMonotonicNumbers(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	events := s.Events()
	if len(events) == 0 {
		t.Fatal("the session published no events")
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Errorf("the event at %d has the sequence number %d, want %d", i, ev.Seq, i+1)
		}
		if ev.Name == "" {
			t.Errorf("the event at %d has no name", i)
		}
		if ev.At == "" {
			t.Errorf("the event at %d has no timestamp", i)
		}
	}
}

// TestTheObserverSeesTheEventsInOrder covers the observer: it sees
// every event, in the session's order.
func TestTheObserverSeesTheEventsInOrder(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	observer := &collectingObserver{done: make(chan struct{})}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Observers = []poo.Observer{observer}

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")
	// The observer's goroutine may still be delivering, so the test
	// waits for it to catch up with the session's own event log.
	deadline := time.After(5 * time.Second)
	for {
		observer.mu.Lock()
		count := len(observer.events)
		observer.mu.Unlock()
		if count >= len(s.Events()) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("the observer saw %d events, want %d", count, len(s.Events()))
		case <-time.After(time.Millisecond):
		}
	}

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.events) < 2 {
		t.Fatalf("the observer saw %d events, want at least 2", len(observer.events))
	}
	for i := 1; i < len(observer.events); i++ {
		if observer.events[i].Seq <= observer.events[i-1].Seq {
			t.Errorf("the observer's events are out of order: %d then %d",
				observer.events[i-1].Seq, observer.events[i].Seq)
			break
		}
	}
}

// TestTheObserverErrorDoesNotSettleTheRun covers the plan's rule: an
// observer's error never settles a run.
func TestTheObserverErrorDoesNotSettleTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	observer := &failingObserver{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Observers = []poo.Observer{observer}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: an observer's error does not settle a run", res.Reason)
	}
}

// TestTheGateRunsOnTheBeforeSettleContinuePath covers the plan's gate:
// a BeforeSettle Continue goes through the gate, and the gate's budget
// check fires on that path.
func TestTheGateRunsOnTheBeforeSettleContinuePath(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	hook := &continuingBeforeSettle{stopAfter: 1}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.Compactor = compactor
	// A window the run cannot answer in, so the gate's budget check
	// stops it.
	opts.ContextLimit = 300

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	// The gate asked the compactor, which is the gate's step 1.
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked: the Continue did not go through the gate")
	}
}

// TestTheGateRunsOnTheInterruptedTurnPath covers the same rule for the
// interrupted turn.
func TestTheGateRunsOnTheInterruptedTurnPath(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	tr := newStallingTransport()
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Compactor = compactor
	opts.ContextLimit = 1200

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled %q: an interrupt does not end the run", res.Reason)
	}
	// The compactor is the gate's first step, so the interrupted turn
	// went through the gate.
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked: the interrupted turn did not go through the gate")
	}
}

// TestTheGateRunsOnTheStepSixPath covers the same rule for the step-6
// continuation: a turn that processed a call goes to the gate.
func TestTheGateRunsOnTheStepSixPath(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the answer"},
	})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Compactor = compactor
	opts.ContextLimit = 300

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked")
	}
}

// TestTheGateRunsOnTheFinalizeInputPath covers the same rule for the
// input delivered at finalize step 1: the abandoned finalization goes
// to the gate.
func TestTheGateRunsOnTheFinalizeInputPath(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Compactor = compactor
	opts.ContextLimit = 300
	// A window the run cannot answer in, so the gate's budget check
	// stops it on the resumed path.
	opts.MaxTurns = 5

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The steer is queued while the run is settling naturally, and it
	// abandons the finalization.
	if _, err := s.Send("and this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked: the abandoned finalization did not go through the gate")
	}
}

// TestTheCompactingStateIsCancelledByAnInterrupt covers the plan's rule:
// an interrupt during the compaction cancels it, and no compaction is
// applied.
func TestTheCompactingStateIsCancelledByAnInterrupt(t *testing.T) {
	// The compactor waits for its context, so the interrupt is what
	// ends it.
	compactor := &blockingCompactor{started: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Compactor = compactor
	opts.ContextLimit = 300

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-compactor.started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonHookError {
		t.Errorf("the run settled %q: an interrupt is not a hook error", res.Reason)
	}
	// The compaction was not applied.
	if compactor.applied {
		t.Error("the compaction was applied after the interrupt")
	}
	// The interrupt during the compaction emits no turn events: the
	// compaction is not a turn. The compaction's own events are the
	// ones the run emitted around it.
	started := 0
	ended := 0
	for _, ev := range s.Events() {
		switch ev.Name {
		case poo.EventCompactionStarted:
			started++
		case poo.EventCompactionEnded:
			ended++
		case poo.EventTurnStarted, poo.EventTurnEnded:
			// The turn the compaction interrupted had already
			// ended, and the resumed turn is a turn of its own.
		}
	}
	if started != 1 {
		t.Errorf("the run emitted %d compaction.started events, want 1", started)
	}
	if ended != 1 {
		t.Errorf("the run emitted %d compaction.ended events, want 1", ended)
	}
}

// TestTheCompactionIsRecorded covers the plan's rule: the compaction is
// recorded, so a replay knows what the compactor did.
func TestTheCompactionIsRecorded(t *testing.T) {
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "handoff"})
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
}

// collectingObserver is an observer that keeps the events it saw.
type collectingObserver struct {
	mu sync.Mutex
	// events holds the events the observer saw.
	events []poo.Event
	// done is closed when the observer is stopped.
	done chan struct{}
}

// Observe keeps the event.
func (o *collectingObserver) Observe(ev poo.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, ev)
	return nil
}

// failingObserver is an observer that always fails.
type failingObserver struct{}

// Observe fails.
func (o *failingObserver) Observe(ev poo.Event) error {
	return context.Canceled
}

// blockingCompactor is a compactor that waits for its context.
type blockingCompactor struct {
	// started is closed once the compaction is running.
	started chan struct{}
	// applied reports that the compaction was applied.
	applied bool
	// once guards the started channel.
	once sync.Once
}

// ShouldCompact says yes.
func (c *blockingCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	return true
}

// Compact waits for its context.
func (c *blockingCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return poo.Compaction{}, ctx.Err()
}

// releasingCompactor blocks inside Compact until the test releases it,
// independent of its context, which lets a timer settle the run while
// the gate is inside the compaction.
type releasingCompactor struct {
	// started is closed once the compaction is running.
	started chan struct{}
	// release is closed to let the compaction return.
	release chan struct{}
	// once guards the started channel.
	once sync.Once
}

// ShouldCompact says yes.
func (c *releasingCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	return true
}

// Compact waits for the release and then fails.
func (c *releasingCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return poo.Compaction{}, errors.New("the compaction failed")
}

// TestARunWindingDownAfterTheSettleDoesNotMoveTheStateOrRecord covers
// the plan's rule that a run-owned completion after the commit point
// never mutates a settled run: a duration timer settles a run whose gate
// is inside a compaction, and the compaction's return must not move the
// state, open a request, or write a record.
func TestARunWindingDownAfterTheSettleDoesNotMoveTheStateOrRecord(t *testing.T) {
	compactor := &releasingCompactor{started: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	opts.MaxDuration = 30 * time.Millisecond

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-compactor.started
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Fatalf("the run settled %q, want budget_exhausted", res.Reason)
	}
	recordsBefore := len(sink.allRecords())
	eventsBefore := len(s.Events())

	close(compactor.release)
	time.Sleep(200 * time.Millisecond)

	if got := s.State(); got != poo.StateSettled {
		t.Errorf("the session is %q after the compaction returned, want settled", got)
	}
	if n := len(tr.Requests()); n != 0 {
		t.Errorf("the winding-down run made %d requests, want none", n)
	}
	if got := len(sink.allRecords()); got != recordsBefore {
		t.Errorf("the winding-down run wrote %d records, want none", got-recordsBefore)
	}
	if got := len(s.Events()); got != eventsBefore {
		t.Errorf("the winding-down run emitted %d events, want none", got-eventsBefore)
	}
}

// TestTheCompactionRecordNamesItsFormAndTokens covers the plan's
// compaction record: it carries the form of the compaction and the
// token estimate before and after it.
func TestTheCompactionRecordNamesItsFormAndTokens(t *testing.T) {
	compaction := poo.Compaction{
		Strategy: "summarize",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 1, Last: 1},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}
	compactor := pootest.NewCompactor(true, compaction)
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	var rec *poo.CompactionRecord
	for _, r := range sink.allRecords() {
		if c, ok := r.(*poo.CompactionRecord); ok {
			rec = c
		}
	}
	if rec == nil {
		t.Fatal("the log holds no compaction record")
	}
	if rec.Form != "replace" {
		t.Errorf("the compaction's form is %q, want replace", rec.Form)
	}
	if rec.TokensBefore <= 0 {
		t.Errorf("the compaction's tokens_before is %d, want a positive estimate", rec.TokensBefore)
	}
	if rec.TokensAfter != rec.TokensBefore {
		t.Errorf("the compaction's tokens_after is %d, want %d while the view is not applied",
			rec.TokensAfter, rec.TokensBefore)
	}
}

// forkCaptureCompactor runs the fork once and records what came back.
type forkCaptureCompactor struct {
	// gates counts the run's gate passes: the first one opens the
	// run's first request, and a compactor cannot fork before one.
	gates       int
	compactions int
	msg         poo.Message
	usage       poo.UsageReport
}

// ShouldCompact compacts on the run's second gate: the first one opens
// the run's first request, and a compactor cannot fork before one.
func (c *forkCaptureCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	c.gates++
	return c.gates == 2
}

// Compact forks one request and records the reply and its usage.
func (c *forkCaptureCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.compactions++
	msg, usage, err := fork(ctx, []poo.Message{{Role: poo.RoleUser, Content: []poo.ContentBlock{{Kind: poo.ContentText, Text: "write the handoff"}}}}, 0)
	if err != nil {
		return poo.Compaction{}, err
	}
	c.msg, c.usage = msg, usage
	return poo.Compaction{Strategy: "forky"}, nil
}

// TestACompactorsForkCountsTowardTheRunBudget covers the plan's rule
// that a fork's usage counts toward MaxRunTokens, not MaxTurns: the
// fork's usage report lands in the run's token count, and the ceiling
// fires on accounting that includes it. The ceiling sits just under a
// million tokens, which only the fork's usage can reach.
func TestACompactorsForkCountsTowardTheRunBudget(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the answer", usage: &poo.UsageReport{InputTokens: 10}},
		{text: "the second answer"},
	})
	tr.SetForkTurns(pootest.Turn{Text: "the handoff", InputTokens: 999990, OutputTokens: 10})
	compactor := &forkCaptureCompactor{}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor
	opts.MaxRunTokens = 1000000
	// The gate runs on a continue path: the first turn continues, and
	// the compaction happens before the second request.
	opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{stopAfter: 1}}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted: the fork's usage is what crosses the ceiling", res.Reason)
	}
	if res.Detail.Source != "max_run_tokens" {
		t.Errorf("the settle source is %q, want max_run_tokens", res.Detail.Source)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d requests, want 2", res.Turns)
	}
	// The fork returned the scripted reply and its usage.
	if compactor.msg.Content[0].Text != "the handoff" {
		t.Errorf("the fork returned %q, want the handoff", compactor.msg.Content[0].Text)
	}
	if compactor.usage.InputTokens != 999990 || compactor.usage.OutputTokens != 10 {
		t.Errorf("the fork's usage was %d/%d, want 999990/10", compactor.usage.InputTokens, compactor.usage.OutputTokens)
	}
	// The compaction itself is recorded.
	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
	// The fork carried the appended message against the rendered prefix.
	forks := tr.Forks()
	if len(forks) != 1 {
		t.Fatalf("the transport served %d forks, want 1", len(forks))
	}
	if len(forks[0].Appended) != 1 || forks[0].Appended[0].Content[0].Text != "write the handoff" {
		t.Errorf("the fork's appended message is %+v, want the handoff request", forks[0].Appended)
	}
}

// TestTheGateRechecksTheOutputRoomAfterTheNotice covers the gate's own
// promise: no request leaves with a max_tokens under the minimum. The
// usage report lands the run just under the notice's ratio, so the
// notice's own tokens are what push the view past the room to answer —
// and the gate settles context_exhausted instead of opening the
// request.
func TestTheGateRechecksTheOutputRoomAfterTheNotice(t *testing.T) {
	// The first turn runs a tool and reports the usage that puts the
	// estimate just inside the notice's ratio, so the notice's own
	// tokens are what take the room to answer away.
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}, usage: &poo.UsageReport{InputTokens: 2475}},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.ContextLimit = 3000
	opts.NoticeRatio = 0.80

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
}

// failingGateCompactor fails the compaction once, on the run's second
// gate: the first one opens the run's first request.
type failingGateCompactor struct {
	gates int
	err   error
}

// ShouldCompact compacts on the run's second gate only.
func (c *failingGateCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	c.gates++
	return c.gates == 2
}

// Compact fails.
func (c *failingGateCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	return poo.Compaction{}, c.err
}

// TestAFailingCompactorOverTheHardLimitSettlesContextExhausted covers
// the compactor-failure row of the failure table: a compaction that
// fails at or above the hard ratio settles context_exhausted with the
// compaction_failed source.
func TestAFailingCompactorOverTheHardLimitSettlesContextExhausted(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the answer", usage: &poo.UsageReport{InputTokens: 2950}},
		{text: "the second answer"},
	})
	compactor := &failingGateCompactor{err: errors.New("the compactor failed")}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor
	opts.ContextLimit = 3000
	// The gate runs on a continue path: the first turn continues, and
	// the compaction fails before the second request.
	opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{stopAfter: 1}}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "compaction_failed" {
		t.Errorf("the settle source is %q, want compaction_failed", res.Detail.Source)
	}
}

// TestAFailingCompactorBelowTheHardLimitContinuesUncompacted covers the
// same row's soft half: below the hard ratio the run carries on
// uncompacted, and the strategy retries at the next gate.
func TestAFailingCompactorBelowTheHardLimitContinuesUncompacted(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the answer"},
		{text: "the second answer"},
	})
	compactor := &failingGateCompactor{err: errors.New("the compactor failed")}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.Compactor = compactor
	opts.ContextLimit = 32768
	opts.BeforeSettle = []poo.BeforeSettleHook{&continuingBeforeSettle{stopAfter: 1}}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: below the hard ratio the failure is not fatal", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d requests, want 2: the run continued uncompacted", res.Turns)
	}
	// A failed compaction is not recorded: the record is what the
	// compactor did, and it did nothing.
	if n := sink.count("compaction"); n != 0 {
		t.Errorf("the log holds %d compaction records, want none", n)
	}
}
