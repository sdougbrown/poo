// Package pootest holds poo's exported test helpers.
//
// The helpers are the pieces a consumer's own tests need: a scripted
// chat transport, an in-memory record sink, a stub decider and a scripted
// compactor. They are exported rather than kept in a test file, because a
// consumer that links poo cannot reach an unexported test helper, and a
// consumer that drives a session needs exactly these.
//
// The package imports core and the standard library, and nothing else.
package pootest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/sdougbrown/poo"
)

// ErrScriptExhausted is the error the scripted transport returns when the
// script holds no more responses.
//
// It is a typed error, so a caller can tell an exhausted script from a
// broken one.
var ErrScriptExhausted = errors.New("pootest: the script holds no more responses")

// Call is one tool call the scripted model makes.
type Call struct {
	// Name is the tool the model called.
	Name string `json:"name"`
	// Arguments are the raw JSON arguments.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// ID is the call's identifier. It is generated when it is empty.
	ID string `json:"id,omitempty"`
}

// Turn is one scripted provider response.
type Turn struct {
	// Text is the assistant's text, if any.
	Text string `json:"text,omitempty"`
	// Thinking is the assistant's reasoning, if any.
	Thinking string `json:"thinking,omitempty"`
	// Calls are the tool calls the model made, in order.
	Calls []Call `json:"calls,omitempty"`
	// Finish is why the provider stopped: "stop", "tool_calls" or
	// "length".
	Finish string `json:"finish,omitempty"`
	// InputTokens and OutputTokens are the token counts the endpoint
	// reported.
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	// Err is the failure the request returns, when it fails.
	Err error `json:"-"`
}

// Script is a recorded provider conversation.
type Script struct {
	// Turns are the provider responses, in order.
	Turns []Turn `json:"turns"`
}

// Transport is a scripted chat transport.
//
// It hands out the scripted responses in order. When the script runs out
// it says so: a transport that repeated the last response would turn a
// one-turn script into an endless loop.
type Transport struct {
	mu sync.Mutex
	// turns are the scripted responses.
	turns []Turn
	// index is the next response to hand out.
	index int
	// requests holds the requests the transport received.
	requests []poo.ChatRequest
	// rendered holds the snapshot of each request.
	rendered []poo.Rendered
	// forks holds the forks the transport received.
	forks []Fork
	// forkTurns are the responses a fork serves, in order.
	forkTurns []Turn
	// forkIndex is the next forked response to hand out.
	forkIndex int
	// err is the failure the transport returns when it is set.
	err error
}

// Fork is one fork request the transport received.
type Fork struct {
	// Prev is the snapshot the fork was based on.
	Prev poo.Rendered
	// Appended are the messages the fork appended.
	Appended []poo.Message
	// MaxTokens is the output ceiling the fork asked for.
	MaxTokens int
}

// NewTransport returns a transport that serves a script.
func NewTransport(turns ...Turn) *Transport {
	return &Transport{turns: turns}
}

// NewTransportFromScript returns a transport that serves a script.
func NewTransportFromScript(s Script) *Transport {
	return &Transport{turns: s.Turns}
}

// SetError makes every request fail with an error.
func (t *Transport) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

// SetForkTurns sets the responses a fork serves.
func (t *Transport) SetForkTurns(turns ...Turn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.forkTurns = turns
}

// Requests returns the requests the transport received, in order.
//
// The requests are copies: a test that changes what it got back does not
// change what the transport recorded.
func (t *Transport) Requests() []poo.ChatRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]poo.ChatRequest, 0, len(t.requests))
	for _, r := range t.requests {
		out = append(out, copyRequest(r))
	}
	return out
}

// Rendered returns the snapshots of the requests, in order.
func (t *Transport) Rendered() []poo.Rendered {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]poo.Rendered, 0, len(t.rendered))
	for _, r := range t.rendered {
		out = append(out, copyRendered(r))
	}
	return out
}

// copyRequest copies a request and the messages under it.
//
// The copy is deep: the content blocks, the raw arguments and the opaque
// provider payloads are copied, so a test that changes what it got back
// does not change what the transport recorded.
func copyRequest(req poo.ChatRequest) poo.ChatRequest {
	out := req
	if req.Messages != nil {
		out.Messages = make([]poo.Message, len(req.Messages))
		for i, m := range req.Messages {
			out.Messages[i] = m
			if m.Content != nil {
				out.Messages[i].Content = make([]poo.ContentBlock, len(m.Content))
				for j, c := range m.Content {
					out.Messages[i].Content[j] = c
					if c.Arguments != nil {
						out.Messages[i].Content[j].Arguments = append([]byte(nil), c.Arguments...)
					}
					if c.Provider != nil {
						p := *c.Provider
						if p.Data != nil {
							p.Data = append([]byte(nil), p.Data...)
						}
						out.Messages[i].Content[j].Provider = &p
					}
				}
			}
			if m.DeliveryNote != nil {
				note := *m.DeliveryNote
				out.Messages[i].DeliveryNote = &note
			}
		}
	}
	if req.Tools != nil {
		out.Tools = make([]poo.ToolSpec, len(req.Tools))
		for i, s := range req.Tools {
			out.Tools[i] = s
			if s.Schema != nil {
				out.Tools[i].Schema = append([]byte(nil), s.Schema...)
			}
		}
	}
	return out
}

// copyRendered copies a snapshot and its body.
func copyRendered(r poo.Rendered) poo.Rendered {
	out := r
	if r.Body != nil {
		out.Body = append([]byte(nil), r.Body...)
	}
	return out
}

// Forks returns the fork requests the transport received, in order.
func (t *Transport) Forks() []Fork {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Fork, 0, len(t.forks))
	for _, f := range t.forks {
		out = append(out, Fork{
			Prev:      copyRendered(f.Prev),
			Appended:  copyRequest(poo.ChatRequest{Messages: f.Appended}).Messages,
			MaxTokens: f.MaxTokens,
		})
	}
	return out
}

// Stream serves the next scripted response.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.requests = append(t.requests, copyRequest(req))
	rendered := poo.Rendered{
		API:      poo.APIChatCompletions,
		Body:     renderBody(req),
		Messages: len(req.Messages),
	}
	t.rendered = append(t.rendered, copyRendered(rendered))

	if t.err != nil {
		return nil, rendered, t.err
	}
	if len(t.turns) == 0 {
		return nil, rendered, errors.New("pootest: the script holds no response")
	}
	if t.index >= len(t.turns) {
		return nil, rendered, ErrScriptExhausted
	}
	turn := t.turns[t.index]
	t.index++

	if turn.Err != nil {
		return nil, rendered, turn.Err
	}
	return newReader(turn, t.index-1), rendered, nil
}

// Fork serves the next forked response.
//
// The fork's snapshot is recorded, so a test can assert that the forked
// request carried the prefix it was supposed to.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.forks = append(t.forks, Fork{Prev: prev, Appended: appended, MaxTokens: maxTokens})
	if t.err != nil {
		return nil, t.err
	}
	if len(t.forkTurns) == 0 {
		return nil, ErrScriptExhausted
	}
	if t.forkIndex >= len(t.forkTurns) {
		return nil, ErrScriptExhausted
	}
	turn := t.forkTurns[t.forkIndex]
	t.forkIndex++
	return newReader(turn, t.forkIndex-1), nil
}

// renderBody renders a request into a body, so the snapshot holds
// something a test can look at.
//
// The body is not the provider's own JSON: the scripted transport has no
// wire shape of its own. It is a stable rendering of the request, so two
// requests that differ differ in their bodies. Every block kind is
// rendered, so a body that holds a tool call or thinking is not the same
// body as one that holds only text.
func renderBody(req poo.ChatRequest) []byte {
	type block struct {
		Kind       string             `json:"kind"`
		Text       string             `json:"text,omitempty"`
		ToolCallID string             `json:"tool_call_id,omitempty"`
		Name       string             `json:"name,omitempty"`
		Arguments  string             `json:"arguments,omitempty"`
		IsError    bool               `json:"is_error,omitempty"`
		Provider   *poo.ProviderBlock `json:"provider,omitempty"`
	}
	type message struct {
		Role    string  `json:"role"`
		Content string  `json:"content"`
		Blocks  []block `json:"blocks,omitempty"`
	}
	out := struct {
		Model     string    `json:"model"`
		Messages  []message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
	}{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}
	for _, m := range req.Messages {
		msg := message{Role: string(m.Role), Content: m.Text()}
		for _, c := range m.Content {
			switch c.Kind {
			case poo.ContentText:
				continue
			case poo.ContentToolCall, poo.ContentToolResult:
				msg.Blocks = append(msg.Blocks, block{
					Kind:       string(c.Kind),
					Text:       c.Text,
					ToolCallID: c.ToolCallID,
					Name:       c.Name,
					Arguments:  string(c.Arguments),
					IsError:    c.IsError,
					Provider:   c.Provider,
				})
			default:
				msg.Blocks = append(msg.Blocks, block{
					Kind:     string(c.Kind),
					Text:     c.Text,
					Provider: c.Provider,
				})
			}
		}
		out.Messages = append(out.Messages, msg)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// reader is the read side of one scripted response.
type reader struct {
	turn Turn
	// index is the turn's number in the script.
	index int
	// stage is what the reader hands out next.
	stage int
	// callIndex numbers the calls within the turn.
	callIndex int
	// callCount is how many calls the turn held before the reader
	// started handing them out. The reader drains the calls as it goes,
	// so the count is what says whether the turn asked for any.
	callCount int
}

// newReader returns a reader for a scripted turn.
func newReader(turn Turn, index int) *reader {
	return &reader{turn: turn, index: index, callCount: len(turn.Calls)}
}

// Next returns the next piece of the scripted response.
func (r *reader) Next() (poo.StreamEvent, error) {
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
			if len(r.turn.Calls) == 0 {
				r.stage++
				continue
			}
			call := r.turn.Calls[0]
			r.turn.Calls = r.turn.Calls[1:]
			r.callIndex++
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("pootest_%d_%d", r.index, r.callIndex)
			}
			return poo.StreamEvent{
				Kind: "tool_call",
				ToolCall: &poo.ToolCallDelta{
					Index:     r.callIndex - 1,
					ID:        id,
					Name:      call.Name,
					Arguments: call.Arguments,
					Complete:  true,
				},
			}, nil
		case 3:
			r.stage++
			finish := r.turn.Finish
			if finish == "" {
				// A turn that asked for tool calls finishes as a tool
				// call, the way a real provider reports it. The count
				// is the turn's own, because the reader has already
				// drained the calls it holds.
				if r.callCount > 0 {
					finish = "tool_calls"
				} else {
					finish = "stop"
				}
			}
			return poo.StreamEvent{Kind: "finish", FinishReason: poo.FinishReason(finish)}, nil
		case 4:
			r.stage++
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
func (r *reader) Close() error { return nil }
