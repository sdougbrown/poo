package poo

import (
	"context"
	"errors"
	"testing"
)

// The hooks share one rule about cancellation: an interrupt ends the
// hook's context with errInterrupted, and the hook's decision is
// discarded rather than turned into a hook error.

// TestTheActivityContextEndsWithErrInterrupted covers the plan's rule:
// every hook's context comes from the run's activity, and an interrupt
// ends it with errInterrupted, which is not the finalization's own
// cancellation.
func TestTheActivityContextEndsWithErrInterrupted(t *testing.T) {
	s := &Session{}
	run := &runState{ctx: context.Background()}
	s.startActivity(run)
	s.mu.Lock()
	s.raiseInterruptLocked(run, "i1")
	s.mu.Unlock()

	cause := context.Cause(s.activityContext(run))
	if !errors.Is(cause, errInterrupted) {
		t.Errorf("the activity's context ended with %v, want errInterrupted", cause)
	}
	if errors.Is(cause, errFinalizing) {
		t.Error("the activity's context ended with the finalization's own cause")
	}
}

// TestAnInterruptedHooksDecisionIsDiscarded covers the plan's rule: the
// decision of a hook whose context ended is discarded, and that is not
// a hook error.
func TestAnInterruptedHooksDecisionIsDiscarded(t *testing.T) {
	s := &Session{opts: Options{HookTimeout: DefaultHookTimeout}}
	hook := &causeHook{done: make(chan struct{})}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errInterrupted)

	out, err := s.callHook(ctx, hook, func(hctx context.Context) (any, error) {
		return hook.AfterTurn(HookContext{Context: hctx})
	})
	if err != nil {
		t.Fatalf("the hook failed with %v, want its decision discarded", err)
	}
	// The hook runs in its own goroutine, so the test waits for it to
	// report the cause its context ended with.
	<-hook.done
	if !out.interrupted {
		t.Error("the hook's decision was taken, want it discarded")
	}
	if !errors.Is(hook.cause, errInterrupted) {
		t.Errorf("the hook's context ended with %v, want errInterrupted", hook.cause)
	}
}

// causeHook is a hook that waits for its context to end and reports the
// cause it ended with.
type causeHook struct {
	// cause is the cause the hook's context ended with.
	cause error
	// done is closed when the hook has reported the cause.
	done chan struct{}
}

// AfterTurn waits for the context to end.
func (h *causeHook) AfterTurn(ctx HookContext) (TurnDecision, error) {
	<-ctx.Done()
	h.cause = context.Cause(ctx)
	if h.done != nil {
		close(h.done)
	}
	return TurnDecision{Stop: true}, nil
}
