package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
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
	Error *support.ErrorBody `json:"error"`
}

// streamReader reads an SSE response.
type streamReader struct {
	body   io.ReadCloser
	frames *support.Frames
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
	pending []poo.StreamItem
	// done reports that the stream is finished.
	done bool
	// err holds the failure that ended the stream.
	err error
	// closed reports that the body is closed.
	closed bool
	// ttft is the time to the first streamed token.
	ttft support.TTFT
	// usageSent reports that the usage report has been produced.
	usageSent bool
	// usage holds the usage report, so a stream that fails before its
	// usage chunk arrives can still report what it knows.
	usage *poo.UsageReport
	// sawToolCall reports that the response carried a tool call.
	sawToolCall bool
	// deferParsed reports that the calls the model writes as text wait
	// for a native call that would win over them.
	deferParsed bool
	// deferred holds the calls the model wrote as text, waiting to see
	// whether the response carries a native call.
	deferred []poo.StreamItem
}

// init makes the scanner when the reader was built without one.
func (r *streamReader) init() {
	if r.frames == nil {
		r.frames = support.NewFrames(support.SSEScanner(r.body))
	}
}

// Next returns the next event.
func (r *streamReader) Next() (poo.StreamItem, error) {
	r.init()

	if len(r.pending) > 0 {
		ev := r.pending[0]
		r.pending = r.pending[1:]
		return ev, nil
	}
	if r.done {
		if r.err != nil {
			return poo.StreamItem{}, r.err
		}
		return poo.StreamItem{}, io.EOF
	}

	for {
		payload, err := r.frames.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				r.done = true
				// The stream ended. Whatever the parser still holds is
				// flushed, so a text-mode tool call that arrived without a
				// trailing newline is not lost.
				return r.close()
			}
			return r.fail(support.ContextError(r.parent, r.ctx, r.ttft.Arrived(), err))
		}
		if err := r.dispatch(payload); err != nil {
			return r.fail(err)
		}
		if ev, ok := support.Take(&r.pending); ok {
			return ev, nil
		}
	}
}

// dispatch turns the event the reader has collected into events.
//
// The events are queued, so they leave the reader in the order the
// endpoint sent them. An event whose payload does not parse is skipped,
// because a chunk the transport cannot read is not worth failing the run
// over.
func (r *streamReader) dispatch(payload []byte) error {
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
func (r *streamReader) close() (poo.StreamItem, error) {
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
	if ev, ok := support.Take(&r.pending); ok {
		return ev, nil
	}
	if r.err != nil {
		return poo.StreamItem{}, r.err
	}
	return poo.StreamItem{}, io.EOF
}

// withoutToolCalls drops the tool call events from a list of events.
//
// The endpoint sent a call of its own, so a call the model wrote as
// text is not one the model made.
func withoutToolCalls(events []poo.StreamItem) []poo.StreamItem {
	out := make([]poo.StreamItem, 0, len(events))
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
// The usage report follows the failure, so a consumer sees the request's
// accounting even when the stream broke. The failure itself is returned by the
// next call.
func (r *streamReader) fail(err error) (poo.StreamItem, error) {
	r.done = true
	r.err = err
	r.pending = r.appendUsage(r.pending)
	if ev, ok := support.Take(&r.pending); ok {
		return ev, nil
	}
	return poo.StreamItem{}, err
}

// ReportUsage returns the usage report the reader holds.
//
// A stream that failed before its usage chunk arrived still produced
// tokens, so the transport reports what it knows.
func (r *streamReader) ReportUsage() *poo.UsageReport {
	return r.usage
}

// providerError turns an error chunk into a provider error.
//
// The chunk carries no status, so its class comes from the code and the
// message, and the server's own failure is the default.
func (r *streamReader) providerError(body *support.ErrorBody) error {
	code := support.CodeString(body.Code)
	message := body.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return &poo.ProviderError{
		Kind:    support.FailureKind(0, code, body.Type, message, poo.ProviderKindServer),
		Message: message,
		Code:    code,
	}
}

// events turns one chunk into the events it carries.
func (r *streamReader) events(chunk streamChunk) ([]poo.StreamItem, error) {
	var out []poo.StreamItem

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
			r.ttft.Mark()
			out = append(out, poo.StreamItem{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "thinking", Text: thinking},
			})
		} else if thinking := delta.Reasoning; thinking != "" {
			r.ttft.Mark()
			out = append(out, poo.StreamItem{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "thinking", Text: thinking},
			})
		}

		if delta.Content != "" {
			r.ttft.Mark()
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
			r.ttft.Mark()
			r.sawToolCall = true
			// The native call wins over the calls the model wrote as
			// text, so the parsed ones are dropped.
			r.deferred = nil
			complete := support.IsCompleteJSON(tc.Function.Arguments)
			out = append(out, poo.StreamItem{
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
			out = append(out, poo.StreamItem{
				Kind:         "finish",
				FinishReason: poo.FinishReason(choice.FinishReason),
			})
		}
	}

	if chunk.Usage != nil {
		u := &poo.UsageReport{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
			TTFTMS:       r.ttft.MS(),
		}
		if d := chunk.Usage.PromptTokensDetails; d != nil {
			cached := d.CachedTokens
			u.CacheReadTokens = &cached
		}
		r.usage = u
		r.usageSent = true
		out = append(out, poo.StreamItem{Kind: "usage", Usage: u})
	}

	return out, nil
}

// appendUsage adds the usage report to a list of events.
//
// Every request gets one, so a consumer can see the time to the first
// token even when the endpoint reports no token counts. A request whose
// endpoint reported its own usage already has one.
func (r *streamReader) appendUsage(evs []poo.StreamItem) []poo.StreamItem {
	if r.usageSent {
		return evs
	}
	r.ensureUsage()
	return append(evs, poo.StreamItem{Kind: "usage", Usage: r.usage})
}

// ensureUsage builds the usage report a request gets when the endpoint
// reported none.
func (r *streamReader) ensureUsage() {
	if r.usage != nil {
		return
	}
	r.usage = &poo.UsageReport{TTFTMS: r.ttft.MS()}
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
