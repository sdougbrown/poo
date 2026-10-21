package poo

import (
	"context"
	"encoding/json"
	"time"
)

// FormatVersion is the version tag written into the first record of a
// session log. It names the record vocabulary, not the Go API.
const FormatVersion = "poo-session-v0"

// RecordMeta is the header every session record carries.
//
// The sink fills Seq and TS when the record is appended; Record is set by
// the record's constructor.
type RecordMeta struct {
	// Record is the dotted record name, e.g. "tool.call".
	Record string `json:"record"`
	// Seq is the session-monotonic sequence number, starting at 1.
	Seq int64 `json:"seq"`
	// TS is the RFC 3339 timestamp with nanosecond precision, in UTC.
	TS string `json:"ts"`
}

// Meta returns the record's header, so a sink can stamp it.
func (m *RecordMeta) Meta() *RecordMeta { return m }

// Record is one entry in a session log.
type Record interface {
	// Meta returns the record's header.
	Meta() *RecordMeta
}

// RecordSink receives the records a run produces, in order.
//
// Append may be called concurrently: the loop, a job's completion and the
// finalization's cleanup path all write, and an observer may write too. An
// implementation serializes internally and never interleaves two records'
// bytes, so a reader of the log never sees half a record.
//
// Core never depends on the order of records written concurrently, except
// that a run's settle record follows that run's own outcome records.
type RecordSink interface {
	// Append writes one record. It returns an error when the record could
	// not be stored.
	Append(ctx context.Context, rec Record) error
	// Flush pushes buffered records out.
	Flush(ctx context.Context) error
}

// SessionModel describes the model a session talks to.
type SessionModel struct {
	// ID is the provider's model identifier.
	ID string `json:"id"`
	// BaseURL is the endpoint the session talks to, without credentials.
	BaseURL string `json:"base_url,omitempty"`
	// MaxOutput is the model's output-token ceiling, 0 when unknown.
	MaxOutput int `json:"max_output,omitempty"`
}

// SessionTool describes one tool linked into a session.
type SessionTool struct {
	// Name is the tool's identifier.
	Name string `json:"name"`
	// Class is the tool's side-effect class.
	Class ToolClass `json:"class"`
}

// BuildInfo names the binary that produced a session.
type BuildInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SessionCreated is the first record of a session log.
type SessionCreated struct {
	RecordMeta
	// Format is the record vocabulary version.
	Format string `json:"format"`
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Mode is the persistence mode: "logged" or "ephemeral".
	Mode string `json:"mode"`
	// CreatedAt is the RFC 3339 creation time in UTC.
	CreatedAt string `json:"created_at"`
	// WorkingDir is the absolute directory the run works in.
	WorkingDir string `json:"working_dir"`
	// Model is the model the session talks to.
	Model SessionModel `json:"model"`
	// Tools are the tools linked into the session, in call order.
	Tools []SessionTool `json:"tools"`
	// ContextLimit is the model's context window in tokens.
	ContextLimit int `json:"context_limit"`
	// ToolsMode is how the session asks for tool calls: "native", "text"
	// or "auto".
	ToolsMode string `json:"tools_mode"`
	// ReasoningEffort is the reasoning_effort the session sends, empty
	// when it sends none.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Build names the binary that produced the session.
	Build BuildInfo `json:"build"`
}

// RunStarted records the beginning of a run.
type RunStarted struct {
	RecordMeta
	// RunID identifies the run.
	RunID string `json:"run_id"`
	// PromptHash is the hex SHA-256 of the prompt text.
	PromptHash string `json:"prompt_hash"`
	// StartedAt is the RFC 3339 start time in UTC.
	StartedAt string `json:"started_at"`
	// ModelID is the model the run talks to.
	ModelID string `json:"model_id"`
	// MaxTurns is the turn ceiling, 0 when the run is unlimited.
	MaxTurns int `json:"max_turns"`
	// ToolsMode is how the run asks for tool calls.
	ToolsMode string `json:"tools_mode"`
	// ReasoningEffort is the reasoning_effort the run sends, empty when
	// it sends none.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Trigger says what started the run: "prompt" or "job".
	Trigger TriggerKind `json:"trigger,omitempty"`
	// Jobs are the job results that started a job run.
	Jobs []JobResult `json:"jobs,omitempty"`
}

// MessageRecord records one message in the model's history.
type MessageRecord struct {
	RecordMeta
	// RunID identifies the run the message belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the message belongs to, 0 for the run's input.
	Turn int `json:"turn"`
	// Origin says who produced the message: "user", "model", "tool",
	// "system", "job" or "hook:<name>".
	Origin Origin `json:"origin"`
	// Role is the message's role.
	Role Role `json:"role"`
	// Content is the message's content blocks.
	Content []ContentBlock `json:"content"`
	// MessageID is the message's session-monotonic id, e.g. "m7". It is
	// set on the messages the model produced.
	MessageID string `json:"message_id,omitempty"`
	// DeliveryNote says how much of the previous reply the user had
	// seen, when the message carries one.
	DeliveryNote *DeliveryNote `json:"delivery_note,omitempty"`
}

// ToolCallRecord records a tool call the model made, with its full arguments.
type ToolCallRecord struct {
	RecordMeta
	// RunID identifies the run the call belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the call belongs to.
	Turn int `json:"turn"`
	// Handle is the call's identifier within the turn, e.g. "r1".
	Handle string `json:"handle"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Arguments are the raw arguments the model sent. They are a JSON
	// object when the model sent valid JSON, and a JSON string holding the
	// raw text when it did not.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultRecord records the outcome of a tool call.
type ToolResultRecord struct {
	RecordMeta
	// RunID identifies the run the result belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the result belongs to.
	Turn int `json:"turn"`
	// Handle is the call's identifier within the turn.
	Handle string `json:"handle"`
	// Tool is the name of the tool that ran.
	Tool string `json:"tool"`
	// Outcome is the call's outcome.
	Outcome Outcome `json:"outcome"`
	// Text is the model-facing text of the result.
	Text string `json:"text"`
	// Structured is the optional side-channel payload.
	Structured json.RawMessage `json:"structured,omitempty"`
}

// Usage records the token accounting of one provider request.
type Usage struct {
	RecordMeta
	// RunID identifies the run the request belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the request belongs to.
	Turn int `json:"turn"`
	// Request is the 1-based number of the request within the run.
	Request int `json:"request"`
	// InputTokens is the number of prompt tokens the endpoint reported.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the number of completion tokens the endpoint
	// reported.
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens is the number of prompt tokens served from the
	// provider's cache. It is absent when the endpoint does not report it.
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`
	// TTFTMS is the time to the first streamed token, in milliseconds.
	TTFTMS float64 `json:"ttft_ms"`
}

// ProviderDetail describes why a provider request failed.
type ProviderDetail struct {
	// Kind is the failure class, e.g. "context_length" or "server".
	Kind string `json:"kind"`
	// Status is the HTTP status, 0 when the failure was not a response.
	Status int `json:"status,omitempty"`
	// Code is the provider's own error code, when it sent one.
	Code string `json:"code,omitempty"`
	// RetryAfterSec is how long the endpoint asked the client to wait, in
	// seconds, when it said so.
	RetryAfterSec int `json:"retry_after_s,omitempty"`
}

// SettleDetail explains why a run settled.
type SettleDetail struct {
	// Source names the condition that settled the run.
	Source string `json:"source"`
	// Text is the human-readable explanation.
	Text string `json:"text,omitempty"`
	// Losing lists the candidates that lost the precedence order.
	Losing []string `json:"losing,omitempty"`
	// Provider is set when a provider request failed.
	Provider *ProviderDetail `json:"provider,omitempty"`
}

// RunSettled records the end of a run.
type RunSettled struct {
	RecordMeta
	// RunID identifies the run.
	RunID string `json:"run_id"`
	// Reason is the settle reason.
	Reason Reason `json:"reason"`
	// Detail explains the outcome.
	Detail SettleDetail `json:"detail"`
	// Turns is the number of provider requests the run made.
	Turns int `json:"turns"`
	// SettledAt is the RFC 3339 settle time in UTC.
	SettledAt string `json:"settled_at"`
}

// NewRecordMeta returns a header for a record of the given type.
func NewRecordMeta(name string) RecordMeta {
	return RecordMeta{Record: name}
}

// Stamp fills in the sequence number and timestamp of a record.
func Stamp(rec Record, seq int64, at time.Time) {
	m := rec.Meta()
	m.Seq = seq
	m.TS = at.UTC().Format(time.RFC3339Nano)
}
