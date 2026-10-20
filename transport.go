package poo

import (
	"context"
	"encoding/json"
)

// ChatRequest is one provider request: the full model view plus the
// instruction on how much output room the model has.
//
// The transport is stateless. Every request carries the whole history.
type ChatRequest struct {
	// Model is the provider's model identifier.
	Model string
	// Messages is the model's history, oldest first.
	Messages []Message
	// Tools are the tools the model may call.
	Tools []ToolSpec
	// MaxTokens is the output-token ceiling for this request. It is always
	// computed by the caller, never left to the endpoint default.
	MaxTokens int
}

// Delta is one piece of a streamed response.
type Delta struct {
	// Kind is "text" or "thinking".
	Kind string
	// Text is the delta's text.
	Text string
}

// ToolCallDelta is a tool call the model requested.
type ToolCallDelta struct {
	// Index groups the fragments of one call. A provider that sends one
	// call whole sends 0.
	Index int
	// ID is the call's identifier.
	ID string
	// Name is the tool the model called.
	Name string
	// Arguments is the raw JSON arguments. It is complete when the call
	// arrives whole, and may be a fragment when the provider streams
	// arguments in pieces.
	Arguments json.RawMessage
	// Complete reports whether Arguments is a complete JSON object.
	Complete bool
}

// FinishReason is why the model stopped.
type FinishReason string

const (
	// FinishStop means the model finished its answer.
	FinishStop FinishReason = "stop"
	// FinishLength means the provider stopped on the output-token limit.
	FinishLength FinishReason = "length"
	// FinishToolCalls means the model asked for tool calls.
	FinishToolCalls FinishReason = "tool_calls"
)

// StreamReader is the read side of a provider response.
//
// The reader yields deltas, tool calls, usage and a finish reason. It is
// closed by the transport when the response ends.
type StreamReader interface {
	// Next returns the next piece of the response. It returns io.EOF when
	// the response is complete.
	Next() (StreamEvent, error)
	// Close releases the response.
	Close() error
}

// StreamEvent is one piece of a provider response.
type StreamEvent struct {
	// Delta is a text or thinking fragment. It is set when Kind is
	// "delta".
	Delta *Delta
	// ToolCall is a tool call request. It is set when Kind is "tool_call".
	ToolCall *ToolCallDelta
	// Usage is the token accounting. It is set when Kind is "usage".
	Usage *UsageReport
	// FinishReason is set when Kind is "finish".
	FinishReason FinishReason
	// Kind names the payload: "delta", "tool_call", "usage" or "finish".
	Kind string
}

// UsageReport is the token accounting a provider reports.
type UsageReport struct {
	// InputTokens is the number of prompt tokens.
	InputTokens int
	// OutputTokens is the number of completion tokens.
	OutputTokens int
	// CacheReadTokens is the number of prompt tokens served from the
	// provider's cache. It is nil when the endpoint does not report it.
	CacheReadTokens *int
	// TTFTMS is the time to the first streamed token, in milliseconds.
	// It is measured by the transport, not reported by the endpoint.
	TTFTMS float64
}

// ChatTransport sends a chat request to a provider.
//
// The interface is the seam the plan's Stage 2 formalises; the skeleton's
// version is the small one the OpenAI transport needs.
type ChatTransport interface {
	// Stream starts a request and returns the response stream.
	Stream(ctx context.Context, req ChatRequest) (StreamReader, error)
}
