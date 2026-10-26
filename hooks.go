package poo

import (
	"context"
	"encoding/json"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// View is a read-only view of the model's history.
//
// The view hands out copies: a caller that mutates what it got back
// changes nothing, and neither the model's history nor another caller's
// copy. Nested content and tool payloads are deep-copied on access.
type View interface {
	// Messages returns the model's history, in model order, after any
	// compaction.
	Messages() []Message
	// Turns returns a summary of each turn.
	Turns() []TurnSummary
	// Result returns the tool result with the given handle.
	Result(handle string) (ToolResult, bool)
}

// TurnSummary describes one turn of the model's history.
type TurnSummary struct {
	// Index is the turn's number in its run, counting from one.
	Index int
	// Session is the turn's number in the session, counting from one
	// and never restarting.
	//
	// The compaction seam names a range of turns by this number, so a
	// range may cross a run boundary, and a turn keeps its number when
	// a compaction removes it.
	Session int
	// Calls are the tool calls the turn made, in order.
	Calls []ToolCallSummary
	// Interrupted reports that the turn was cut short by an interrupt.
	Interrupted bool
}

// ToolCallSummary describes one tool call of a turn.
type ToolCallSummary struct {
	// Handle is the call's handle, e.g. "r1".
	Handle string
	// Tool is the name of the tool the model called.
	Tool string
	// Outcome is the call's outcome, when the call has one.
	Outcome Outcome
	// Summary is the call's human-facing description.
	Summary string
}

// Budget is the context budget of a run.
type Budget struct {
	// ContextLimit is the model's context window, in tokens.
	ContextLimit int
	// EstimatedInput is the estimated number of prompt tokens of the
	// next request.
	EstimatedInput int
	// NoticeRatio is the share of the window at which the model is told
	// that the context is filling up.
	NoticeRatio float64
	// HardRatio is the share of the window at which the run stops.
	HardRatio float64
	// CharsPerToken is the calibrated characters-per-token ratio.
	CharsPerToken float64
	// Turns is the number of provider requests the run has made.
	//
	// A compaction strategy needs it: the handoff strategy asks for a
	// handoff only after the run has asked the model something, and a
	// replay applies a recorded compaction after the same number of
	// requests.
	Turns int
}

// HookContext is what a hook is given.
//
// The context carries the run's identity, the turn, the model's history
// as a read-only view and the budget. A hook may append to the history
// through its decision, but it may never edit what the model has already
// seen.
type HookContext struct {
	context.Context
	// SessionID identifies the session.
	SessionID string
	// RunID identifies the run. A job's completion names the run that
	// started the job, which may already have settled.
	RunID string
	// Job is the identifier of the job the hook runs for, e.g. "j3". It is
	// empty when the hook runs for the run itself.
	Job string
	// Turn is the turn the hook is called in, counting from one.
	Turn int
	// Interrupted reports that the activity was interrupted. It is set
	// for AfterTurn and for the policy slot, so a pending ask resolves as
	// interrupted rather than cancelled.
	Interrupted bool
	// History is the model's view of the history, as a read-only view.
	History View
	// Budget is the run's context budget.
	Budget Budget
	// Decider is the decision model, or nil when there is none.
	Decider decide.Decider
	// Permissions is the policy slot's seam to the core.
	//
	// The field is set for the policy slot only: the slot reports the
	// asks it made and the answers that resolved them, and the core
	// publishes the permission events. It is nil for the consumer's own
	// hooks, which do not decide permissions.
	Permissions PermissionReporter
}

// SystemPromptEdit changes the system prompt.
//
// The edit is applied to the builder the run is rendering, so a hook can
// set, append, remove or insert a section without knowing the whole
// prompt.
type SystemPromptEdit func(prompt.Sections)

// StartDecision is what a session-start or before-run hook returns.
//
// A hook that returns nothing changes nothing.
type StartDecision struct {
	// Append are messages to add to the model's history.
	Append []Message
	// SystemPrompt changes the system prompt, when it is set.
	SystemPrompt SystemPromptEdit
}

// ToolCallInfo describes the call a tool hook is asked about.
type ToolCallInfo struct {
	// Spec is the tool's description.
	Spec ToolSpec
	// Prepared is the validated call, when the call got that far.
	Prepared Prepared
	// Arguments are the call's raw arguments, as the model wrote them.
	//
	// The arguments are what a policy rule matches a command against,
	// and what a doom loop's exact key is built from.
	Arguments json.RawMessage
	// PrepareFailed reports that the call failed its preparation, so
	// there is no prepared value. Only the doom loop judges such a call;
	// the rules do not, because the call is not a valid call.
	PrepareFailed bool
	// Handle is the call's handle, e.g. "r1".
	Handle string
	// Job is the identifier of the job the call runs as, e.g. "j3". It is
	// empty when the call runs in the turn.
	Job string
	// RepeatCount is how many times the model has made this exact call
	// in the run.
	RepeatCount int
	// NearRepeatCount is how many times the model has made a call with
	// the same primary argument.
	NearRepeatCount int
	// BatchIndex is the call's position in its batch, counting from
	// zero.
	BatchIndex int
}

// ToolDecision is what a before-tool hook returns.
//
// The decision types are the plan's: Allow, Deny and Result.
//
// A decision that names nothing allows the call. When a decision names
// more than one thing, a denial wins over a result, and a result wins
// over an allowance: a hook that both refuses and short-circuits
// refuses.
type ToolDecision struct {
	// Allowed reports that the call may run.
	Allowed bool
	// Denied reports that the call may not run.
	Denied bool
	// Detail explains a denial.
	Detail string
	// Result short-circuits the call with a synthetic result.
	Result *ToolResult
}

// Allow allows a call to run.
func Allow() ToolDecision { return ToolDecision{Allowed: true} }

// Deny refuses a call, with an explanation the model sees.
func Deny(detail string) ToolDecision { return ToolDecision{Denied: true, Detail: detail} }

// ShortCircuit ends a call with a synthetic result, without running the
// tool.
//
// The plan names this decision "Result", but the core already has a
// Result type for a run's outcome, so the constructor is named for what
// it does instead.
func ShortCircuit(r ToolResult) ToolDecision { return ToolDecision{Allowed: true, Result: &r} }

// ResultDecision is what an after-tool hook returns.
//
// The decision types are the plan's: Keep, Annotate, Replace and Redact.
//
// A decision that names nothing keeps the result. When a decision names
// more than one thing, a redaction wins over a replacement, and a
// replacement wins over an annotation: the chain sees the most
// consequential change.
type ResultDecision struct {
	// Kept reports that the result stands as it is.
	Kept bool
	// Annotated is text added to the result the model reads.
	Annotated string
	// Replaced is a result that takes the place of the original.
	Replaced *ToolResult
	// Redacted reports that the original must never be written to the
	// record.
	Redacted bool
}

// Keep leaves a result as it is.
func Keep() ResultDecision { return ResultDecision{Kept: true} }

// Annotate adds text to the result the model reads.
func Annotate(text string) ResultDecision { return ResultDecision{Kept: true, Annotated: text} }

// Replace substitutes a result for the original. The original is kept in
// the session record.
func Replace(r ToolResult) ResultDecision { return ResultDecision{Replaced: &r} }

// Redact drops the original from the record and shows the model the
// given result instead. Use it for secrets.
//
// The original never reaches the record, the events or the store, so
// the result the hook names is all that is left of it.
func Redact(r ToolResult) ResultDecision { return ResultDecision{Redacted: true, Replaced: &r} }

// TurnDecision is what an after-turn hook returns.
//
// The decision types are the plan's: nothing, Append and Stop.
type TurnDecision struct {
	// Append are messages to add to the model's history.
	Append []Message
	// Stop ends the run, with an explanation.
	Stop bool
	// Detail explains the stop.
	Detail string
}

// AppendMessages adds messages to the model's history at the end of a
// turn.
func AppendMessages(msgs ...Message) TurnDecision { return TurnDecision{Append: msgs} }

// StopTurn ends the run at the end of a turn, with an explanation.
func StopTurn(detail string) TurnDecision { return TurnDecision{Stop: true, Detail: detail} }

// SettleDecision is what a before-settle hook returns.
//
// The decision types are the plan's: Accept, Continue and Stop.
//
// A decision that names nothing accepts the settle. When a decision
// names more than one thing, a stop wins over a continuation: a hook
// that both stops and continues stops.
type SettleDecision struct {
	// Accepted reports that the run may settle as it would.
	Accepted bool
	// Continue is a message that sends the run back to the model.
	Continue *Message
	// Stop ends the run, with an explanation.
	Stop bool
	// Detail explains the stop.
	Detail string
}

// Accept lets the run settle.
func Accept() SettleDecision { return SettleDecision{Accepted: true} }

// Continue sends the run back to the model with a message.
func Continue(m Message) SettleDecision { return SettleDecision{Continue: &m} }

// StopSettle ends the run, with an explanation.
func StopSettle(detail string) SettleDecision { return SettleDecision{Stop: true, Detail: detail} }

// SessionStartHook runs once per session, before the first run.
type SessionStartHook interface {
	// SessionStart is called when the session is built.
	SessionStart(HookContext) (StartDecision, error)
}

// BeforeRunHook runs on each run start, before the first request.
type BeforeRunHook interface {
	// BeforeRun is called when a run begins. The trigger says what
	// started the run.
	BeforeRun(HookContext, RunTrigger) (StartDecision, error)
}

// BeforeToolHook runs after a call is prepared and before it runs.
type BeforeToolHook interface {
	// BeforeTool is called for every prepared call.
	BeforeTool(HookContext, ToolCallInfo) (ToolDecision, error)
}

// AfterToolHook runs after a call runs and before the model sees the
// result.
type AfterToolHook interface {
	// AfterTool is called for every finished call.
	AfterTool(HookContext, ToolCallInfo, ToolResult) (ResultDecision, error)
}

// AfterTurnHook runs at every turn end, including an interrupted turn.
type AfterTurnHook interface {
	// AfterTurn is called when a turn ends.
	AfterTurn(HookContext) (TurnDecision, error)
}

// BeforeSettleHook runs when the model stopped on its own.
type BeforeSettleHook interface {
	// BeforeSettle is called with the run's candidate settle reason.
	BeforeSettle(HookContext, Reason) (SettleDecision, error)
}

// PolicyHook is the before-tool slot the permission engine fills.
//
// The policy slot is a BeforeTool hook that only ever allows or denies: an
// ask happens inside the hook, not in a decision of its own. A result
// returned by a policy hook is ignored, because the policy slot decides
// whether the call runs at all, not what it returns.
type PolicyHook = BeforeToolHook

// Observer sees every event.
//
// An observer never settles a run. Its error is logged, and its events
// are dropped when its buffer is full.
type Observer interface {
	// Observe is called for every event.
	Observe(Event) error
}

// EventName names an event.
//
// The set is closed: the core emits these events and nothing else, and
// the names are the ones the plan and the wire protocol use.
type EventName string

const (
	// EventRunStarted is emitted when a run begins.
	EventRunStarted EventName = "run.started"
	// EventRunSettling is emitted when the run enters finalization.
	EventRunSettling EventName = "run.settling"
	// EventRunSettled is emitted when the run ends.
	EventRunSettled EventName = "run.settled"
	// EventInputQueued is emitted when an input is accepted.
	EventInputQueued EventName = "input.queued"
	// EventInputDelivered is emitted when an input reaches the model.
	EventInputDelivered EventName = "input.delivered"
	// EventInputDropped is emitted when an input is never delivered.
	EventInputDropped EventName = "input.dropped"
	// EventInputHeld is emitted when a job result is held.
	EventInputHeld EventName = "input.held"
	// EventTurnInterrupted is emitted when a turn is cut short.
	EventTurnInterrupted EventName = "turn.interrupted"
	// EventTurnStarted is emitted when a turn begins.
	EventTurnStarted EventName = "turn.started"
	// EventTurnEnded is emitted when a turn ends.
	EventTurnEnded EventName = "turn.ended"
	// EventJobStarted is emitted when a background job starts.
	EventJobStarted EventName = "job.started"
	// EventJobEnded is emitted when a background job ends.
	EventJobEnded EventName = "job.ended"
	// EventMessageDelta is emitted for each piece of a streamed message.
	EventMessageDelta EventName = "message.delta"
	// EventProviderRetry is emitted when a request is retried.
	EventProviderRetry EventName = "provider.retry"
	// EventToolCall is emitted when the model calls a tool.
	EventToolCall EventName = "tool.call"
	// EventToolResult is emitted when a call's result is ready.
	EventToolResult EventName = "tool.result"
	// EventPermissionAsked is emitted when the run needs an answer.
	EventPermissionAsked EventName = "permission.asked"
	// EventPermissionAnswered is emitted when the answer arrives.
	EventPermissionAnswered EventName = "permission.answered"
	// EventDecision is emitted when a decision call finishes.
	EventDecision EventName = "decision"
	// EventUsage is emitted with a request's token accounting.
	EventUsage EventName = "usage"
	// EventContextNotice is emitted when the context is filling up.
	EventContextNotice EventName = "context.notice"
	// EventCompactionStarted is emitted when a compaction begins.
	EventCompactionStarted EventName = "compaction.started"
	// EventCompactionEnded is emitted when a compaction ends.
	EventCompactionEnded EventName = "compaction.ended"
	// EventObserverDropped names the observer-overflow diagnostic. It
	// is a record-only diagnostic, never delivered to observers; the
	// event bus writes the coalesced record.
	EventObserverDropped EventName = "observer.dropped"
	// EventModelsChanged is emitted when the session switches models.
	EventModelsChanged EventName = "session.models_changed"
)

// Event is one thing that happened in a session.
//
// The events carry a session-monotonic sequence number, so an observer
// sees them in order.
type Event struct {
	// Seq is the session-monotonic sequence number, starting at 1.
	Seq int64 `json:"seq"`
	// Name is the event's name, e.g. "run.started".
	Name EventName `json:"name"`
	// At is the RFC 3339 time of the event, in UTC.
	At string `json:"at"`
	// Payload is the event's own data.
	Payload map[string]any `json:"payload,omitempty"`
}

// RunTrigger says what started a run.
type RunTrigger struct {
	// Kind is "prompt" or "job".
	Kind TriggerKind
	// Prompt is the prompt text, for a prompt run.
	Prompt string
	// Jobs are the job results that started the run, for a job run.
	Jobs []JobResult
}

// TriggerKind names what started a run.
type TriggerKind string

const (
	// TriggerPrompt means a person started the run.
	TriggerPrompt TriggerKind = "prompt"
	// TriggerJob means one or more background jobs woke the session.
	TriggerJob TriggerKind = "job"
)

// JobResult is a background job's result.
//
// The job and the handle are two different things. The job is the
// background job itself, named j<n>. The handle is the tool call the
// job came from, named r<n>: the call returned at once with a "started"
// outcome, and the job carries that call's handle, so a reader can tie
// the job back to the call that started it.
type JobResult struct {
	// Job is the job's identifier, e.g. "j3".
	Job string `json:"job"`
	// Handle is the handle of the tool call the job came from (that
	// call's ToolEnv.Handle), e.g.
	// "r1".
	Handle string `json:"handle"`
	// Tool is the name of the tool the job ran.
	Tool string `json:"tool"`
	// Text is the job's result, as the model reads it.
	Text string `json:"text"`
	// Outcome is the job's outcome.
	Outcome Outcome `json:"outcome"`
	// Fatal reports that the job failed with an error that wraps
	// ErrFatal. The run that receives the result settles tool_fatal.
	Fatal bool `json:"fatal,omitempty"`
}
