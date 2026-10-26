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
	// filters are the text filters the quirks added, in order. The
	// parser is the last of them, so it reads the text the others
	// passed on.
	filters []textFilter
	// ctx is the request's context. A stream that ends because the
	// context ended reports the context's error, not a network failure.
	ctx context.Context
	// parent is the context the transport's own timeout was applied to.
	// A stream that the run's context ended reports the parent's error;
	// a stream that the transport's timeout ended is a network failure.
	parent context.Context
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
	// sawText reports that the response carried text.
	sawText bool
	// sawToolCall reports that the response carried a tool call.
	sawToolCall bool
	// deferParsed reports that the calls the model writes as text wait
	// for a native call that would win over them.
	deferParsed bool
	// deferred holds the calls the model wrote as text, waiting to see
	// whether the response carries a native call.
	deferred []poo.StreamEvent
	// emptyToolCalls reports that the model asked for tool calls and
	// sent none. The failure is reported when the response ends, so the
	// usage the endpoint sends after the finish reason is not lost.
	emptyToolCalls bool
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
	// The calls the model wrote as text wait for a native call that
	// would win over them. The endpoint sent one, so the calls the
	// model wrote are not the ones it made.
	flushed := flushFilters(r.filters)
	if r.sawToolCall {
		flushed = withoutToolCalls(flushed)
	} else {
		r.pending = append(r.pending, r.deferred...)
	}
	r.deferred = nil
	r.pending = append(r.pending, flushed...)
	r.pending = r.appendUsage(r.pending)
	// The model asked for tool calls and sent none: the response is the
	// server's own failure, and it is retried like one. The failure is
	// reported after the usage, so the tokens the attempt cost are
	// counted.
	if r.emptyToolCalls && !r.sawText && !r.sawToolCall {
		r.err = &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the model asked for tool calls but sent none",
		}
	}
	if ev, ok := r.take(); ok {
		return ev, nil
	}
	if r.err != nil {
		return poo.StreamEvent{}, r.err
	}
	return poo.StreamEvent{}, io.EOF
}

// withoutToolCalls drops the tool call events from a list of events.
//
// The endpoint sent a call of its own, so a call the model wrote as
// text is not one the model made.
func withoutToolCalls(events []poo.StreamEvent) []poo.StreamEvent {
	out := make([]poo.StreamEvent, 0, len(events))
	for _, ev := range events {
		if ev.Kind == "tool_call" {
			continue
		}
		out = append(out, ev)
	}
	return out
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
// names it, and the kind the failure had before the reclassification
// stays in the error's code, where the plan keeps it.
func (r *streamReader) contextError(err error) error {
	if r.parent != nil && r.parent.Err() != nil {
		return r.parent.Err()
	}
	if r.ctx != nil && r.ctx.Err() != nil {
		kind := poo.ProviderKindNetwork
		if r.ttftSet {
			return &poo.ProviderError{
				Kind:    poo.ProviderKindInterruptedStream,
				Code:    string(kind),
				Message: r.ctx.Err().Error(),
			}
		}
		return &poo.ProviderError{Kind: kind, Message: r.ctx.Err().Error()}
	}
	return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
}

// providerError turns an error chunk into a provider error.
//
// The chunk carries no status, so its class comes from the code and the
// message, and the server's own failure is the default.
func (r *streamReader) providerError(body *errorBody) error {
	code := codeString(body.Code)
	message := body.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return &poo.ProviderError{
		Kind:    failureKind(0, code, body.Type, message, poo.ProviderKindServer),
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
			r.sawText = true
			// The filters own the text: they strip the call markup and
			// the thinking tags, and they emit the prose themselves.
			// Emitting the raw delta as well would double the prose and
			// leave the markup in the model's history.
			for _, ev := range applyFilters(r.filters, delta.Content) {
				if ev.Kind == "tool_call" && r.deferParsed {
					// The call the model wrote as text waits: a native
					// call wins over it.
					r.deferred = append(r.deferred, ev)
					continue
				}
				out = append(out, ev)
			}
		}

		for _, tc := range delta.ToolCalls {
			// A tool call is the first token too: a turn that only asks
			// for a tool call still has a time to its first token.
			r.markFirstToken()
			r.sawToolCall = true
			// The native call wins over the calls the model wrote as
			// text, so the parsed ones are dropped.
			r.deferred = nil
			complete := isCompleteJSON(tc.Function.Arguments)
			out = append(out, poo.StreamEvent{
				Kind: "tool_call",
				ToolCall: &poo.ToolCallDelta{
					Index:     tc.Index,
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: []byte(tc.Function.Arguments),
					Complete:  complete,
					// The endpoint sent the call in its own field.
					Origin: poo.CallOriginWire,
				},
			})
		}

		if choice.FinishReason != "" {
			if choice.FinishReason == "tool_calls" && !r.sawText && !r.sawToolCall {
				// The model asked for tool calls and sent none. The
				// response is the server's own failure, and it is
				// retried like one. The failure is reported when the
				// response ends, so the usage chunk that follows the
				// finish reason is not lost.
				r.emptyToolCalls = true
			}
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
