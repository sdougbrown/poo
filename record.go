package poo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sdougbrown/poo/decide"
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
	// Origin says who produced the record: "model", "tool", "user",
	// "steer", "hook:<name>" and the rest of the closed set.
	//
	// It is empty on the records that no one produced: the session's own
	// header, a run's boundaries, the usage accounting.
	Origin Origin `json:"origin,omitempty"`
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
	// Background is the tool's background mode. A session loaded from
	// the record is incompatible with the current build when a tool's
	// mode changed.
	Background Background `json:"background,omitempty"`
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

// SystemPromptRecord records a rendered system prompt.
//
// Every rendered prompt that differs from the previous one is written
// whole, with its hash and the variant it rendered in. Replay uses the
// recorded text and never re-renders.
type SystemPromptRecord struct {
	RecordMeta
	// RunID identifies the run the prompt belongs to.
	RunID string `json:"run_id"`
	// Variant is the variant the prompt rendered in.
	Variant string `json:"variant,omitempty"`
	// Text is the full rendered prompt.
	Text string `json:"text"`
	// Hash is the hex SHA-256 of the text.
	Hash string `json:"hash"`
}

// MessageRecord records one message in the model's history.
type MessageRecord struct {
	RecordMeta
	// RunID identifies the run the message belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the message belongs to, 0 for the run's input.
	Turn int `json:"turn"`
	// Role is the message's role.
	Role Role `json:"role"`
	// Content is the message's content blocks.
	Content []ContentBlock `json:"content"`
	// MessageID is the message's session-monotonic id, e.g. "m7". It is
	// set on the messages the model produced.
	MessageID string `json:"message_id,omitempty"`
	// Partial reports that the message was cut short.
	Partial bool `json:"partial,omitempty"`
	// Cause says why the message was cut short: "interrupt" or
	// "provider_error". It is set only on a partial message.
	Cause Cause `json:"cause,omitempty"`
	// DeliveryNote says how much of the previous reply the user had
	// seen, when the message carries one.
	DeliveryNote *DeliveryNote `json:"delivery_note,omitempty"`
}

// CallMode says how a tool call ran.
type CallMode string

const (
	// CallModeSync means the call ran in the turn.
	CallModeSync CallMode = "sync"
	// CallModeJob means the call became a background job.
	CallModeJob CallMode = "job"
)

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
	// Mode says how the call ran: "sync" in the turn, or "job" in the
	// background.
	Mode CallMode `json:"mode,omitempty"`
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
	// Original is the result the tool produced, when a hook replaced
	// it. The model has not seen that result yet, so the log keeps it.
	//
	// It is absent for a redacted result: the original never reaches
	// the record.
	Original *ToolResultData `json:"original,omitempty"`
	// Redacted reports that a hook redacted the result.
	Redacted bool `json:"redacted,omitempty"`
	// RedactedBy names the hook that redacted the result.
	RedactedBy string `json:"redacted_by,omitempty"`
	// Hooks are the after-tool hooks' decisions, in the order the
	// hooks ran. A redacted chain's decisions carry no contents.
	Hooks []HookDecision `json:"hooks,omitempty"`
}

// PermissionAskedRecord records a permission ask.
//
// The ask names the call the run is waiting on, so a reader of the log
// can see what the run asked and why.
type PermissionAskedRecord struct {
	RecordMeta
	// RunID identifies the run that is waiting.
	RunID string `json:"run_id"`
	// AskID names the ask.
	AskID string `json:"ask_id"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Summary describes the call.
	Summary string `json:"summary,omitempty"`
	// Class is the tool's class.
	Class ToolClass `json:"class,omitempty"`
	// Reason is why the call is being asked about.
	Reason string `json:"reason,omitempty"`
	// RepeatCount holds the doom loop's counts, when the ask is one.
	RepeatCount *RepeatCounts `json:"repeat_count,omitempty"`
}

// PermissionAnsweredRecord records the answer that resolved an ask, or a
// doom loop's deterministic outcome.
type PermissionAnsweredRecord struct {
	RecordMeta
	// RunID identifies the run the answer belongs to.
	RunID string `json:"run_id"`
	// AskID names the ask the answer resolves. It is empty for a doom
	// loop's deterministic action, which answers no ask.
	AskID string `json:"ask_id,omitempty"`
	// Decision is "allow" or "deny".
	Decision string `json:"decision"`
	// AnsweredBy names who answered.
	AnsweredBy string `json:"answered_by"`
	// Provenance is how the ask was resolved.
	Provenance string `json:"provenance"`
	// Reason is why the call was being asked about.
	Reason string `json:"reason,omitempty"`
}

// ToolResultData is a tool result's own data.
//
// It is the payload of the original result a hook replaced.
type ToolResultData struct {
	// Text is the model-facing text of the result.
	Text string `json:"text"`
	// Structured is the optional side-channel payload.
	Structured json.RawMessage `json:"structured,omitempty"`
	// IsError marks a recoverable failure.
	IsError bool `json:"is_error,omitempty"`
}

// HookDecision is what an after-tool hook decided about a result.
type HookDecision struct {
	// Hook names the hook that decided.
	Hook string `json:"hook"`
	// Action is what the hook did: "keep", "annotate", "replace" or
	// "redact".
	Action string `json:"action"`
	// Text is the text the hook added or substituted. It is absent
	// when the chain was redacted: the record must not carry what a
	// redaction removed.
	Text string `json:"text,omitempty"`
}

// ObserverDropped records the events an observer missed.
//
// The record is written at most once a second per observer, and it is
// never delivered to an observer: overflow diagnostics must not cause
// more overflow.
type ObserverDropped struct {
	RecordMeta
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Observer names the observer that fell behind.
	Observer string `json:"observer"`
	// Count is how many events the observer missed.
	Count int `json:"count"`
	// DroppedAt is the RFC 3339 time of the report, in UTC.
	DroppedAt string `json:"dropped_at"`
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

// ProviderAttempt is the record of one attempt of a request that
// failed.
//
// The attempts are written for the audit: a reader of the log sees
// what a request tried and what every attempt failed with.
type ProviderAttempt struct {
	RecordMeta
	// RunID identifies the run the request belongs to.
	RunID string `json:"run_id"`
	// Turn is the turn the request belongs to.
	Turn int `json:"turn"`
	// Request is the 1-based number of the request within the run.
	Request int `json:"request"`
	// Attempt is the attempt's number, counting from one.
	Attempt int `json:"attempt"`
	// Error is the failure's message.
	Error string `json:"error"`
	// Kind is the provider's error kind, when the failure is one.
	Kind string `json:"kind,omitempty"`
	// DelayMS is how long the transport waited before the next attempt.
	DelayMS int `json:"delay_ms,omitempty"`
}

// ProviderDetail describes why a provider request failed.
type ProviderDetail struct {
	// Kind is the failure class, e.g. "context_length" or "server".
	Kind string `json:"kind"`
	// Status is the HTTP status, 0 when the failure was not a response.
	Status int `json:"status,omitempty"`
	// Code is the provider's own error code, when it sent one. It also
	// holds the kind a failure had before it was reclassified: an
	// interrupted stream keeps the kind it broke in here.
	Code string `json:"code,omitempty"`
	// RetryAfterSec is how long the endpoint asked the client to wait, in
	// seconds, when it said so.
	RetryAfterSec int `json:"retry_after_s,omitempty"`
	// ResetsAt is when the endpoint says its limit resets, in RFC 3339.
	// It is absent when the endpoint said nothing about it.
	ResetsAt string `json:"resets_at,omitempty"`
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
	// Handoff is the note a compactor wrote when it refused to
	// compact. It is set when the run settled as context_exhausted
	// because a Compactor ended it.
	Handoff string `json:"handoff,omitempty"`
	// UndeliveredInput lists the accepted inputs that never reached the
	// model, with the mode they were accepted in.
	UndeliveredInput []UndeliveredInput `json:"undelivered_input,omitempty"`
	// JobsRunning lists the jobs that were still running when the run
	// settled.
	JobsRunning []string `json:"jobs_running,omitempty"`
	// Result is the payload of a valid finish call, when the run
	// settled as finished.
	Result json.RawMessage `json:"result,omitempty"`
}

// UndeliveredInput is an accepted input that never reached the model.
//
// The input was accepted and recorded, so it is never lost: it is
// reported here with the reason the run settled.
type UndeliveredInput struct {
	// InputID is the input's identifier.
	InputID string `json:"input_id"`
	// Mode is how the input was accepted: "steer", "interrupt" or
	// "follow_up".
	Mode string `json:"mode"`
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

// NewRecordMetaFrom returns a header for a record of the given type and
// origin.
//
// The origin says who produced the record, and every record carries one:
// "model", "tool", "user", "steer", "hook:<name>" and the rest of the
// closed set. A record that no one produced — the session's own header, a
// run's boundaries — has no origin.
func NewRecordMetaFrom(name string, origin Origin) RecordMeta {
	return RecordMeta{Record: name, Origin: origin}
}

// Stamp fills in the sequence number and timestamp of a record.
func Stamp(rec Record, seq int64, at time.Time) {
	m := rec.Meta()
	m.Seq = seq
	m.TS = at.UTC().Format(time.RFC3339Nano)
}

// CompactionRecord records what a compactor did.
//
// The store keeps every original record, and the compaction record says
// what the model's view was replaced with. The record's own fields
// carry the plan's vocabulary, and the compaction itself is kept beside
// them, so a replay applies the compaction again without running the
// strategy that made it.
type CompactionRecord struct {
	RecordMeta
	// RunID identifies the run the compaction belongs to.
	RunID string `json:"run_id"`
	// Strategy names the strategy that produced the compaction.
	Strategy string `json:"strategy"`
	// Form is the compaction's form: replace, rewrite, window or end.
	Form string `json:"form"`
	// Cut is the range of turns a replacement covers. It is set for a
	// replacement only.
	Cut *TurnRange `json:"cut,omitempty"`
	// Replacement are the messages that replace the cut's turns. It is
	// set for a replacement only.
	Replacement []Message `json:"replacement,omitempty"`
	// TokensBefore is the estimated input before the compaction.
	TokensBefore int `json:"tokens_before"`
	// TokensAfter is the estimated input after the compaction.
	TokensAfter int `json:"tokens_after"`
	// Compaction is the compaction itself.
	Compaction Compaction `json:"compaction"`
}

// HandoffRecord records the note a compactor wrote when it refused to
// compact.
//
// The note is what a consumer carries on: it seeds the next session.
type HandoffRecord struct {
	RecordMeta
	// RunID identifies the run the handoff ends.
	RunID string `json:"run_id"`
	// Strategy names the strategy that wrote the handoff.
	Strategy string `json:"strategy"`
	// Handoff is the note the next agent picks up.
	Handoff string `json:"handoff"`
	// CreatedAt is the RFC 3339 time the handoff was written.
	CreatedAt string `json:"created_at"`
}

// ModelsChanged records that the session switched models.
//
// The switch is recorded between the runs, so a reader of the log knows
// which model answered which turn.
type ModelsChanged struct {
	RecordMeta
	// From is the name of the model the session talked to.
	From string `json:"from"`
	// To is the name of the model the session talks to now.
	To string `json:"to"`
	// Warnings are the things the caller should know about the switch.
	Warnings []string `json:"warnings,omitempty"`
	// Omitted are the provider blocks that are left out of the requests
	// from now on, because they belong to another API.
	Omitted []OmittedBlock `json:"omitted,omitempty"`
}

// OmittedBlock is one kind of provider block left out of the requests.
//
// The block stays in the history, and the requests leave it out: the
// model that made it is not the model the session talks to now.
type OmittedBlock struct {
	// API is the API the block belonged to.
	API API `json:"api"`
	// Type is the provider's own name for the payload.
	Type string `json:"type"`
	// Count is how many blocks of this kind are left out.
	Count int `json:"count"`
}

// DecisionRecord records one decision call.
//
// One record is written per logical call, whatever the number of wire
// requests it took. A call that did not answer is recorded too, with
// the outcome that says why.
type DecisionRecord struct {
	RecordMeta
	// Origin says who asked the questions.
	Origin string `json:"origin"`
	// Questions are the questions that were asked.
	Questions []decide.Question `json:"questions"`
	// StateHash is the hex SHA-256 of the state payload.
	StateHash string `json:"state_hash"`
	// Answers are the answers, when the call succeeded.
	Answers []decide.Answer `json:"answers,omitempty"`
	// Outcome is "ok", "deferred" or "invalid".
	Outcome string `json:"outcome"`
	// Error is the failure, when the call did not succeed.
	Error string `json:"error,omitempty"`
	// LatencyMS is how long the call took, in milliseconds.
	LatencyMS float64 `json:"latency_ms"`
	// Requests is the number of wire requests the call made. It is
	// absent when the decider did not report one.
	Requests *int `json:"requests,omitempty"`
	// At is the RFC 3339 time of the call, in UTC.
	At string `json:"at"`
}
