package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// ScriptedTurn is one recorded provider response.
//
// A replay reads the provider's answers out of a recorded session and
// feeds them back to the runner, so the runner can be checked against
// what it did last time without a live model.
type ScriptedTurn struct {
	// Text is the assistant's text, if any.
	Text string `json:"text,omitempty"`
	// Thinking is the assistant's reasoning, if any.
	Thinking string `json:"thinking,omitempty"`
	// Calls are the tool calls the model made, in order.
	Calls []ScriptedCall `json:"calls,omitempty"`
	// Finish is why the provider stopped: "stop" or "tool_calls".
	Finish string `json:"finish,omitempty"`
	// InputTokens and OutputTokens are the token counts the endpoint
	// reported.
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// ScriptedCall is one recorded tool call.
type ScriptedCall struct {
	// Name is the tool the model called.
	Name string `json:"name"`
	// Arguments is the raw JSON arguments.
	Arguments json.RawMessage `json:"arguments"`
}

// Script is a recorded provider conversation.
//
// It is the part of a session log that the provider produced: the
// assistant's messages, in order.
type Script struct {
	// Turns are the provider responses, in order.
	Turns []ScriptedTurn `json:"turns"`
}

// ScriptFromLog builds a script from a recorded session log.
//
// A log that holds no assistant message is not a script: there is
// nothing to replay.
func ScriptFromLog(log *Log) (*Script, error) {
	script := &Script{}
	for _, r := range log.Records {
		if r.Record != "message" || r.Origin != "model" {
			continue
		}
		turn := ScriptedTurn{Finish: "stop"}
		for _, c := range r.Content {
			switch c.Kind {
			case "text":
				turn.Text += c.Text
			case "thinking":
				turn.Thinking += c.Text
			case "tool_call":
				turn.Calls = append(turn.Calls, ScriptedCall{Name: c.Name, Arguments: c.Arguments})
				turn.Finish = "tool_calls"
			}
		}
		script.Turns = append(script.Turns, turn)
	}
	if len(script.Turns) == 0 {
		return nil, fmt.Errorf("eval: %s holds no assistant message to replay", log.Path)
	}
	return script, nil
}

// ToScript turns the recorded script into a pootest script.
//
// The recorded format stays the eval's own, so a recording from an older
// runner still replays. The transport is pootest's.
func (s *Script) ToScript() pootest.Script {
	out := pootest.Script{Turns: make([]pootest.Turn, 0, len(s.Turns))}
	for _, t := range s.Turns {
		turn := pootest.Turn{
			Text:         t.Text,
			Thinking:     t.Thinking,
			Finish:       t.Finish,
			InputTokens:  t.InputTokens,
			OutputTokens: t.OutputTokens,
		}
		for _, c := range t.Calls {
			turn.Calls = append(turn.Calls, pootest.Call{
				Name:      c.Name,
				Arguments: c.Arguments,
			})
		}
		out.Turns = append(out.Turns, turn)
	}
	return out
}

// scriptedTransport replays a recorded conversation.
//
// The transport is pootest's scripted transport: it hands out the
// recorded responses in order, and when the recording runs out it says
// so. A transport that repeated the last response would turn a one-turn
// recording into an endless loop, and the comparison would then report a
// difference that is only the loop.
type scriptedTransport = pootest.Transport

// ErrScriptExhausted is the error the transport returns when the
// recording holds no more responses.
//
// It is a typed error, so a caller can tell an exhausted script from a
// broken one.
var ErrScriptExhausted = pootest.ErrScriptExhausted

// newScriptedTransport returns a transport that replays a script.
func newScriptedTransport(turns []ScriptedTurn) *scriptedTransport {
	script := Script{Turns: turns}
	return pootest.NewTransportFromScript(script.ToScript())
}

// CallStep is one tool call in a replayed sequence.
type CallStep struct {
	// Index is the step's number, counting from one.
	Index int `json:"index"`
	// Turn is the turn the call was made in.
	Turn int `json:"turn"`
	// Tool is the tool the model called.
	Tool string `json:"tool"`
	// Arguments is the raw JSON arguments.
	Arguments json.RawMessage `json:"arguments"`
}

// ReplayResult is the result of replaying a session.
type ReplayResult struct {
	// Path is the session that was replayed.
	Path string `json:"path"`
	// Recorded is the tool-call sequence the recording holds.
	Recorded []CallStep `json:"recorded"`
	// Replayed is the tool-call sequence the replay produced.
	Replayed []CallStep `json:"replayed"`
	// Compactions is the number of compactions the replay applied.
	//
	// A replay applies the compactions the recording holds, and never
	// runs the strategy that made them.
	Compactions int `json:"compactions"`
	// Match reports whether the two sequences are the same.
	Match bool `json:"match"`
	// Differences describe where the two sequences part.
	Differences []string `json:"differences,omitempty"`
	// Jobs are the job results the replay delivered, in order.
	Jobs []string `json:"jobs,omitempty"`
}

// Replay re-runs a recorded session through the current runner.
//
// The runner reads the session's provider responses from the recording,
// so the replay needs no model and no network. The result is the
// recorded tool-call sequence beside the replayed one, and whether they
// are the same.
//
// The replay runs on the recording's own limits: its context limit and
// its turn count, unless the caller names its own.
func Replay(path string, opts ReplayOptions) (*ReplayResult, error) {
	log, err := ParseLog(path)
	if err != nil {
		return nil, err
	}
	script, err := ScriptFromLog(log)
	if err != nil {
		return nil, err
	}

	contextLimit := opts.ContextLimit
	if contextLimit <= 0 {
		contextLimit = log.ContextLimit()
	}
	if contextLimit <= 0 {
		contextLimit = DefaultReplayContextLimit
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = log.SettleTurns()
	}

	transport := &replayNativeTransport{scriptedTransport: newScriptedTransport(script.Turns)}
	recorder := &callRecorder{}

	// The replay sends the prompt the recording holds. It never renders
	// the current default again: a prompt change since the recording
	// would otherwise change the replay's budget and its requests.
	var beforeRun []poo.BeforeRunHook
	if prompts := recordedSystemPrompts(log); len(prompts) > 0 {
		beforeRun = []poo.BeforeRunHook{poo.NewRecordedPrompts(prompts)}
	}

	// The recording's compactions are applied as they were recorded,
	// and the strategy that made them is never run.
	compactions := recordedCompactions(log)

	session, err := poo.New(poo.Options{
		Transport: transport,
		Tools:     map[string]poo.Tool{},
		ToolOrder: nil,
		Sink:      recorder,
		SessionID: "replay",
		Model: poo.ModelInfo{
			ModelID:      "replay",
			API:          poo.APIChatCompletions,
			ContextLimit: contextLimit,
		},
		MaxTurns:   maxTurns,
		WorkingDir: opts.WorkingDir,
		BeforeRun:  beforeRun,
		Compactor:  newRecordedCompactor(compactions, transport),
	})
	if err != nil {
		return nil, fmt.Errorf("eval: replaying %s: %w", path, err)
	}
	defer session.Close()

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	// A recording may hold a job result that reached the model in a
	// woken run. The replay queues it again, so the run the recording
	// woke is the run the replay wakes.
	if jobs := replayJobResults(log); len(jobs) > 0 {
		for _, job := range jobs {
			session.QueueJobResult(job)
		}
		// The woken run consumes the recorded responses it used, so the
		// prompt below continues the conversation after it.
		if _, err := session.Wait(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("eval: replaying %s: waiting for the woken run: %w", path, err)
		}
	}
	if _, err := session.Prompt(ctx, "replay"); err != nil {
		// A script that runs out is not a failure: the recording held
		// what it held, and the replay compared what it could.
		if !errors.Is(err, ErrScriptExhausted) {
			return nil, fmt.Errorf("eval: replaying %s: %w", path, err)
		}
	}
	if _, err := session.Wait(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return nil, fmt.Errorf("eval: replaying %s: %w", path, err)
	}

	result := &ReplayResult{
		Path:        path,
		Recorded:    recordedCalls(log),
		Replayed:    recorder.calls,
		Compactions: recorder.compactions,
		Jobs:        recorder.jobs,
	}
	// A recorded compaction that the replay could not apply is a
	// failure: a replay that silently skipped it measured a conversation
	// the recording never had.
	if result.Compactions != len(compactions) {
		return nil, fmt.Errorf("eval: replaying %s: %d of %d recorded compactions were not applied",
			path, result.Compactions, len(compactions))
	}
	result.Match, result.Differences = diffCalls(result.Recorded, result.Replayed)
	return result, nil
}

// replayNativeTransport is the scripted transport with the native
// compaction capability.
//
// A recording's window was produced by the transport that recorded it,
// and the replay hands that window back through the same capability.
type replayNativeTransport struct {
	*scriptedTransport
	mu     sync.Mutex
	window *poo.ProviderWindow
}

// setWindow names the window the next native compaction returns.
func (t *replayNativeTransport) setWindow(w *poo.ProviderWindow) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.window = w
}

// Compact returns the recorded window.
func (t *replayNativeTransport) Compact(ctx context.Context, prev poo.Rendered) (poo.ProviderWindow, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.window == nil {
		return poo.ProviderWindow{}, fmt.Errorf("eval: the recording holds no window to apply")
	}
	return *t.window, nil
}

// replayJobResults returns the job results a recording holds that never
// reached the model.
//
// The results are the core's own: the log is read into the records the
// core pairs them from, so the replay and a restore agree on what was
// delivered.
func replayJobResults(log *Log) []poo.JobResult {
	records := make([]poo.Record, 0, len(log.Records))
	for _, r := range log.Records {
		switch r.Record {
		case "input.delivered":
			records = append(records, &poo.InputDeliveredRecord{InputID: r.InputID})
		case "job.ended":
			records = append(records, &poo.JobEndedRecord{
				JobID:   r.JobID,
				Handle:  r.Handle,
				Tool:    r.Tool,
				Outcome: poo.Outcome(r.Outcome),
				InputID: r.InputID,
				Text:    r.Text,
				Fatal:   r.Fatal,
			})
		}
	}
	return poo.PendingJobResults(records)
}

// recordedSystemPrompts returns the session's rendered prompts, in run
// order.
func recordedSystemPrompts(log *Log) []string {
	// The system_prompt record carries the whole prompt, so a log that
	// has one is read from it. An older log that predates the record
	// keeps the prompt on its system message, which is the fallback.
	var prompts []string
	for _, r := range log.Records {
		if r.Record == "system_prompt" {
			prompts = append(prompts, r.Text)
		}
	}
	if len(prompts) > 0 {
		return prompts
	}
	for _, r := range log.Records {
		if r.Record != "message" || r.Origin != "system" {
			continue
		}
		var b strings.Builder
		for _, c := range r.Content {
			if c.Kind == "text" {
				b.WriteString(c.Text)
			}
		}
		prompts = append(prompts, b.String())
	}
	return prompts
}

// recordedCompaction is one compaction a recording holds.
type recordedCompaction struct {
	// turns is the number of requests the recording had made when the
	// compaction happened.
	turns int
	// compaction is the compaction itself.
	compaction poo.Compaction
}

// recordedCompactions returns the compactions a log holds, in order.
//
// The number of requests a compaction followed is the number of the
// model's messages before it, so the replay applies it at the same
// point the recording did.
func recordedCompactions(log *Log) []recordedCompaction {
	var out []recordedCompaction
	turns := 0
	for _, r := range log.Records {
		switch r.Record {
		case "message":
			if r.Origin == "model" {
				turns++
			}
		case "compaction":
			if r.Compaction == nil {
				continue
			}
			out = append(out, recordedCompaction{turns: turns, compaction: *r.Compaction})
		}
	}
	return out
}

// recordedCompactor applies the compactions a recording holds.
//
// The strategy that made them is never run: the recorded compaction is
// applied as it was recorded.
type recordedCompactor struct {
	mu sync.Mutex
	// compactions are the recorded compactions, in order.
	compactions []recordedCompaction
	// applied counts the compactions the compactor handed out.
	applied int
	// transport receives the recorded window of a window compaction.
	transport *replayNativeTransport
}

// newRecordedCompactor returns a compactor that applies the recorded
// compactions.
//
// A recording with no compaction gets no compactor, so the replay never
// enters the compacting state.
func newRecordedCompactor(compactions []recordedCompaction, transport *replayNativeTransport) poo.Compactor {
	if len(compactions) == 0 {
		return nil
	}
	return &recordedCompactor{compactions: compactions, transport: transport}
}

// ShouldCompact says yes once the run has made the requests the
// recording had made when it compacted.
func (c *recordedCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.applied >= len(c.compactions) {
		return false
	}
	return budget.Turns >= c.compactions[c.applied].turns
}

// Compact hands out the next recorded compaction.
func (c *recordedCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.applied >= len(c.compactions) {
		return poo.Compaction{}, fmt.Errorf("eval: the recording holds no more compactions")
	}
	compaction := c.compactions[c.applied].compaction
	c.applied++
	if compaction.Window != nil && c.transport != nil {
		c.transport.setWindow(compaction.Window)
	}
	return compaction, nil
}

// DefaultReplayContextLimit is the context window a replay uses when the
// recording names none.
const DefaultReplayContextLimit = 32768

// ReplayOptions configure a replay.
type ReplayOptions struct {
	// Context is the context the replay runs under.
	Context context.Context
	// WorkingDir is the directory the replayed tools work in.
	WorkingDir string
	// ContextLimit is the context window the replay uses.
	ContextLimit int
	// MaxTurns is the turn limit the replay uses.
	MaxTurns int
}

// recordedCalls returns the tool-call sequence a log holds.
//
// The sequence comes from the assistant's messages: a message's
// tool_call blocks are the calls the model made. The log's own
// tool.call records hold the same calls, but a recording that only has
// the messages still holds the sequence.
func recordedCalls(log *Log) []CallStep {
	var out []CallStep
	for _, r := range log.Records {
		if r.Record != "message" || r.Origin != "model" {
			continue
		}
		for _, c := range r.Content {
			if c.Kind != "tool_call" {
				continue
			}
			out = append(out, CallStep{
				Index:     len(out) + 1,
				Turn:      r.Turn,
				Tool:      c.Name,
				Arguments: c.Arguments,
			})
		}
	}
	return out
}

// callRecorder collects the tool calls a replay made.
type callRecorder struct {
	// calls are the calls, in order.
	calls []CallStep
	// compactions counts the compactions the replay applied.
	compactions int
	// jobs are the job results the replay delivered, in order.
	jobs []string
}

// Append records one session record.
func (c *callRecorder) Append(ctx context.Context, rec poo.Record) error {
	switch r := rec.(type) {
	case *poo.ToolCallRecord:
		c.calls = append(c.calls, CallStep{
			Index:     len(c.calls) + 1,
			Turn:      r.Turn,
			Tool:      r.Tool,
			Arguments: r.Arguments,
		})
	case *poo.CompactionRecord:
		// The compaction reached the store, so the replay applied it.
		c.compactions++
	case *poo.MessageRecord:
		if r.Origin != poo.OriginJob {
			return nil
		}
		for _, block := range r.Content {
			if block.Kind == poo.ContentText {
				c.jobs = append(c.jobs, block.Text)
			}
		}
	}
	return nil
}

// Flush pushes buffered records out.
func (c *callRecorder) Flush(ctx context.Context) error { return nil }

// diffCalls compares two tool-call sequences.
//
// The comparison is on the tool's name and the arguments: the handle and
// the turn may move when the runner changes, and the call itself may
// not.
func diffCalls(recorded, replayed []CallStep) (bool, []string) {
	var differences []string
	n := len(recorded)
	if len(replayed) < n {
		n = len(replayed)
	}
	for i := 0; i < n; i++ {
		a, b := recorded[i], replayed[i]
		if a.Tool != b.Tool {
			differences = append(differences, fmt.Sprintf("step %d: recorded %s, replayed %s", i+1, a.Tool, b.Tool))
			continue
		}
		if !sameArgs(a.Arguments, b.Arguments) {
			differences = append(differences, fmt.Sprintf("step %d: recorded %s %s, replayed %s %s",
				i+1, a.Tool, string(a.Arguments), b.Tool, string(b.Arguments)))
		}
	}
	if len(recorded) > len(replayed) {
		differences = append(differences, fmt.Sprintf("the recording holds %d call(s) and the replay produced %d", len(recorded), len(replayed)))
	}
	if len(replayed) > len(recorded) {
		differences = append(differences, fmt.Sprintf("the replay produced %d call(s) and the recording holds %d", len(replayed), len(recorded)))
	}
	return len(differences) == 0, differences
}

// sameArgs reports whether two argument sets are the same.
//
// The arguments are compared as JSON, so a difference in whitespace or
// key order does not count.
func sameArgs(a, b json.RawMessage) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return strings.TrimSpace(string(a)) == strings.TrimSpace(string(b))
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return jsonEqual(av, bv)
}

// jsonEqual reports whether two decoded JSON values are the same.
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, ok := bv[k]
			if !ok || !jsonEqual(v, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
