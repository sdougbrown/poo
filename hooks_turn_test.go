package poo_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The after-turn hooks see the turn that just closed, and the records
// they add belong to the run: the seal keeps them from following the
// run's settle record.

// recordingTurnHook is an after-turn hook that records what it was
// asked and answers with a fixed decision.
type recordingTurnHook struct {
	mu sync.Mutex
	// calls counts the turns the hook saw.
	calls int
	// interrupted holds the interrupted flag of every turn, in order.
	interrupted []bool
	// outcomes holds the outcomes of the calls of every turn, in order.
	outcomes [][]poo.Outcome
	// decision is what the hook answers.
	decision poo.TurnDecision
	// wait blocks the hook until the channel is closed.
	wait chan struct{}
}

// AfterTurn records the turn and answers.
func (h *recordingTurnHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.calls++
	h.interrupted = append(h.interrupted, ctx.Interrupted)
	var outcomes []poo.Outcome
	for _, turn := range ctx.History.Turns() {
		for _, call := range turn.Calls {
			outcomes = append(outcomes, call.Outcome)
		}
	}
	h.outcomes = append(h.outcomes, outcomes)
	decision := h.decision
	wait := h.wait
	h.mu.Unlock()
	if wait != nil {
		<-wait
	}
	return decision, nil
}

// Calls returns how many turns the hook saw.
func (h *recordingTurnHook) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// Interrupted holds the interrupted flag of every turn, in order.
func (h *recordingTurnHook) Interrupted() []bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]bool(nil), h.interrupted...)
}

// Outcomes holds the outcomes of the calls of every turn, in order.
func (h *recordingTurnHook) Outcomes() [][]poo.Outcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]poo.Outcome(nil), h.outcomes...)
}

// TestTheAfterTurnHookSeesAStartedResult covers the plan's rule: a call
// that became a job is a processed call, and the after-turn hook sees
// it.
func TestTheAfterTurnHookSeesAStartedResult(t *testing.T) {
	hook := &recordingTurnHook{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "done"},
	})
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	opts := jobOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to start", func() bool { return tool.Calls() == 1 })
	waitFor(t, "the turn to end", func() bool { return hook.Calls() >= 1 })
	tool.ReleaseAll()
	waitFor(t, "the run to settle", func() bool { return s.State() == poo.StateSettled })

	outcomes := hook.Outcomes()
	if len(outcomes) == 0 {
		t.Fatal("the hook saw no turn")
	}
	found := false
	for _, outcome := range outcomes[0] {
		if outcome == poo.OutcomeStarted {
			found = true
		}
	}
	if !found {
		t.Errorf("the hook's turn holds the outcomes %v, want the started one", outcomes[0])
	}
}

// TestTheAfterTurnHookSeesAnInterruptedTurn covers the plan's rule: an
// interrupted turn reaches the after-turn hooks with the interrupted
// flag set.
func TestTheAfterTurnHookSeesAnInterruptedTurn(t *testing.T) {
	hook := &recordingTurnHook{}
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	tool := &echoTool{result: "the effect", call: func(ctx context.Context) (poo.ToolResult, error) {
		once.Do(func() { close(started) })
		<-release
		return poo.ToolResult{Text: "the effect"}, nil
	}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The interrupt arrives while the tool is running, so the turn is
	// cut short.
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	close(release)
	waitFor(t, "the turn to end", func() bool { return hook.Calls() >= 1 })

	interrupted := hook.Interrupted()
	if len(interrupted) == 0 {
		t.Fatal("the hook saw no turn")
	}
	if !interrupted[0] {
		t.Errorf("the hook saw the interrupted flag %v, want it set", interrupted)
	}
}

// TestAHooksRecordNeverFollowsTheSettleRecord covers the plan's rule: a
// hook that runs while the finalization is under way cannot add a
// record after the run settled.
func TestAHooksRecordNeverFollowsTheSettleRecord(t *testing.T) {
	hook := &recordingTurnHook{decision: poo.AppendMessages(poo.NewTextMessage(poo.RoleUser, "the hook's note"))}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &hookRecordSink{entered: make(chan struct{}), release: make(chan struct{})}
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The hook's record is held in the store, so the run is being
	// finalized while the hook's write is in flight.
	<-sink.entered
	go s.Cancel("cancel:test")
	waitFor(t, "the run to start settling", func() bool {
		for _, ev := range s.Events() {
			if ev.Name == poo.EventRunSettling {
				return true
			}
		}
		return false
	})
	// The record is released, and the finalization waits for it: the
	// hook's record belongs to the run, and the run's records come
	// before the run's settle record.
	close(sink.release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	names := sink.names()
	hookRecord, settled := -1, -1
	for i, name := range names {
		switch name {
		case "message":
			if hookRecord < 0 {
				hookRecord = i
			}
		case "run.settled":
			settled = i
		}
	}
	if hookRecord < 0 {
		t.Fatalf("the store holds no message record: %v", names)
	}
	if settled < 0 {
		t.Fatalf("the store holds no settle record: %v", names)
	}
	if hookRecord > settled {
		t.Errorf("the records are %v, want the hook's message before the settle record", names)
	}
}

// hookRecordSink holds the record a hook added, so the run can be
// finalized while the hook's write is in flight.
type hookRecordSink struct {
	recordingSink
	// entered is signalled when the hook's record reaches the store.
	entered chan struct{}
	// release lets the record through.
	release chan struct{}
	// once guards the entered signal.
	once sync.Once
}

// Append holds the record of a hook's message.
func (s *hookRecordSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "message" && strings.HasPrefix(string(rec.Meta().Origin), "hook:") {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestTheAfterTurnHookSeesTheTurnsCalls covers the plan's rule: the
// after-turn hook sees the turn that just closed.
func TestTheAfterTurnHookSeesTheTurnsCalls(t *testing.T) {
	hook := &recordingTurnHook{}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	outcomes := hook.Outcomes()
	if len(outcomes) == 0 {
		t.Fatal("the hook saw no turn")
	}
	if len(outcomes[0]) != 1 || outcomes[0][0] != poo.OutcomeOK {
		t.Errorf("the hook's turn holds the outcomes %v, want the call's own", outcomes[0])
	}
}
