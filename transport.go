package poo

import (
	"context"
	"encoding/json"
	"time"
)

// ChatRequest is one provider request: the full model view plus the
// instruction on how much output room the model has.
//
// The transport is stateless. Every request carries the whole history.
type ChatRequest struct {
	// Model is the provider's model identifier.
	Model string
	// Info is the model info the request is shaped from.
	//
	// A transport uses it for everything the model decides: the tools
	// mode, the reasoning and temperature controls, the sampling
	// defaults, the quirks and the output limit. A zero model info
	// leaves the transport's own configuration in charge, so a consumer
	// that calls Stream directly can keep using Config.Info.
	Info ModelInfo
	// Messages is the model's history, oldest first.
	Messages []Message
	// Tools are the tools the model may call.
	Tools []ToolSpec
	// MaxTokens is the output-token limit for this request. It is always
	// computed by the caller, never left to the endpoint default.
	MaxTokens int
	// OnAttempt is called for every attempt that failed, so the caller
	// can write the attempt to its record.
	//
	// The attempt that ended the request is reported too, and the
	// attempt says whether another one followed it.
	//
	// The call happens on the goroutine that reads the response, and
	// the attempt is reported before the wait that follows it.
	//
	// A nil reporter drops the attempts.
	OnAttempt func(Attempt)
}

// Attempt is one attempt of a request that failed.
//
// A request that fails before the endpoint answered is tried again, and
// every attempt that failed is reported, so the record can hold them.
type Attempt struct {
	// Number is the attempt's number, counting from one.
	Number int
	// Err is the failure the attempt ended with.
	Err error
	// Delay is how long the transport waited before the next attempt.
	// It is zero when no other attempt followed.
	Delay time.Duration
	// Retried reports that another attempt followed this one.
	Retried bool
}

// Delta is one piece of a streamed response.
type Delta struct {
	// Kind is "text" or "thinking".
	Kind string
	// Text is the delta's text.
	Text string
}

// CallOrigin says where a tool call came from.
type CallOrigin string

const (
	// CallOriginWire is a call the endpoint sent in its own field.
	CallOriginWire CallOrigin = "wire"
	// CallOriginParsedText is a call the model wrote as text and the
	// transport read out of it.
	CallOriginParsedText CallOrigin = "parsed_text"
)

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
	// Complete reports whether this fragment's Arguments is a complete JSON
	// object. It is informational: a call's arguments can arrive over several
	// fragments, so the core decides whether a call is complete by parsing
	// the assembled arguments, not from this field.
	Complete bool
	// Origin says where the call came from: "wire" for a call the
	// endpoint sent, "parsed_text" for a call the model wrote as text.
	// It is empty when the transport did not say.
	Origin CallOrigin
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

// UsageReporter is implemented by a stream reader that can report the
// usage of a request that failed before its usage arrived.
//
// A provider that dies mid-stream still cost tokens, so the transport
// reports what it knows. The core uses it to write the usage record of a
// request that broke.
type UsageReporter interface {
	// ReportUsage returns the usage the request produced, or nil when
	// there is none.
	ReportUsage() *UsageReport
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
	// Error is a failure the transport reports about the response. It is
	// set when Kind is "error".
	Error *StreamError
	// Kind names the payload: "delta", "tool_call", "usage", "finish" or
	// "error".
	Kind string
	// Attempt is the request attempt that produced the event, counting
	// from one. A transport that retries sets it; zero means the
	// transport did not say.
	Attempt int
}

// StreamError is a failure in a response that did not stop the stream.
//
// A tool call the model wrote as text and that could not be parsed is
// one: the text is not lost, and the run carries on.
type StreamError struct {
	// Message explains the failure.
	Message string
	// Text is the text the failure is about, when the failure is about
	// text.
	Text string
	// Recoverable reports that the run can carry on: the failure is
	// visible and the model gets another turn.
	Recoverable bool
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

// Rendered is the immutable snapshot of a request as it was sent.
//
// It holds the serialized request body exactly as it went out. A fork
// sends a body that is byte-identical to this one, apart from the
// messages appended after the last one and the output limit.
//
// The snapshot is what makes the payload, rather than a cache hit,
// reproducible: a compaction strategy that forks the last request sends
// the same prefix, so a server's prefix cache can hit.
type Rendered struct {
	// API is the wire API the body belongs to.
	API API
	// Body is the serialized request body, exactly as sent.
	Body []byte
	// Messages is the number of messages the body carried, so a fork
	// knows where the appended messages go.
	Messages int
}

// ChatTransport sends a chat request to a provider.
//
// The transport owns the wire shape. Core never sees a provider's own
// JSON, and a transport never decides what the model sees: it converts
// the model's view into the API's shape and the API's answers back into
// the model's shapes.
type ChatTransport interface {
	// Stream starts a request and returns the response stream along
	// with the snapshot of the request that was sent.
	Stream(ctx context.Context, req ChatRequest) (StreamReader, Rendered, error)
	// Fork sends a body that is byte-identical to the snapshot, apart
	// from the appended messages and the output limit.
	//
	// A transport that cannot fork returns a typed error: the
	// compaction seam needs a fork, and a transport that cannot do one
	// cannot take part in a compaction.
	Fork(ctx context.Context, prev Rendered, appended []Message, maxTokens int) (StreamReader, error)
}
