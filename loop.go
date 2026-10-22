package poo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// State is the skeleton's agent-loop state.
type State string

const (
	// StateRequesting means the run is asking the provider for a response.
	StateRequesting State = "requesting"
	// StateExecuting means the run is running the tool calls the model
	// asked for.
	StateExecuting State = "executing"
	// StateSettled means the run has settled.
	StateSettled State = "settled"
)

// Options configure a run.
type Options struct {
	// Transport sends the provider requests.
	Transport ChatTransport
	// Tools are the tools the model may call, keyed by name.
	Tools map[string]Tool
	// ToolOrder is the order the tools are offered to the model in.
	ToolOrder []string
	// Sink receives the session records.
	Sink RecordSink
	// SessionID identifies the session.
	SessionID string
	// RunID identifies the run.
	RunID string
	// ModelID is the provider's model identifier.
	ModelID string
	// ContextLimit is the model's context window in tokens. It is
	// required: 0 leaves no room for an answer, so the run settles
	// context_exhausted (source no_output_room) before it sends a
	// request.
	ContextLimit int
	// MaxOutput is the model's output-token ceiling, 0 when unknown.
	MaxOutput int
	// MaxTurns is the turn ceiling. 0 means unlimited.
	MaxTurns int
	// SystemPrompt is the system message prepended to the model's view.
	SystemPrompt string
	// WorkingDir is the directory the tools work in.
	WorkingDir string
	// Env is the scrubbed child environment for the tools.
	Env []string
	// Settings holds the tools' tunable knobs.
	Settings ToolSettings
	// ToolsMode is how the run asks for tool calls.
	ToolsMode string
	// ReasoningEffort is the reasoning_effort the run sends, empty when
	// it sends none.
	ReasoningEffort string
	// MessageIDs hands out the session-monotonic message ids. A session
	// owns one counter, and a run continues it, so a second run's ids
	// do not repeat the first run's.
	//
	// Nil means the run starts its own counter at one.
	MessageIDs *MessageIDs
}

// Run is one prompt through to settled.
type Run struct {
	opts Options
	// history is the model's view: what has been said so far.
	history []Message
	// turn counts the provider requests made so far.
	turn int
	// requests counts the provider requests made so far.
	requests int
	// seq numbers the records written to the sink.
	seq int64
	// sunk reports whether the sink failed.
	sunk bool
	// state is where the run is in the loop.
	state State
	// messageIDs hands out the session-monotonic message ids. The
	// counter belongs to the session, so a run's ids continue where the
	// session's last ones stopped.
	messageIDs *MessageIDs
}

// State returns where the run is in the loop.
//
// The states are the skeleton's: requesting, executing and settled. There
// is no streaming state, because the skeleton's transport reads a whole
// response before the run acts on it.
func (r *Run) State() State { return r.state }

// Result is the outcome of a run.
type Result struct {
	// Reason is the settle reason.
	Reason Reason
	// Detail explains the outcome.
	Detail SettleDetail
	// Turns is the number of provider requests the run made.
	Turns int
}

// NewRun builds a run. The caller drives it with Run.Execute.
func NewRun(opts Options) *Run {
	ids := opts.MessageIDs
	if ids == nil {
		ids = &MessageIDs{}
	}
	return &Run{opts: opts, state: StateRequesting, messageIDs: ids}
}

// Execute runs the loop until it settles.
//
// The loop is the skeleton's small version of the plan's FSM: requesting,
// executing, settled. It has no hooks, no finalize protocol and no
// compaction; those arrive in later stages.
func (r *Run) Execute(ctx context.Context, prompt string) (Result, error) {
	promptHash := hashText(prompt)

	if err := r.write(ctx, &RunStarted{
		RecordMeta: NewRecordMeta("run.started"),
		RunID:      r.opts.RunID,
		PromptHash: promptHash,
		StartedAt:  nowRFC3339(),
		ModelID:    r.opts.ModelID,
		MaxTurns:   r.opts.MaxTurns,
		ToolsMode:  r.opts.ToolsMode,

		ReasoningEffort: r.opts.ReasoningEffort,
		Trigger:         TriggerPrompt,
	}); err != nil {
		return Result{}, err
	}

	// The system prompt is the first thing the model sees, and it is
	// frozen for the length of the run.
	if r.opts.SystemPrompt != "" {
		msg := NewTextMessage(RoleSystem, r.opts.SystemPrompt)
		msg.Origin = OriginSystem
		r.history = append(r.history, msg)
		if err := r.write(ctx, &MessageRecord{
			RecordMeta: NewRecordMetaFrom("message", OriginSystem),
			RunID:      r.opts.RunID,
			Role:       msg.Role,
			Content:    msg.Content,
		}); err != nil {
			return Result{}, err
		}
	}

	user := NewTextMessage(RoleUser, prompt)
	user.Origin = OriginUser
	r.history = append(r.history, user)
	if err := r.write(ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", OriginUser),
		RunID:      r.opts.RunID,
		Turn:       0,
		Role:       user.Role,
		Content:    user.Content,
	}); err != nil {
		return Result{}, err
	}

	reason, detail, err := r.loop(ctx)
	if err != nil {
		return Result{}, err
	}
	r.state = StateSettled

	// The settle record is written with a context of its own: the run's
	// context may already have ended, and the log still has to say the
	// run ended.
	settle := &RunSettled{
		RecordMeta: NewRecordMeta("run.settled"),
		RunID:      r.opts.RunID,
		Reason:     reason,
		Detail:     detail,
		Turns:      r.turn,
		SettledAt:  nowRFC3339(),
	}
	if err := r.write(ctx, settle); err != nil {
		return Result{}, err
	}
	if r.opts.Sink != nil {
		fctx, cancel := recordContext(ctx)
		defer cancel()
		if err := r.opts.Sink.Flush(fctx); err != nil {
			return Result{}, err
		}
	}

	return Result{Reason: reason, Detail: detail, Turns: r.turn}, nil
}

// loop drives the requesting/executing cycle.
func (r *Run) loop(ctx context.Context) (Reason, SettleDetail, error) {
	for {
		if err := ctx.Err(); err != nil {
			return ReasonCancelled, cancelDetail(err), nil
		}

		// The next-request gate. The skeleton has no compactor, no budget
		// ratio and no 80% notice, so the only check is the output room.
		maxTokens, room := r.maxTokens()
		if !room {
			return ReasonContextExhausted, SettleDetail{
				Source: "no_output_room",
				Text:   fmt.Sprintf("computed max_tokens %d is under the 256-token minimum", maxTokens),
			}, nil
		}

		if r.opts.MaxTurns > 0 && r.turn >= r.opts.MaxTurns {
			return ReasonMaxTurns, SettleDetail{
				Source: "max_turns",
				Text:   fmt.Sprintf("the run reached its %d-turn ceiling", r.opts.MaxTurns),
			}, nil
		}

		r.turn++
		r.requests++
		r.state = StateRequesting

		resp, err := r.request(ctx, maxTokens)
		if err != nil {
			reason, detail, serr := r.settleForError(err)
			if serr != nil {
				return "", SettleDetail{}, serr
			}
			// The request failed, but the usage it produced is still
			// written: every request gets a usage record, so a consumer
			// can see the request's accounting even when the stream
			// broke. The write is best-effort, because the record that
			// says the run ended matters more than the accounting.
			if resp != nil && resp.usage != nil {
				r.writeBestEffort(ctx, &Usage{
					RecordMeta:      NewRecordMeta("usage"),
					RunID:           r.opts.RunID,
					Turn:            r.turn,
					Request:         r.requests,
					InputTokens:     resp.usage.InputTokens,
					OutputTokens:    resp.usage.OutputTokens,
					CacheReadTokens: resp.usage.CacheReadTokens,
					TTFTMS:          resp.usage.TTFTMS,
				})
			}
			return reason, detail, nil
		}

		// The model's response is part of the history whether or not it
		// asked for tool calls.
		assistant := Message{Role: RoleAssistant, Content: resp.content, Origin: OriginModel}
		assistant.ID = r.messageIDs.Next()
		for _, call := range resp.calls {
			assistant.Content = append(assistant.Content, ContentBlock{
				Kind:       ContentToolCall,
				ToolCallID: call.ID,
				Name:       call.Name,
				Arguments:  recordArgs(call.Arguments),
			})
		}
		if len(assistant.Content) > 0 {
			r.history = append(r.history, assistant)
			if err := r.write(ctx, &MessageRecord{
				RecordMeta: NewRecordMetaFrom("message", OriginModel),
				RunID:      r.opts.RunID,
				Turn:       r.turn,
				Role:       assistant.Role,
				Content:    assistant.Content,
				MessageID:  assistant.ID,
			}); err != nil {
				return "", SettleDetail{}, err
			}
		}

		// The usage record follows the message it belongs to. Like the
		// accounting of a failed request, it is worth less than the
		// record that says the run ended, so a refused write is dropped.
		if resp.usage != nil {
			r.writeBestEffort(ctx, &Usage{
				RecordMeta:      NewRecordMeta("usage"),
				RunID:           r.opts.RunID,
				Turn:            r.turn,
				Request:         r.requests,
				InputTokens:     resp.usage.InputTokens,
				OutputTokens:    resp.usage.OutputTokens,
				CacheReadTokens: resp.usage.CacheReadTokens,
				TTFTMS:          resp.usage.TTFTMS,
			})
		}

		if resp.finish == FinishLength {
			// The provider stopped on the output-token limit.
			return ReasonProviderError, SettleDetail{
				Source:   "finish:length",
				Text:     "the provider stopped on the output-token limit",
				Provider: &ProviderDetail{Kind: string(ProviderKindOutputTruncated)},
			}, nil
		}

		if len(resp.calls) == 0 {
			return ReasonEndTurn, SettleDetail{Source: "end_turn"}, nil
		}

		// Executing: run the calls in model order.
		r.state = StateExecuting
		for i, call := range resp.calls {
			if err := r.execute(ctx, i+1, call); err != nil {
				return "", SettleDetail{}, err
			}
		}
	}
}

// maxTokens computes the output room for the next request.
//
// The plan's rule: min(model MaxOutput or 8192, context_limit − estimated
// input − 256). A value under 256 means there is no room to answer.
func (r *Run) maxTokens() (int, bool) {
	limit := r.opts.MaxOutput
	if limit <= 0 {
		limit = 8192
	}
	room := r.opts.ContextLimit - r.estimatedInput() - 256
	if room < limit {
		limit = room
	}
	return limit, limit >= 256
}

// estimatedInput estimates the prompt tokens of the next request.
func (r *Run) estimatedInput() int {
	chars := 0
	for _, m := range r.history {
		chars += estimateMessageChars(m)
	}
	// The plan estimates at 3.5 chars per token.
	return chars * 10 / 35
}

// execute runs one tool call and records it.
func (r *Run) execute(ctx context.Context, index int, call ToolCall) error {
	handle := fmt.Sprintf("r%d", index)

	tool, ok := r.opts.Tools[call.Name]
	if !ok {
		text := fmt.Sprintf("unknown tool %q", call.Name)
		return r.recordCall(ctx, r.turn, handle, call, text)
	}

	if err := r.write(ctx, &ToolCallRecord{
		RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
		RunID:      r.opts.RunID,
		Turn:       r.turn,
		Handle:     handle,
		Tool:       call.Name,
		Arguments:  recordArgs(call.Arguments),
	}); err != nil {
		return err
	}

	prepared, err := tool.Prepare(call.Arguments, PrepareEnv{WorkingDir: r.opts.WorkingDir})
	if err != nil {
		return r.recordResult(ctx, r.turn, handle, call, ToolResult{
			Text:    err.Error(),
			IsError: true,
		})
	}

	result, err := tool.Call(ctx, prepared, ToolEnv{
		PrepareEnv: PrepareEnv{WorkingDir: r.opts.WorkingDir},
		SessionID:  r.opts.SessionID,
		RunID:      r.opts.RunID,
		Handle:     handle,
		Env:        r.opts.Env,
		Settings:   r.opts.Settings,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return r.recordResult(ctx, r.turn, handle, call, ToolResult{
			Text:    err.Error(),
			IsError: true,
		})
	}

	return r.recordResult(ctx, r.turn, handle, call, result)
}

// recordCall records a call that never reached the tool.
func (r *Run) recordCall(ctx context.Context, turn int, handle string, call ToolCall, text string) error {
	if err := r.write(ctx, &ToolCallRecord{
		RecordMeta: NewRecordMetaFrom("tool.call", OriginModel),
		RunID:      r.opts.RunID,
		Turn:       turn,
		Handle:     handle,
		Tool:       call.Name,
		Arguments:  recordArgs(call.Arguments),
	}); err != nil {
		return err
	}
	return r.recordResult(ctx, turn, handle, call, ToolResult{Text: text, IsError: true})
}

// recordResult records a tool result and appends it to the history.
func (r *Run) recordResult(ctx context.Context, turn int, handle string, call ToolCall, result ToolResult) error {
	outcome := OutcomeOK
	if result.IsError {
		outcome = OutcomeError
	}
	if err := r.write(ctx, &ToolResultRecord{
		RecordMeta: NewRecordMetaFrom("tool.result", OriginTool),
		RunID:      r.opts.RunID,
		Turn:       turn,
		Handle:     handle,
		Tool:       call.Name,
		Outcome:    outcome,
		Text:       result.Text,
		Structured: result.Structured,
	}); err != nil {
		return err
	}

	// The result goes back to the model under the call's provider-side id,
	// because that is the id the provider matches it against.
	msg := ToolResultMessage(call.ID, call.Name, result.Text, result.IsError)
	msg.Origin = OriginTool
	r.history = append(r.history, msg)
	return r.write(ctx, &MessageRecord{
		RecordMeta: NewRecordMetaFrom("message", OriginTool),
		RunID:      r.opts.RunID,
		Turn:       turn,
		Role:       msg.Role,
		Content:    msg.Content,
	})
}

// request sends one provider request and reads the whole response.
//
// A response that broke part way through is returned along with the error,
// so the caller can still write the usage it produced.
func (r *Run) request(ctx context.Context, maxTokens int) (*response, error) {
	reader, _, err := r.opts.Transport.Stream(ctx, ChatRequest{
		Model:     r.opts.ModelID,
		Messages:  r.history,
		Tools:     r.toolSpecs(),
		MaxTokens: maxTokens,
	})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	resp, err := readResponse(ctx, reader, r.opts.RunID, r.turn, r.requests, r.opts.Sink)
	if err != nil && resp != nil && resp.usage == nil {
		// The stream broke before its usage arrived. A request that
		// failed still cost tokens, so the transport's own report is
		// used.
		if reporter, ok := reader.(UsageReporter); ok {
			if usage := reporter.ReportUsage(); usage != nil {
				resp.usage = usage
			}
		}
	}
	return resp, err
}

// toolSpecs returns the tools the model may call, in offer order.
func (r *Run) toolSpecs() []ToolSpec {
	specs := make([]ToolSpec, 0, len(r.opts.ToolOrder))
	for _, name := range r.opts.ToolOrder {
		if t, ok := r.opts.Tools[name]; ok {
			specs = append(specs, t.Spec())
		}
	}
	return specs
}

// write appends one record to the sink.
func (r *Run) write(ctx context.Context, rec Record) error {
	if r.opts.Sink == nil {
		return nil
	}
	if r.sunk {
		return fmt.Errorf("poo: the session store is broken")
	}
	wctx, cancel := recordContext(ctx)
	defer cancel()
	Stamp(rec, r.seq+1, time.Now())
	if err := r.opts.Sink.Append(wctx, rec); err != nil {
		r.sunk = true
		return fmt.Errorf("poo: writing %s: %w", rec.Meta().Record, err)
	}
	r.seq++
	return nil
}

// recordWriteBudget bounds one record write or flush.
var recordWriteBudget = 5 * time.Second

// recordContext returns the context a record write runs under.
//
// A record is written even when the run's context has ended, because the
// log has to say what the run did and how it ended. The write is still
// bounded, so a store that never answers cannot hang the process.
func recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recordWriteBudget)
}

// writeBestEffort appends one record to the sink and ignores the
// failure.
//
// The accounting of a request that failed is worth less than the record
// that says the run ended, so a write that fails here neither stops the
// run nor marks the sink broken.
func (r *Run) writeBestEffort(ctx context.Context, rec Record) {
	if r.opts.Sink == nil || r.sunk {
		return
	}
	wctx, cancel := recordContext(ctx)
	defer cancel()
	Stamp(rec, r.seq+1, time.Now())
	if r.opts.Sink.Append(wctx, rec) == nil {
		r.seq++
	}
}

// cancelDetail explains a cancelled settle. A deadline has its own
// source, so a consumer can tell it from a plain cancel without reading
// the text.
func cancelDetail(err error) SettleDetail {
	source := SourceCancelContext
	if errors.Is(err, context.DeadlineExceeded) {
		source = SourceCancelDeadline
	}
	return SettleDetail{Source: source, Text: err.Error()}
}

// settleForError maps a request error to a settle reason.
func (r *Run) settleForError(err error) (Reason, SettleDetail, error) {
	// A cancellation and a deadline are the same outcome: the run was
	// stopped before the model finished. The plan's reason set has no
	// separate one for a deadline.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ReasonCancelled, cancelDetail(err), nil
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		kind := string(pe.Kind)
		// The wait the endpoint asked for travels with the failure:
		// the plan's provider_limit row settles on a Retry-After over
		// thirty seconds, so the record has to carry the number it
		// settles on.
		// It is rounded up, so a wait just over thirty seconds is not
		// recorded as thirty, which the rule reads as retryable.
		retryAfter := int((pe.RetryAfter + time.Second - 1) / time.Second)
		if pe.Kind == ProviderKindContextLength {
			return ReasonContextExhausted, SettleDetail{
				Source:   "provider:context_length",
				Text:     pe.Message,
				Provider: &ProviderDetail{Kind: kind, Status: pe.Status, Code: pe.Code, RetryAfterSec: retryAfter},
			}, nil
		}
		return ReasonProviderError, SettleDetail{
			Source:   "provider:" + kind,
			Text:     pe.Message,
			Provider: &ProviderDetail{Kind: kind, Status: pe.Status, Code: pe.Code, RetryAfterSec: retryAfter},
		}, nil
	}
	return "", SettleDetail{}, err
}

// response is what one provider request produced.
type response struct {
	// content is the assistant message's blocks.
	content []ContentBlock
	// calls are the tool calls the model asked for.
	calls []ToolCall
	// finish is why the model stopped.
	finish FinishReason
	// usage is the token accounting the endpoint reported, if any.
	usage *UsageReport
}

// collect fills the response with what the stream carried.
//
// A reasoning stream becomes its own content block, so the model's
// thinking survives without breaking the text. The calls are ordered by
// the provider's index, so the model sees them in the order it asked for
// them.
func (r *response) collect(text, thinking string, calls map[int]*ToolCall, order []int) {
	if thinking != "" {
		r.content = append(r.content, ContentBlock{Kind: ContentThinking, Text: thinking})
	}
	if text != "" {
		r.content = append(r.content, ContentBlock{Kind: ContentText, Text: text})
	}
	sort.Ints(order)
	for _, idx := range order {
		c := calls[idx]
		if c.Name == "" {
			continue
		}
		r.calls = append(r.calls, *c)
	}
}

// splitKeyBase is where the keys of calls that share a provider's index
// start, above the text parser's own.
const splitKeyBase = 1 << 20

// callSlot names a call by the index and the id a provider sent it with.
type callSlot struct {
	index int
	id    string
}

// readResponse drains a stream into a response.
//
// The usage is collected rather than written, so the caller can write it
// after the assistant's message: the log then reads in the order the
// events happened.
//
// A stream that fails part way through returns the response it managed to
// read along with the failure, so the caller can still write the usage.
func readResponse(ctx context.Context, reader StreamReader, runID string, turn, request int, sink RecordSink) (*response, error) {
	resp := &response{finish: FinishStop}
	var text strings.Builder
	var thinking strings.Builder
	calls := make(map[int]*ToolCall)
	var order []int
	// current maps a provider index to the call its latest fragment joined.
	current := make(map[int]int)
	// byID maps a provider index and a call id to the call they name.
	byID := make(map[callSlot]int)
	// splitKey numbers a call that arrived at an index in use. It starts
	// above the text parser's calls, which are numbered from 1000, and
	// skips any key a provider's own index has taken.
	splitKey := splitKeyBase

	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			resp.collect(text.String(), thinking.String(), calls, order)
			return resp, err
		}
		switch ev.Kind {
		case "delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Kind {
			case "thinking":
				thinking.WriteString(ev.Delta.Text)
			default:
				text.WriteString(ev.Delta.Text)
			}
		case "tool_call":
			if ev.ToolCall == nil {
				continue
			}
			// A provider may split one call across several events, and
			// may send several calls in one event. The index groups the
			// fragments of one call, but some providers send every
			// parallel call at index 0, each with its own id. So a
			// fragment with an id already seen at its index continues
			// that call, a fragment with a new id at an index in use
			// starts a new call, and a fragment with no id continues the
			// latest call at its index.
			idx, id := ev.ToolCall.Index, ev.ToolCall.ID
			key, ok := current[idx]
			if id != "" {
				if k, seen := byID[callSlot{idx, id}]; seen {
					key, ok = k, true
				} else if ok && calls[key].ID != "" {
					ok = false
				}
			}
			if !ok {
				key = idx
				for {
					if _, taken := calls[key]; !taken {
						break
					}
					key = splitKey
					splitKey++
				}
				calls[key] = &ToolCall{}
				order = append(order, key)
			}
			existing := calls[key]
			current[idx] = key
			if id != "" {
				byID[callSlot{idx, id}] = key
			}
			if ev.ToolCall.ID != "" {
				existing.ID = ev.ToolCall.ID
			}
			if ev.ToolCall.Name != "" {
				existing.Name = ev.ToolCall.Name
			}
			if len(ev.ToolCall.Arguments) > 0 {
				existing.Arguments = append(existing.Arguments, ev.ToolCall.Arguments...)
			}
		case "usage":
			if ev.Usage != nil {
				usage := *ev.Usage
				resp.usage = &usage
			}
		case "finish":
			if ev.FinishReason != "" {
				resp.finish = ev.FinishReason
			}
		}
	}

	resp.collect(text.String(), thinking.String(), calls, order)
	return resp, nil
}

// hashText returns the hex SHA-256 of a text.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// nowRFC3339 returns the current time in UTC, RFC 3339 with nanoseconds.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// recordArgs returns the arguments as JSON, or the raw text as a JSON
// string when the model did not send valid JSON.
//
// A record always holds valid JSON, so a consumer can decode the arguments
// without a special case, and a model that sent malformed arguments is
// still recorded faithfully.
func recordArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	if json.Valid(raw) {
		return raw
	}
	quoted, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage("{}")
	}
	return quoted
}

// estimateMessageChars returns the approximate serialized size of a message.
func estimateMessageChars(m Message) int {
	chars := len(string(m.Role))
	for _, c := range m.Content {
		chars += len(c.Text) + len(c.Name) + len(c.ToolCallID)
		if len(c.Arguments) > 0 {
			chars += len(c.Arguments)
		}
	}
	return chars
}
