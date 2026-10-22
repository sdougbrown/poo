package perm

import (
	"context"
	"fmt"
	"time"

	"github.com/sdougbrown/poo"
)

// Decision is an Asker's answer; defer advances the chain.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionDefer Decision = "defer"
)

// Asker answers a permission request within its budget. Zero Budget
// uses the chain deadline. Errors, panics and unknown answers defer.
type Asker interface {
	Available() bool
	Budget() time.Duration
	Ask(context.Context, Ask) (Decision, error)
}

// Availability reports whether an Asker can ever answer in this mode.
type Availability interface{ CanEverAnswer() bool }

const fallbackName = "fallback"

type chain struct {
	askers   []Asker
	timeout  time.Duration
	fallback Action
	clock    clock
}

type clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type resolution struct {
	decision   Decision
	answeredBy string
	provenance string
}

// guard is the single recovery boundary for extension calls, including
// metadata. A panic has the same meaning as an error: this link defers.
func guard[T any](call func() (T, error)) (out T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("perm: extension panicked: %v", r)
		}
	}()
	return call()
}

func (c *chain) ask(ctx context.Context, ask Ask) resolution {
	return c.askReported(ctx, ask, nil)
}

// askReported sends all chain inputs to the run's request. The chain
// owns no terminal state; late worker outputs use the same attempt token
// and resolution boundary as timers, cancellation and the seal.
func (c *chain) askReported(ctx context.Context, ask Ask, reporter poo.PermissionReporter) resolution {
	deadline := c.clock.Now().Add(c.timeout)
	public := poo.PermissionAsk{AskID: ask.AskID, Tool: ask.Tool, Summary: ask.Summary, Class: ask.Class, Reason: ask.Reason, RepeatCount: ask.RepeatCount}
	opts := poo.PermissionRequestOptions{Context: ctx, Deadline: deadline, Now: c.clock.Now, Fallback: string(c.fallback)}
	var request *poo.PermissionRequest
	if owner, ok := reporter.(poo.PermissionRequests); ok {
		request = owner.BeginPermission(public, opts)
	} else {
		request = poo.NewPermissionRequest(public, opts)
		if reporter != nil {
			reporter.Asked(public)
		}
	}
	chainCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	for _, asker := range c.askers {
		if asker == nil {
			continue
		}
		select {
		case <-request.Done():
			return resolutionOf(request)
		default:
		}
		c.attempt(chainCtx, ctx, ask, asker, request, deadline)
	}
	request.Send(poo.PermissionInput{Kind: poo.PermissionExhausted})
	return resolutionOf(request)
}

func resolutionOf(p *poo.PermissionRequest) resolution {
	answer := p.Result()
	return resolution{decision: Decision(answer.Decision), answeredBy: answer.AnsweredBy, provenance: answer.Provenance}
}

// attempt bounds metadata by the remaining chain budget and Ask by its
// own absolute budget. The worker never mutates a terminal decision.
func (c *chain) attempt(chainCtx, runCtx context.Context, ask Ask, asker Asker, p *poo.PermissionRequest, chainDeadline time.Time) {
	start := c.clock.Now()
	token := p.Send(poo.PermissionInput{Kind: poo.PermissionAttempt, Deadline: chainDeadline})
	select {
	case <-p.Done():
		return
	default:
	}
	ctx, cancel := context.WithCancel(chainCtx)
	defer cancel()
	budgetReady := make(chan time.Time)
	proceed := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := guard(func() (struct{}, error) {
			if !asker.Available() {
				return struct{}{}, nil
			}
			budget := asker.Budget()
			deadline := chainDeadline
			if budget > 0 && start.Add(budget).Before(deadline) {
				deadline = start.Add(budget)
			}
			name := askerName(asker)
			select {
			case budgetReady <- deadline:
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			}
			select {
			case <-proceed:
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			}
			remaining := deadline.Sub(c.clock.Now())
			askCtx, stop := context.WithTimeout(ctx, remaining)
			defer stop()
			decision, err := asker.Ask(askCtx, ask)
			if err != nil {
				return struct{}{}, err
			}
			p.Send(poo.PermissionInput{Kind: poo.PermissionExplicitAnswer, Attempt: token, Deadline: deadline, Answer: poo.PermissionAnswer{Decision: string(decision), AnsweredBy: name}})
			return struct{}{}, nil
		})
		if err != nil {
			p.Send(poo.PermissionInput{Kind: poo.PermissionDeferred, Attempt: token})
		}
	}()
	var timer <-chan time.Time
	for {
		select {
		case deadline := <-budgetReady:
			p.Send(poo.PermissionInput{Kind: poo.PermissionBudget, Attempt: token, Deadline: deadline})
			timer = c.clock.After(deadline.Sub(c.clock.Now()))
			close(proceed)
		case <-finished:
			p.Send(poo.PermissionInput{Kind: poo.PermissionDeferred, Attempt: token})
			return
		case <-timer:
			p.Send(poo.PermissionInput{Kind: poo.PermissionAttemptExpired, Attempt: token})
			return
		case <-p.Done():
			return
		case <-chainCtx.Done():
			kind := poo.PermissionChainExpired
			if runCtx.Err() != nil {
				kind = poo.PermissionCancel
				if poo.Interrupted(runCtx) || ask.Interrupted {
					kind = poo.PermissionInterrupt
				}
			}
			p.Send(poo.PermissionInput{Kind: kind})
			return
		}
	}
}
