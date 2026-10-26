package openai

import (
	"encoding/json"
	"strings"

	"github.com/sdougbrown/poo"
)

// The parser reads tool calls out of a model's text.
//
// Local models emit tool calls in several shapes, and a harness that only
// understands one of them scores near zero on a baseline for the wrong
// reason. The shapes below are the ones that matter:
//
//   - Hermes: <tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>
//   - Qwen:   <tool_call><name>bash</name><arguments>{"command":"ls"}</arguments></tool_call>
//   - Mistral: [TOOL_CALLS] [{"name": "bash", "arguments": {"command": "ls"}}]
//   - bare:    a JSON object on its own, or a fenced ```json block holding one
//
// The parser is off by default. The `native` tools mode leaves it disabled,
// because a server that speaks tool_calls properly does not need it.
//
// The parser owns the text: markup that belongs to a tool call is removed
// from the model's own prose, so the assistant message holds the reasoning
// and the tool call separately.
//
// Text that looks like a call but does not parse is not dropped: it stays
// in the prose and the parser reports why it could not be read.

// callKind names one of the recognised call shapes.
type callKind int

const (
	// kindTagged is a <tool_call> ... </tool_call> block.
	kindTagged callKind = iota
	// kindFence is a ```json ... ``` block.
	kindFence
	// kindBare is a JSON object standing on its own.
	kindBare
	// kindMistral is a [TOOL_CALLS] [...] block.
	kindMistral
)

// callFormats holds the shapes the parser reads.
//
// A model writes its calls in one shape, and the model info names it. A
// format that names no shape reads them all.
type callFormats struct {
	// hermes reads a tagged block with a JSON body.
	hermes bool
	// qwen reads a tagged block with an XML body.
	qwen bool
	// mistral reads a [TOOL_CALLS] block.
	mistral bool
	// json reads a JSON object standing on its own.
	json bool
}

// formatsFor returns the shapes a text format names.
//
// An empty format, or "auto", reads every shape.
func formatsFor(format string) callFormats {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "hermes":
		return callFormats{hermes: true}
	case "qwen_xml", "qwen":
		return callFormats{qwen: true}
	case "mistral":
		return callFormats{mistral: true}
	case "json":
		return callFormats{json: true}
	default:
		return callFormats{hermes: true, qwen: true, mistral: true, json: true}
	}
}

// parser reads tool calls from a text stream.
type parser struct {
	// enabled reports whether the parser runs at all.
	enabled bool
	// buf holds text seen so far that has not been emitted or consumed.
	buf string
	// inCall reports whether a call is open.
	inCall bool
	// callKind is the shape of the open call.
	callKind callKind
	// callBody holds the open call's body.
	callBody string
	// callText holds the open call's raw text, so a body that turns out
	// not to be a call can be put back into the prose.
	callText string
	// callOpen holds the open call's opening marker, as the model wrote
	// it, so the raw text can be rebuilt from the opening and the body.
	callOpen string
	// depth is the brace depth of an open bare-JSON call.
	depth int
	// inString reports whether a bare-JSON call is inside a string.
	inString bool
	// escaped reports whether the last byte of a bare-JSON call escaped
	// the closing quote.
	escaped bool
	// seq numbers the calls the parser finds.
	seq int
	// midLine reports that the last prose the parser emitted did not end
	// in a newline, so the start of the buffer is in the middle of a
	// line.
	midLine bool
	// formats holds the shapes the parser reads.
	formats callFormats
	// lastReason is why the last body the parser read was not a call.
	lastReason string
}

// newParser returns a parser for a tools mode and a text format.
//
// The mode decides whether the parser runs at all, and the format
// decides which shapes it reads.
func newParser(mode, format string) *parser {
	p := &parser{enabled: mode == "text" || mode == "auto"}
	p.formats = formatsFor(format)
	return p
}

// on reports whether the parser runs.
//
// A disabled parser leaves the text alone, so the transport emits the raw
// content delta itself.
func (p *parser) on() bool { return p != nil && p.enabled }

// ToolCallOpen and ToolCallClose are the tags that mark a tool call
// block in text mode.
const (
	ToolCallOpen  = "<tool_call>"
	ToolCallClose = "</tool_call>"
)

// MistralCallsOpen is the marker that opens a Mistral-style call list.
//
// The list is a JSON array of calls, and it follows the marker.
const MistralCallsOpen = "[TOOL_CALLS]"

// textCallIndexBase is where a parsed call's index starts.
//
// The provider numbers its own calls from zero, so the parsed calls
// start well past that. The two kinds never share a slot, which matters
// in the auto mode, where both are read.
const textCallIndexBase = 1000

// feed takes a text delta and returns the events it produced.
func (p *parser) feed(text string) []poo.StreamItem {
	if !p.on() {
		return nil
	}
	p.buf += text
	return p.drain()
}

// flush returns the events the parser holds at the end of a stream.
//
// A call that arrived without its closing marker is still a call, as long
// as its body parses. A body that does not parse stays in the model's
// prose, exactly as the model wrote it.
func (p *parser) flush() []poo.StreamItem {
	if !p.on() {
		return nil
	}
	var out []poo.StreamItem
	if p.inCall {
		// The call never closed. The body is what arrived, plus the tail
		// the parser held back in case it was the start of the closing
		// marker.
		body := p.callBody + p.buf
		if events, _, ok := p.parseBody(body); ok {
			out = append(out, events...)
		} else {
			// The block is not a call, so the model's own text goes back
			// into the prose. No closing marker is invented: the model
			// never wrote one.
			text := p.callText + p.buf
			out = append(out, textEvent(text))
			out = append(out, parseErrorEvent(text, p.lastReason))
		}
		p.inCall = false
		p.callBody = ""
		p.callText = ""
		p.buf = ""
	}
	if rest := strings.TrimSpace(p.buf); rest != "" {
		// A bare JSON object that arrived without a trailing newline.
		if events, _, ok := p.parseCallBody(rest); ok {
			out = append(out, events...)
			p.buf = ""
			return out
		}
	}
	if p.buf != "" {
		out = append(out, textEvent(p.buf))
		p.buf = ""
	}
	return out
}

// drain consumes the buffer, returning the events it produced.
func (p *parser) drain() []poo.StreamItem {
	var out []poo.StreamItem
	for {
		if p.inCall {
			ev, done := p.consumeCall()
			if !done {
				break
			}
			out = append(out, p.track(ev...)...)
			continue
		}

		idx, kind, ok := p.findCallStart()
		if !ok {
			break
		}
		if kind == kindFence && !strings.Contains(p.buf[idx+len("```"):], "\n") {
			// The fence's language line has not ended, so whether the
			// block is a json call is not known yet. The prose before it
			// is emitted, and the rest waits for more text.
			if idx > 0 {
				out = append(out, p.track(textEvent(p.buf[:idx]))...)
				p.buf = p.buf[idx:]
			}
			return out
		}
		if idx > 0 {
			out = append(out, p.track(textEvent(p.buf[:idx]))...)
			p.buf = p.buf[idx:]
		}
		p.openCall(kind)
	}

	// Whatever is left may be the start of a call that has not arrived
	// yet, so only a safe prefix is emitted.
	if !p.inCall {
		safe := p.safePrefix()
		if safe > 0 {
			out = append(out, p.track(textEvent(p.buf[:safe]))...)
			p.buf = p.buf[safe:]
		}
	}
	return out
}

// openCall starts a new call of the given shape.
//
// The text before the call's opening is prose and is emitted separately,
// so the buffer starts at the call's own text. The opening itself is
// kept, so a block that turns out not to be a call goes back into the
// prose exactly as the model wrote it.
func (p *parser) openCall(kind callKind) {
	p.inCall = true
	p.callKind = kind
	p.callBody = ""
	p.callText = ""
	p.callOpen = ""
	p.depth = 0
	p.inString = false
	p.escaped = false

	switch kind {
	case kindTagged:
		p.callText = p.buf[:len(ToolCallOpen)]
		p.callOpen = p.callText
		p.buf = p.buf[len(ToolCallOpen):]
	case kindMistral:
		p.callText = p.buf[:len(MistralCallsOpen)]
		p.callOpen = p.callText
		p.buf = p.buf[len(MistralCallsOpen):]
	case kindFence:
		open := p.buf[:len("```")]
		p.buf = p.buf[len("```"):]
		// A fence may name the language.
		if i := strings.IndexByte(p.buf, '\n'); i >= 0 {
			open += p.buf[:i+1]
			p.buf = p.buf[i+1:]
		} else {
			p.buf = ""
		}
		p.callText = open
		p.callOpen = open
	}
}

// consumeCall advances an open call, returning the events it produced.
//
// The call's body is accumulated separately from the buffer, because the
// buffer is emptied as the text arrives. The closing marker may straddle
// the two, so the search is over the whole body and what is left of the
// buffer, and the text before the marker is moved into the body.
func (p *parser) consumeCall() ([]poo.StreamItem, bool) {
	switch p.callKind {
	case kindTagged:
		return p.consumeUntil(ToolCallClose)
	case kindFence:
		return p.consumeUntil("```")
	case kindMistral:
		return p.consumeArray()
	default:
		return p.consumeJSON()
	}
}

// consumeUntil advances a call that ends at a marker.
//
// The marker may straddle the body and the buffer, so the search is over
// the two together.
func (p *parser) consumeUntil(marker string) ([]poo.StreamItem, bool) {
	combined := p.callBody + p.buf
	// The search starts far enough back that a marker split across the
	// body and the buffer is still found.
	start := len(p.callBody) - len(marker)
	if start < 0 {
		start = 0
	}
	i := strings.Index(combined[start:], marker)
	if i < 0 {
		// The marker has not arrived. All but the last few characters
		// are body now; the rest may be the start of the marker.
		keep := len(marker) - 1
		if keep > len(combined) {
			keep = len(combined)
		}
		split := len(combined) - keep
		// The raw text is rebuilt rather than appended to: the body is
		// the whole of the call so far, not only what arrived now.
		p.callBody = combined[:split]
		p.callText = p.callOpen + p.callBody
		p.buf = combined[split:]
		return nil, false
	}
	i += start
	p.callBody = combined[:i]
	p.callText = p.callOpen + p.callBody + marker
	p.buf = combined[i+len(marker):]
	p.inCall = false
	if events, _, ok := p.parseBody(p.callBody); ok {
		return events, true
	}
	return []poo.StreamItem{textEvent(p.callText), parseErrorEvent(p.callText, p.lastReason)}, true
}

// consumeArray advances a call that is a JSON array.
//
// The array ends at its matching bracket. The scan runs over the new
// text only: the body already counted the brackets it holds, and
// counting them again would unbalance the depth.
func (p *parser) consumeArray() ([]poo.StreamItem, bool) {
	for i := 0; i < len(p.buf); i++ {
		c := p.buf[i]
		if p.inString {
			if p.escaped {
				p.escaped = false
				continue
			}
			switch c {
			case '\\':
				p.escaped = true
			case '"':
				p.inString = false
			}
			continue
		}
		switch c {
		case '"':
			p.inString = true
		case '[':
			p.depth++
		case ']':
			p.depth--
			if p.depth == 0 {
				p.callBody += p.buf[:i+1]
				p.callText += p.buf[:i+1]
				p.buf = p.buf[i+1:]
				p.inCall = false
				if events, _, ok := p.parseMistral(p.callBody); ok {
					return events, true
				}
				return []poo.StreamItem{textEvent(p.callText), parseErrorEvent(p.callText, p.lastReason)}, true
			}
		}
	}
	p.callBody += p.buf
	p.callText += p.buf
	p.buf = ""
	return nil, false
}

// consumeJSON advances a call that is a bare JSON object.
//
// The body ends at the object's matching brace. The scan runs over the
// new text only: the body already counted the braces it holds, and
// counting them again would unbalance the depth.
func (p *parser) consumeJSON() ([]poo.StreamItem, bool) {
	for i := 0; i < len(p.buf); i++ {
		c := p.buf[i]
		if p.inString {
			if p.escaped {
				p.escaped = false
				continue
			}
			switch c {
			case '\\':
				p.escaped = true
			case '"':
				p.inString = false
			}
			continue
		}
		switch c {
		case '"':
			p.inString = true
		case '{':
			p.depth++
		case '}':
			p.depth--
			if p.depth == 0 {
				p.callBody += p.buf[:i+1]
				p.callText += p.buf[:i+1]
				p.buf = p.buf[i+1:]
				p.inCall = false
				if events, _, ok := p.parseBody(p.callBody); ok {
					return events, true
				}
				return []poo.StreamItem{textEvent(p.callText), parseErrorEvent(p.callText, p.lastReason)}, true
			}
		}
	}
	p.callBody += p.buf
	p.callText += p.buf
	p.buf = ""
	return nil, false
}

// findCallStart locates the next call in the buffer.
//
// It returns the index, the shape, and whether a call was found. Only
// the shapes the parser reads are looked for.
func (p *parser) findCallStart() (int, callKind, bool) {
	var starts []callStart
	if p.formats.hermes || p.formats.qwen {
		if i := strings.Index(p.buf, ToolCallOpen); i >= 0 {
			starts = append(starts, callStart{index: i, kind: kindTagged})
		}
	}
	if p.formats.json {
		if i := strings.Index(p.buf, "```"); i >= 0 {
			starts = append(starts, callStart{index: i, kind: kindFence})
		}
	}
	if p.formats.mistral {
		if i := strings.Index(p.buf, MistralCallsOpen); i >= 0 {
			starts = append(starts, callStart{index: i, kind: kindMistral})
		}
	}
	if p.formats.json {
		if i := p.findBareStart(); i >= 0 {
			starts = append(starts, callStart{index: i, kind: kindBare})
		}
	}

	best := callStart{index: -1}
	for _, s := range starts {
		if best.index < 0 || s.index < best.index {
			best = s
		}
	}
	if best.index < 0 {
		return 0, 0, false
	}
	return best.index, best.kind, true
}

// callStart is a call the parser found in its buffer.
type callStart struct {
	// index is where the call starts.
	index int
	// kind is the shape of the call.
	kind callKind
}

// track records what a batch of events leaves behind: prose that does
// not end in a newline leaves the buffer mid-line, and a call does not.
// It returns the events unchanged.
func (p *parser) track(events ...poo.StreamItem) []poo.StreamItem {
	for _, ev := range events {
		switch ev.Kind {
		case "delta":
			if ev.Delta != nil && ev.Delta.Text != "" {
				p.midLine = !strings.HasSuffix(ev.Delta.Text, "\n")
			}
		case "tool_call":
			p.midLine = false
		}
	}
	return events
}

// findBareStart locates a JSON object that stands on its own.
//
// The object must start a line, so that prose with a brace in it is not
// mistaken for a call. The start of the buffer is a line start only when
// the prose emitted before it ended in a newline.
func (p *parser) findBareStart() int {
	for i := 0; i < len(p.buf); i++ {
		if p.buf[i] != '{' {
			continue
		}
		if i == 0 && p.midLine {
			continue
		}
		if i != 0 && p.buf[i-1] != '\n' {
			continue
		}
		rest := strings.TrimSpace(p.buf[i:])
		if !strings.HasPrefix(rest, "{") {
			continue
		}
		return i
	}
	return -1
}

// safePrefix returns the length of the buffer prefix that cannot be the
// start of a call.
func (p *parser) safePrefix() int {
	keep := 0
	for _, marker := range []string{ToolCallOpen, "```", MistralCallsOpen} {
		if !p.readsMarker(marker) {
			continue
		}
		for n := 1; n < len(marker); n++ {
			if strings.HasSuffix(p.buf, marker[:n]) {
				if n > keep {
					keep = n
				}
			}
		}
	}
	if keep >= len(p.buf) {
		return 0
	}
	return len(p.buf) - keep
}

// readsMarker reports whether the parser looks for a marker.
func (p *parser) readsMarker(marker string) bool {
	switch marker {
	case ToolCallOpen:
		return p.formats.hermes || p.formats.qwen
	case MistralCallsOpen:
		return p.formats.mistral
	default:
		return p.formats.json
	}
}

// parseBody reads a call body, in the shape the formats allow.
//
// It returns the events the call produced, the reason the body is not a
// call, and whether it is one.
func (p *parser) parseBody(body string) ([]poo.StreamItem, string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		p.lastReason = "the call is empty"
		return nil, p.lastReason, false
	}
	reason := "the call is not one this model writes"
	if p.formats.hermes || p.formats.json {
		events, why, ok := p.parseCallBody(body)
		if ok {
			return events, "", true
		}
		reason = why
	}
	if p.formats.qwen {
		events, why, ok := p.parseXMLCall(body)
		if ok {
			return events, "", true
		}
		reason = why
	}
	p.lastReason = reason
	return nil, reason, false
}

// parseCallBody reads a JSON call body.
func (p *parser) parseCallBody(body string) ([]poo.StreamItem, string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, "the call is empty", false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, "the call is not a JSON object", false
	}
	name, ok := rawString(raw, "name")
	if !ok {
		return nil, "the call has no name", false
	}
	args := json.RawMessage("{}")
	for _, key := range []string{"arguments", "parameters", "args", "input"} {
		v, ok := raw[key]
		if !ok {
			continue
		}
		// The tools take an object, so arguments that are not one are
		// not a call. The block stays in the model's prose.
		if !isJSONObject(v) {
			return nil, "the call's arguments are not an object", false
		}
		args = v
		break
	}
	ev, ok := p.toolEvent(name, string(args))
	if !ok {
		return nil, "the call has no name", false
	}
	return []poo.StreamItem{ev}, "", true
}

// parseMistral reads a Mistral-style call list.
//
// The list is a JSON array of calls, and the arguments of a call may be
// an object or a string holding one.
func (p *parser) parseMistral(body string) ([]poo.StreamItem, string, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, "the call list is empty", false
	}
	var calls []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(body), &calls); err != nil {
		return nil, "the call list is not a JSON array", false
	}
	if len(calls) == 0 {
		return nil, "the call list is empty", false
	}
	var out []poo.StreamItem
	for _, c := range calls {
		args := "{}"
		if len(c.Arguments) > 0 {
			raw := c.Arguments
			// The arguments may be a string holding the object.
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				raw = json.RawMessage(text)
			}
			if !isJSONObject(raw) {
				return nil, "the call's arguments are not an object", false
			}
			args = string(raw)
		}
		ev, ok := p.toolEvent(c.Name, args)
		if !ok {
			return nil, "the call has no name", false
		}
		out = append(out, ev)
	}
	return out, "", true
}

// parseXMLCall reads a Qwen-style XML call body.
func (p *parser) parseXMLCall(body string) ([]poo.StreamItem, string, bool) {
	nm := between(body, "<name>", "</name>")
	if nm == "" {
		return nil, "the call has no name element", false
	}
	args := "{}"
	if am := between(body, "<arguments>", "</arguments>"); am != "" {
		am = strings.TrimSpace(am)
		if !isJSONObject(json.RawMessage(am)) {
			return nil, "the call's arguments are not an object", false
		}
		args = am
	}
	ev, ok := p.toolEvent(strings.TrimSpace(nm), args)
	if !ok {
		return nil, "the call has no name", false
	}
	return []poo.StreamItem{ev}, "", true
}

// isJSONObject reports whether a raw JSON value is an object.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	return json.Valid([]byte(trimmed))
}

// between returns the text between two markers, or the empty string.
func between(s, open, close string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// toolEvent builds a tool call event.
func (p *parser) toolEvent(name, args string) (poo.StreamItem, bool) {
	if name == "" {
		return poo.StreamItem{}, false
	}
	if !isJSONObject(json.RawMessage(args)) {
		args = "{}"
	}
	p.seq++
	return poo.StreamItem{
		Kind: "tool_call",
		ToolCall: &poo.ToolCallDelta{
			// The parsed calls start after the provider's own calls, so
			// the two kinds never share a slot in the auto mode.
			Index:     textCallIndexBase + p.seq - 1,
			ID:        "text_call_" + itoa(p.seq),
			Name:      name,
			Arguments: []byte(args),
			Complete:  true,
			// The call was read out of the model's own text, and the
			// record says so.
			Origin: poo.CallOriginParsedText,
		},
	}, true
}

// textEvent builds a text delta.
func textEvent(text string) poo.StreamItem {
	return poo.StreamItem{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: text}}
}

// thinkingEvent builds a thinking delta.
func thinkingEvent(text string) poo.StreamItem {
	return poo.StreamItem{Kind: "delta", Delta: &poo.Delta{Kind: "thinking", Text: text}}
}

// parseErrorEvent builds the event that reports a tool call the model
// wrote in text and that could not be parsed.
//
// The failure is recoverable: the text is not lost, and the model gets
// another turn.
func parseErrorEvent(text, reason string) poo.StreamItem {
	if reason == "" {
		reason = "the call is not one this model writes"
	}
	return poo.StreamItem{
		Kind: "error",
		Error: &poo.StreamError{
			Message:     "your tool call could not be parsed: " + reason,
			Text:        text,
			Recoverable: true,
		},
	}
}

// rawString reads a string field out of a decoded object.
func rawString(raw map[string]json.RawMessage, key string) (string, bool) {
	v, ok := raw[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

// itoa formats a small non-negative int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
