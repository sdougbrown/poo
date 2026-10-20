package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sdougbrown/poo"
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

// scriptedTransport replays a recorded conversation.
//
// The transport hands out the recorded responses in order. When the
// recorded conversation runs out, it repeats the last one, so a harness
// that asks for more turns than the recording holds still gets an
// answer instead of an error.
type scriptedTransport struct {
	// turns are the recorded responses.
	turns []ScriptedTurn
	// index is the next response to hand out.
	index int
	// requests counts the requests the transport served.
	requests int
}

// newScriptedTransport returns a transport that replays a script.
func newScriptedTransport(turns []ScriptedTurn) *scriptedTransport {
	return &scriptedTransport{turns: turns}
}

// Stream returns the next recorded response.
func (t *scriptedTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, error) {
	if len(t.turns) == 0 {
		return nil, fmt.Errorf("eval: the script holds no response")
	}
	idx := t.index
	if idx >= len(t.turns) {
		idx = len(t.turns) - 1
	}
	t.index++
	t.requests++

	turn := t.turns[idx]
	reader := &scriptedReader{turn: turn, index: idx}
	return reader, nil
}

// scriptedReader is the read side of one recorded response.
type scriptedReader struct {
	turn  ScriptedTurn
	index int
	// stage is what the reader hands out next.
	stage int
	// callIndex numbers the calls within the turn.
	callIndex int
}

// Next returns the next piece of the recorded response.
func (r *scriptedReader) Next() (poo.StreamEvent, error) {
	for {
		switch r.stage {
		case 0:
			r.stage++
			if r.turn.Thinking != "" {
				return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "thinking", Text: r.turn.Thinking}}, nil
			}
		case 1:
			r.stage++
			if r.turn.Text != "" {
				return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: r.turn.Text}}, nil
			}
		case 2:
			r.stage++
			if len(r.turn.Calls) == 0 {
				continue
			}
			call := r.turn.Calls[0]
			r.turn.Calls = r.turn.Calls[1:]
			r.callIndex++
			return poo.StreamEvent{
				Kind: "tool_call",
				ToolCall: &poo.ToolCallDelta{
					Index:     r.callIndex - 1,
					ID:        fmt.Sprintf("replay_%d_%d", r.index, r.callIndex),
					Name:      call.Name,
					Arguments: call.Arguments,
					Complete:  true,
				},
			}, nil
		case 3:
			r.stage++
			if r.turn.InputTokens == 0 && r.turn.OutputTokens == 0 {
				continue
			}
			return poo.StreamEvent{
				Kind: "usage",
				Usage: &poo.UsageReport{
					InputTokens:  r.turn.InputTokens,
					OutputTokens: r.turn.OutputTokens,
				},
			}, nil
		default:
			return poo.StreamEvent{}, io.EOF
		}
	}
}

// Close releases the response.
func (r *scriptedReader) Close() error { return nil }

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
func Replay(path string, opts ReplayOptions) (*ReplayResult, error) {
	log, err := ParseLog(path)
	if err != nil {
		return nil, err
	}
	script, err := ScriptFromLog(log)
	if err != nil {
		return nil, err
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
		ContextLimit: opts.ContextLimit,
		MaxTurns:     opts.MaxTurns,
		WorkingDir:   opts.WorkingDir,
	})
	if _, err := run.Execute(opts.Context, "replay"); err != nil {
		return nil, fmt.Errorf("eval: replaying %s: %w", path, err)
	}

	result := &ReplayResult{
		Path:     path,
		Recorded: recordedCalls(log),
		Replayed: recorder.calls,
	}
	result.Match, result.Differences = diffCalls(result.Recorded, result.Replayed)
	return result, nil
}

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
