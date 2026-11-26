package poo

import (
	"encoding/json"
	"time"
)

// The continuation: what a durable session does next after a crash.
//
// A durable session's recovery plans a continuation instead of settling
// the run it left open. The plan is the core's to carry out: it names the
// work the resumed run redoes, and the state it resumes in. The session
// store plans it, because only the records know what was left unsettled;
// the core resumes it, because only the core knows how a run is driven.

// ContinuationPlan is what a durable session does next.
//
// The plan names the work a continuation resumes, and the state it resumes
// in. It is what the recovery leaves: the records that had to be resolved
// without the model are already on the log, and everything here is the
// continuation's.
type ContinuationPlan struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run that continues.
	RunID string `json:"run_id"`
	// Turn is the last turn the run recorded a record in.
	Turn int `json:"turn"`
	// Phase is where the run resumes.
	Phase State `json:"phase"`
	// Hook is the turn hook the run resumes inside, and nil when the run
	// did not stop in one.
	Hook *HookResume `json:"hook,omitempty"`
	// ReplayCalls are the calls to run again, in the order the model made
	// them.
	ReplayCalls []PlannedCall `json:"replay_calls,omitempty"`
	// LiveAsks are the asks the run still waits on, in the order it asked
	// them.
	LiveAsks []PlannedAsk `json:"live_asks,omitempty"`
	// PendingInputs are the inputs that never reached the model, in the
	// order they were queued. The continuation delivers them; the plan
	// writes no record for them, so an input a crash left queued is still
	// the user's message.
	PendingInputs []PlannedInput `json:"pending_inputs,omitempty"`
	// Subscriptions are the feed instances to take on again, in the order
	// they first subscribed.
	Subscriptions []PendingSubscription `json:"subscriptions,omitempty"`
	// Resumptions is how many times the run was resumed before this plan:
	// the count the resumption budget is spent from.
	Resumptions int `json:"resumptions,omitempty"`
	// PartialDiscarded reports that the run's last reply was cut short,
	// and that the continuation drops it.
	PartialDiscarded bool `json:"partial_discarded,omitempty"`
	// CompactionDiscarded reports that the run stopped with a compaction
	// as its last record, and that the continuation discards it and
	// decides again at the next request.
	CompactionDiscarded bool `json:"compaction_discarded,omitempty"`
}

// PlannedCall is a call the continuation runs again.
type PlannedCall struct {
	// RunID identifies the run the call belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the call belongs to.
	Turn int `json:"turn"`
	// Handle is the call's identifier within the turn.
	Handle string `json:"handle"`
	// CallID is the provider's id of the call.
	CallID string `json:"call_id,omitempty"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Arguments are the call's arguments, as the model gave them.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Class is the tool's class, and Idempotent says whether the tool
	// declared that running it twice is safe. Both are zero for a tool the
	// session no longer knows.
	Class      ToolKind `json:"class,omitempty"`
	Idempotent bool     `json:"idempotent,omitempty"`
	// Reason says why the call is replayed: "read_only", "idempotent" or
	// "not_started".
	Reason string `json:"reason,omitempty"`
}

// PlannedAsk is a permission ask the continuation waits on again.
type PlannedAsk struct {
	// RunID identifies the run that is waiting.
	RunID string `json:"run_id"`
	// AskID names the ask.
	AskID string `json:"ask_id"`
	// Handle is the call's identifier within the turn, when the log pairs
	// the ask with a call.
	Handle string `json:"handle,omitempty"`
	// CallID is the provider's id of the call the ask is about, when the
	// log pairs the ask with one.
	CallID string `json:"call_id,omitempty"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Summary describes the call.
	Summary string `json:"summary,omitempty"`
	// Class is the tool's class.
	Class ToolKind `json:"class,omitempty"`
	// Reason is why the call is being asked about.
	Reason AskReason `json:"reason,omitempty"`
	// Arguments are the call's arguments, as the model gave them, so the
	// continuation asks about the same call.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Deadline is the wall-clock time the ask expires, and the zero time
	// when it has none; the continuation asks again with this deadline,
	// not with a fresh one.
	Deadline time.Time `json:"deadline,omitempty"`
	// Fallback is the decision the ask resolves to when the deadline
	// passes.
	Fallback string `json:"fallback,omitempty"`
}

// PlannedInput is an input the continuation delivers.
type PlannedInput struct {
	// InputID is the input's identifier.
	InputID string `json:"input_id"`
	// RunID identifies the run the input belongs to.
	RunID string `json:"run_id,omitempty"`
	// Mode is how the input was accepted.
	Mode InputMode `json:"mode"`
	// Text is the message the input carries.
	Text string `json:"text,omitempty"`
	// DeliveryNote says how much of the previous reply the user had seen,
	// when the input carries one.
	DeliveryNote *DeliveryNote `json:"delivery_note,omitempty"`
	// Attachments are the files the input carries beside its text.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// HookResume is the turn hook a run resumes inside.
type HookResume struct {
	// Phase is the hook's phase: "after_turn" or "before_settle".
	Phase string `json:"phase"`
	// HookIndex is the position in the chain the run resumes at: the hook
	// whose start the log never ended.
	HookIndex int `json:"hook_index"`
	// Interrupted reports that the turn was being interrupted as the hook
	// ran.
	Interrupted bool `json:"interrupted,omitempty"`
}

// PendingSubscription is a feed instance that is still subscribed.
//
// A subscription is not a pending operation: it is the state a session had
// when it stopped, and a durable session's continuation takes it on again.
type PendingSubscription struct {
	// Feed is the feed's name, e.g. "inbox".
	Feed string `json:"feed"`
	// Instance is the instance's identifier.
	Instance string `json:"instance"`
	// Scope is the instance's scope.
	Scope string `json:"scope,omitempty"`
	// Prompt is the prompt the instance was started with, when it has one.
	Prompt string `json:"prompt,omitempty"`
	// Origin is the origin the instance's records carry.
	Origin Origin `json:"origin,omitempty"`
}

// PendingRunInfo describes the run a durable session has left to continue.
//
// It is the wire's view of the continuation: enough for a client to show
// that the session holds a run, and to ask the session to carry it on.
type PendingRunInfo struct {
	// RunID identifies the run.
	RunID string `json:"run_id"`
	// Turn is the last turn the run recorded a record in.
	Turn int `json:"turn"`
	// Phase is where the run resumes.
	Phase State `json:"phase"`
	// Resumptions is how many times the run was resumed already.
	Resumptions int `json:"resumptions"`
}

// AwaitingInfo describes what a run is waiting on.
//
// A resumed run that waits on a permission ask publishes what it waits on,
// so a client can answer it. The ask ID is the log's, and the deadline is
// the wall-clock time it expires at.
type AwaitingInfo struct {
	// Kind is what the run waits on. The only kind is AskKindPermission.
	Kind string `json:"kind"`
	// AskID names the ask a client answers by.
	AskID string `json:"ask_id,omitempty"`
	// Deadline is the wall-clock time the wait ends, and the zero time
	// when it has none.
	Deadline time.Time `json:"deadline,omitempty"`
}

// AskKindPermission is the kind of a run that waits on a permission ask.
const AskKindPermission = "permission"

// LeaseLostWatch is the seam a session's store offers when its ownership is
// a lease.
//
// A session that writes through a lease watches it: the lease it lost is
// another owner's session, so the run stops writing at once. A store that
// holds its authority for the process's life offers no channel, and the
// session watches nothing.
type LeaseLostWatch interface {
	// LeaseLost is the channel that closes when the authority is lost.
	LeaseLost() <-chan struct{}
}

// WithContinuation gives the restore the continuation it carries out.
//
// A durable session's store plans what is left to do; the restore takes
// the plan, and the session holds it until Session.ResumeRun carries it
// out. A nil plan is a session with no run to continue.
func WithContinuation(plan *ContinuationPlan) Option {
	return func(opts *Options) { opts.continuation = plan }
}

// WithAutoResume makes a restore resume the run it found.
//
// A restored session with a continuation holds it, idle, until ResumeRun is
// called. With this option the restore calls it itself, so a caller that
// loads a durable session finds it already carrying on.
func WithAutoResume(auto bool) Option {
	return func(opts *Options) { opts.autoResume = auto }
}

// coveredCalls returns the call ids a continuation answers, and the ask ids
// it waits on.
//
// The restore refuses a session whose history holds a call with no result,
// because the provider rejects such a request. A continuation is the one
// exception: the calls it replays and the asks it re-asks are answered by
// the run it resumes, not by the records.
func (plan *ContinuationPlan) coveredCalls() (map[string]bool, map[string]bool) {
	if plan == nil {
		return nil, nil
	}
	calls := make(map[string]bool, len(plan.ReplayCalls)+len(plan.LiveAsks))
	asks := make(map[string]bool, len(plan.LiveAsks))
	for _, call := range plan.ReplayCalls {
		if call.CallID != "" {
			calls[call.CallID] = true
		}
	}
	for _, ask := range plan.LiveAsks {
		if ask.CallID != "" {
			calls[ask.CallID] = true
		}
		if ask.AskID != "" {
			asks[ask.AskID] = true
		}
	}
	return calls, asks
}
