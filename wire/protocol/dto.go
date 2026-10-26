package protocol

import "encoding/json"

// The data the client and the server exchange.
//
// Every shape here is part of the contract: a field that is added is
// announced by a capability, and a field that goes away bumps the
// protocol version.
//
// A protocol.X is never a poo.X: the wire copy uses strings where the
// core copy uses typed enums.

// Client is the client the handshake names.
type Client struct {
	// Name is the client's name.
	Name string `json:"name"`
	// Version is the client's version.
	Version string `json:"version"`
	// Capabilities are the things the client can do.
	Capabilities ClientCapabilities `json:"capabilities,omitempty"`
}

// ClientCapabilities are the things a client says it can do.
//
// A client that does not declare permission_answers is never asked
// about a permission: it may not answer, and asking it would wait for
// a reply that never comes.
type ClientCapabilities struct {
	// PermissionAnswers says the client answers permission asks.
	PermissionAnswers bool `json:"permission_answers,omitempty"`
}

// Build names the binary the client is talking to.
type Build struct {
	// Name is the binary's name.
	Name string `json:"name"`
	// Version is the binary's version.
	Version string `json:"version"`
	// PooVersion is the version of poo the binary was built from.
	PooVersion string `json:"poo_version,omitempty"`
}

// Capabilities are the things this binary can do.
//
// The client reads them to know what it may ask for. A key the client
// does not know is ignored: a later binary may add one.
type Capabilities struct {
	// Decider says the binary has a decision model.
	Decider bool `json:"decider"`
	// Compaction says the binary can compact a session's view.
	Compaction bool `json:"compaction"`
	// Persistence lists the persistence modes this process can serve.
	Persistence []string `json:"persistence"`
	// Jobs says the binary can run a tool call in the background.
	Jobs bool `json:"jobs"`
}

// ToolInfo describes one tool the session offers.
//
// The class says what the tool does to the world, and the background
// mode says whether the model may run it in the background.
type ToolInfo struct {
	// Name is the tool's name.
	Name string `json:"name"`
	// Class is "read_only" or "side_effecting".
	Class string `json:"class"`
	// Background is "never", "allowed" or "always".
	Background string `json:"background"`
}

// ProfileInfo describes one profile the binary offers.
//
// A profile is a named tool set with its own defaults.
type ProfileInfo struct {
	// Name is the profile's name.
	Name string `json:"name"`
	// Tools are the tools the profile links, in offer order.
	Tools []ToolInfo `json:"tools"`
	// Unmatched says what the profile does with a side-effecting call
	// no rule covers: "allow", "ask" or "deny".
	Unmatched string `json:"unmatched,omitempty"`
}

// ModelRoles are the models a session talks to.
//
// The names are entries in the settings' models map. A role that is not
// set is a role the session does not have.
type ModelRoles struct {
	// Chat is the name of the chat model.
	Chat string `json:"chat,omitempty"`
	// Decider is the name of the decision model.
	Decider string `json:"decider,omitempty"`
}

// ModelInfo describes one model in the catalog.
type ModelInfo struct {
	// Name is the name the settings and the wire use for the model.
	Name string `json:"name"`
	// ModelID is what the endpoint calls the model.
	ModelID string `json:"model_id"`
	// API is the wire API the model is reached through.
	API string `json:"api,omitempty"`
	// Ops are the operations the model serves.
	Ops []string `json:"ops,omitempty"`
	// Inputs are the modalities the model reads.
	Inputs []string `json:"inputs,omitempty"`
	// ContextLimit is the model's context window in tokens.
	ContextLimit int `json:"context_limit,omitempty"`
	// MaxOutput is the most tokens the model writes in one response.
	MaxOutput int `json:"max_output,omitempty"`
	// ToolsMode is how the model makes tool calls.
	ToolsMode string `json:"tools_mode,omitempty"`
}

// PromptOverrides are the system prompt changes a client asks for.
//
// The shape is the settings' system_prompt block. A client may not
// change the prompt of a session that is running: the change takes
// effect at the next run.
type PromptOverrides struct {
	// BaseFile is the path of a file holding the base prompt. The
	// binary reads it, so a client names a file the binary may read.
	BaseFile string `json:"base_file,omitempty"`
	// Replace is the whole prompt.
	Replace string `json:"replace,omitempty"`
	// Append is text added to the prompt.
	Append string `json:"append,omitempty"`
	// Sections are the sections the client sets, by name.
	Sections map[string]string `json:"sections,omitempty"`
	// Remove are the sections the client drops, by name.
	Remove []string `json:"remove,omitempty"`
}

// InitializeParams are the handshake's params.
type InitializeParams struct {
	// ProtocolVersions are the versions the client speaks, highest
	// first or in any order.
	ProtocolVersions []int `json:"protocol_versions"`
	// Client names the client and says what it can do.
	Client Client `json:"client"`
}

// InitializeResult is the handshake's result.
//
// It tells the client what this binary can do, which matters because
// custom binaries differ.
type InitializeResult struct {
	// ProtocolVersion is the version the client and the server agreed
	// on.
	ProtocolVersion int `json:"protocol_version"`
	// Build names the binary.
	Build Build `json:"build"`
	// Profiles are the profiles the binary offers.
	Profiles []ProfileInfo `json:"profiles"`
	// Models are the models the binary talks to.
	Models ModelRoles `json:"models"`
	// Capabilities are the things the binary can do.
	Capabilities Capabilities `json:"capabilities"`
}

// CreateParams are the params of session/create.
type CreateParams struct {
	// WorkingDir is the directory the session's tools work in.
	WorkingDir string `json:"working_dir"`
	// Profile is the profile to build the session from. It defaults to
	// the binary's default profile.
	Profile string `json:"profile,omitempty"`
	// Mode is the persistence mode: "logged" or "ephemeral". It
	// defaults to the process default.
	Mode string `json:"mode,omitempty"`
	// Models overrides the settings' roles.
	Models *ModelRoles `json:"models,omitempty"`
	// PromptOverrides changes the session's system prompt.
	PromptOverrides *PromptOverrides `json:"prompt_overrides,omitempty"`
	// SessionDir is where the session's records live, for a logged
	// session.
	SessionDir string `json:"session_dir,omitempty"`
}

// LoadParams are the params of session/load.
type LoadParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// SessionDir is where the session's records live. It defaults to
	// the settings' session directory.
	SessionDir string `json:"session_dir,omitempty"`
}

// SessionInfo is what a client learns about a session it opened.
//
// The create and load results share it.
type SessionInfo struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Mode is the session's persistence mode.
	Mode string `json:"mode"`
	// SessionDir is the session's directory. It is present only for a
	// logged session, and a client that resumes later keeps it.
	SessionDir string `json:"session_dir,omitempty"`
	// Tools are the tools the session offers.
	Tools []ToolInfo `json:"tools"`
	// Models are the models the session talks to.
	Models ModelRoles `json:"models"`
	// RecoveredTail says the load truncated a torn tail.
	RecoveredTail bool `json:"recovered_tail,omitempty"`
	// InterruptedRuns are the runs the load marked interrupted.
	InterruptedRuns []string `json:"interrupted_runs,omitempty"`
	// Warnings are the things the client should know about the
	// session.
	Warnings []string `json:"warnings,omitempty"`
}

// GetParams are the params of session/get.
type GetParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
}

// JobInfo describes one of a session's jobs.
type JobInfo struct {
	// JobID is the job's identifier.
	JobID string `json:"job"`
	// Tool is the name of the tool the job runs.
	Tool string `json:"tool"`
	// Handle is the handle of the tool call the job came from.
	Handle string `json:"handle,omitempty"`
	// RunID identifies the run that started the job.
	RunID string `json:"run_id,omitempty"`
	// StartedAt is when the job started.
	StartedAt string `json:"started_at,omitempty"`
	// Status is "running" or "held".
	Status string `json:"status"`
}

// PromptStateInfo is the session's system prompt.
type PromptStateInfo struct {
	// Text is the rendered prompt.
	Text string `json:"text"`
	// Hash is the hash of the text.
	Hash string `json:"hash"`
	// Sections are the names of the prompt's sections, in order.
	Sections []string `json:"sections"`
	// Preview says the text is a preview render: no run has rendered
	// the prompt yet.
	Preview bool `json:"preview"`
}

// GetResult is the result of session/get.
type GetResult struct {
	// State is where the session is in the loop.
	State string `json:"state"`
	// RunID identifies the active run, when there is one.
	RunID string `json:"run_id,omitempty"`
	// Jobs are the session's jobs.
	Jobs []JobInfo `json:"jobs"`
	// Prompt is the session's system prompt.
	Prompt PromptStateInfo `json:"prompt"`
	// Broken says the session's store failed and the session cannot be
	// used any more.
	Broken bool `json:"broken"`
}

// ConfigureParams are the params of session/configure.
type ConfigureParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// PromptOverrides changes the session's system prompt.
	PromptOverrides *PromptOverrides `json:"prompt_overrides,omitempty"`
	// Models changes the models the session talks to.
	Models *ModelRoles `json:"models,omitempty"`
}

// ConfigureResult is the result of session/configure.
type ConfigureResult struct {
	// Models are the models the session talks to now.
	Models ModelRoles `json:"models"`
}

// ModelsParams are the params of models/list.
type ModelsParams struct {
	// Refresh says the catalog re-probes the endpoints.
	Refresh bool `json:"refresh,omitempty"`
}

// ModelsResult is the result of models/list.
type ModelsResult struct {
	// Models are the effective catalog.
	Models []ModelInfo `json:"models"`
}

// CloseParams are the params of session/close.
type CloseParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
}

// HistoryParams are the params of session/history.
type HistoryParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// BeforeSeq ends the page before this sequence number. Zero takes
	// the newest page.
	BeforeSeq int64 `json:"before_seq,omitempty"`
	// Limit is how many items the page holds. It is at most 500, and
	// defaults to 100.
	Limit int `json:"limit,omitempty"`
}

// HistoryItem is one entry in a session's history.
//
// The item is a projection: it says what happened and where, and it
// holds no more than a client shows.
type HistoryItem struct {
	// Seq is the record's sequence number. The next page asks for the
	// items before it.
	Seq int64 `json:"seq"`
	// Type names the item: "message", "tool_call", "tool_result",
	// "permission", "compaction", "run_started", "run_settled",
	// "job_started" or "job_ended".
	Type string `json:"type"`
	// RunID identifies the run the item belongs to.
	RunID string `json:"run_id,omitempty"`
	// Turn is the turn the item belongs to.
	Turn int `json:"turn,omitempty"`
	// Role is the message's role, for a message item.
	Role string `json:"role,omitempty"`
	// Text is the item's text.
	Text string `json:"text,omitempty"`
	// Handle is the tool call's handle, for a tool item.
	Handle string `json:"handle,omitempty"`
	// Tool is the tool the item is about.
	Tool string `json:"tool,omitempty"`
	// Outcome is the tool result's outcome.
	Outcome string `json:"outcome,omitempty"`
	// Decision is the permission answer's decision.
	Decision string `json:"decision,omitempty"`
	// Reason is the run's settle reason, for a settled run.
	Reason string `json:"reason,omitempty"`
	// At is the record's timestamp.
	At string `json:"at,omitempty"`
}

// HistoryResult is the result of session/history.
//
// The page comes back oldest first, and the paging goes backwards:
// has_more says that older items exist before the first returned one.
type HistoryResult struct {
	// Items are the entries, oldest first.
	Items []HistoryItem `json:"items"`
	// HasMore says that older entries exist.
	HasMore bool `json:"has_more"`
}

// Delivered says how much of a reply the user had seen.
//
// The reference names an assistant message in the session, and the count
// says how many of its characters the user had seen.
type Delivered struct {
	// MessageID names the assistant message.
	MessageID string `json:"message_id"`
	// UpToChars is how many code points of the message the user had
	// seen. It is never negative.
	UpToChars int `json:"upto_chars"`
}

// PromptParams are the params of agent/prompt.
type PromptParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Text is the prompt.
	Text string `json:"text"`
	// Delivered says how much of the last reply the user had seen.
	Delivered *Delivered `json:"delivered,omitempty"`
}

// PromptResult is the result of agent/prompt.
type PromptResult struct {
	// RunID identifies the run the prompt started.
	RunID string `json:"run_id"`
}

// MessageParams are the params of agent/message.
type MessageParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Text is the message.
	Text string `json:"text"`
	// Mode says when the message reaches the model: "steer",
	// "interrupt" or "follow_up". It defaults to "steer".
	Mode string `json:"mode,omitempty"`
	// Delivered says how much of the last reply the user had seen.
	Delivered *Delivered `json:"delivered,omitempty"`
}

// MessageResult is the result of agent/message.
type MessageResult struct {
	// InputID identifies the accepted input.
	InputID string `json:"input_id"`
}

// InspectWait asks the server to wait for something.
type InspectWait struct {
	// Until is what to wait for. It is "settled".
	Until string `json:"until"`
	// RunID identifies the run to wait for.
	RunID string `json:"run_id"`
	// TimeoutMS is how long to wait, in milliseconds.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// InspectParams are the params of agent/inspect.
type InspectParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// AfterSeq pages the events: only the events after this sequence
	// number are returned.
	AfterSeq int64 `json:"after_seq,omitempty"`
	// Wait asks the server to wait for the run to settle.
	Wait *InspectWait `json:"wait,omitempty"`
}

// SettleDetail explains how a run settled.
//
// It is the detail object of the settle reasons: the source says what
// settled the run, and the rest says what happened.
type SettleDetail struct {
	// Source names the condition that settled the run.
	Source string `json:"source"`
	// Text is the explanation.
	Text string `json:"text,omitempty"`
	// Losing lists the candidates that lost the precedence order.
	Losing []string `json:"losing,omitempty"`
	// Provider is set when a provider request failed.
	Provider *ProviderDetail `json:"provider,omitempty"`
	// Handoff is the note a compactor wrote.
	Handoff string `json:"handoff,omitempty"`
	// UndeliveredInput lists the accepted inputs that never reached the
	// model.
	UndeliveredInput []UndeliveredInput `json:"undelivered_input,omitempty"`
	// JobsRunning lists the jobs that were still running at the settle.
	JobsRunning []string `json:"jobs_running,omitempty"`
	// Result is the payload of a valid finish call.
	Result json.RawMessage `json:"result,omitempty"`
}

// UndeliveredInput is an accepted input that never reached the model.
type UndeliveredInput struct {
	// InputID identifies the input.
	InputID string `json:"input_id"`
	// Mode is how the input was accepted.
	Mode string `json:"mode"`
}

// ProviderDetail describes a provider failure.
type ProviderDetail struct {
	// Kind names the failure: "rate_limited", "usage_limit",
	// "plan_inactive", "auth_failed", "server", "network",
	// "invalid_request" or "interrupted_stream".
	Kind string `json:"kind"`
	// Status is the endpoint's HTTP status, when it sent one.
	Status int `json:"status,omitempty"`
	// Code is the endpoint's own code, when it sent one.
	Code string `json:"code,omitempty"`
	// RetryAfterS is how long the endpoint asked the client to wait.
	RetryAfterS int `json:"retry_after_s,omitempty"`
	// ResetsAt is when the limit resets, in RFC 3339.
	ResetsAt string `json:"resets_at,omitempty"`
}

// Settled is how a run settled.
type Settled struct {
	// RunID identifies the run.
	RunID string `json:"run_id"`
	// Reason is the settle reason.
	Reason string `json:"reason"`
	// Detail explains the outcome.
	Detail SettleDetail `json:"detail"`
}

// InspectResult is the result of agent/inspect.
type InspectResult struct {
	// Events are the session's events, oldest first.
	Events []Event `json:"events"`
	// NextSeq is the sequence number the next page asks for.
	NextSeq int64 `json:"next_seq"`
	// State is where the session is in the loop.
	State string `json:"state"`
	// Settled is the outcome of the run the client waited for.
	Settled *Settled `json:"settled,omitempty"`
	// TimedOut says the wait ran out. It is not a failure: the client
	// asks again.
	TimedOut bool `json:"timed_out"`
}

// CancelParams are the params of agent/cancel.
type CancelParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run to cancel.
	RunID string `json:"run_id"`
}

// JobCancelParams are the params of job/cancel.
type JobCancelParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// JobID is the job's identifier.
	JobID string `json:"job"`
}

// ConfigParams are the params of config/get.
type ConfigParams struct{}

// Event is one session event.
//
// The event is a notification's params: the session it belongs to, its
// sequence number, its type and whatever the event carries.
type Event struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// Seq is the session-monotonic sequence number.
	Seq int64 `json:"seq"`
	// Type names the event, e.g. "run.started".
	Type string `json:"type"`
	// At is the event's timestamp.
	At string `json:"at,omitempty"`
	// Payload is the event's own data.
	Payload map[string]any `json:"-"`
}

// MarshalJSON renders the event as the notification's params object.
//
// The event's own data is spread into the object, so a client reads one
// flat shape: the session, the sequence number, the type and whatever
// the event carries.
func (e Event) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(e.Payload)+4)
	for k, v := range e.Payload {
		out[k] = v
	}
	out["session_id"] = e.SessionID
	out["seq"] = e.Seq
	out["type"] = e.Type
	if e.At != "" {
		out["at"] = e.At
	}
	return json.Marshal(out)
}

// AskParams are the params of a permission ask.
//
// The ask is the server's request to the client: the client answers it
// with a decision.
type AskParams struct {
	// SessionID identifies the session.
	SessionID string `json:"session_id"`
	// RunID identifies the run that is waiting.
	RunID string `json:"run_id"`
	// AskID names the ask. The request's id is the ask's id, so the
	// client answers it without keeping a mapping of its own.
	AskID string `json:"ask_id"`
	// Tool is the name of the tool the model called.
	Tool string `json:"tool"`
	// Summary describes the call.
	Summary string `json:"summary"`
	// Class is the tool's class: "read_only" or "side_effecting".
	Class string `json:"class"`
	// Reason is why the call is being asked about: "policy" or
	// "doom_loop".
	Reason string `json:"reason"`
	// RepeatCount holds the doom loop's counts, for a doom-loop ask.
	RepeatCount *RepeatCount `json:"repeat_count,omitempty"`
}

// RepeatCount is the doom loop's count of a repeated call.
type RepeatCount struct {
	// Exact is how many times the model made this exact call.
	Exact int `json:"exact"`
	// Near is how many times it made a call with the same primary
	// argument.
	Near int `json:"near"`
}

// AskResult is the client's answer to a permission ask.
type AskResult struct {
	// Decision is "allow" or "deny".
	Decision string `json:"decision"`
}

// The decisions a client may answer an ask with.
const (
	// DecisionAllow lets the call run.
	DecisionAllow = "allow"
	// DecisionDeny refuses the call.
	DecisionDeny = "deny"
)
