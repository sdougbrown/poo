package openai

import (
	"bufio"
	"context"
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
	// ctx is the request's context. A stream that ends because the
	// context ended reports the context's error, not a network failure.
	ctx context.Context
	// cancel releases the request's context, when the transport made one.
	cancel context.CancelFunc
	// pending holds events the reader has decoded but not yet returned.
	pending []poo.StreamEvent
	// data holds the `data:` lines of the event being read. An event's
	// payload may span several lines, which the SSE grammar joins with a
	// newline.
	data []string
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
	// usage holds the usage report, so a stream that fails before its
	// usage chunk arrives can still report what it knows.
	usage *poo.UsageReport
}

// newSSEScanner returns a scanner for SSE lines. A single event line can
// be large, so the line limit is 16 MiB.
func newSSEScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return sc
}

// init makes the scanner when the reader was built without one.
func (r *streamReader) init() {
	if r.scanner == nil {
		r.scanner = newSSEScanner(r.body)
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
				return r.fail(r.contextError(err))
			}
			// The stream ended without a [DONE] marker. Whatever the
			// parser still holds is flushed, so a text-mode tool call
			// that arrived without a trailing newline is not lost.
			return r.close()
		}

		line := r.scanner.Text()
		if line == "" {
			// A blank line ends the event.
			if err := r.dispatch(); err != nil {
				return r.fail(err)
			}
			if ev, ok := r.take(); ok {
				return ev, nil
			}
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
			return r.close()
		}

		// A server that separates its events with a single newline
		// never sends the blank line that ends an event, so the event
		// is dispatched as soon as the next one starts. An event whose
		// payload is not valid JSON yet is kept, because it may still
		// be a payload spread over several data lines.
		complete := len(r.data) > 0 && json.Valid([]byte(strings.Join(r.data, "\n")))
		if complete {
			if err := r.dispatch(); err != nil {
				return r.fail(err)
			}
			r.data = append(r.data, payload)
			if ev, ok := r.take(); ok {
				return ev, nil
			}
			continue
		}
		r.data = append(r.data, payload)
	}
}

// take removes the next queued event and reports whether there was one.
func (r *streamReader) take() (poo.StreamEvent, bool) {
	if len(r.pending) == 0 {
		return poo.StreamEvent{}, false
	}
	ev := r.pending[0]
	r.pending = r.pending[1:]
	return ev, true
}

// dispatch turns the event the reader has collected into events.
//
// The events are queued, so they leave the reader in the order the
// endpoint sent them. An event whose payload does not parse is skipped,
// because a chunk the transport cannot read is not worth failing the run
// over.
func (r *streamReader) dispatch() error {
	if len(r.data) == 0 {
		return nil
	}
	payload := strings.Join(r.data, "\n")
	r.data = nil

	var chunk streamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil
	}
	if chunk.Error != nil {
		r.done = true
		err := r.providerError(chunk.Error)
		r.err = err
		return err
	}

	events, err := r.events(chunk)
	if err != nil {
		r.done = true
		r.err = err
		return err
	}
	r.pending = append(r.pending, events...)
	return nil
}

// close ends the stream and returns the events that close it.
//
// The parser is flushed, so a text-mode tool call that arrived without a
// trailing newline is not lost, and the usage report is produced, so every
// request has one.
func (r *streamReader) close() (poo.StreamEvent, error) {
	if err := r.dispatch(); err != nil {
		// The event ended the stream. The usage is still reported.
		return r.fail(err)
	}
	r.pending = append(r.pending, r.parser.flush()...)
	r.pending = r.appendUsage(r.pending)
	if ev, ok := r.take(); ok {
		return ev, nil
	}
	if r.err != nil {
		return poo.StreamEvent{}, r.err
	}
	return poo.StreamEvent{}, io.EOF
}

// fail ends the stream with a failure.
//
// A complete event still waiting for its dispatch is queued first, so
// it is not lost with the connection. An in-band error it carries is the
// more specific failure and replaces the read error. The usage report
// follows, so a consumer sees the request's accounting even when the
// stream broke. The failure itself is returned by the next call.
func (r *streamReader) fail(err error) (poo.StreamEvent, error) {
	if derr := r.dispatch(); derr != nil {
		err = derr
	}
	r.done = true
	r.err = err
	r.pending = r.appendUsage(r.pending)
	if ev, ok := r.take(); ok {
		return ev, nil
	}
	return poo.StreamEvent{}, err
}

// ReportUsage returns the usage report the reader holds.
//
// A stream that failed before its usage chunk arrived still produced
// tokens, so the transport reports what it knows.
func (r *streamReader) ReportUsage() *poo.UsageReport {
	return r.usage
}

// contextError turns a read failure into the context's error when the
// context is what ended the request.
//
// A stream that already carried a delta cannot be retried: the model has
// seen part of the answer. The failure says so, so the settle reason
// names it.
func (r *streamReader) contextError(err error) error {
	if r.ctx != nil && r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	kind := poo.ProviderKindNetwork
	if r.ttftSet {
		kind = poo.ProviderKindInterruptedStream
	}
	return &poo.ProviderError{Kind: kind, Message: err.Error()}
}

// providerError turns an error chunk into a provider error.
func (r *streamReader) providerError(body *errorBody) error {
	code := codeString(body.Code)
	message := body.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return &poo.ProviderError{
		Kind:    classifyError(code, message, poo.ProviderKindServer),
		Message: message,
		Code:    code,
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
			if r.parser.on() {
				// The parser owns the text: it strips the call markup
				// and emits the prose itself. Emitting the raw delta as
				// well would double the prose and leave the markup in
				// the model's history.
				out = append(out, r.parser.feed(delta.Content)...)
			} else {
				out = append(out, poo.StreamEvent{
					Kind:  "delta",
					Delta: &poo.Delta{Kind: "text", Text: delta.Content},
				})
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
		r.usage = u
		r.usageSent = true
		out = append(out, poo.StreamEvent{Kind: "usage", Usage: u})
	}

	return out, nil
}

// markFirstToken records the time the first token arrived.
//
// A token is a non-empty text or thinking delta, or a tool call. A chunk
// that only announces the assistant's role carries no token, so it does
// not start the clock.
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

// appendUsage adds the usage report to a list of events.
//
// Every request gets one, so a consumer can see the time to the first
// token even when the endpoint reports no token counts. A request whose
// endpoint reported its own usage already has one.
func (r *streamReader) appendUsage(evs []poo.StreamEvent) []poo.StreamEvent {
	if r.usageSent {
		return evs
	}
	r.ensureUsage()
	return append(evs, poo.StreamEvent{Kind: "usage", Usage: r.usage})
}

// ensureUsage builds the usage report a request gets when the endpoint
// reported none.
func (r *streamReader) ensureUsage() {
	if r.usage != nil {
		return
	}
	r.usage = &poo.UsageReport{TTFTMS: r.ttftMS()}
	r.usageSent = true
}

// Close releases the response.
func (r *streamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
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
