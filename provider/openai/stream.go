package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// streamChunk is the wire shape of one SSE data payload.
type streamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role string `json:"role"`
			// Content is the text delta.
			Content string `json:"content"`
			// ReasoningContent is the thinking delta, on the servers that
			// send it.
			ReasoningContent string `json:"reasoning_content"`
			// Reasoning is the thinking delta under its other name.
			Reasoning string `json:"reasoning"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message *struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		// PromptTokensDetails carries the cache-read count, when the
		// endpoint reports it.
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *errorBody `json:"error"`
}

// streamReader reads an SSE response.
type streamReader struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	parser  *parser
	// pending holds events the reader has decoded but not yet returned.
	pending []poo.StreamEvent
	// done reports that the stream is finished.
	done bool
	// err holds the failure that ended the stream.
	err error
	// closed reports that the body is closed.
	closed bool
	// started is when the request was sent, for the time to the first
	// token.
	started time.Time
	// ttft is the time to the first streamed token.
	ttft time.Duration
	// ttftSet reports that the first token has arrived.
	ttftSet bool
	// usageSent reports that the usage report has been produced.
	usageSent bool
}

// newScanner configures a scanner for SSE lines.
func (r *streamReader) init() {
	if r.scanner == nil {
		r.scanner = bufio.NewScanner(r.body)
		r.scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	}
}

// Next returns the next event.
func (r *streamReader) Next() (poo.StreamEvent, error) {
	r.init()

	if len(r.pending) > 0 {
		ev := r.pending[0]
		r.pending = r.pending[1:]
		return ev, nil
	}
	if r.done {
		if r.err != nil {
			return poo.StreamEvent{}, r.err
		}
		return poo.StreamEvent{}, io.EOF
	}

	for {
		if !r.scanner.Scan() {
			r.done = true
			if err := r.scanner.Err(); err != nil {
				r.err = &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
				return poo.StreamEvent{}, r.err
			}
			// The stream ended without a [DONE] marker. Whatever the
			// parser still holds is flushed, so a text-mode tool call
			// that arrived without a trailing newline is not lost.
			evs := r.parser.flush()
			evs = append(evs, r.usageEvent())
			if len(evs) > 0 {
				r.pending = append(r.pending, evs...)
				ev := r.pending[0]
				r.pending = r.pending[1:]
				return ev, nil
			}
			return poo.StreamEvent{}, io.EOF
		}

		line := r.scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// Comments and other SSE fields carry no payload.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			r.done = true
			evs := r.parser.flush()
			evs = append(evs, r.usageEvent())
			if len(evs) > 0 {
				r.pending = append(r.pending, evs...)
				ev := r.pending[0]
				r.pending = r.pending[1:]
				return ev, nil
			}
			return poo.StreamEvent{}, io.EOF
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// A chunk the transport cannot read is skipped rather than
			// failing the run.
			continue
		}
		// The first chunk of any kind is the endpoint's first token.
		r.markFirstToken()
		if chunk.Error != nil && chunk.Error.Message != "" {
			r.done = true
			r.err = &poo.ProviderError{
				Kind:    poo.ProviderKindServer,
				Message: chunk.Error.Message,
			}
			return poo.StreamEvent{}, r.err
		}

		events, err := r.events(chunk)
		if err != nil {
			r.done = true
			r.err = err
			return poo.StreamEvent{}, err
		}
		if len(events) > 0 {
			r.pending = append(r.pending, events...)
			ev := r.pending[0]
			r.pending = r.pending[1:]
			return ev, nil
		}
	}
}

// events turns one chunk into the events it carries.
func (r *streamReader) events(chunk streamChunk) ([]poo.StreamEvent, error) {
	var out []poo.StreamEvent

	for _, choice := range chunk.Choices {
		delta := choice.Delta
		// Some servers send a final chunk with a full message instead of
		// deltas.
		if choice.Message != nil {
			delta.Role = choice.Message.Role
			delta.Content = choice.Message.Content
			delta.ToolCalls = choice.Message.ToolCalls
		}

		if thinking := delta.ReasoningContent; thinking != "" {
			r.markFirstToken()
			out = append(out, poo.StreamEvent{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "thinking", Text: thinking},
			})
		} else if thinking := delta.Reasoning; thinking != "" {
			r.markFirstToken()
			out = append(out, poo.StreamEvent{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "thinking", Text: thinking},
			})
		}

		if delta.Content != "" {
			r.markFirstToken()
			out = append(out, poo.StreamEvent{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "text", Text: delta.Content},
			})
			// The parser reads the text, in text mode.
			if r.parser != nil {
				out = append(out, r.parser.feed(delta.Content)...)
			}
		}

		for _, tc := range delta.ToolCalls {
			// A tool call is the first token too: a turn that only asks
			// for a tool call still has a time to its first token.
			r.markFirstToken()
			complete := isCompleteJSON(tc.Function.Arguments)
			out = append(out, poo.StreamEvent{
				Kind: "tool_call",
				ToolCall: &poo.ToolCallDelta{
					Index:     tc.Index,
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: []byte(tc.Function.Arguments),
					Complete:  complete,
				},
			})
		}

		if choice.FinishReason != "" {
			out = append(out, poo.StreamEvent{
				Kind:         "finish",
				FinishReason: poo.FinishReason(choice.FinishReason),
			})
		}
	}

	if chunk.Usage != nil {
		u := &poo.UsageReport{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
			TTFTMS:       r.ttftMS(),
		}
		if d := chunk.Usage.PromptTokensDetails; d != nil {
			cached := d.CachedTokens
			u.CacheReadTokens = &cached
		}
		r.usageSent = true
		out = append(out, poo.StreamEvent{Kind: "usage", Usage: u})
	}

	return out, nil
}

// markFirstToken records the time the first token arrived.
func (r *streamReader) markFirstToken() {
	if r.ttftSet {
		return
	}
	r.ttftSet = true
	r.ttft = time.Since(r.started)
}

// ttftMS returns the time to the first token in milliseconds.
func (r *streamReader) ttftMS() float64 {
	if !r.ttftSet {
		return 0
	}
	return float64(r.ttft.Microseconds()) / 1000
}

// usageEvent returns the usage report for the request.
//
// Every request gets one, so a consumer can see the time to the first
// token even when the endpoint reports no token counts.
func (r *streamReader) usageEvent() poo.StreamEvent {
	if r.usageSent {
		return poo.StreamEvent{}
	}
	r.usageSent = true
	return poo.StreamEvent{
		Kind: "usage",
		Usage: &poo.UsageReport{
			TTFTMS: r.ttftMS(),
		},
	}
}

// Close releases the response.
func (r *streamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.body.Close()
}

// isCompleteJSON reports whether a string is a complete JSON object.
func isCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return json.Valid([]byte(s))
}
