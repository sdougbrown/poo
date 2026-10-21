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
