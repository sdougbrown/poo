package poo

import (
	"context"
	"testing"
)

// TestResumeSettlesANonNaturalCandidate covers the rule that a run is
// never left without a settler: a non-natural candidate that appears
// between an abandoned finalization and the resume settles the run
// instead of being ignored.
func TestResumeSettlesANonNaturalCandidate(t *testing.T) {
	s := &Session{
		opts:       Options{ContextLimit: 1},
		state:      StateRequesting,
		events:     newEventBus(),
		messageIDs: &MessageIDs{},
		repeats:    newRepeatCounter(),
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	run := &runState{
		id:           "run",
		ctx:          ctx,
		cancel:       cancel,
		pendingCalls: map[string]*issuedCall{},
		candidates: []candidate{newCandidate(ReasonToolFatal, SettleDetail{
			Source: "tool:echo",
			Text:   "the tool failed",
		})},
	}
	s.run = run

	if _, ok := s.resume(run); ok {
		t.Fatal("resume carried the run on with a non-natural candidate")
	}
	if !run.committed {
		t.Error("the run was left without a settler")
	}
	if got := s.State(); got != StateSettled {
		t.Errorf("the session is %q, want settled", got)
	}
	if run.result.Reason != ReasonToolFatal {
		t.Errorf("the run settled %q, want tool_fatal", run.result.Reason)
	}
}
