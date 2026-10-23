package perm

import (
	"context"
	"runtime"
	"testing"
	"time"
)

type reviewFuncAsker struct {
	fn   func(context.Context) (Decision, error)
	name func() string
}

func (*reviewFuncAsker) Available() bool                                    { return true }
func (*reviewFuncAsker) Budget() time.Duration                              { return time.Hour }
func (a *reviewFuncAsker) Ask(ctx context.Context, _ Ask) (Decision, error) { return a.fn(ctx) }
func (a *reviewFuncAsker) AskerName() string {
	if a.name != nil {
		return a.name()
	}
	return "review"
}

func TestReviewExpiredAnswerCannotAllow(t *testing.T) {
	clock := newFakeClock()
	a := &reviewFuncAsker{fn: func(context.Context) (Decision, error) {
		// Model an answer processed after the deadline; timer delivery can lag the clock.
		clock.mu.Lock()
		clock.now = clock.now.Add(2 * time.Hour)
		clock.mu.Unlock()
		return DecisionAllow, nil
	}}
	c := &chain{askers: []Asker{a}, timeout: time.Hour, fallback: ActionDeny, clock: clock}
	got := c.ask(context.Background(), Ask{})
	if got.decision != DecisionDeny || got.provenance != "timeout" {
		t.Fatalf("answer processed beyond chain deadline: %+v; want deny/timeout", got)
	}
}

// Yield while select operands are evaluated, allowing both ready cases to be observed.
type reviewYieldContext struct{ context.Context }

func (c reviewYieldContext) Done() <-chan struct{} { runtime.Gosched(); return c.Context.Done() }

func TestReviewAnswerRacesCancellation(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	allowed := 0
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		a := &reviewFuncAsker{fn: func(context.Context) (Decision, error) { cancel(); return DecisionAllow, nil }}
		c := &chain{askers: []Asker{a}, timeout: time.Hour, fallback: ActionAllow, clock: systemClock{}}
		got := c.ask(reviewYieldContext{ctx}, Ask{})
		if got.decision == DecisionAllow {
			allowed++
		}
		cancel()
	}
	if allowed != 0 {
		t.Fatalf("answer and cancellation both ready: allowed %d/100 cancelled asks; want 0", allowed)
	}
}

func TestReviewAnswerRacesDeadline(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	allowed := 0
	for i := 0; i < 100; i++ {
		clock := newFakeClock()
		a := &reviewFuncAsker{fn: func(context.Context) (Decision, error) { clock.Fire(time.Hour); return DecisionAllow, nil }}
		c := &chain{askers: []Asker{a}, timeout: time.Hour, fallback: ActionDeny, clock: clock}
		got := c.ask(reviewYieldContext{context.Background()}, Ask{})
		if got.decision == DecisionAllow {
			allowed++
		}
	}
	if allowed != 0 {
		t.Fatalf("answer and deadline both ready: allowed %d/100 expired asks despite fallback deny; want 0", allowed)
	}
}
