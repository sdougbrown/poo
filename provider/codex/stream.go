package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// The response side.
//
// The endpoint sends the Responses API's events, with the Codex backend's
// own spelling of the terminal ones: response.done is response.completed,
// and response.failed carries the failure the run settles with.

// codexEvent is one SSE data payload.
type codexEvent struct {
	Type        string          `json:"type"`
	OutputIndex int             `json:"output_index"`
	Delta       string          `json:"delta"`
	Arguments   string          `json:"arguments"`
	Item        json.RawMessage `json:"item"`
	Response    *codexResponse  `json:"response"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	Error       *codexEventErr  `json:"error"`
}

// codexEventErr is the error an event or a response names.
type codexEventErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// codexItem is the output item an event adds or completes.
type codexItem struct {
	Type             string      `json:"type"`
	CallID           string      `json:"call_id"`
	Name             string      `json:"name"`
	Arguments        string      `json:"arguments"`
	Content          []codexPart `json:"content"`
	Summary          []codexPart `json:"summary"`
	EncryptedContent string      `json:"encrypted_content"`
}

// codexPart is one part of an item's content or summary.
type codexPart struct {
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

// text is the part's text, whether it is a summary part or a content
// part, and whether it holds an answer or a refusal.
func (p codexPart) text() string {
	if p.Text != "" {
		return p.Text
	}
	return p.Refusal
}

// codexResponse is the response a terminal event carries.
type codexResponse struct {
	Status string `json:"status"`
	// Output is the response's items, whole. The encrypted reasoning
	// payload may arrive only here.
	Output            []json.RawMessage `json:"output"`
	Error             *codexEventErr    `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *codexUsage `json:"usage"`
}

// codexUsage is the token accounting a terminal event carries.
type codexUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// slotKey names what one output index of the response produced.
//
// The kind is part of the key: a text answer, a reasoning summary and a
// tool call are three namespaces on the wire, and an event that names no
// index lands on an index another kind already took.
type slotKey struct {
	// index is the output index the event carried.
	index int
	// kind is what the slot holds: "text", "thinking" or "tool_call".
	kind string
}

// itemSlot is what one output index of the response produced so far.
//
// The endpoint streams a reasoning item, a message and a tool call as
// one slot each, keyed by the output index the deltas carry.
type itemSlot struct {
	// kind is what the slot holds: "text", "thinking" or "tool_call".
	kind string
	// text is the text or thinking the deltas assembled.
	text strings.Builder
	// arguments is the tool call's JSON, as the deltas assembled it.
	arguments strings.Builder
}

// streamReader reads a Codex SSE response.
type streamReader struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	// ctx is the request's context. A stream that ends because the
	// context ended reports the context's error, not a network failure.
	ctx context.Context
	// parent is the context the transport's own timeout was applied to.
	// A stream the run's context ended reports the parent's error; a
	// stream the transport's timeout ended is a network failure.
	parent context.Context
	// cancel releases the request's context, when the transport made one.
	cancel context.CancelFunc
	// pending holds the events the reader has decoded but not returned.
	pending []poo.StreamItem
	// data holds the `data:` lines of the event being read. An event's
	// payload may span several lines, which the SSE grammar joins with a
	// newline.
	data []string
	// slots maps an output index and a kind to what it has produced so
	// far.
	slots map[slotKey]*itemSlot
	// reasoning holds the reasoning items the response produced, in
	// order, so the core records them with the response's message.
	reasoning []poo.ProviderBlock
	// done reports that the stream is finished.
	done bool
	// sawTerminal reports that a terminal response event arrived.
	sawTerminal bool
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
	// terminal event arrives can still report what it knows.
	usage *poo.UsageReport
	// sawToolCall reports that the response carried a tool call.
	sawToolCall bool
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
	if r.slots == nil {
		r.slots = map[slotKey]*itemSlot{}
	}
}

// Next returns the next event.
func (r *streamReader) Next() (poo.StreamItem, error) {
	r.init()

	if ev, ok := r.take(); ok {
		return ev, nil
	}
	if r.done {
		return r.finishReading()
	}

	for {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return r.fail(r.contextError(err))
			}
			// The stream ended. Whatever event is still buffered is
			// dispatched, and a stream that never named its end fails.
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
			if r.done {
				return r.finishReading()
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
			if r.done {
				return r.finishReading()
			}
			continue
		}
		r.data = append(r.data, payload)
	}
}

// take removes the next queued event and reports whether there was one.
func (r *streamReader) take() (poo.StreamItem, bool) {
	if len(r.pending) == 0 {
		return poo.StreamItem{}, false
	}
	ev := r.pending[0]
	r.pending = r.pending[1:]
	return ev, true
}

// finishReading returns what is left of a stream that has ended.
func (r *streamReader) finishReading() (poo.StreamItem, error) {
	if r.err != nil {
		return poo.StreamItem{}, r.err
	}
	return poo.StreamItem{}, io.EOF
}

// dispatch turns the event the reader has collected into events.
//
// An event whose payload does not parse is skipped: a chunk the
// transport cannot read is not worth failing the run over, and the
// endpoint's own error event is what fails it.
func (r *streamReader) dispatch() error {
	if len(r.data) == 0 {
		return nil
	}
	payload := strings.Join(r.data, "\n")
	r.data = nil

	var ev codexEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return nil
	}
	if ev.Type == "" {
		return nil
	}
	items, err := r.events(ev)
	if err != nil {
		return err
	}
	r.pending = append(r.pending, items...)
	return nil
}

// close ends a stream whose bytes ran out.
//
// The event still buffered is dispatched first. A stream that never
// named its terminal response event is a protocol failure: the run
// cannot know whether the answer was complete, and the retry layer
// decides whether it may be asked for again.
func (r *streamReader) close() (poo.StreamItem, error) {
	if err := r.dispatch(); err != nil {
		return r.fail(err)
	}
	if !r.sawTerminal {
		return r.fail(&poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the stream ended before a terminal response event",
		})
	}
	if ev, ok := r.take(); ok {
		return ev, nil
	}
	return r.finishReading()
}

// fail ends the stream with a failure.
//
// The usage report follows, so a consumer sees the request's accounting
// even when the stream broke; the failure itself is returned by the next
// call.
func (r *streamReader) fail(err error) (poo.StreamItem, error) {
	r.done = true
	r.err = err
	if !r.usageSent {
		r.pending = append(r.pending, r.usageItem())
	}
	if ev, ok := r.take(); ok {
		return ev, nil
	}
	return poo.StreamItem{}, err
}

// ReportUsage returns the usage the response reported.
//
// A stream that failed before its terminal event still produced tokens,
// so the transport reports what it knows.
func (r *streamReader) ReportUsage() *poo.UsageReport {
	return r.usage
}

// ReportProviderBlocks returns the reasoning items the response
// produced, in order.
//
// The endpoint returns the reasoning it thought with as an item that is
// opaque to the harness: the item's summary is what the model wrote, and
// its encrypted payload is what has to travel back with the next
// request. The core records them with the response's message, so the
// next request gives them back.
func (r *streamReader) ReportProviderBlocks() []poo.ProviderBlock {
	return r.reasoning
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

// events turns one event into the events the reader queues.
func (r *streamReader) events(ev codexEvent) ([]poo.StreamItem, error) {
	switch ev.Type {
	case "response.output_item.added":
		return r.itemAdded(ev), nil
	case "response.output_text.delta", "response.refusal.delta":
		return r.textDelta(ev, "text"), nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return r.textDelta(ev, "thinking"), nil
	case "response.reasoning_summary_part.done":
		return r.thinkingGap(ev), nil
	case "response.function_call_arguments.delta":
		return r.callDelta(ev), nil
	case "response.function_call_arguments.done":
		return r.callDone(ev), nil
	case "response.output_item.done":
		return r.itemDone(ev), nil
	case "response.completed", "response.done", "response.incomplete":
		return r.terminal(ev)
	case "response.failed":
		r.sawTerminal = true
		r.done = true
		return nil, r.responseFailure(ev)
	case "error":
		r.sawTerminal = true
		r.done = true
		return nil, r.eventFailure(ev)
	}
	return nil, nil
}

// itemAdded starts the slot an output item announces.
//
// A tool call's slot carries the call on its first fragment, because the
// core creates a call from the fragment that names it and appends the
// arguments the later fragments carry.
func (r *streamReader) itemAdded(ev codexEvent) []poo.StreamItem {
	item, ok := decodeItem(ev.Item)
	if !ok {
		return nil
	}
	switch item.Type {
	case "reasoning":
		r.slotFor(ev.OutputIndex, "thinking")
	case "message":
		r.slotFor(ev.OutputIndex, "text")
	case "function_call":
		r.slotFor(ev.OutputIndex, "tool_call")
		r.markFirstToken()
		r.sawToolCall = true
		return []poo.StreamItem{{
			Kind: "tool_call",
			ToolCall: &poo.ToolCallDelta{
				Index:  ev.OutputIndex,
				ID:     item.CallID,
				Name:   item.Name,
				Origin: poo.CallOriginWire,
			},
		}}
	}
	return nil
}

// textDelta emits a text or thinking fragment.
func (r *streamReader) textDelta(ev codexEvent, kind string) []poo.StreamItem {
	if ev.Delta == "" {
		return nil
	}
	slot := r.slotFor(ev.OutputIndex, kind)
	if slot == nil {
		return nil
	}
	r.markFirstToken()
	slot.text.WriteString(ev.Delta)
	return []poo.StreamItem{{
		Kind:  "delta",
		Delta: &poo.Delta{Kind: kind, Text: ev.Delta},
	}}
}

// thinkingGap emits the break between two reasoning summary parts.
func (r *streamReader) thinkingGap(ev codexEvent) []poo.StreamItem {
	slot := r.slotFor(ev.OutputIndex, "thinking")
	if slot == nil || slot.text.Len() == 0 {
		return nil
	}
	slot.text.WriteString("\n\n")
	return []poo.StreamItem{{
		Kind:  "delta",
		Delta: &poo.Delta{Kind: "thinking", Text: "\n\n"},
	}}
}

// callDelta emits a tool call's argument fragment.
func (r *streamReader) callDelta(ev codexEvent) []poo.StreamItem {
	slot := r.slots[slotKey{ev.OutputIndex, "tool_call"}]
	if slot == nil || ev.Delta == "" {
		return nil
	}
	slot.arguments.WriteString(ev.Delta)
	return []poo.StreamItem{{
		Kind:     "tool_call",
		ToolCall: argumentFragment(slot, ev.OutputIndex, ev.Delta),
	}}
}

// callDone emits the argument text the deltas did not carry.
//
// The endpoint repeats the call's whole argument string when it is done.
// Only the part the core has not seen is emitted, because the core
// assembles a call by appending its fragments. An argument string that
// disagrees with the fragments is left alone for the same reason: there
// is no way to replace what the core already holds.
func (r *streamReader) callDone(ev codexEvent) []poo.StreamItem {
	slot := r.slots[slotKey{ev.OutputIndex, "tool_call"}]
	if slot == nil {
		return nil
	}
	have := slot.arguments.String()
	if ev.Arguments == "" || ev.Arguments == have || !strings.HasPrefix(ev.Arguments, have) {
		return nil
	}
	suffix := ev.Arguments[len(have):]
	slot.arguments.WriteString(suffix)
	return []poo.StreamItem{{
		Kind:     "tool_call",
		ToolCall: argumentFragment(slot, ev.OutputIndex, suffix),
	}}
}

// itemDone closes the slot an output item finishes.
//
// A server that only sends the finished item, without the deltas, is
// read here: the item's own text is emitted as the one delta the answer
// has. A reasoning item is held as a provider block, so the reasoning
// the endpoint encrypted can travel back with the next request.
func (r *streamReader) itemDone(ev codexEvent) []poo.StreamItem {
	item, ok := decodeItem(ev.Item)
	if !ok {
		return nil
	}
	switch item.Type {
	case "reasoning":
		if block, ok := reasoningBlock(ev.Item, item); ok {
			r.reasoning = append(r.reasoning, block)
		}
		slot := r.slotFor(ev.OutputIndex, "thinking")
		if slot == nil || slot.text.Len() > 0 {
			return nil
		}
		text := partsText(item.Summary)
		if text == "" {
			text = partsText(item.Content)
		}
		if text == "" {
			return nil
		}
		slot.text.WriteString(text)
		return []poo.StreamItem{{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "thinking", Text: text},
		}}
	case "message":
		slot := r.slotFor(ev.OutputIndex, "text")
		if slot == nil || slot.text.Len() > 0 {
			return nil
		}
		text := partsText(item.Content)
		if text == "" {
			return nil
		}
		slot.text.WriteString(text)
		return []poo.StreamItem{{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "text", Text: text},
		}}
	case "function_call":
		return r.finishCall(ev.OutputIndex, item)
	}
	return nil
}

// finishCall emits the arguments a finished call still owes the core.
//
// A call the endpoint never announced is announced here: the item names
// it, and the core needs the name before it can run anything.
func (r *streamReader) finishCall(index int, item codexItem) []poo.StreamItem {
	key := slotKey{index, "tool_call"}
	var items []poo.StreamItem
	if _, announced := r.slots[key]; !announced {
		r.markFirstToken()
		r.sawToolCall = true
		items = append(items, poo.StreamItem{
			Kind: "tool_call",
			ToolCall: &poo.ToolCallDelta{
				Index:  index,
				ID:     item.CallID,
				Name:   item.Name,
				Origin: poo.CallOriginWire,
			},
		})
	}
	slot := r.slotFor(index, "tool_call")
	defer delete(r.slots, key)
	arguments := item.Arguments
	have := slot.arguments.String()
	switch {
	case arguments == "" && have == "":
		// A call with no arguments is a call with an empty object: the
		// core parses what it assembles, and nothing is not valid JSON.
		slot.arguments.WriteString("{}")
		return append(items, poo.StreamItem{
			Kind:     "tool_call",
			ToolCall: argumentFragment(slot, index, "{}"),
		})
	case strings.HasPrefix(arguments, have):
		suffix := arguments[len(have):]
		if suffix == "" {
			return items
		}
		slot.arguments.WriteString(suffix)
		return append(items, poo.StreamItem{
			Kind:     "tool_call",
			ToolCall: argumentFragment(slot, index, suffix),
		})
	}
	return items
}

// argumentFragment is one piece of a tool call's arguments.
func argumentFragment(slot *itemSlot, index int, text string) *poo.ToolCallDelta {
	return &poo.ToolCallDelta{
		Index:     index,
		Arguments: []byte(text),
		Complete:  isCompleteJSON(slot.arguments.String()),
		Origin:    poo.CallOriginWire,
	}
}

// terminal ends the stream with the terminal response event's outcome.
//
// The response's status says why it stopped: a completed response is the
// model's answer, an incomplete one stopped on its output limit or on a
// reason the run has to know, and a failed one carries the failure. A
// status this transport does not know is read as an answer, because a
// provider that adds one has still said the response ended.
func (r *streamReader) terminal(ev codexEvent) ([]poo.StreamItem, error) {
	resp := ev.Response
	r.sawTerminal = true
	r.done = true
	if resp == nil {
		// The endpoint ended the response without describing it: the run
		// still gets the finish and the usage, because the stream did
		// end and the answer it carried is all there is.
		r.pending = append(r.pending, poo.StreamItem{Kind: "finish", FinishReason: r.finishReason(resp)})
		r.pending = append(r.pending, r.usageItem())
		return nil, nil
	}
	if resp.Usage != nil {
		r.setUsage(resp.Usage)
	}
	// The terminal response carries its items whole, and the reasoning
	// item's encrypted payload rides only there.
	if blocks, ok := terminalReasoning(resp.Output); ok {
		r.reasoning = blocks
	}

	switch resp.Status {
	case "incomplete":
		reason := ""
		if resp.IncompleteDetails != nil {
			reason = resp.IncompleteDetails.Reason
		}
		if reason != "max_output_tokens" {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindServer,
				Code:    reason,
				Message: "the response is incomplete: " + reason,
			}
		}
		r.pending = append(r.pending, poo.StreamItem{Kind: "finish", FinishReason: poo.FinishLength})
	case "failed", "cancelled":
		return nil, r.responseFailure(ev)
	default:
		r.pending = append(r.pending, poo.StreamItem{Kind: "finish", FinishReason: r.finishReason(resp)})
	}
	r.pending = append(r.pending, r.usageItem())
	return nil, nil
}

// finishReason returns why a response that ended stopped.
//
// A response that called a tool stopped to have it run; every other
// response that ended is the model's answer.
func (r *streamReader) finishReason(resp *codexResponse) poo.FinishReason {
	if r.sawToolCall {
		return poo.FinishToolCalls
	}
	return poo.FinishStop
}

// responseFailure is the failure a failed terminal event names.
func (r *streamReader) responseFailure(ev codexEvent) error {
	resp := ev.Response
	if resp != nil && resp.Usage != nil {
		r.setUsage(resp.Usage)
	}
	code, message := "", ""
	if resp != nil && resp.Error != nil {
		code, message = resp.Error.Code, resp.Error.Message
	}
	if message == "" {
		if resp != nil && resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
			message = "the response is incomplete: " + resp.IncompleteDetails.Reason
		} else {
			message = "the endpoint reported a failed response"
		}
	}
	if code == "" {
		code = ev.Code
	}
	return &poo.ProviderError{
		Kind:    failureKind(0, code, "", message, poo.ProviderKindServer),
		Message: message,
		Code:    code,
	}
}

// eventFailure is the failure an in-band error event names.
func (r *streamReader) eventFailure(ev codexEvent) error {
	code, message := ev.Code, ev.Message
	if ev.Error != nil {
		if ev.Error.Code != "" {
			code = ev.Error.Code
		}
		if ev.Error.Message != "" {
			message = ev.Error.Message
		}
	}
	if message == "" {
		message = "the endpoint reported an error"
	}
	return &poo.ProviderError{
		Kind:    failureKind(0, code, "", message, poo.ProviderKindServer),
		Message: message,
		Code:    code,
	}
}

// setUsage records the token accounting the terminal event carried.
//
// The endpoint's input count is the whole prompt, the tokens it served
// from its cache included, and poo's input count is the same number: the
// run's budget counts the prompt it sent. The cached tokens ride beside
// it, because a reader that wants the cache hit rate asks for them and
// not for a subtraction.
func (r *streamReader) setUsage(u *codexUsage) {
	var cacheRead *int
	if details := u.InputTokensDetails; details != nil {
		value := details.CachedTokens
		cacheRead = &value
	}
	r.usage = &poo.UsageReport{
		InputTokens:     u.InputTokens,
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: cacheRead,
	}
}

// usageItem returns the event that reports the request's usage.
//
// Every request gets one, so a consumer can see the time to the first
// token even when the endpoint reported no token counts.
func (r *streamReader) usageItem() poo.StreamItem {
	if r.usage == nil {
		r.usage = &poo.UsageReport{}
	}
	r.usage.TTFTMS = r.ttftMS()
	r.usageSent = true
	return poo.StreamItem{Kind: "usage", Usage: r.usage}
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

// slotFor returns the slot an output index has for a kind of content, and
// makes it when the response has not announced it.
//
// The kinds are separate slots on one index because the wire separates
// them: an event that names no index lands on an index another kind took,
// and it is still the item it says it is.
func (r *streamReader) slotFor(index int, kind string) *itemSlot {
	key := slotKey{index, kind}
	if slot, ok := r.slots[key]; ok {
		return slot
	}
	slot := &itemSlot{kind: kind}
	r.slots[key] = slot
	return slot
}

// markFirstToken records the time the first token arrived.
//
// A token is a non-empty text or thinking delta, or a tool call. An event
// that only announces an item carries no token, so it does not start the
// clock.
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

// terminalReasoning returns the reasoning items the terminal response
// carries, and reports whether it carried any.
//
// The endpoint streams a reasoning item as a summary, and reports the item
// again in the terminal response's output, where the encrypted payload
// that has to travel back with the next request rides. The terminal items
// are the response's items whole, so they replace what the stream holds
// when there are any; a response that reports none keeps the items its
// events carried.
func terminalReasoning(output []json.RawMessage) ([]poo.ProviderBlock, bool) {
	var blocks []poo.ProviderBlock
	for _, raw := range output {
		item, ok := decodeItem(raw)
		if !ok || item.Type != "reasoning" {
			continue
		}
		if block, keep := reasoningBlock(raw, item); keep {
			blocks = append(blocks, block)
		}
	}
	return blocks, len(blocks) > 0
}

// reasoningBlock returns the provider block a reasoning item rides as,
// and whether the endpoint could take the item back.
//
// The item is opaque: the transport keeps it verbatim, because only the
// endpoint can read what it encrypted. An item that carries no encrypted
// payload cannot be given back — the next request would name a reasoning
// item the endpoint cannot match — so it is not a block at all; its
// summary already travelled as the thinking text.
func reasoningBlock(raw json.RawMessage, item codexItem) (poo.ProviderBlock, bool) {
	if item.EncryptedContent == "" {
		return poo.ProviderBlock{}, false
	}
	return poo.ProviderBlock{
		API:  poo.APIResponses,
		Type: "reasoning",
		Data: append(json.RawMessage(nil), raw...),
	}, true
}

// decodeItem decodes the item an event carries.
func decodeItem(raw json.RawMessage) (codexItem, bool) {
	var item codexItem
	if len(raw) == 0 {
		return item, false
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return item, false
	}
	return item, true
}

// partsText joins an item's parts into the text they carry.
func partsText(parts []codexPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.text())
	}
	return b.String()
}

// isCompleteJSON reports whether a string is a complete JSON object.
func isCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return json.Valid([]byte(s))
}
