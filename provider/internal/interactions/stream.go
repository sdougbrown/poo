package interactions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// maxToolArguments bounds the arguments one tool call accumulates.
//
// A stream that never ends its step, or ends it after a boundless run of
// fragments, is a stream a hostile endpoint can hold open and fill; the bound
// is what the request refuses rather than a limit a model hits.
const maxToolArguments = 4 << 20

// event is the wire shape of one SSE payload.
//
// Every event names its own kind in the payload's event_type, which is the
// field the API's own grammar is written in; the SSE frame's own event: line
// carries the same name and is not read.
type event struct {
	EventType string `json:"event_type"`
	// Index is the step the event belongs to.
	Index int `json:"index"`
	// Step is the step a start event opens.
	Step *stepStart `json:"step"`
	// Delta is the piece of the step a delta event carries.
	Delta *delta `json:"delta"`
	// Interaction is the interaction a created or completed event carries.
	Interaction *interaction `json:"interaction"`
	// Error is a failure the stream reports mid-flight.
	Error *support.ErrorBody `json:"error"`
}

// stepStart is the step a start event names.
type stepStart struct {
	Type string `json:"type"`
	// ID, Name and Arguments carry a function call, whose arguments are
	// empty here and arrive as fragments.
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// delta is one piece of a step.
type delta struct {
	Type string `json:"type"`
	// Text is a text piece of a model output.
	Text string `json:"text"`
	// Content is the part a thought summary carries.
	Content *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	// Signature is the signature a thought carries, one piece at a time.
	Signature string `json:"signature"`
	// Arguments is a piece of a function call's arguments.
	Arguments string `json:"arguments"`
}

// interaction is the interaction an event reports on.
type interaction struct {
	// ID is the interaction's id.
	ID string `json:"id"`
	// Status is how the interaction ended: "completed", "requires_action"
	// or "incomplete". A created event reports "in_progress".
	Status string `json:"status"`
	// Usage is the accounting of a completed interaction.
	Usage *Usage `json:"usage"`
}

// Usage is the accounting an interaction reports.
type Usage struct {
	// TotalInputTokens is the whole prompt, the tokens a cache served
	// included.
	TotalInputTokens int `json:"total_input_tokens"`
	// TotalOutputTokens is what the model wrote, the thought tokens
	// excluded.
	TotalOutputTokens int `json:"total_output_tokens"`
	// TotalCachedTokens is the part of the prompt a cache served.
	TotalCachedTokens int `json:"total_cached_tokens"`
	// TotalThoughtTokens is what the model thought.
	TotalThoughtTokens int `json:"total_thought_tokens"`
}

// Reader reads an Interactions SSE answer.
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
	// usage holds the usage report, so a stream that fails before its
	// interaction completed can still report what it knows.
	usage *poo.UsageReport
	// usageSent reports that the usage report has been produced.
	usageSent bool
	// calls holds the tool calls the steps have made so far, by the index of
	// the step that carries them, so the arguments a call accumulates stay
	// with the call they belong to.
	calls map[int]*poo.ToolCallDelta
	// signature holds the first thought signature the response carried.
	//
	// The core attaches one signature to the one thinking block it keeps, so
	// the first is the one the next request carries back. A later signature
	// has no block of its own to ride, and reporting it as required data
	// would end the session on the next turn.
	signature string
	// status is how the interaction ended, and it is what says whether the
	// stream finished at all: a stream that ends without it was cut off.
	status string
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
// An event the reader cannot read is skipped, because an event the reader
// cannot read is not worth failing the run over.
func (r *Reader) dispatch(payload []byte) error {
	var ev event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil
	}
	switch ev.EventType {
	case "step.start":
		if ev.Step == nil {
			return nil
		}
		if ev.Step.Type != StepFunctionCall {
			return nil
		}
		r.ttft.Mark()
		call := &poo.ToolCallDelta{
			Index: ev.Index,
			ID:    ev.Step.ID,
			Name:  ev.Step.Name,
			// The endpoint sent the call in its own step.
			Origin: poo.CallOriginWire,
		}
		r.calls[ev.Index] = call
		// The event carries a copy, not the call itself: the call is the one
		// the fragments accumulate into, and an event that shares it would
		// change as the step goes on.
		out := *call
		r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: &out})
	case "step.delta":
		return r.dispatchDelta(ev)
	case "step.stop":
		call, ok := r.calls[ev.Index]
		if !ok || call.Complete {
			return nil
		}
		// The step ended. What it accumulated is whole now, and the core has
		// already seen every fragment of it.
		call.Complete = true
		out := *call
		out.Arguments = nil
		out.Complete = true
		r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: &out})
	case "interaction.completed":
		if ev.Interaction == nil {
			return nil
		}
		if ev.Interaction.Usage != nil {
			r.recordUsage(ev.Interaction.Usage)
		}
		finish, err := finishOf(ev.Interaction.Status)
		if err != nil {
			return err
		}
		r.status = ev.Interaction.Status
		r.pending = append(r.pending, poo.StreamItem{Kind: "finish", FinishReason: finish})
	case "error":
		if ev.Error == nil {
			return nil
		}
		return r.providerError(ev.Error)
	}
	return nil
}

// dispatchDelta turns one piece of a step into the event it carries.
func (r *Reader) dispatchDelta(ev event) error {
	if ev.Delta == nil {
		return nil
	}
	switch ev.Delta.Type {
	case "text":
		if ev.Delta.Text != "" {
			r.ttft.Mark()
			r.pending = append(r.pending, poo.StreamItem{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "text", Text: ev.Delta.Text},
			})
		}
	case "thought_summary":
		text := ""
		if ev.Delta.Content != nil && ev.Delta.Content.Type == "text" {
			text = ev.Delta.Content.Text
		}
		if text != "" {
			r.ttft.Mark()
			r.pending = append(r.pending, poo.StreamItem{
				Kind:  "delta",
				Delta: &poo.Delta{Kind: "thinking", Text: text},
			})
		}
	case "thought_signature":
		if ev.Delta.Signature != "" && r.signature == "" {
			r.signature = ev.Delta.Signature
		}
	case "arguments_delta":
		call, ok := r.calls[ev.Index]
		if !ok || ev.Delta.Arguments == "" {
			return nil
		}
		r.ttft.Mark()
		call.Arguments = append(call.Arguments, ev.Delta.Arguments...)
		if len(call.Arguments) > maxToolArguments {
			return &poo.ProviderError{
				Kind:    poo.ProviderKindServer,
				Message: "the tool arguments a step accumulated exceed the bound a request accepts",
			}
		}
		// Only the part the core has not seen is emitted, because the core
		// assembles a call by appending its fragments.
		out := *call
		out.Arguments = []byte(ev.Delta.Arguments)
		out.Complete = support.IsCompleteJSON(string(call.Arguments))
		r.pending = append(r.pending, poo.StreamItem{Kind: "tool_call", ToolCall: &out})
	}
	return nil
}

// finishOf maps the status an interaction ended with to the reason the core
// names.
//
// A status the API has for a failed turn is a failure: the answer is not a
// short one, and the run must not read it as one.
func finishOf(status string) (poo.FinishReason, error) {
	switch status {
	case "completed":
		return poo.FinishStop, nil
	case "requires_action":
		return poo.FinishToolCalls, nil
	case "incomplete":
		return poo.FinishLength, nil
	}
	return "", &poo.ProviderError{
		Kind:    poo.ProviderKindServer,
		Message: "the interaction ended with status " + status,
	}
}

// recordUsage records the token accounting of a completed interaction.
//
// The input count is the whole prompt, the tokens a cache served included,
// which is what the run's budget counts. The answer's count takes the thought
// tokens with it: the API reports what the model wrote without them, and the
// thought tokens are the answer's cost that the model spent them on.
func (r *Reader) recordUsage(u *Usage) {
	r.usage = &poo.UsageReport{
		InputTokens:  u.TotalInputTokens,
		OutputTokens: u.TotalOutputTokens + u.TotalThoughtTokens,
	}
	if u.TotalCachedTokens > 0 {
		cached := u.TotalCachedTokens
		r.usage.CacheReadTokens = &cached
	}
}

// close ends the stream and returns the events that close it.
//
// A stream whose interaction never completed is an interrupted turn, not a
// short answer: the endpoint stopped talking before it said what became of the
// interaction, and a turn that looks finished would carry a half answer into
// the next request.
func (r *Reader) close() (poo.StreamItem, error) {
	if r.status == "" {
		r.err = &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the stream ended before the interaction completed",
		}
	}
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
// accounting even when the stream broke. The failure itself is returned by the
// next call.
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
// A stream that failed before its interaction completed still produced tokens,
// so the reader reports what it knows.
func (r *Reader) ReportUsage() *poo.UsageReport {
	return r.usage
}

// ReportProviderBlocks returns the thought signature the response carried.
//
// A signature belongs to the request that follows: the API checks a returned
// thought against the one it signed, so the block is required, and the harness
// carries it beside the thinking it signed. The first signature is the one
// reported, because the core keeps one thinking block and attaches one
// companion to it.
func (r *Reader) ReportProviderBlocks() []poo.ProviderBlock {
	if r.signature == "" {
		return nil
	}
	encoded, err := json.Marshal(r.signature)
	if err != nil {
		return nil
	}
	return []poo.ProviderBlock{{
		API:      poo.APIGoogleInteractions,
		Type:     "thought_signature",
		Data:     encoded,
		Required: true,
	}}
}

// providerError turns an error event into a provider error.
//
// The event carries no HTTP status, so the class starts from the code: the
// API's codes are its own words, and the shared table reads a status. The
// table below is the status each of the API's documented codes stands for, and
// a code outside it is left to the message and the table's own needles.
func (r *Reader) providerError(body *support.ErrorBody) error {
	code := support.CodeString(body.Code)
	message := body.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return support.NewProviderError(codeStatus(code), code, body.Type, message, poo.ProviderKindServer)
}

// codeStatus is the HTTP status the API's error codes stand for.
//
// The reference lists both spellings for the codes an HTTP failure also
// carries: the API's own name, and the snake_case name of the status. Both are
// here, because a stream's error event may carry either.
func codeStatus(code string) int {
	switch strings.ToLower(code) {
	case "authentication", "unauthenticated":
		return http.StatusUnauthorized
	case "permission_denied":
		return http.StatusForbidden
	case "payment_required":
		return http.StatusPaymentRequired
	case "rate_limit_exceeded", "quota_exceeded", "too_many_requests", "resource_exhausted":
		return http.StatusTooManyRequests
	case "deadline_exceeded", "gateway_timeout":
		return http.StatusGatewayTimeout
	case "service_unavailable":
		return http.StatusServiceUnavailable
	case "api_error":
		return http.StatusInternalServerError
	}
	return 0
}

// Close releases the response.
func (r *Reader) Close() error {
	return r.closer.Close()
}
