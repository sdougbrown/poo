package poo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Role is the author of a message in the model's history.
type Role string

const (
	// RoleSystem carries the system prompt.
	RoleSystem Role = "system"
	// RoleUser carries input from the user or from a tool.
	RoleUser Role = "user"
	// RoleAssistant carries model output.
	RoleAssistant Role = "assistant"
	// RoleTool carries a tool result back to the model.
	RoleTool Role = "tool"
)

// ContentKind names the type of a content block.
type ContentKind string

const (
	// ContentText is plain text.
	ContentText ContentKind = "text"
	// ContentThinking is model reasoning the provider returned separately.
	ContentThinking ContentKind = "thinking"
	// ContentToolCall is a request from the model to run a tool.
	ContentToolCall ContentKind = "tool_call"
	// ContentToolResult is the result of a tool call.
	ContentToolResult ContentKind = "tool_result"
)

// ContentBlock is one piece of a message's content.
type ContentBlock struct {
	Kind ContentKind `json:"kind"`
	Text string      `json:"text,omitempty"`

	// ToolCallID and Name are set when Kind is ContentToolCall.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	// Arguments holds the raw JSON arguments of a tool call.
	Arguments json.RawMessage `json:"arguments,omitempty"`

	// ToolCallID is reused for a tool result, so the field above carries it.
	// IsError marks a tool result that the model must see as a failure.
	IsError bool `json:"is_error,omitempty"`
}

// Message is one turn of the conversation as the model sees it.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Text returns the concatenated text blocks of the message.
func (m Message) Text() string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Kind == ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// ToolCalls returns the tool call blocks of the message, in order.
func (m Message) ToolCalls() []ContentBlock {
	var out []ContentBlock
	for _, c := range m.Content {
		if c.Kind == ContentToolCall {
			out = append(out, c)
		}
	}
	return out
}

// NewTextMessage builds a message with a single text block.
func NewTextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentBlock{{Kind: ContentText, Text: text}}}
}

// ToolResultMessage builds the message that carries a tool result back.
func ToolResultMessage(id, name, text string, isError bool) Message {
	return Message{
		Role: RoleTool,
		Content: []ContentBlock{{
			Kind:       ContentToolResult,
			ToolCallID: id,
			Name:       name,
			Text:       text,
			IsError:    isError,
		}},
	}
}

// ToolCall is a request from the model to run a tool.
type ToolCall struct {
	// ID is the provider's identifier for the call. The tool's result
	// goes back to the model under this id, because that is the id the
	// provider matches it against.
	ID string `json:"id"`
	// Name is the tool the model called.
	Name string `json:"name"`
	// Arguments are the raw JSON arguments.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is what a tool returns to the model.
type ToolResult struct {
	// Text is the model-facing text.
	Text string `json:"text"`
	// Structured is an optional side channel for consumers.
	Structured json.RawMessage `json:"structured,omitempty"`
	// IsError marks a recoverable failure. The model sees the text and
	// may correct itself; the run continues.
	IsError bool `json:"is_error,omitempty"`
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
	Class ToolClass `json:"class"`
	// PrimaryArg names the argument most likely to drive a doom loop.
	PrimaryArg string `json:"primary_arg,omitempty"`
}

// ToolClass is the side-effect class of a tool.
type ToolClass string

const (
	// ClassReadOnly marks a tool that cannot change anything.
	ClassReadOnly ToolClass = "read_only"
	// ClassSideEffecting marks a tool that can change the world.
	ClassSideEffecting ToolClass = "side_effecting"
)

// PrepareEnv is the environment a tool sees while validating arguments.
type PrepareEnv struct {
	// WorkingDir is the absolute directory the run works in.
	WorkingDir string
}

// ToolEnv is the environment a tool sees while running.
type ToolEnv struct {
	PrepareEnv
	// SessionID and RunID identify the run.
	SessionID string
	RunID     string
	// Handle is this call's identifier, e.g. "r1".
	Handle string
	// Env is the scrubbed child environment for a subprocess tool.
	Env []string
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

// Tool is a tool the model can call.
type Tool interface {
	// Spec describes the tool.
	Spec() ToolSpec
	// Prepare decodes and validates raw arguments. It has no side effects.
	Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error)
	// Call runs the tool.
	Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error)
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
