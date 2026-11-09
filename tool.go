package poo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

// ToolInfo describes one tool as a session's metadata records it.
//
// It is the tool's name, its side-effect class and its background mode:
// what a restored session checks the tool against.
type ToolInfo struct {
	// Name is the tool's identifier.
	Name string `json:"name"`
	// Class is the tool's side-effect class.
	Class ToolKind `json:"class"`
	// Background is the tool's background mode. A session loaded from
	// the record is incompatible with the current build when a tool's
	// mode changed.
	Background Background `json:"background,omitempty"`
}

// ToolSpec describes a tool to the model and to the core.
type ToolSpec struct {
	// Name is the tool's identifier, as the model calls it.
	Name string `json:"name"`
	// Description tells the model what the tool does.
	Description string `json:"description"`
	// Schema is the JSON schema of the arguments, for the model only.
	Schema json.RawMessage `json:"schema"`
	// Class marks a tool that only reads (ReadOnly) or one that changes
	// something (SideEffecting).
	Class ToolKind `json:"class"`
	// PrimaryArg names the argument most likely to drive a doom loop.
	PrimaryArg string `json:"primary_arg,omitempty"`
	// Background says whether the model may run the tool in the
	// background.
	Background Background `json:"background,omitempty"`
}

// ToolKind is the side-effect class of a tool.
type ToolKind string

const (
	// ClassReadOnly marks a tool that cannot change anything.
	ClassReadOnly ToolKind = "read_only"
	// ClassSideEffecting marks a tool that can change the world.
	ClassSideEffecting ToolKind = "side_effecting"
)

// Background says whether a tool call may become a background job.
//
// The zero value is Never, so a tool that says nothing is an ordinary
// synchronous tool.
type Background string

const (
	// BackgroundNever means every call runs in the turn.
	BackgroundNever Background = ""
	// BackgroundAllowed means the model may ask for the call to run in
	// the background, with a "background" argument.
	BackgroundAllowed Background = "allowed"
	// BackgroundAlways means every call runs in the background.
	BackgroundAlways Background = "always"
)

// PrepareEnv is the environment a tool sees while validating arguments.
type PrepareEnv struct {
	// WorkingDir is the absolute directory the run works in.
	WorkingDir string
	// ProtectedPaths are the paths the tool must not touch, already
	// expanded.
	ProtectedPaths []string
}

// ToolEnv is the environment a tool sees while running.
type ToolEnv struct {
	PrepareEnv
	// SessionID and RunID identify the run.
	SessionID string
	RunID     string
	// Handle is this call's identifier, e.g. "r1".
	Handle string
	// Job is the identifier of the job this call runs as, e.g. "j3". It is
	// empty when the call runs in the turn.
	JobID string
	// Env is the scrubbed child environment for a subprocess tool.
	Env []string
	// Decider is the decision model, or nil when there is none.
	Decider decide.Decider
	// Nested runs nested tool calls, or nil when this call cannot make
	// one. Only a call that runs in the turn has it; a background job
	// does not, and a nested call's own tool does not, so the nesting
	// is one call deep.
	Nested NestedCaller
	// Settings holds the tool's own settings block.
	Settings ToolSettings
}

// ToolSettings holds the tunable knobs a tool reads.
type ToolSettings struct {
	// BashTimeoutS is the bash tool's timeout for a call, and the ceiling
	// on a call's own `timeout`.
	BashTimeoutS int
	// ObservationCapChars is the cap on the observation shown to the model.
	ObservationCapChars int
	// ReadOutputCapChars is the cap on the read tool's output.
	ReadOutputCapChars int
}

// NestedResult is what a nested tool call produced.
type NestedResult struct {
	// Handle is the nested call's own handle, e.g. "r7".
	Handle string
	// Tool is the tool that ran.
	Tool string
	// Outcome is the call's outcome, decided by the core.
	Outcome Outcome
	// Text is the recorded result text, with the handle line first.
	Text string
	// Structured is the optional side-channel payload.
	Structured json.RawMessage
	// IsError reports a recoverable failure.
	IsError bool
}

// NestedCaller runs a nested tool call from inside a tool's Call.
//
// A nested call is a tool call a running tool makes. It is validated
// like a model-made call, it is judged by the permission engine and the
// doom loop, and its call and result are recorded with the mode
// "nested" — but its result never enters the model's history, and its
// tool's own environment carries no nested caller of its own, so the
// nesting is one call deep.
type NestedCaller interface {
	// Call runs one nested call of the named tool with the raw
	// arguments. The parent handle names the call that is running.
	//
	// A refused, failed or cancelled nested call is reported as data
	// in the result, not as an error: the caller decides what its
	// script does with a denial. The error result is for the nested
	// call that never got to be judged at all — an unknown tool, a
	// run that is winding down, a run that was interrupted.
	Call(ctx context.Context, parent, tool string, raw json.RawMessage) (NestedResult, error)
}

// Prepared is a validated tool call, ready to run.
type Prepared interface {
	// Summary is a short human-facing description of the call, as the
	// event stream and the transcript show it.
	//
	// A permission ask must not rest on a model-authored description, so
	// a tool may name what its ask shows instead: see AskSummaryer.
	Summary() string
}

// CanonicalKeyer is implemented by a prepared call the core cannot
// re-encode.
//
// The core builds the doom loop's exact key from the prepared call: it
// re-encodes the value as JSON with sorted keys. A prepared call whose
// value marshals to no fields returns its canonical key instead, and a
// call that does neither falls back to its canonicalized raw
// arguments.
type CanonicalKeyer interface {
	// CanonicalKey returns the call's prepared input as a JSON object.
	// Two calls that prepare to the same input return the same key.
	CanonicalKey() json.RawMessage
}

// EvidenceArgumenter is implemented by a prepared call that says which
// of its arguments a decision model may see.
//
// A tool that does not implement it exposes the raw arguments the model
// wrote.
type EvidenceArgumenter interface {
	// EvidenceArguments returns the arguments a decision model may see,
	// as a JSON object.
	EvidenceArguments() json.RawMessage
}

// AskSummaryer is implemented by a prepared call whose permission ask
// must show something other than its summary.
//
// The event stream and transcript show the summary, which the model may write.
// Permission decisions need the call itself, not the model's description.
// A tool whose summary comes from its arguments names the call here.
// A tool that does not implement it asks with its summary.
type AskSummaryer interface {
	// AskSummary returns the call's permission-ask text.
	AskSummary() string
}

// Tool is a tool the model can call.
type Tool interface {
	// Spec describes the tool.
	Spec() ToolSpec
	// Prepare decodes and validates raw arguments. It has no side effects.
	Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error)
	// Call runs the tool.
	Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error)
}

// PromptContributor is implemented by a tool that needs prompt guidance.
//
// A tool left out of the build takes its prompt guidance with it, so the
// prompt never mentions a tool the model cannot call.
type PromptContributor interface {
	// PromptSection renders the tool's own section of the system prompt.
	PromptSection(env prompt.Env) string
}

// PromptVariantContributor is implemented by a tool that offers
// alternative texts for its prompt section.
//
// A tool that implements it keeps the same section name: the variants
// are the alternatives that section's text may take, and the prompt
// renders the one the session's variant names. A tool that does not
// implement it is unaffected.
type PromptVariantContributor interface {
	// PromptVariants renders the tool's section of the system prompt
	// for each variant it offers, keyed by variant name.
	//
	// A variant that is not among them is not offered: the section
	// renders its default text for it.
	PromptVariants(env prompt.Env) map[string]string
}

// ErrInvalidArgs marks a validation failure. It is recoverable: the model
// sees the message and may correct the call.
type ErrInvalidArgs struct {
	Message string
}

func (e *ErrInvalidArgs) Error() string { return e.Message }

// InvalidArgsf builds an ErrInvalidArgs with a formatted message.
func InvalidArgsf(format string, args ...any) *ErrInvalidArgs {
	return &ErrInvalidArgs{Message: fmt.Sprintf(format, args...)}
}
