package poo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/prompt"
)

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
	Class ToolClass `json:"class"`
	// PrimaryArg names the argument most likely to drive a doom loop.
	PrimaryArg string `json:"primary_arg,omitempty"`
	// Background says whether the model may run the tool in the
	// background.
	Background Background `json:"background,omitempty"`
}

// ToolClass is the side-effect class of a tool.
type ToolClass string

const (
	// ClassReadOnly marks a tool that cannot change anything.
	ClassReadOnly ToolClass = "read_only"
	// ClassSideEffecting marks a tool that can change the world.
	ClassSideEffecting ToolClass = "side_effecting"
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
	Job string
	// Env is the scrubbed child environment for a subprocess tool.
	Env []string
	// Decider is the decision model, or nil when there is none.
	Decider decide.Decider
	// Settings holds the tool's own settings block.
	Settings ToolSettings
}

// ToolSettings holds the tunable knobs a tool reads.
type ToolSettings struct {
	// BashTimeoutSeconds is the per-call timeout for the bash tool.
	BashTimeoutSeconds int
	// ObservationCapChars is the cap on the observation shown to the model.
	ObservationCapChars int
	// ReadOutputCapChars is the cap on the read tool's output.
	ReadOutputCapChars int
}

// Prepared is a validated tool call, ready to run.
type Prepared interface {
	// Summary is a short human-facing description of the call.
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
