package perm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sdougbrown/poo"
)

// Decision is an asker's answer to a permission ask.
type Decision string

const (
	// DecisionAllow lets the call run.
	DecisionAllow Decision = "allow"
	// DecisionDeny refuses the call.
	DecisionDeny Decision = "deny"
	// DecisionDefer passes the ask on: the chain moves to the next
	// asker, and the fallback decides when the chain is exhausted.
	DecisionDefer Decision = "defer"
)

// Asker answers a permission ask.
//
// An asker that cannot answer defers. The chain moves on to the next
// asker, and the fallback decides when the chain is exhausted.
type Asker interface {
	// Available reports whether the asker can answer an ask now.
	//
	// An unavailable asker is skipped without waiting, so an asker that
	// is not there never holds a run up.
	Available() bool
	// Budget is how long the asker may take to answer.
	//
	// A budget of zero means the asker has no budget of its own, and
	// the chain's deadline is the only limit.
	Budget() time.Duration
	// Ask answers the ask.
	//
	// An error is a deferral: the chain moves on to the next asker.
	Ask(ctx context.Context, ask Ask) (Decision, error)
}

// Availability is implemented by an asker that knows whether it can
// ever answer an ask.
//
// An asker that is unavailable now may become available later: a wire
// client that has not connected yet will. An asker that can never
// answer — a wire asker in a build with no wire — says so, and the
// load-time warning names it.
type Availability interface {
	// CanEverAnswer reports whether the asker may answer an ask at
	// some point in this build.
	CanEverAnswer() bool
}

// The name the fallback's answers carry.
const fallbackName = "fallback"

// errBudget is the failure a missed budget reports.
//
// The asker may still be answering; its answer is dropped.
var errBudget = errors.New("perm: the asker missed its budget")

// errCancelled is the failure a cancelled run reports.
//
// The run ended while the ask was pending, and the ask resolves as
// cancelled.
var errCancelled = errors.New("perm: the run ended while the ask was pending")

// chain is the asker chain.
//
// The chain asks the askers in order. An asker that is unavailable is
// skipped without waiting, and an asker that defers, fails or misses
// its budget passes the ask on. When the chain is exhausted the
// fallback decides.
type chain struct {
	// askers are the chain's askers, in order.
	askers []Asker
	// timeout is how long the whole chain may take.
	timeout time.Duration
	// fallback is the decision when the chain is exhausted.
	fallback Action
	// clock reads the time the chain measures with.
	clock clock
}

// clock is the time the chain reads.
//
// The chain waits on a context and on the clock's timers, and it never
// sleeps on its own. A test may replace the clock with one that fires
// on demand, so a deadline is reached without waiting for it.
type clock interface {
	// Now returns the current time.
	Now() time.Time
	// After returns a channel that fires after the given duration.
	After(d time.Duration) <-chan time.Time
}

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }

// After returns a channel that fires after the given duration.
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// resolution is how an ask was resolved.
type resolution struct {
	// decision is what was decided: allow or deny.
	decision Decision
	// answeredBy names who decided: the asker that answered, or
	// "fallback".
	answeredBy string
	// provenance is how the ask was resolved: "answered", "timeout",
	// "default", "cancelled" or "interrupted".
	provenance string
}

// ask asks the chain about a call.
//
// Every way an ask can end goes through the same checks: an answer, a
// missed deadline, a cancelled run and a panicking asker all resolve
// here, and only a valid explicit allow survives. An ask resolved
// because the run ended is never allowed.
func (c *chain) ask(ctx context.Context, ask Ask) resolution {
	deadline := c.clock.Now().Add(c.timeout)
	for _, asker := range c.askers {
		if asker == nil {
			continue
		}
		if res, done := c.stopped(ctx, ask, deadline); done {
			return res
		}
		if !askerAvailable(asker) {
			// An asker that is not there is skipped without waiting.
			continue
		}
		if res, done := c.stopped(ctx, ask, deadline); done {
			return res
		}
		remaining := deadline.Sub(c.clock.Now())
		if remaining <= 0 {
			return c.fallbackResolution(poo.ProvenanceTimeout)
		}
		decision, err := c.call(ctx, asker, ask, remaining)
		if err != nil {
			if errors.Is(err, errCancelled) {
				return cancelledResolution(poo.Interrupted(ctx) || ask.Interrupted)
			}
			// A failure, a missed budget and a disconnect are all
			// deferrals: the chain moves on.
			continue
		}
		switch decision {
		case DecisionAllow, DecisionDeny:
			// The answer is rechecked against the run's end and the
			// chain's deadline, so which channel a select chose cannot
			// let an expired or cancelled approval through.
			if res, done := c.stopped(ctx, ask, deadline); done {
				return res
			}
			return resolution{
				decision:   decision,
				answeredBy: askerName(asker),
				provenance: poo.ProvenanceAnswered,
			}
		}
		// Anything that is not an explicit allow or deny — including a
		// decision the asker made up — is a deferral.
	}
	// The chain is exhausted. A deadline that ran out is a timeout, and
	// anything else is the default.
	if res, done := c.stopped(ctx, ask, deadline); done {
		return res
	}
	return c.fallbackResolution(poo.ProvenanceDefault)
}

// stopped returns the resolution a run that ended or a chain that
// missed its deadline has, before any answer is used.
//
// Cancellation wins over the deadline: a run that ended never allows a
// call, whatever the clock says.
func (c *chain) stopped(ctx context.Context, ask Ask, deadline time.Time) (resolution, bool) {
	if ctx.Err() != nil {
		return cancelledResolution(poo.Interrupted(ctx) || ask.Interrupted), true
	}
	if !c.clock.Now().Before(deadline) {
		return c.fallbackResolution(poo.ProvenanceTimeout), true
	}
	return resolution{}, false
}

// cancelledResolution returns the resolution of an ask the run ended
// before it was answered.
//
// The ask is refused: a run that is ending never allows a call. An
// interrupt is its own provenance, so a client can tell it from a
// cancelled run.
func cancelledResolution(interrupted bool) resolution {
	provenance := poo.ProvenanceCancelled
	if interrupted {
		provenance = poo.ProvenanceInterrupted
	}
	return resolution{
		decision:   DecisionDeny,
		answeredBy: "cancelled",
		provenance: provenance,
	}
}

// askerAvailable reports whether an asker can answer now.
//
// A panicking asker is unavailable: the chain skips it rather than
// taking the process down.
func askerAvailable(asker Asker) (available bool) {
	defer func() {
		if recover() != nil {
			available = false
		}
	}()
	return asker.Available()
}

// askerBudget returns an asker's budget.
//
// A panicking asker has no budget of its own, and the chain's deadline
// is the only limit.
func askerBudget(asker Asker) (budget time.Duration) {
	defer func() {
		if recover() != nil {
			budget = 0
		}
	}()
	return asker.Budget()
}

// call asks one asker, under its own budget.
//
// The asker runs in its own goroutine, so an asker that ignores its
// context never holds the chain up: the budget ends the wait, and the
// answer that arrives after it is dropped. A panic is a deferral, not a
// crash: the asker could not answer, so the chain moves on.
func (c *chain) call(ctx context.Context, asker Asker, ask Ask, remaining time.Duration) (Decision, error) {
	budget := askerBudget(asker)
	if budget <= 0 || budget > remaining {
		budget = remaining
	}
	askerCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	timer := c.clock.After(budget)
	result := make(chan askerResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result <- askerResult{decision: DecisionDefer, err: fmt.Errorf("perm: the asker panicked: %v", r)}
			}
		}()
		decision, err := asker.Ask(askerCtx, ask)
		result <- askerResult{decision: decision, err: err}
	}()
	select {
	case out := <-result:
		if out.err != nil {
			return DecisionDefer, out.err
		}
		return out.decision, nil
	case <-timer:
		// The asker missed its budget. The answer it may still send is
		// dropped.
		return DecisionDefer, errBudget
	case <-ctx.Done():
		// The run ended while the ask was pending.
		return DecisionDefer, errCancelled
	}
}

// askerResult is what an asker returned.
type askerResult struct {
	// decision is what the asker decided.
	decision Decision
	// err is the failure the asker reported.
	err error
}

// fallbackResolution returns the fallback's decision.
func (c *chain) fallbackResolution(provenance string) resolution {
	decision := DecisionDeny
	if c.fallback == ActionAllow {
		decision = DecisionAllow
	}
	return resolution{
		decision:   decision,
		answeredBy: fallbackName,
		provenance: provenance,
	}
}
