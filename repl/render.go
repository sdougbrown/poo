package repl

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// Colors and glyphs the transcript uses. Everything a person reads
// goes through the renderer, so the palette lives here and nowhere
// else.
const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// maxResultLines is how many lines of a tool result the transcript
// shows. A longer result keeps its head and says what was cut.
const maxResultLines = 8

// renderer writes the transcript: the rendered replies, the tool
// calls' decoration and the notices.
//
// Everything it writes is mirrored into a transcript buffer, so a
// pager can show the session again without the server.
type renderer struct {
	// out is the terminal the transcript goes to.
	out io.Writer
	// sink is where the transcript's lines go: out, or the editor's own
	// writer while the editor holds out, so the prompt is redrawn under
	// each line instead of being overwritten by it.
	sink io.Writer
	// term says out is a terminal.
	term bool
	// tr renders markdown for the terminal. It is nil for a writer
	// that is not a terminal, where markdown renders as plain text.
	tr *glamour.TermRenderer
	// transcript mirrors what was written, ANSI included.
	transcript strings.Builder
	// wrap is the width the markdown wraps at.
	wrap int
}

// newRenderer builds the renderer for out.
//
// A terminal gets the auto style, which picks the palette from the
// terminal's own background; anything else gets plain text, so a test
// or a pipe reads the same words without the escapes.
func newRenderer(out io.Writer) *renderer {
	r := &renderer{out: out, sink: out, wrap: 80}
	r.term = termIsTerminal(out)
	if r.term {
		if f, ok := out.(interface{ Fd() uintptr }); ok {
			if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 4 {
				r.wrap = w - 2
			}
		}
		tr, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(r.wrap),
		)
		if err == nil {
			r.tr = tr
		}
	}
	return r
}

// write appends a chunk to the transcript and the terminal.
func (r *renderer) write(s string) {
	r.transcript.WriteString(s)
	fmt.Fprint(r.sink, s)
}

// userPrompt echoes the prompt the session runs.
func (r *renderer) userPrompt(text string) {
	r.write(ansiBold + "❯ " + text + ansiReset + "\n")
}

// message renders a complete reply as markdown.
//
// The reply is the model's own text: code fences become highlighted
// blocks, and a terminal gets the palette the auto style picked.
func (r *renderer) message(text string) {
	r.write(r.markdown(text))
}

// markdown renders one markdown source for the terminal.
func (r *renderer) markdown(text string) string {
	if r.tr == nil {
		return strings.TrimRight(text, "\n") + "\n"
	}
	out, err := r.tr.Render(text)
	if err != nil {
		// A source glamour refused is still the model's words: they
		// go out plain rather than not at all.
		return strings.TrimRight(text, "\n") + "\n"
	}
	return out
}

// thinking renders a model's thinking block, dimmed.
func (r *renderer) thinking(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	r.write(ansiDim + "· thinking ·\n" + text + ansiReset + "\n")
}

// toolCall renders one tool call.
func (r *renderer) toolCall(tool, summary string) {
	line := ansiCyan + "⚙ " + tool + ansiReset
	if summary != "" {
		line += " " + ansiDim + firstLine(summary) + ansiReset
	}
	r.write(line + "\n")
}

// toolResult renders one tool result.
func (r *renderer) toolResult(tool, outcome, text string, elided int) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	shown := lines
	if len(lines) > maxResultLines {
		shown = lines[:maxResultLines]
	}
	var b strings.Builder
	for _, line := range shown {
		b.WriteString("  " + ansiDim + line + ansiReset + "\n")
	}
	if rest := len(lines) - len(shown) + elided; rest > 0 {
		b.WriteString(fmt.Sprintf("  %s… +%d more lines%s\n", ansiDim, rest, ansiReset))
	}
	if outcome != "ok" && outcome != "" {
		b.WriteString(fmt.Sprintf("  %s%s: %s%s\n", ansiRed, tool, outcome, ansiReset))
	}
	r.write(b.String())
}

// settle renders a run's outcome and what it cost.
func (r *renderer) settle(reason string, turns int, input, output int) {
	line := ansiDim + fmt.Sprintf("✓ %s · %d turn(s) · %s in · %s out",
		reason, turns, shortTokens(input), shortTokens(output)) + ansiReset
	r.write(line + "\n")
}

// notice writes a dim line that is not part of the conversation.
func (r *renderer) notice(format string, args ...any) {
	r.write(ansiDim + fmt.Sprintf(format, args...) + ansiReset + "\n")
}

// warning writes a warning the session reported.
func (r *renderer) warning(text string) {
	r.write(ansiYellow + "⚠ " + text + ansiReset + "\n")
}

// failure writes a failure the repl could not get past.
func (r *renderer) failure(text string) {
	r.write(ansiRed + "✗ " + text + ansiReset + "\n")
}

// transcriptText returns the transcript so far.
func (r *renderer) transcriptText() string {
	return r.transcript.String()
}

// firstLine returns the first line of a summary.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// shortTokens renders a token count the statusline's way: thousands
// get one decimal, everything else stays whole.
//
// A count that would round to 1000.0k reads as a whole megabyte
// instead: the unit follows the number the reader sees.
func shortTokens(n int) string {
	switch {
	case n >= 999_950:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	case n >= 0:
		return fmt.Sprintf("%d", n)
	default:
		return "?"
	}
}
