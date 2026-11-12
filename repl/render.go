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
	// sink is where the transcript's lines go: out, or the editor's own
	// writer while the editor holds out, so the prompt is redrawn under
	// each line instead of being overwritten by it.
	sink io.Writer
	// term says out is a terminal.
	term bool
	// tr renders markdown for the terminal. It is nil for a writer
	// that is not a terminal, where markdown renders as plain text.
	tr *glamour.TermRenderer
	// transcript mirrors the writes that went through write: the
	// rendered replies, the streamed raw text until erase takes it
	// back, ANSI included. The cursor-control sequences the streaming
	// writes — the hints, the erases — go to the terminal directly,
	// and are not part of it.
	transcript bytes.Buffer
	// pending holds the tail of a streamed delta that may be an escape
	// sequence still arriving: a sequence split across deltas is
	// stripped when its tail lands, not leaked as bytes.
	pending string
	// wrap is the width the markdown wraps at.
	wrap int
	// width is the terminal's own width in columns: the width the
	// streamed text is laid out at. It is zero for a writer that is
	// not a terminal, which streams nothing.
	width int
	// editor says the sink is the line editor's writer, whose redraws
	// erase their own row: streamed text shares that row, so it goes
	// out sealed rather than plain.
	editor bool
	// liveWidth reads the terminal's width now, when the renderer has a
	// reader for it: the streamed rows and the editor's prompt must
	// wrap at the same width.
	liveWidth func() int
	// block is the message the deltas are streaming right now.
	block streamBlock
}

// streamBlock is the raw text one message printed as it arrived, and
// the place in the transcript where the rendered message goes.
type streamBlock struct {
	// started says the block has text: the transcript's mark is set
	// and the hint, if one stood, is gone.
	started bool
	// stalled says the terminal changed width while the block was
	// streaming. The rows already painted wrap at the old width, so
	// painting stops until the render replaces them at the new one.
	stalled bool
	// committed is the rows the block has sealed above the prompt's
	// row: full rows the terminal will not erase, because each ends in
	// its own newline.
	committed int
	// tail is the text the block has not sealed: the partial row that
	// sits directly above the prompt's row, rewritten whole on every
	// delta. Empty when the text ended on a row boundary and the
	// prompt sits under the last committed row.
	tail string
	// mark is the transcript's length before the raw text landed in
	// it: where the rendered message goes.
	mark int
	// hint says the writing hint stands on the terminal, in the place
	// the text will.
	hint bool
}

// rows is the rows the block fills above the prompt's row: the
// committed rows plus the partial one when it stands.
func (b streamBlock) rows() int {
	if b.tail != "" {
		return b.committed + 1
	}
	return b.committed
}

// newRenderer builds the renderer for out.
//
// A terminal gets the auto style, which picks the palette from the
// terminal's own background, and the width it lays text out at;
// anything else gets plain text, so a test or a pipe reads the same
// words without the escapes, and streams nothing.
func newRenderer(out io.Writer) *renderer {
	r := &renderer{out: out, sink: out, wrap: 80}
	r.term = termIsTerminal(out)
	if r.term {
		if f, ok := out.(interface{ Fd() uintptr }); ok {
			// A terminal that will not say how wide it is keeps the
			// width the markdown is wrapped at, so the streamed rows and
			// the render they are replaced by break in the same place.
			r.width = r.wrap
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
	if r.streams() {
		// The editor's cursor may stand mid-row, where the person's
		// last keystroke left it: the line starts at column zero on the
		// terminal, and the transcript keeps the line without it.
		s = "\r" + s
	}
	fmt.Fprint(r.sink, s)
}

// streams reports whether text goes to the terminal as it arrives.
// Only a terminal with the editor streaming: anything else takes the
// rendered message when it completes, so its bytes stay the same from
// run to run.
func (r *renderer) streams() bool {
	return r.editor && r.width > 0
}

// hint shows the dim line that the model is writing, on a row of its
// own above the prompt, until the message's first text arrives.
func (r *renderer) hint() {
	if !r.streams() || r.block.hint || r.block.tail != "" || r.block.committed > 0 {
		return
	}
	// Through the editor's writer, so the Refresh clears the prompt's
	// row, paints the hint, and redraws the prompt below; the newline
	// seals the row, and a keystroke cannot take it back. The carriage
	// return starts the row at column zero, wherever the cursor stood.
	fmt.Fprint(r.sink, "\r"+ansiDim+"… writing"+ansiReset+"\r\n")
	r.block.hint = true
}

// takeHint takes the writing hint back off the terminal.
func (r *renderer) takeHint() {
	if !r.block.hint {
		return
	}
	// The hint stands on a sealed row above the prompt: climb up and
	// take the row away inside one Refresh, and the prompt's redraw
	// fills the gap. The carriage return erases the row whole, from
	// column zero, wherever the cursor stood.
	fmt.Fprint(r.sink, "\r"+ansi.CursorUp(1)+ansi.EraseDisplay(0))
	r.block.hint = false
}

// streamText prints a text delta to the terminal as it arrives, and
// keeps the copy the transcript holds until the message renders.
//
// Escape sequences the delta carries are stripped first: a model's text
// is data, and a sequence split across deltas is held pending and
// stripped when its tail lands.
//
// The editor erases its own row on every redraw, so text that shares
// the prompt's row dies with the next keystroke. Each delta therefore
// paints rows sealed with their own newline: completed rows commit and
// stand, and the one partial row sits directly above the prompt,
// rewritten whole on every delta, hard-wrapped at the terminal's
// width.
func (r *renderer) streamText(text string) {
	if !r.streams() || text == "" {
		return
	}
	if !r.block.started {
		r.takeHint()
		r.block.started = true
		r.block.mark = r.transcript.Len()
	}
	if r.stalledNow() {
		r.block.stalled = true
	}
	clean, pending := stripStream(r.pending + text)
	r.pending = pending
	if clean == "" {
		return
	}
	r.transcript.WriteString(clean)
	if r.block.stalled {
		// The rows on the terminal wrap at the old width; the render
		// at the flush clears them and lays the message out again.
		return
	}
	rows := hardRows(r.block.tail+clean, r.width)
	committed, tail := rows[:len(rows)-1], rows[len(rows)-1]
	var seq strings.Builder
	// The paint starts at column zero: the cursor may stand mid-row,
	// over the prompt or a draft, and the rows count from column zero.
	seq.WriteString("\r")
	if r.block.tail != "" {
		// Up one row, to the partial row this delta repaints.
		seq.WriteString(ansi.CursorUp(1))
	}
	seq.WriteString(ansi.EraseDisplay(0))
	for _, row := range committed {
		seq.WriteString(row)
		seq.WriteString("\r\n")
	}
	if tail != "" {
		seq.WriteString(tail)
		seq.WriteString("\r\n")
	}
	fmt.Fprint(r.sink, seq.String())
	r.block.committed += len(committed)
	r.block.tail = tail
}

// stalledNow reports that the terminal's width has changed under a
// block that is painting: the rows it committed wrap at the old width,
// and counting them at the new one would climb to the wrong row.
func (r *renderer) stalledNow() bool {
	if r.liveWidth == nil {
		return false
	}
	width := r.liveWidth()
	if width <= 0 || width == r.width {
		return false
	}
	r.width = width
	return true
}

// hardRows splits a text the way the block paints it: rows of at most
// width columns, each newline ending a row, the last row left open.
func hardRows(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	// Hardwrap measures grapheme clusters, which is what the terminal
	// lays out; counting runes would wrap a family emoji early.
	return strings.Split(ansi.Hardwrap(expandTabs(text), width, true), "\n")
}

// expandTabs puts the spaces a terminal's tab stops would, counting from
// the start of the tab's own line.
func expandTabs(text string) string {
	if !strings.ContainsRune(text, '\t') {
		return text
	}
	var b strings.Builder
	col := 0
	for _, r := range text {
		switch r {
		case '\n':
			b.WriteRune(r)
			col = 0
		case '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		default:
			b.WriteRune(r)
			col += ansi.StringWidth(string(r))
		}
	}
	return b.String()
}

// stripStream removes the escape sequences a delta carries and holds
// back a tail that may be a sequence still arriving — an ESC whose
// sequence has not landed is not a byte to print.
func stripStream(text string) (clean, pending string) {
	i := strings.LastIndexByte(text, '\x1b')
	if i < 0 {
		return ansi.Strip(text), ""
	}
	if completeEscape(text[i:]) {
		return ansi.Strip(text), ""
	}
	if tail := text[i:]; len(tail) > maxPending {
		// An escape that never ends is not a sequence the terminal is
		// meant to obey: the held bytes go rather than growing without
		// a bound, since a server controls the deltas.
		return ansi.Strip(text[:i]), ""
	}
	return ansi.Strip(text[:i]), text[i:]
}

// maxPending is the most bytes an unfinished escape sequence may hold
// back. A real sequence is a few bytes; anything longer is data the
// stream is not going to be told to obey.
const maxPending = 64

// completeEscape says whether the text from its ESC holds a sequence
// the terminal has all of: a CSI with its final byte, an OSC with its
// terminator, or the two-byte escapes.
func completeEscape(tail string) bool {
	if len(tail) < 2 || tail[0] != '\x1b' {
		return false
	}
	switch tail[1] {
	case '[':
		return strings.IndexFunc(tail[2:], isFinalByte) >= 0
	case ']':
		return strings.IndexByte(tail[2:], 0x07) >= 0 || strings.Contains(tail[2:], "\x1b\\")
	}
	return true
}

// isFinalByte reports that b ends a control sequence: the range ECMA-48
// gives the final byte. The editor's key filter reads the same range, so
// a sequence it swallows and one the stream strips agree on where the
// sequence ends.
func isFinalByte(b rune) bool {
	return b >= 0x40 && b <= 0x7e
}

// replace takes the streamed block off the terminal and puts the
// rendered message in its place, in one write the editor's Refresh
// wraps: the erase and the paint share the write, so the cursor never
// sits where readline does not expect it between them. A pipe takes
// the rendered message alone.
func (r *renderer) replace(content string) {
	r.pending = ""
	if !r.streams() || !r.block.started {
		r.takeHint()
		if content != "" {
			r.write(content)
		}
		return
	}
	r.transcript.Truncate(r.block.mark)
	r.transcript.WriteString(content)
	// The render's newlines go out as CRLFs: a bare line feed keeps
	// the column, and the next row would start mid-line.
	content = strings.TrimLeft(content, "\n")
	content = strings.ReplaceAll(content, "\n", "\r\n")
	var seq strings.Builder
	// The climb starts at column zero, wherever the cursor stood: the
	// rows the block filled count from there.
	seq.WriteString("\r")
	if lines := r.block.rows(); lines > 0 {
		seq.WriteString(ansi.CursorUp(lines))
	}
	seq.WriteString(ansi.EraseDisplay(0))
	seq.WriteString(content)
	fmt.Fprint(r.sink, seq.String())
	r.block = streamBlock{}
}

// userPrompt echoes the prompt the session runs.
func (r *renderer) userPrompt(text string) {
	r.write(ansiBold + "❯ " + text + ansiReset + "\n")
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

// thinkingText renders one thinking block dimmed.
func (r *renderer) thinkingText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return ansiDim + "· thinking ·\n" + text + ansiReset + "\n"
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
