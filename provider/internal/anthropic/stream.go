package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// streamEvent is the wire shape of one SSE data payload.
type streamEvent struct {
	Type string `json:"type"`

	// Message is the message a stream opens with.
	Message *struct {
		Usage *Usage `json:"usage"`
	} `json:"message"`
	// Usage is the accounting the closing events report.
	Usage *Usage `json:"usage"`
	// Delta is the delta of a content block's piece, and of the message's
	// own close: the same field carries both, told apart by the event's
	// type.
	Delta *struct {
		// StopReason is why the message ended, in the delta that closes
		// it.
		StopReason string `json:"stop_reason"`
		// Type is the kind of a block delta.
		Type string `json:"type"`
		// Text is a text delta.
		Text string `json:"text"`
		// Thinking is a thinking delta.
		Thinking string `json:"thinking"`
		// Signature is the signature of a thinking block, complete in the
		// delta that carries it.
		Signature string `json:"signature"`
		// PartialJSON is a piece of a tool use's arguments.
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`

	// Index is the block the event belongs to.
	Index int `json:"index"`
	// ContentBlock is the block a stream opens.
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`

	// Error is a failure the stream reports mid-flight.
	Error *support.ErrorBody `json:"error"`
}

// Usage is the accounting a stream reports.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// Reader reads a Messages SSE answer.
type Reader struct {
	// closer releases the response and the request's bound.
	closer support.Closer
	frames *support.Frames
	// ctx is the request's context. A stream that ends because the context
	// ended reports the context's error, not a network failure.
	ctx context.Context
	// parent is the context the transport's own timeout was applied to.
	parent context.Context
	// pending holds events the reader has decoded but not yet returned.
	pending []poo.StreamItem
	// done reports that the stream is finished.
	done bool
	// err holds the failure that ended the stream.
	err error
	// ttft is the time to the first streamed token.
	ttft support.TTFT
	// usage holds the usage report, so a stream that fails before its last
	// event arrives can still report what it knows.
	usage *poo.UsageReport
	// usageSent reports that the usage report has been produced.
	usageSent bool
	// calls holds the tool calls the blocks have made so far, by the index
	// of the block that carries them, so the arguments a call accumulates
	// stay with the call they belong to.
	calls map[int]*poo.ToolCallDelta
	// signatures holds the signatures the thinking blocks carried, in the
	// order their blocks opened.
	signatures []string
}

// NewReader returns a reader over the answer a request opened.
func NewReader(stream support.Stream) *Reader {
	return &Reader{
		closer: support.Closer{Body: stream.Body, Cancel: stream.Cancel},
		frames: support.NewFrames(support.SSEScanner(stream.Reader)),
		ctx:    stream.Context,
		parent: stream.Parent,
		ttft:   stream.TTFT,
		calls:  map[int]*poo.ToolCallDelta{},
	}
}

// Next returns the next event.
func (r *Reader) Next() (poo.StreamItem, error) {
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

// dispatch turns one event payload into the events it carries.
//
// An event whose payload does not parse is skipped, because an event the
// reader cannot read is not worth failing the run over.
func (r *Reader) dispatch(payload []byte) error {
	var event streamEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil
	}
	switch event.Type {
	case "error":
		if event.Error == nil {
			return nil
		}
		r.done = true
		err := r.providerError(event.Error)
		r.err = err
		return err
	case "message_start":
		if event.Message != nil && event.Message.Usage != nil {
			r.recordUsage(event.Message.Usage)
		}
	case "content_block_start":
		if event.ContentBlock == nil {
			return nil
		}
		switch event.ContentBlock.Type {
		case "tool_use":
			r.ttft.Mark()
			call := &poo.ToolCallDelta{
				Index: event.Index,
				ID:    event.ContentBlock.ID,
				Name:  event.ContentBlock.Name,
				// The endpoint sent the call in its own block.
				Origin: poo.CallOriginWire,
			}
			r.calls[event.Index] = call
			r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: call})
		}
	case "content_block_delta":
		if event.Delta == nil {
			return nil
		}
		switch event.Delta.Type {
		case "text_delta":
			if event.Delta.Text != "" {
				r.ttft.Mark()
				r.pending = append(r.pending, poo.StreamItem{
					Kind:  "delta",
					Delta: &poo.Delta{Kind: "text", Text: event.Delta.Text},
				})
			}
		case "thinking_delta":
			if event.Delta.Thinking != "" {
				r.ttft.Mark()
				r.pending = append(r.pending, poo.StreamItem{
					Kind:  "delta",
					Delta: &poo.Delta{Kind: "thinking", Text: event.Delta.Thinking},
				})
			}
		case "signature_delta":
			if event.Delta.Signature != "" {
				r.signatures = append(r.signatures, event.Delta.Signature)
			}
		case "input_json_delta":
			call, ok := r.calls[event.Index]
			if !ok || event.Delta.PartialJSON == "" {
				return nil
			}
			r.ttft.Mark()
			call.Arguments = append(call.Arguments, event.Delta.PartialJSON...)
			out := *call
			out.Arguments = append([]byte(nil), call.Arguments...)
			out.Complete = support.IsCompleteJSON(string(out.Arguments))
			r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: &out})
		}
	case "content_block_stop":
		if call, ok := r.calls[event.Index]; ok && !call.Complete {
			out := *call
			out.Arguments = append([]byte(nil), call.Arguments...)
			out.Complete = true
			r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: &out})
		}
	case "message_delta":
		if event.Usage != nil {
			r.recordUsage(event.Usage)
		}
		if event.Delta != nil && event.Delta.StopReason != "" {
			r.pending = append(r.pending, poo.StreamItem{
				Kind:         "finish",
				FinishReason: finish(event.Delta.StopReason),
			})
		}
	case "message_stop":
		r.done = true
	case "ping":
	}
	return nil
}

// finish maps the API's stop reason to the reason the core names.
func finish(reason string) poo.FinishReason {
	switch reason {
	case "max_tokens":
		return poo.FinishLength
	case "tool_use":
		return poo.FinishToolCalls
	default:
		return poo.FinishStop
	}
}

// recordUsage folds a usage report into the one the reader holds.
//
// The report opens with the input the request cost and closes with the
// output the answer cost, so each report fills in what it carries.
func (r *Reader) recordUsage(u *Usage) {
	if r.usage == nil {
		r.usage = &poo.UsageReport{}
	}
	if u.InputTokens > 0 {
		r.usage.InputTokens = u.InputTokens
	}
	if u.OutputTokens > 0 {
		r.usage.OutputTokens = u.OutputTokens
	}
	if u.CacheReadInputTokens > 0 {
		cached := u.CacheReadInputTokens
		r.usage.CacheReadTokens = &cached
	}
}

// close ends the stream and returns the events that close it.
//
// The usage report is produced, so every request has one, and the signatures
// the thinking blocks carried are the provider blocks the response produced.
func (r *Reader) close() (poo.StreamItem, error) {
	r.pending = r.appendUsage(r.pending)
	if ev, ok := support.Take(&r.pending); ok {
		return ev, nil
	}
	if r.err != nil {
		return poo.StreamItem{}, r.err
	}
	return poo.StreamItem{}, io.EOF
}

// fail ends the stream with a failure.
//
// The usage report follows the failure, so a consumer sees the request's
// accounting even when the stream broke. The failure itself is returned by
// the next call.
func (r *Reader) fail(err error) (poo.StreamItem, error) {
	r.done = true
	r.err = err
	r.pending = r.appendUsage(r.pending)
	if ev, ok := support.Take(&r.pending); ok {
		return ev, nil
	}
	return poo.StreamItem{}, err
}

// appendUsage adds the usage report to a list of events.
func (r *Reader) appendUsage(evs []poo.StreamItem) []poo.StreamItem {
	if r.usageSent {
		return evs
	}
	if r.usage == nil {
		r.usage = &poo.UsageReport{}
	}
	r.usage.TTFTMS = r.ttft.MS()
	r.usageSent = true
	return append(evs, poo.StreamItem{Kind: "usage", Usage: r.usage})
}

// ReportUsage returns the usage report the reader holds.
//
// A stream that failed before its last event arrived still produced tokens,
// so the reader reports what it knows.
func (r *Reader) ReportUsage() *poo.UsageReport {
	return r.usage
}

// ReportProviderBlocks returns the thinking signatures the response carried.
//
// A signature belongs to the request that follows: the API checks a returned
// thinking block against the one it signed, so the block is required, and
// the harness carries it beside the thinking it signed.
func (r *Reader) ReportProviderBlocks() []poo.ProviderBlock {
	var out []poo.ProviderBlock
	for _, signature := range r.signatures {
		encoded, err := json.Marshal(signature)
		if err != nil {
			continue
		}
		out = append(out, poo.ProviderBlock{
			API:      poo.APIAnthropicMessages,
			Type:     "thinking_signature",
			Data:     encoded,
			Required: true,
		})
	}
	return out
}

// providerError turns an error event into a provider error.
//
// The event carries no status, so its class comes from the type and the
// message, and the server's own failure is the default.
func (r *Reader) providerError(body *support.ErrorBody) error {
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

// Close releases the response.
func (r *Reader) Close() error {
	return r.closer.Close()
}
