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
// reason. The three shapes below are the ones that matter:
//
//   - Hermes: <tool_call>{"name": "bash", "arguments": {"command": "ls"}}</tool_call>
//   - Qwen:   <tool_call><name>bash</name><arguments>{"command":"ls"}</arguments></tool_call>
//   - bare:    a JSON object on its own, or a fenced ```json block holding one
//
// The parser is off by default. The `native` tools mode leaves it disabled,
// because a server that speaks tool_calls properly does not need it.
//
// The parser owns the text: markup that belongs to a tool call is removed
// from the model's own prose, so the assistant message holds the reasoning
// and the tool call separately.

// callKind names one of the recognised call shapes.
type callKind int

const (
	// kindTagged is a <tool_call> ... </tool_call> block.
	kindTagged callKind = iota
	// kindFence is a ```json ... ``` block.
	kindFence
	// kindBare is a JSON object standing on its own.
	kindBare
)

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
	// depth is the brace depth of an open bare-JSON call.
	depth int
	// inString reports whether a bare-JSON call is inside a string.
	inString bool
	// escaped reports whether the last byte of a bare-JSON call escaped
	// the closing quote.
	escaped bool
	// seq numbers the calls the parser finds.
	seq int
}

// newParser returns a parser for a tools mode.
func newParser(mode string) *parser {
	switch mode {
	case "text", "auto":
		return &parser{enabled: true}
	default:
		return &parser{enabled: false}
	}
}

// on reports whether the parser runs.
//
// A disabled parser leaves the text alone, so the transport emits the raw
// content delta itself.
func (p *parser) on() bool { return p != nil && p.enabled }

// taggedOpen and taggedClose mark a tool call block.
const (
	taggedOpen  = "<tool_call>"
	taggedClose = "</tool_call>"
)

// textCallIndexBase is where a parsed call's index starts.
//
// The provider numbers its own calls from zero, so the parsed calls
// start well past that. The two kinds never share a slot, which matters
// in the auto mode, where both are read.
const textCallIndexBase = 1000

// feed takes a text delta and returns the events it produced.
func (p *parser) feed(text string) []poo.StreamEvent {
	if !p.enabled {
		return nil
	}
	p.buf += text
	return p.drain()
}

// flush returns the events the parser holds at the end of a stream.
//
// A call that arrived without its closing marker is still a call, as long
// as its body parses.
func (p *parser) flush() []poo.StreamEvent {
	if !p.enabled {
		return nil
	}
	var out []poo.StreamEvent
	if p.inCall {
		// The call never closed. The body is what arrived, plus the tail
		// the parser held back in case it was the start of the closing
		// marker.
		body := p.callBody + p.buf
		if ev, ok := p.parseBody(body); ok {
			out = append(out, ev)
		} else {
			text := p.callText
			if p.callKind == kindTagged {
				text += taggedClose
			} else if p.callKind == kindFence {
				text += "```"
			}
			out = append(out, textEvent(text))
		}
		p.inCall = false
		p.callBody = ""
		p.callText = ""
		p.buf = ""
	}
	if rest := strings.TrimSpace(p.buf); rest != "" {
		// A bare JSON object that arrived without a trailing newline.
		if ev, ok := p.parseCallBody(rest); ok {
			out = append(out, ev)
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
func (p *parser) drain() []poo.StreamEvent {
	var out []poo.StreamEvent
	for {
		if p.inCall {
			ev, done := p.consumeCall()
			if !done {
				break
			}
			out = append(out, ev...)
			continue
		}

		idx, kind, ok := p.findCallStart()
		if !ok {
			break
		}
		if idx > 0 {
			out = append(out, textEvent(p.buf[:idx]))
			p.buf = p.buf[idx:]
		}
		p.openCall(kind)
	}

	// Whatever is left may be the start of a call that has not arrived
	// yet, so only a safe prefix is emitted.
	if !p.inCall {
		safe := p.safePrefix()
		if safe > 0 {
			out = append(out, textEvent(p.buf[:safe]))
			p.buf = p.buf[safe:]
		}
	}
	return out
}

// openCall starts a new call of the given shape.
//
// The text before the call's opening is prose and is emitted separately,
// so the buffer starts at the call's own text.
func (p *parser) openCall(kind callKind) {
	p.inCall = true
	p.callKind = kind
	p.callBody = ""
	p.callText = ""
	p.depth = 0
	p.inString = false
	p.escaped = false

	switch kind {
	case kindTagged:
		p.buf = p.buf[len(taggedOpen):]
	case kindFence:
		p.buf = p.buf[len("```"):]
		// A fence may name the language.
		if i := strings.IndexByte(p.buf, '\n'); i >= 0 {
			p.buf = p.buf[i+1:]
		} else {
			p.buf = ""
		}
	}
	p.callText = p.openText(kind)
}

// openText returns the markup that opened the call, so it can be restored
// when the body turns out not to be a call.
func (p *parser) openText(kind callKind) string {
	switch kind {
	case kindTagged:
		return taggedOpen
	case kindFence:
		return "```"
	default:
		return ""
	}
}

// consumeCall advances an open call, returning the events it produced.
//
// The call's body is accumulated separately from the buffer, because the
// buffer is emptied as the text arrives. The closing marker may straddle
// the two, so the search is over the whole body and what is left of the
// buffer, and the text before the marker is moved into the body.
func (p *parser) consumeCall() ([]poo.StreamEvent, bool) {
	switch p.callKind {
	case kindTagged:
		return p.consumeUntil(taggedClose)
	case kindFence:
		return p.consumeUntil("```")
	default:
		return p.consumeJSON()
	}
}

// consumeUntil advances a call that ends at a marker.
//
// The marker may straddle the body and the buffer, so the search is over
// the two together.
func (p *parser) consumeUntil(marker string) ([]poo.StreamEvent, bool) {
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
		p.callBody = combined[:split]
		p.callText += combined[:split]
		p.buf = combined[split:]
		return nil, false
	}
	i += start
	p.callBody = combined[:i]
	p.callText += combined[:i] + marker
	p.buf = combined[i+len(marker):]
	p.inCall = false
	if ev, ok := p.parseBody(p.callBody); ok {
		return []poo.StreamEvent{ev}, true
	}
	return []poo.StreamEvent{textEvent(p.callText)}, true
}

// consumeJSON advances a call that is a bare JSON object.
//
// The body ends at the object's matching brace. The scan runs over the
// new text only: the body already counted the braces it holds, and
// counting them again would unbalance the depth.
func (p *parser) consumeJSON() ([]poo.StreamEvent, bool) {
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
				if ev, ok := p.parseBody(p.callBody); ok {
					return []poo.StreamEvent{ev}, true
				}
				return []poo.StreamEvent{textEvent(p.callText)}, true
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
// It returns the index, the shape, and whether a call was found.
func (p *parser) findCallStart() (int, callKind, bool) {
	tagged := strings.Index(p.buf, taggedOpen)
	fence := strings.Index(p.buf, "```")
	bare := p.findBareStart()

	best := -1
	kind := kindBare
	switch {
	case tagged >= 0 && (fence < 0 || tagged < fence) && (bare < 0 || tagged < bare):
		best, kind = tagged, kindTagged
	case fence >= 0 && (bare < 0 || fence < bare):
		best, kind = fence, kindFence
	case bare >= 0:
		best, kind = bare, kindBare
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, kind, true
}

// findBareStart locates a JSON object that stands on its own.
//
// The object must start a line, so that prose with a brace in it is not
// mistaken for a call.
func (p *parser) findBareStart() int {
	for i := 0; i < len(p.buf); i++ {
		if p.buf[i] != '{' {
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
	for _, marker := range []string{taggedOpen, "```"} {
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

// parseBody reads a call body, in either the JSON or the XML shape.
func (p *parser) parseBody(body string) (poo.StreamEvent, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return poo.StreamEvent{}, false
	}
	if ev, ok := p.parseCallBody(body); ok {
		return ev, true
	}
	return p.parseXMLCall(body)
}

// parseCallBody reads a JSON call body.
func (p *parser) parseCallBody(body string) (poo.StreamEvent, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return poo.StreamEvent{}, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return poo.StreamEvent{}, false
	}
	name, ok := rawString(raw, "name")
	if !ok {
		return poo.StreamEvent{}, false
	}
	args := json.RawMessage("{}")
	for _, key := range []string{"arguments", "parameters", "args", "input"} {
		if v, ok := raw[key]; ok {
			if isCompleteJSON(string(v)) {
				args = v
			}
			break
		}
	}
	return p.toolEvent(name, string(args))
}

// parseXMLCall reads a Qwen-style XML call body.
func (p *parser) parseXMLCall(body string) (poo.StreamEvent, bool) {
	nm := between(body, "<name>", "</name>")
	if nm == "" {
		return poo.StreamEvent{}, false
	}
	args := "{}"
	if am := between(body, "<arguments>", "</arguments>"); am != "" {
		if isCompleteJSON(strings.TrimSpace(am)) {
			args = strings.TrimSpace(am)
		}
	}
	return p.toolEvent(strings.TrimSpace(nm), args)
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
func (p *parser) toolEvent(name, args string) (poo.StreamEvent, bool) {
	if name == "" {
		return poo.StreamEvent{}, false
	}
	if !isCompleteJSON(args) {
		args = "{}"
	}
	p.seq++
	return poo.StreamEvent{
		Kind: "tool_call",
		ToolCall: &poo.ToolCallDelta{
			// The parsed calls start after the provider's own calls, so
			// the two kinds never share a slot in the auto mode.
			Index:     textCallIndexBase + p.seq - 1,
			ID:        "text_call_" + itoa(p.seq),
			Name:      name,
			Arguments: []byte(args),
			Complete:  true,
		},
	}, true
}

// textEvent builds a text delta.
func textEvent(text string) poo.StreamEvent {
	return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: text}}
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
