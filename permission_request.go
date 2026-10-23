package poo

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PermissionInput is a message to a pending permission request.
//
// Answers and deadlines name the attempt that produced them, so an
// input for an attempt that already ended is dropped instead of applied.
type PermissionInput struct {
	// Kind is the transition the input asks for.
	Kind PermissionInputKind
	// Attempt is the token of the attempt the input belongs to.
	Attempt uint64
	// Deadline is the attempt's deadline, as the input was produced
	// under.
	Deadline time.Time
	// Answer is the answer an explicit or policy input carries.
	Answer PermissionAnswer
}

// PermissionInputKind identifies a permission state transition.
type PermissionInputKind uint8

const (
	// PermissionAttempt starts an Asker's attempt. Deadline is the
	// attempt's deadline.
	PermissionAttempt PermissionInputKind = iota
	// PermissionBudget narrows the attempt to the Asker's own budget.
	PermissionBudget
	// PermissionExplicitAnswer is an Asker's answer to an attempt.
	PermissionExplicitAnswer
	// PermissionAttemptExpired reports that the attempt's budget ran out.
	PermissionAttemptExpired
	// PermissionDeferred reports that the Asker could not answer, so the
	// chain moves on.
	PermissionDeferred
	// PermissionChainExpired reports that the chain's deadline ran out.
	PermissionChainExpired
	// PermissionExhausted reports that the chain had no answer to give.
	PermissionExhausted
	// PermissionNoAsker reports that the chain has no askers at all.
	PermissionNoAsker
	// PermissionCancel reports that the run ended.
	PermissionCancel
	// PermissionInterrupt reports that an interrupt ended the activity.
	PermissionInterrupt
	// PermissionSeal reports that the finalization sealed the run.
	PermissionSeal
	// PermissionPolicyAnswer is the policy's deterministic outcome.
	PermissionPolicyAnswer
)

// PermissionRequestOptions supplies the chain's clock, deadline and fallback.
type PermissionRequestOptions struct {
	// Context is the run's context. A cancelled context interrupts the
	// request.
	Context context.Context
	// Now reads the time the request measures its deadlines with.
	Now func() time.Time
	// Deadline is the chain's deadline.
	Deadline time.Time
	// Fallback is the decision when the chain is exhausted. Only
	// "allow" allows; anything else denies.
	Fallback string
}

// PermissionRequest owns one ask from pending to resolved.
//
// The run and the policy share this value; neither keeps a second
// resolution state. The finalization seals it, and its records have one
// writer.
type PermissionRequest struct {
	mu              sync.Mutex
	options         PermissionRequestOptions
	ask             PermissionAsk
	attempt         uint64
	attemptDeadline time.Time
	active          bool
	answer          *PermissionAnswer
	done            chan struct{}
	// logDrop reports an input that was dropped as stale.
	logDrop func(PermissionInput)

	// io serializes the request's records, including seal cleanup. A
	// submitted append is never abandoned or retried by another writer.
	io            sync.Mutex
	askedWritten  bool
	answerWritten bool
	writing       bool
}

// NewPermissionRequest constructs a request for a standalone policy.
// Session policies obtain their run-owned request from PermissionRequests.
func NewPermissionRequest(ask PermissionAsk, opts PermissionRequestOptions) *PermissionRequest {
	if opts.Context == nil {
		opts.Context = context.Background()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Fallback != "allow" {
		opts.Fallback = "deny"
	}
	return &PermissionRequest{ask: ask, options: opts, done: make(chan struct{}), logDrop: func(in PermissionInput) {
		slog.Debug("stale permission input dropped", "ask_id", ask.AskID, "attempt", in.Attempt)
	}}
}

// PermissionRequests is the run-owned permission seam supplied by core.
type PermissionRequests interface {
	BeginPermission(PermissionAsk, PermissionRequestOptions) *PermissionRequest
}

// Done closes at the request's only terminal transition.
func (p *PermissionRequest) Done() <-chan struct{} { return p.done }

// Result returns the terminal answer, or an empty answer while pending.
func (p *PermissionRequest) Result() PermissionAnswer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.answer == nil {
		return PermissionAnswer{}
	}
	return *p.answer
}

// Send applies one input under the request lock. Expired attempts cannot
// answer, even when their result and deadline channels are both ready.
// The returned attempt token tags subsequent messages from that Asker.
func (p *PermissionRequest) Send(in PermissionInput) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	drop := func() {
		if p.logDrop != nil {
			p.logDrop(in)
		}
	}
	if p.answer != nil {
		drop()
		return p.attempt
	}
	resolve := func(answer PermissionAnswer) {
		answer.AskID, answer.Reason = p.ask.AskID, p.ask.Reason
		if answer.Decision != "allow" {
			answer.Decision = "deny"
		}
		p.answer = &answer
		p.active = false
		close(p.done)
	}
	cancel := func(interrupted bool) {
		provenance := ProvenanceCancelled
		if interrupted {
			provenance = ProvenanceInterrupted
		}
		resolve(PermissionAnswer{Decision: "deny", AnsweredBy: "cancelled", Provenance: provenance})
	}
	if in.Kind == PermissionSeal || in.Kind == PermissionCancel || in.Kind == PermissionInterrupt {
		cancel(in.Kind == PermissionInterrupt)
		return p.attempt
	}
	if in.Kind == PermissionBudget || in.Kind == PermissionExplicitAnswer || in.Kind == PermissionAttemptExpired || in.Kind == PermissionDeferred {
		if in.Attempt != p.attempt || !p.active {
			drop()
			return p.attempt
		}
		if in.Kind == PermissionExplicitAnswer && (!in.Deadline.Equal(p.attemptDeadline) || !p.options.Now().Before(p.attemptDeadline)) {
			p.active = false
			drop()
			return p.attempt
		}
	}
	if p.options.Context.Err() != nil {
		cancel(Interrupted(p.options.Context))
		return p.attempt
	}
	now := p.options.Now()
	fallback := func(provenance string) {
		resolve(PermissionAnswer{Decision: p.options.Fallback, AnsweredBy: "fallback", Provenance: provenance})
	}
	if !p.options.Deadline.IsZero() && !now.Before(p.options.Deadline) {
		if in.Kind == PermissionExplicitAnswer {
			drop()
		}
		fallback(ProvenanceTimeout)
		return p.attempt
	}
	switch in.Kind {
	case PermissionAttempt:
		p.attempt++
		p.attemptDeadline, p.active = in.Deadline, true
	case PermissionBudget:
		if in.Deadline.Before(p.attemptDeadline) {
			p.attemptDeadline = in.Deadline
		}
	case PermissionExplicitAnswer, PermissionAttemptExpired, PermissionDeferred:
		if in.Kind != PermissionExplicitAnswer {
			p.active = false
			break
		}
		p.active = false
		if in.Answer.Decision == "allow" || in.Answer.Decision == "deny" {
			in.Answer.Provenance = ProvenanceAnswered
			resolve(in.Answer)
		}
	case PermissionChainExpired:
		fallback(ProvenanceTimeout)
	case PermissionExhausted:
		fallback(ProvenanceDefault)
	case PermissionNoAsker:
		fallback(ProvenanceNoAsker)
	case PermissionPolicyAnswer:
		if in.Answer.AnsweredBy == "policy" || in.Answer.Decision != "allow" {
			resolve(in.Answer)
		} else {
			drop()
		}
	}
	return p.attempt
}
