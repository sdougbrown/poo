package perm

import (
	"context"
	"sync"
	"testing"
	"time"
)

// The chain's deadlines are reached without waiting for them: the test
// fires the timer it wants to reach, and the fake clock is the only
// thing that moves time forward.

// fakeClock is a clock that fires on demand.
//
// The chain reads the clock and waits on its timers, so a test can
// reach a deadline without spending one.
type fakeClock struct {
	mu sync.Mutex
	// now is the clock's time.
	now time.Time
	// timers holds the waiting timers, by duration.
	timers map[time.Duration][]chan time.Time
}

// newFakeClock returns a clock that starts at the epoch.
func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0).UTC(), timers: map[time.Duration][]chan time.Time{}}
}

// Now returns the clock's time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that fires when the clock is told to.
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers[d] = append(c.timers[d], ch)
	return ch
}

// Fire fires every timer with the given duration, and the clock moves
// to that moment.
func (c *fakeClock) Fire(d time.Duration) {
	c.mu.Lock()
	chans := c.timers[d]
	delete(c.timers, d)
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	for _, ch := range chans {
		ch <- now
	}
}

// Pending returns how many timers are waiting.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, chans := range c.timers {
		n += len(chans)
	}
	return n
}

// blockingAsker waits for a release and then answers.
//
// The asker ignores its context on purpose: a test uses it to see that
// the chain does not wait for an asker that misses its budget.
type blockingAsker struct {
	// started is closed when the asker was asked.
	started chan struct{}
	// release is closed when the asker may answer.
	release chan struct{}
	// decision is what the asker answers.
	decision Decision
	// budget is how long the asker may take.
	budget time.Duration
	// asked counts the asks the asker was given.
	asked int
}

// newBlockingAsker returns an asker that waits for a release.
func newBlockingAsker(decision Decision, budget time.Duration) *blockingAsker {
	return &blockingAsker{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		decision: decision,
		budget:   budget,
	}
}

// Ask waits for the release and answers.
func (a *blockingAsker) Ask(ctx context.Context, ask Ask) (Decision, error) {
	a.asked++
	close(a.started)
	<-a.release
	return a.decision, nil
}

// Available reports that the asker can answer.
func (a *blockingAsker) Available() bool { return true }

// Budget is how long the asker may take.
func (a *blockingAsker) Budget() time.Duration { return a.budget }

// Asks returns how many asks the asker was given.
func (a *blockingAsker) Asks() int { return a.asked }

// TestTheChainSkipsAnUnavailableAsker covers the plan's rule: an
// unavailable asker is skipped without waiting, and the fallback
// applies at once.
func TestTheChainSkipsAnUnavailableAsker(t *testing.T) {
	clock := newFakeClock()
	asker := &unavailableAsker{}
	chain := &chain{
		askers:   []Asker{asker},
		timeout:  time.Hour,
		fallback: ActionDeny,
		clock:    clock,
	}

	res := chain.ask(context.Background(), Ask{Tool: "read"})

	if res.decision != DecisionDeny {
		t.Errorf("the ask was decided %q, want the fallback's refusal", res.decision)
	}
	if res.provenance != "default" {
		t.Errorf("the ask's provenance is %q, want %q", res.provenance, "default")
	}
	if asker.asked != 0 {
		t.Errorf("the asker was asked %d time(s), want 0: an unavailable asker is skipped", asker.asked)
	}
	if pending := clock.Pending(); pending != 0 {
		t.Errorf("the chain started %d timer(s), want 0: no wait happened", pending)
	}
}

// TestAPerAskerTimeoutMovesOnToTheNextAsker covers the plan's rule: an
// asker that misses its budget passes the ask on.
func TestAPerAskerTimeoutMovesOnToTheNextAsker(t *testing.T) {
	clock := newFakeClock()
	slow := newBlockingAsker(DecisionDeny, 5*time.Minute)
	defer close(slow.release)
	fast := &scriptedAsker{decision: DecisionAllow}
	chain := &chain{
		askers:   []Asker{slow, fast},
		timeout:  time.Hour,
		fallback: ActionDeny,
		clock:    clock,
	}

	go func() {
		<-slow.started
		clock.Fire(5 * time.Minute)
	}()
	res := chain.ask(context.Background(), Ask{Tool: "read"})

	if res.decision != DecisionAllow {
		t.Errorf("the ask was decided %q, want the second asker's allowance", res.decision)
	}
	if res.answeredBy != "scripted" {
		t.Errorf("the ask was answered by %q, want the second asker", res.answeredBy)
	}
	if fast.asked != 1 {
		t.Errorf("the second asker was asked %d time(s), want 1", fast.asked)
	}
}

// TestALateAnswerIsDropped covers the plan's rule: an answer that
// arrives after the ask is resolved is dropped.
func TestALateAnswerIsDropped(t *testing.T) {
	clock := newFakeClock()
	slow := newBlockingAsker(DecisionDeny, 5*time.Minute)
	fast := &scriptedAsker{decision: DecisionAllow}
	chain := &chain{
		askers:   []Asker{slow, fast},
		timeout:  time.Hour,
		fallback: ActionDeny,
		clock:    clock,
	}

	go func() {
		<-slow.started
		clock.Fire(5 * time.Minute)
	}()
	res := chain.ask(context.Background(), Ask{Tool: "read"})

	// The first asker answers after the ask was resolved, and its
	// refusal changes nothing.
	close(slow.release)
	if res.decision != DecisionAllow {
		t.Errorf("the ask was decided %q, want the allowance the resolved ask carried", res.decision)
	}
}

// TestTheChainDeadlineAppliesTheFallback covers the plan's rule: a chain
// that runs out of time applies the fallback.
func TestTheChainDeadlineAppliesTheFallback(t *testing.T) {
	clock := newFakeClock()
	asker := newBlockingAsker(DecisionAllow, 0)
	defer close(asker.release)
	chain := &chain{
		askers:   []Asker{asker},
		timeout:  time.Hour,
		fallback: ActionDeny,
		clock:    clock,
	}

	go func() {
		<-asker.started
		clock.Fire(time.Hour)
	}()
	res := chain.ask(context.Background(), Ask{Tool: "read"})

	if res.decision != DecisionDeny {
		t.Errorf("the ask was decided %q, want the fallback's refusal", res.decision)
	}
	if res.provenance != "timeout" {
		t.Errorf("the ask's provenance is %q, want %q", res.provenance, "timeout")
	}
}

// TestTheChainExhaustedAppliesTheDefault covers the plan's rule: a chain
// whose askers all deferred applies the fallback.
func TestTheChainExhaustedAppliesTheDefault(t *testing.T) {
	clock := newFakeClock()
	asker := &scriptedAsker{decision: DecisionDefer}
	chain := &chain{
		askers:   []Asker{asker},
		timeout:  time.Hour,
		fallback: ActionAllow,
		clock:    clock,
	}

	res := chain.ask(context.Background(), Ask{Tool: "read"})

	if res.decision != DecisionAllow {
		t.Errorf("the ask was decided %q, want the fallback's allowance", res.decision)
	}
	if res.provenance != "default" {
		t.Errorf("the ask's provenance is %q, want %q", res.provenance, "default")
	}
}

// TestTheFallbackCanAllow covers the plan's rule: the fallback is a
// decision, and it can allow.
func TestTheFallbackCanAllow(t *testing.T) {
	clock := newFakeClock()
	chain := &chain{
		timeout:  time.Hour,
		fallback: ActionAllow,
		clock:    clock,
	}

	res := chain.ask(context.Background(), Ask{Tool: "read"})

	if res.decision != DecisionAllow {
		t.Errorf("the ask was decided %q, want the fallback's allowance", res.decision)
	}
	if res.answeredBy != "fallback" {
		t.Errorf("the ask was answered by %q, want the fallback", res.answeredBy)
	}
}

// TestARunCancelResolvesTheAskAsCancelled covers the plan's rule: a run
// that ends while the ask is pending resolves the ask as cancelled, and
// a cancellation never allows.
func TestARunCancelResolvesTheAskAsCancelled(t *testing.T) {
	clock := newFakeClock()
	asker := newBlockingAsker(DecisionAllow, 5*time.Minute)
	defer close(asker.release)
	chain := &chain{
		askers:   []Asker{asker},
		timeout:  time.Hour,
		fallback: ActionAllow,
		clock:    clock,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-asker.started
		cancel()
	}()

	res := chain.ask(ctx, Ask{Tool: "read"})

	if res.decision != DecisionDeny {
		t.Errorf("the ask was decided %q, want a refusal: a cancellation never allows", res.decision)
	}
	if res.provenance != "cancelled" {
		t.Errorf("the ask's provenance is %q, want %q", res.provenance, "cancelled")
	}
}

// unavailableAsker is an asker that is never there.
type unavailableAsker struct {
	// asked counts the asks the asker was given.
	asked int
}

// Ask counts the ask.
func (a *unavailableAsker) Ask(ctx context.Context, ask Ask) (Decision, error) {
	a.asked++
	return DecisionAllow, nil
}

// Available reports that the asker is not there.
func (a *unavailableAsker) Available() bool { return false }

// Budget is the asker's budget.
func (a *unavailableAsker) Budget() time.Duration { return time.Minute }

// scriptedAsker answers with a fixed decision.
type scriptedAsker struct {
	// decision is what the asker answers.
	decision Decision
	// asked counts the asks the asker was given.
	asked int
}

// Ask answers the ask.
func (a *scriptedAsker) Ask(ctx context.Context, ask Ask) (Decision, error) {
	a.asked++
	return a.decision, nil
}

// Available reports that the asker can answer.
func (a *scriptedAsker) Available() bool { return true }

// Budget is the asker's budget.
func (a *scriptedAsker) Budget() time.Duration { return time.Minute }

// AskerName is the name the asker's answers carry.
func (a *scriptedAsker) AskerName() string { return "scripted" }
