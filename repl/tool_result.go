package repl

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// The repl reads a bash result's observation off the wire by the
// envelope's own field names, without importing the tool that built it.
const (
	bashElidedMarker = "… elided …"
	resultIndent     = "  "
	clipTail         = "…"
)

// resultPreview is one tool result as the transcript shows it.
type resultPreview struct {
	// handle is the model-facing handle line the result opened with.
	handle string
	// body is the result's own lines, sanitized and bounded to the
	// terminal's width and the row cap.
	body []string
	// moreLines is how many body lines the row cap left out.
	moreLines int
	// elidedChars is how many characters the tool or the wire left out
	// of the middle.
	elidedChars int
	// hint is the tool's own note about what it left out.
	hint string
	// status is the command's exit status, its timeout, or the kill that
	// left it without one.
	status string
}

// bashObservation is the envelope a bash result carries. Its text
// fields stay raw so a null reads as malformed, not as an empty string.
type bashObservation struct {
	Output     json.RawMessage `json:"output"`
	OutputHead json.RawMessage `json:"output_head"`
	OutputTail json.RawMessage `json:"output_tail"`
	Stderr     json.RawMessage `json:"stderr"`
	StderrHead json.RawMessage `json:"stderr_head"`
	StderrTail json.RawMessage `json:"stderr_tail"`
	Hint       json.RawMessage `json:"hint"`
	Elided     *int            `json:"elided_chars"`
	Returncode json.RawMessage `json:"returncode"`
	TimedOut   *bool           `json:"timed_out"`

	// code is the return code Returncode names, nil when the command was
	// killed.
	code *int
}

// previewResult reads one tool result for the transcript.
//
// A bash observation decodes into the command's own output and its exit
// status; anything else reads as the text it came as, bounded and
// sanitized. A result that does not decode is never guessed at: its
// text is what the model saw, so its text is what the person sees.
func previewResult(text string, elided, width, rows int) resultPreview {
	handle, rest := splitHandle(text)
	obs, ok := decodeBashObservation(rest)
	if !ok {
		return textPreview(text, elided, width, rows)
	}
	p := resultPreview{
		handle:      clipLine(sanitizeLine(handle), width),
		body:        obs.body(width),
		elidedChars: elided,
		hint:        clipLine(sanitizeLine(obs.hintText()), width),
		status:      obs.status(),
	}
	if obs.Elided != nil {
		p.elidedChars += *obs.Elided
	}
	if len(p.body) > rows {
		p.moreLines = len(p.body) - rows
		if obs.elidedMiddle() {
			// A capped result ends in the tail: the rows keep it, and
			// the head gives way.
			p.body = p.body[len(p.body)-rows:]
		} else {
			p.body = p.body[:rows]
		}
	}
	return p
}

// textPreview bounds a result the repl does not decode: its own lines,
// sanitized and clipped, with the head kept and the rest counted.
func textPreview(text string, elided, width, rows int) resultPreview {
	p := resultPreview{elidedChars: elided}
	for _, line := range resultLines(text) {
		p.body = append(p.body, clipLine(sanitizeLine(line), width))
	}
	if len(p.body) > rows {
		p.moreLines = len(p.body) - rows
		p.body = p.body[:rows]
	}
	return p
}

// splitHandle takes the "[r1 · bash]" line off a result, when the
// result opens with one, so the envelope below it can be decoded.
func splitHandle(text string) (handle, rest string) {
	line, body, found := strings.Cut(text, "\n")
	if !found || !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") || !strings.Contains(line, " · ") {
		return "", text
	}
	return line, body
}

// decodeBashObservation reads a bash result's envelope. The envelope
// names the return code, the timeout and an output stream. A text that
// does not is another tool's result, and a malformed field falls back.
// The tool's name is not part of the decision: the cli registry's
// calls return the same envelope the bash tool does.
func decodeBashObservation(text string) (bashObservation, bool) {
	var obs bashObservation
	if err := json.Unmarshal([]byte(text), &obs); err != nil {
		return bashObservation{}, false
	}
	if obs.Returncode == nil || obs.TimedOut == nil {
		return bashObservation{}, false
	}
	if obs.Output == nil && obs.OutputHead == nil && obs.OutputTail == nil {
		return bashObservation{}, false
	}
	for _, raw := range []json.RawMessage{
		obs.Output, obs.OutputHead, obs.OutputTail,
		obs.Stderr, obs.StderrHead, obs.StderrTail,
		obs.Hint,
	} {
		if _, ok := observationText(raw); !ok {
			return bashObservation{}, false
		}
	}
	if string(obs.Returncode) != "null" {
		var code int
		if err := json.Unmarshal(obs.Returncode, &code); err != nil {
			return bashObservation{}, false
		}
		obs.code = &code
	}
	if obs.Elided != nil && *obs.Elided < 0 {
		return bashObservation{}, false
	}
	return obs, true
}

// observationText reads one string field: an absent field reads empty,
// and a null or non-string reports false.
func observationText(raw json.RawMessage) (string, bool) {
	if raw == nil {
		return "", true
	}
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var value string
	_ = json.Unmarshal(raw, &value)
	return value, true
}

// elidedMiddle reports that the envelope cut characters out of a
// stream's middle, so the body carries a head and a tail.
func (o bashObservation) elidedMiddle() bool {
	return o.OutputHead != nil || o.OutputTail != nil ||
		o.StderrHead != nil || o.StderrTail != nil
}

func (o bashObservation) hintText() string {
	hint, _ := observationText(o.Hint)
	return hint
}

// body is the observation's streams, clipped to the terminal's width.
func (o bashObservation) body(width int) []string {
	lines := streamLines(o.Output, o.OutputHead, o.OutputTail)
	if stderr := streamLines(o.Stderr, o.StderrHead, o.StderrTail); len(stderr) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "stderr:")
		}
		lines = append(lines, stderr...)
	}
	for i, line := range lines {
		lines[i] = clipLine(sanitizeLine(line), width)
	}
	return lines
}

// streamLines renders one stream: its whole output, or its head and
// tail with the elided middle marked between them.
func streamLines(whole, head, tail json.RawMessage) []string {
	if all, _ := observationText(whole); all != "" {
		return resultLines(all)
	}
	headText, _ := observationText(head)
	tailText, _ := observationText(tail)
	if headText == "" && tailText == "" {
		return nil
	}
	lines := resultLines(headText)
	lines = append(lines, bashElidedMarker)
	return append(lines, resultLines(tailText)...)
}

// status names how the command ended, and says nothing when it exited
// zero.
func (o bashObservation) status() string {
	switch {
	case o.TimedOut != nil && *o.TimedOut:
		return "timed out"
	case o.code == nil:
		// A process killed by a signal, or by the run's own
		// cancellation, has no exit status.
		return "no exit status (killed)"
	case *o.code != 0:
		return fmt.Sprintf("exit %d", *o.code)
	}
	return ""
}

func resultLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// sanitizeLine makes one line of a tool result safe to print: tabs
// expand, escape sequences strip, and cursor controls go. Source
// indentation stays, because it is spaces.
func sanitizeLine(line string) string {
	if line == "" {
		return ""
	}
	line = expandTabs(ansi.Strip(line))
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)
}

// sanitizeText makes text the session recorded safe to print, line by
// line: every line goes through sanitizeLine, and the line breaks stay,
// so a block keeps the rows it had.
func sanitizeText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = sanitizeLine(line)
	}
	return strings.Join(lines, "\n")
}

// clipLine ends a line at the terminal's width, marking the cut.
func clipLine(line string, width int) string {
	if width <= 0 || ansi.StringWidth(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, clipTail)
}
