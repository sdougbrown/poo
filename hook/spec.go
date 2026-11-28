// Package hook defines poo's declarative hooks.
//
// A hook is a policy file rather than code: it names the point it runs
// at, the calls it matches, the evidence it gathers, and a small graph
// of typed questions whose answers pick a terminal decision. The core
// keeps ownership of every lifecycle step, so a hook's whole vocabulary
// is its typed decision and an error.
//
// The spec is checked before any runner exists. Validate turns a Spec
// into a Compiled, and a spec that cannot be executed is a build
// failure: an unknown point, a matcher the point cannot use, a decision
// the point cannot carry, or a graph that loops or strands a step. An
// answer no branch covers is not a failure: the runner reads it as the
// hook being unsure, and takes the spec's on_uncertain policy.
//
// The package imports the standard library, the core package, poo/decide
// and poo/exec only, so a build that links no tool, no provider and no
// code mode still checks and runs its hooks.
package hook

import (
	"encoding/json"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

//go:generate go run github.com/umpire-tools/umpire-go-gen@v0.3.1 -i schema/hook.umpire.json -output-file policy_gen.go -pkg hook

// The bounds a hook spec cannot exceed.
//
// A hook sits in a run's path, so its whole budget is small: the steps
// it may ask, the probes it may run, the evidence and probe output it
// may carry, and the time it has to answer before the run's own timeout
// closes on it.
const (
	// MaxSteps is the most steps one hook's graph may have.
	MaxSteps = 8
	// MaxProbes is the most probes one hook may run.
	MaxProbes = 4
	// MaxProbeOutputBytes is the most output one probe may hand back.
	MaxProbeOutputBytes = 8192
	// MaxEvidenceBytes is the most evidence one hook may hand back.
	MaxEvidenceBytes = 32768
	// DefaultFiringTimeoutMs is how long a hook may take when its spec
	// names no timeout. A runner clamps it under the hook call's own
	// timeout so a fail-open answer always arrives.
	DefaultFiringTimeoutMs = 10000
)

// Point is the place in the run's lifecycle a hook runs at.
type Point string

// The points a hook may run at.
const (
	// PointBeforeRun runs before a run starts and returns a start
	// decision.
	PointBeforeRun Point = "before_run"
	// PointBeforeTool runs before a tool call and returns an allow or a
	// denial.
	PointBeforeTool Point = "before_tool"
	// PointAfterTool runs after a tool call and returns what to keep of
	// its result.
	PointAfterTool Point = "after_tool"
	// PointAfterTurn runs after a turn and returns whether to go on.
	PointAfterTurn Point = "after_turn"
	// PointBeforeSettle runs at settle and returns whether the run is
	// done.
	PointBeforeSettle Point = "before_settle"
)

// FailurePolicy says what a runner does when a hook cannot answer.
type FailurePolicy string

// The policies a hook may name.
const (
	// FailOpen takes the point's neutral decision: the hook's own say
	// is dropped and the run continues as if the hook had not fired.
	FailOpen FailurePolicy = "fail_open"
	// FailClosed takes the point's blocking decision, the one
	// fail_closed_message words. A point with no blocking decision has
	// no fail_closed to take, and the spec is refused.
	FailClosed FailurePolicy = "fail_closed"
	// FailRun fails the run through the core's own finalizer.
	FailRun FailurePolicy = "fail_run"
)

// ActionKind is the decision a branch's terminal takes.
type ActionKind string

// The decisions a branch may take, by point.
const (
	// ActionPass starts the run unchanged.
	ActionPass ActionKind = "pass"
	// ActionAppend adds the message to the run's prompt.
	ActionAppend ActionKind = "append"
	// ActionAllow lets the tool call proceed.
	ActionAllow ActionKind = "allow"
	// ActionDeny refuses the tool call and shows the message.
	ActionDeny ActionKind = "deny"
	// ActionShortCircuit answers the tool call without running it.
	ActionShortCircuit ActionKind = "short_circuit"
	// ActionKeep keeps the tool result as it is.
	ActionKeep ActionKind = "keep"
	// ActionAnnotate appends the message to the tool result.
	ActionAnnotate ActionKind = "annotate"
	// ActionReplace replaces the tool result with the message.
	ActionReplace ActionKind = "replace"
	// ActionRedact redacts the tool result.
	ActionRedact ActionKind = "redact"
	// ActionStop stops the turn and shows the message.
	ActionStop ActionKind = "stop"
	// ActionAccept settles the run.
	ActionAccept ActionKind = "accept"
	// ActionContinue keeps the run going and shows the message.
	ActionContinue ActionKind = "continue"
)

// The matcher values each point's fields allow.
//
// They are the core's own words, so a matcher cannot name a run a trigger
// the core stopped naming, or a reason it renamed.
const (
	// TriggerPrompt matches a run a person started.
	TriggerPrompt = poo.TriggerPrompt
	// TriggerJob matches a run a job started.
	TriggerJob = poo.TriggerJob
	// TriggerFeed matches a run a feed started.
	TriggerFeed = poo.TriggerFeed
	// ClassReadOnly matches a tool that only reads.
	ClassReadOnly = poo.ClassReadOnly
	// ClassSideEffecting matches a tool that changes something.
	ClassSideEffecting = poo.ClassSideEffecting
	// ReasonEndTurn matches a settle the model ended.
	ReasonEndTurn = poo.ReasonEndTurn
	// ReasonOutputTruncated matches a settle the output limit ended.
	ReasonOutputTruncated = poo.ReasonOutputTruncated
)

// The question kinds a step may ask.
const (
	// QuestionBool asks a yes-or-no question.
	QuestionBool = "bool"
	// QuestionChoice asks which of a set of options fits.
	QuestionChoice = "choice"
	// QuestionScore asks how strongly something holds.
	QuestionScore = "score"
)

// Spec is one hook's declaration.
type Spec struct {
	// Point is where the hook runs.
	Point Point `json:"point"`
	// Match narrows the calls the hook fires on. An empty matcher fires
	// on every call at the point.
	Match Match `json:"match,omitempty"`
	// MaxFiresPerRun is how many times the hook may fire in one run.
	// Zero sets no cap of the hook's own.
	MaxFiresPerRun int `json:"max_fires_per_run,omitempty"`
	// FiringTimeoutMs is how long the hook has to answer. Zero takes
	// DefaultFiringTimeoutMs. A runner keeps its own budget under the
	// session's hook deadline, so a longer spec timeout never outlives
	// the call it answers.
	FiringTimeoutMs int `json:"firing_timeout_ms,omitempty"`
	// OnError is what the runner does when the hook fails. An absent
	// policy is FailOpen.
	OnError FailurePolicy `json:"on_error,omitempty"`
	// OnUncertain is what the runner does when no branch is sure
	// enough. An absent policy is FailOpen.
	OnUncertain FailurePolicy `json:"on_uncertain,omitempty"`
	// FailClosedMessage is the text a FailClosed policy hands the model.
	// It is required when either policy is FailClosed, and allowed only
	// then.
	FailClosedMessage string `json:"fail_closed_message,omitempty"`
	// Context is the evidence the hook is shown.
	Context ContextSpec `json:"context,omitempty"`
	// Entry names the first step.
	Entry string `json:"entry"`
	// Steps are the graph's steps, by name.
	Steps map[string]Step `json:"steps"`
}

// Match narrows the calls a hook fires on. Each point accepts its own
// fields and refuses the rest, so a matcher that means nothing there is
// a spec error rather than a hook that never fires.
type Match struct {
	// Trigger is the kind of run this point matches: one of the Trigger
	// values. before_run only.
	Trigger poo.TriggerKind `json:"trigger,omitempty"`
	// Tool is the tool's name. The tool points match on it.
	Tool string `json:"tool,omitempty"`
	// Class is the tool's class: ClassReadOnly or ClassSideEffecting.
	// The tool points match on it, and a matcher names a tool or a
	// class, never both.
	Class poo.ToolKind `json:"class,omitempty"`
	// CommandPrefix is what the call's command starts with. It needs a
	// tool, and it excludes CommandContains.
	CommandPrefix string `json:"command_prefix,omitempty"`
	// CommandContains are substrings the call's command may hold: any
	// one of them holding is the matcher holding. It needs a tool, and it
	// excludes CommandPrefix.
	CommandContains []string `json:"command_contains,omitempty"`
	// IsError matches a result the tool reported as an error.
	// after_tool only.
	IsError *bool `json:"is_error,omitempty"`
	// MinTurn matches from this turn on. after_turn and before_settle
	// only.
	MinTurn *int `json:"min_turn,omitempty"`
	// Interrupted matches a turn that was interrupted. after_turn only.
	Interrupted *bool `json:"interrupted,omitempty"`
	// Reason is the settle reason this point matches: one of the Reason
	// values. before_settle only.
	Reason poo.Reason `json:"reason,omitempty"`
}

// ContextSpec is the evidence a hook is shown.
type ContextSpec struct {
	// Turns is how many recent turns the evidence holds.
	Turns int `json:"turns,omitempty"`
	// IncludeToolCall adds the call the hook is about.
	IncludeToolCall bool `json:"include_tool_call,omitempty"`
	// IncludeWorkingDir adds the directory the session works in.
	IncludeWorkingDir bool `json:"include_working_dir,omitempty"`
	// Probes are the facts the hook gathers before it asks.
	Probes []Probe `json:"probes,omitempty"`
}

// Probe is one fact a hook gathers before it asks its questions. A
// probe runs a command or a read-only tool, never both: a command is a
// subprocess and a tool is the session's own read.
type Probe struct {
	// Name names the probe's evidence. It is unique within a hook.
	Name string `json:"name"`
	// Command is the argv a command probe runs.
	Command []string `json:"command,omitempty"`
	// Tool is the tool a tool probe calls.
	Tool *ToolProbe `json:"tool,omitempty"`
	// TimeoutMs is how long the probe may take, in milliseconds. Zero
	// takes 3000 ms. The hook's firing context bounds it either way, so
	// a probe never outlives the hook that asked for it.
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

// ToolProbe is a read-only tool call a probe makes.
type ToolProbe struct {
	// Name is the tool's name.
	Name string `json:"name"`
	// Args are the call's arguments, as the tool's own JSON object.
	Args json.RawMessage `json:"args,omitempty"`
}

// Step is one question and what each of its answers does.
type Step struct {
	// TimeoutMs is how long the step's own ask may take. Zero leaves the
	// step under the hook's timeout.
	TimeoutMs int `json:"timeout_ms,omitempty"`
	// Question is what the step asks.
	Question QuestionSpec `json:"question"`
	// Branches route the answers, by answer key. A step needs at least
	// one; an answer no branch covers is the hook being unsure, and
	// takes the spec's on_uncertain policy.
	Branches map[string]Branch `json:"branches"`
}

// QuestionSpec is one typed question.
//
// The kinds carry different fields, so a question names its kind and the
// fields that kind uses. A field another kind owns is a spec error.
type QuestionSpec struct {
	// Kind is the question's shape: one of the Question values.
	Kind string `json:"kind"`
	// Instructions say what the model decides.
	Instructions string `json:"instructions"`
	// True describes what "yes" means, and False what "no" means. Bool
	// only.
	True string `json:"true,omitempty"`
	// False describes what "no" means. Bool only.
	False string `json:"false,omitempty"`
	// Options are the answers. Choice only.
	Options []decide.Option `json:"options,omitempty"`
	// Levels are the scale's levels. Score only.
	Levels []string `json:"levels,omitempty"`
}

// Branch is what one answer does.
//
// A branch either moves to another step or takes the terminal decision.
// It carries the confidence the answer needs to be followed at all:
// below MinProbability no branch is sure enough, and the runner takes
// the hook's on_uncertain policy.
type Branch struct {
	// MinProbability is the smallest probability the answer needs, above
	// zero and at most one.
	MinProbability float64 `json:"min_probability"`
	// Next is the step the answer moves to.
	Next string `json:"next,omitempty"`
	// Action is the decision the answer takes.
	Action *Action `json:"action,omitempty"`
}

// Action is a branch's terminal decision.
type Action struct {
	// Kind is the decision, one of the ActionKind values the point
	// allows.
	Kind ActionKind `json:"kind"`
	// Message is the text the decision carries, for the decisions that
	// show one.
	Message string `json:"message,omitempty"`
}

// Compiled is a spec the check accepted, in the form a runner reads.
//
// The check copies what it keeps, so a Compiled does not see a later
// change to the Spec it came from. A Compiled is read-only: a runner
// reads it, and nothing rewrites a policy mid-run.
type Compiled struct {
	// Name is the hook's name. It is the bare name, so the core's own
	// origin renders it as hook:<name>.
	Name string
	// Point is where the hook runs.
	Point Point
	// Match narrows the calls the hook fires on.
	Match Match
	// Context is the evidence the hook is shown.
	Context ContextSpec
	// Entry names the first step.
	Entry string
	// Steps are the graph's steps, by name.
	Steps map[string]CompiledStep
	// MaxFiresPerRun is how many times the hook may fire in one run.
	MaxFiresPerRun int
	// FiringTimeoutMs is how long the hook has to answer, with the
	// default already taken.
	FiringTimeoutMs int
	// OnError is what the runner does when the hook fails.
	OnError FailurePolicy
	// OnUncertain is what the runner does when no branch is sure enough.
	OnUncertain FailurePolicy
	// FailClosedMessage is the text a FailClosed policy hands the model.
	FailClosedMessage string
	// Idempotent reports that the hook gathers no command probe, so
	// running it again repeats no operator's command.
	Idempotent bool
}

// CompiledStep is a step with its question compiled and its answers
// checked against that question.
type CompiledStep struct {
	// Name is the step's name.
	Name string
	// TimeoutMs is how long the step's own ask may take. Zero leaves the
	// step under the hook's timeout.
	TimeoutMs int
	// Question is the compiled question. Its ID is
	// hook:<hook name>:<step name>.
	Question decide.Question
	// Branches route the answers, by answer key.
	Branches map[string]CompiledBranch
}

// CompiledBranch is a checked branch.
type CompiledBranch struct {
	// MinProbability is the smallest probability the answer needs.
	MinProbability float64
	// Next is the step the answer moves to.
	Next string
	// Action is the decision the answer takes.
	Action *Action
}
