package repl

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
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
	// tr renders markdown for the terminal. It is nil for a writer
	// that is not a terminal, where markdown renders as plain text.
	tr *glamour.TermRenderer
	// transcript mirrors what was written, ANSI included.
	transcript bytes.Buffer
	// wrap is the width the markdown wraps at.
	wrap int
	// width is the terminal's own width in columns: the width the
	// streamed text is laid out at. It is zero for a writer that is
	// not a terminal, which streams nothing.
	width int
	// block is the message the deltas are streaming right now.
	block streamBlock
}

// streamBlock is the raw text one message printed as it arrived, and
// the place in the transcript where the rendered message goes.
type streamBlock struct {
	// raw is the text printed so far. The line count the erase needs
	// comes from it, because a terminal wraps what it prints.
	raw strings.Builder
	// mark is the transcript's length before the raw text landed in
	// it: where the rendered message goes.
	mark int
	// hint says the writing hint stands on the terminal, in the place
	// the text will.
	hint bool
}

// newRenderer builds the renderer for out.
//
// A terminal gets the auto style, which picks the palette from the
// terminal's own background, and the width it lays text out at;
// anything else gets plain text, so a test or a pipe reads the same
// words without the escapes, and streams nothing.
func newRenderer(out io.Writer) *renderer {
	r := &renderer{out: out, wrap: 80}
	if termIsTerminal(out) {
		if f, ok := out.(interface{ Fd() uintptr }); ok {
			// A terminal that will not say how wide it is keeps the
			// width the markdown is wrapped at.
			r.width = r.wrap + 2
			if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 4 {
				r.width = w
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
	fmt.Fprint(r.out, s)
}

// streams reports whether text goes to the terminal as it arrives.
// Only a terminal streams: a pipe takes the rendered message when it
// completes, so its bytes stay the same from run to run.
func (r *renderer) streams() bool {
	return r.width > 0
}

// hint shows the dim line that the model is writing, in the place the
// streamed text will take.
//
// It stands until the message's first text arrives. A message whose
// text has started needs no hint, and a pipe shows none at all.
func (r *renderer) hint() {
	if !r.streams() || r.block.hint || r.block.raw.Len() > 0 {
		return
	}
	fmt.Fprint(r.out, ansiDim+"… writing"+ansiReset)
	r.block.hint = true
}

// takeHint takes the writing hint back off the terminal.
func (r *renderer) takeHint() {
	if r.block.hint {
		fmt.Fprint(r.out, "\r"+ansi.EraseLine(0))
		r.block.hint = false
	}
}

// streamText prints a text delta to the terminal as it arrives, raw,
// and keeps the copy the transcript holds until the message renders.
//
// The text is not markdown yet: a stream that is still arriving can end
// mid-word or mid-fence, so rendering waits for the whole message.
func (r *renderer) streamText(text string) {
	if !r.streams() || text == "" {
		return
	}
	if r.block.raw.Len() == 0 {
		// The block starts here, on the line the hint stood on.
		r.takeHint()
		r.block.mark = r.transcript.Len()
	}
	r.block.raw.WriteString(text)
	r.write(text)
}

// erase takes back what the stream put on the terminal - the raw text,
// or the hint that stood in for it - and leaves the cursor where the
// block began, with everything below it cleared.
//
// Nothing above the block is touched: the block starts right after the
// last thing printed, and the cursor move is counted from the lines the
// terminal wrapped it over. The raw text leaves the transcript with it,
// so the rendered message takes its place rather than joining it.
func (r *renderer) erase() {
	r.takeHint()
	if r.block.raw.Len() == 0 {
		return
	}
	lines := wrappedLines(r.block.raw.String(), r.width)
	// Back to the block's first column, up to its first line - the
	// cursor is in the last one - and clear from there down.
	var seq strings.Builder
	seq.WriteString("\r")
	if lines > 1 {
		seq.WriteString(ansi.CursorUp(lines - 1))
	}
	seq.WriteString(ansi.EraseDisplay(0))
	fmt.Fprint(r.out, seq.String())
	r.transcript.Truncate(r.block.mark)
	r.block.raw.Reset()
}

// wrappedLines counts the terminal lines a raw text fills when it is
// printed at width columns.
//
// A line that fills the width exactly counts once, not twice: the
// terminal keeps the cursor on its last cell until the next character
// arrives, and the erase counts from the cursor. An empty line counts
// once, the one the cursor sits on, and that includes the empty last
// line a trailing newline leaves behind.
func wrappedLines(text string, width int) int {
	if width <= 0 {
		return 1
	}
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		n := (ansi.StringWidth(line) + width - 1) / width
		if n < 1 {
			n = 1
		}
		lines += n
	}
	return lines
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
func shortTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	case n >= 0:
		return fmt.Sprintf("%d", n)
	default:
		return "?"
	}
}
