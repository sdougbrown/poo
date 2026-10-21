package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// ScriptedTurn is one recorded provider response.
//
// A replay reads the provider's answers out of a recorded session and
// feeds them back to the harness, so the harness can be checked against
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
// harness still replays. The transport is pootest's.
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
	// Match reports whether the two sequences are the same.
	Match bool `json:"match"`
	// Differences describe where the two sequences part.
	Differences []string `json:"differences,omitempty"`
}

// Replay re-runs a recorded session through the current harness.
//
// The harness reads the session's provider responses from the recording,
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

	transport := newScriptedTransport(script.Turns)
	recorder := &callRecorder{}

	run := poo.NewRun(poo.Options{
		Transport:    transport,
		Tools:        map[string]poo.Tool{},
		ToolOrder:    nil,
		Sink:         recorder,
		SessionID:    "replay",
		RunID:        "replay",
		ModelID:      "replay",
		ContextLimit: contextLimit,
		MaxTurns:     maxTurns,
		WorkingDir:   opts.WorkingDir,
	})
	if _, err := run.Execute(opts.Context, "replay"); err != nil {
		// A script that runs out is not a failure: the recording held
		// what it held, and the replay compared what it could.
		if !errors.Is(err, ErrScriptExhausted) {
			return nil, fmt.Errorf("eval: replaying %s: %w", path, err)
		}
	}

	result := &ReplayResult{
		Path:     path,
		Recorded: recordedCalls(log),
		Replayed: recorder.calls,
	}
	result.Match, result.Differences = diffCalls(result.Recorded, result.Replayed)
	return result, nil
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
	// MaxTurns is the turn ceiling the replay uses.
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
}

// Append records one session record.
func (c *callRecorder) Append(ctx context.Context, rec poo.Record) error {
	call, ok := rec.(*poo.ToolCallRecord)
	if !ok {
		return nil
	}
	c.calls = append(c.calls, CallStep{
		Index:     len(c.calls) + 1,
		Turn:      call.Turn,
		Tool:      call.Tool,
		Arguments: call.Arguments,
	})
	return nil
}

// Flush pushes buffered records out.
func (c *callRecorder) Flush(ctx context.Context) error { return nil }

// diffCalls compares two tool-call sequences.
//
// The comparison is on the tool's name and the arguments: the handle and
// the turn may move when the harness changes, and the call itself may
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
