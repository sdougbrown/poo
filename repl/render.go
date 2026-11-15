package repl

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldmarktext "github.com/yuin/goldmark/text"
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

// maxResultLines is how many body lines of a tool result the
// transcript shows. A longer result keeps its head and says what was
// cut.
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
	// transcript mirrors the writes that went through write, and the
	// rendered chunks a stream commits as they finish: the conversation
	// as the pager shows it again. The cursor-control sequences the
	// streaming writes — the erases, the repaints of the preview — go to
	// the terminal directly, and are not part of it.
	transcript bytes.Buffer
	// pending holds the tail of a streamed delta that may be an escape
	// sequence still arriving: a sequence split across deltas is
	// stripped when its tail lands, not leaked as bytes.
	pending string
	// wrap is the width the markdown wraps at.
	wrap int
	// width is the terminal's own width in columns. It is zero for a
	// writer that is not a terminal, which streams nothing.
	width int
	// editor says the sink is the line editor's writer, whose redraws
	// erase their own row: streamed rows are sealed with their own
	// newline so the redraw passes below them.
	editor bool
	// liveWidth reads the terminal's width now, when the renderer has a
	// reader for it: the streamed rows and the editor's prompt must
	// wrap at the same width.
	liveWidth func() int
	// block is the rows a live stream has painted above the prompt.
	block streamBlock
	// live is the message the deltas are streaming right now.
	live streamLive
}

// streamBlock is what a live stream has standing on the terminal.
type streamBlock struct {
	// started says the block has content: the hint, if one stood, is
	// gone.
	started bool
	// stalled says the terminal changed width while the block was
	// streaming. The rows already painted wrap at the old width, so
	// painting stops until the flush renders them again.
	stalled bool
	// previewRows is how many rows the uncommitted preview fills
	// directly above the prompt's row: the rows the next delta erases
	// before painting again.
	previewRows int
	// hint says the writing hint stands on the terminal, where the
	// text will.
	hint bool
}

// streamLive is one message being streamed: the markdown a segment has
// not committed yet, and the segments a stalled block collected after
// the last row it could paint.
type streamLive struct {
	// kind is the segment's kind: "text" for the reply, "thinking" for
	// everything that is not the reply. Empty when no segment is open.
	kind string
	// source is the markdown the segment has not committed. A thinking
	// segment opens with thinkingIntro, so the heading commits with the
	// text it introduces.
	source string
	// parked are the segments a stalled block could not paint, oldest
	// first. They render when the block is flushed.
	parked []streamSegment
}

// streamSegment is one kind's markdown, waiting to be rendered.
type streamSegment struct {
	kind   string
	source string
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
		s = "\r" + terminalNewlines(s)
	}
	fmt.Fprint(r.sink, s)
}

// terminalNewlines converts bare LF to CRLF without duplicating carriage returns.
func terminalNewlines(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(s[i])
	}
	return b.String()
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

// streams reports whether text goes to the terminal as it arrives.
// Only a terminal with the editor streaming: anything else takes the
// rendered message when it completes, so its bytes stay the same from
// run to run.
func (r *renderer) streams() bool {
	return r.editor && r.width > 0
}

// hint shows the dim line that the model is writing, on a row of its
// own above the prompt, until the message's first delta arrives.
func (r *renderer) hint() {
	if !r.streams() || r.block.hint || r.block.started {
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

// streamDelta renders one delta of a message as it arrives. The reply
// streams as markdown paragraphs, committed one block at a time; the
// unfinished tail is a preview rendered by the same markdown renderer,
// so the words do not move or change style when the block settles.
// Thinking streams the same way, dimmed under its own heading. Each
// kind keeps the place it arrived in relative to the other, so a
// thought after the reply does not jump back above it.
//
// Escape sequences the delta carries are stripped first: a model's text
// is data, and a sequence split across deltas is held pending and
// stripped when its tail lands.
//
// The editor erases its own row on every redraw, so every row the
// stream paints is sealed with its own newline and the prompt redraw
// passes below it.
func (r *renderer) streamDelta(kind, text string) {
	if text == "" {
		return
	}
	if kind != "text" {
		// A kind the repl does not know reads with the thinking: it
		// must not join the reply's body.
		kind = "thinking"
	}
	clean, pending := stripStream(r.pending + text)
	r.pending = pending
	if clean == "" {
		return
	}
	if !r.streams() {
		r.bufferDelta(kind, clean)
		return
	}
	if !r.block.started {
		r.takeHint()
		r.block.started = true
	}
	if !r.block.stalled && r.stalledNow() {
		r.block.stalled = true
	}
	if r.block.stalled {
		r.bufferDelta(kind, clean)
		return
	}
	if r.live.kind != kind {
		r.switchKind(kind)
	}
	r.live.source += clean
	r.paintStream()
}

// contentWidth is the columns the terminal has now: the live width
// when the terminal says one, the renderer's own otherwise.
func (r *renderer) contentWidth() int {
	if r.liveWidth != nil {
		if live := r.liveWidth(); live > 0 {
			return live
		}
	}
	return r.width
}

// stalledNow reports that the terminal's width has changed under a
// block that is painting: the rows already painted wrap at the old
// width, so painting stops until the flush lays them out again.
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

// thinkingIntro opens a thinking segment. It is markdown in the
// segment's own source, so the preview and the committed block render
// the heading the same way.
const thinkingIntro = "· thinking ·\n\n"

// switchKind closes the segment the deltas were writing and opens one
// of another kind. The closed segment's preview is already its final
// rendering, so it is sealed rather than rendered again.
func (r *renderer) switchKind(kind string) {
	var seq strings.Builder
	if r.block.previewRows > 0 {
		erasePreview(&seq, r.block.previewRows)
	}
	if body := r.chunkBody(r.live.kind, r.live.source); body != "" {
		r.transcript.WriteString(body)
		seq.WriteString(sealedRows(body))
	}
	r.block.previewRows = 0
	r.live.kind = kind
	r.live.source = ""
	if kind == "thinking" {
		r.live.source = thinkingIntro
	}
	if seq.Len() > 0 {
		fmt.Fprint(r.sink, seq.String())
	}
}

// paintStream commits the paragraphs the open source has finished with
// and repaints the tail that is still being written. The committed
// rows are written once and never touched again; only the preview is
// erased and painted on the next delta.
func (r *renderer) paintStream() {
	done, rest := splitStream(r.live.source)
	var seq strings.Builder
	erasePreview(&seq, r.block.previewRows)
	r.block.previewRows = 0
	if done != "" {
		if body := r.chunkBody(r.live.kind, done); body != "" {
			r.transcript.WriteString(body)
			seq.WriteString(sealedRows(body))
		}
		r.live.source = rest
	}
	if body := r.chunkBody(r.live.kind, rest); body != "" {
		seq.WriteString(sealedRows(body))
		r.block.previewRows = strings.Count(body, "\n")
	}
	fmt.Fprint(r.sink, seq.String())
}

// bufferDelta adds a delta to the open segment without painting it: a
// sink that cannot stream renders the segments whole at the flush, and
// a stalled block collects everything after the last row it painted.
func (r *renderer) bufferDelta(kind, text string) {
	if r.live.kind != kind {
		if r.live.kind != "" {
			r.live.parked = append(r.live.parked, streamSegment{kind: r.live.kind, source: r.live.source})
		}
		r.live.kind = kind
		r.live.source = ""
		if kind == "thinking" {
			r.live.source = thinkingIntro
		}
	}
	r.live.source += text
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

// finish ends the streamed block: a pipe and a stalled block render
// what they collected, and a live block seals the preview that already
// stands, so no row it painted moves. The block's rows are above the
// prompt and stay there; the next write starts below them.
func (r *renderer) finish() {
	r.takeHint()
	if !r.streams() {
		if body := r.pendingBody(); body != "" {
			r.write(body)
		}
		r.resetStream()
		return
	}
	if r.block.stalled {
		if body := r.pendingBody(); body != "" {
			var seq strings.Builder
			erasePreview(&seq, r.block.previewRows)
			seq.WriteString(sealedRows(body))
			r.transcript.WriteString(body)
			fmt.Fprint(r.sink, seq.String())
		}
		r.resetStream()
		return
	}
	if body := r.chunkBody(r.live.kind, r.live.source); body != "" {
		// The preview already shows this body: sealing it in the
		// transcript is all that is left.
		r.transcript.WriteString(body)
	}
	r.resetStream()
}

// resetStream closes the block.
func (r *renderer) resetStream() {
	r.pending = ""
	r.block = streamBlock{}
	r.live = streamLive{}
}

// pendingBody renders every segment the stream has not sealed yet,
// oldest first.
func (r *renderer) pendingBody() string {
	var b strings.Builder
	for _, seg := range r.live.parked {
		b.WriteString(r.chunkBody(seg.kind, seg.source))
	}
	b.WriteString(r.chunkBody(r.live.kind, r.live.source))
	return b.String()
}

// chunkBody renders one markdown chunk the way the stream paints it:
// the leading blank glamour puts above a block is dropped, since the
// row the chunk lands on is already the right place for it. A thinking
// chunk is dimmed whole.
func (r *renderer) chunkBody(kind, source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	body := strings.TrimLeft(r.markdown(source), "\n")
	if kind == "thinking" {
		body = dimText(body)
	}
	return body
}

// dimText dims a rendered block whole. A reset inside the block is
// followed by the dim again, so the renderer's own styling does not
// brighten the rest of the line it touches. The dim is closed before the
// block's trailing newlines, so the rows the block fills end where its
// text does.
func dimText(text string) string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return text
	}
	trailing := text[len(trimmed):]
	trimmed = strings.ReplaceAll(trimmed, ansiReset, ansiReset+ansiDim)
	return ansiDim + trimmed + ansiReset + trailing
}

// erasePreview moves to the first row of the preview and clears from
// there down, including the prompt's row. The carriage return starts
// at column zero, wherever the cursor stood.
func erasePreview(seq *strings.Builder, rows int) {
	seq.WriteString("\r")
	if rows > 0 {
		seq.WriteString(ansi.CursorUp(rows))
	}
	seq.WriteString(ansi.EraseDisplay(0))
}

// sealedRows writes a rendered block as rows the terminal keeps: each
// line ends in its own CRLF, so the editor's redraw passes below the
// block instead of erasing the row it shares.
func sealedRows(body string) string {
	if body == "" {
		return ""
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return terminalNewlines(body)
}

// streamParser reads the block structure of the open source. It is
// the same goldmark the markdown renderer already depends on, so the
// stream commits on the boundaries the render will use.
var streamParser = goldmark.DefaultParser()

// splitStream splits the open markdown source after its last completed
// paragraph, when another top-level block follows it. A paragraph's
// last line has a known end, so everything above the split is complete,
// fences and lists included; the block after it stays pending whole.
// Nothing is committed when no paragraph ends before the final block.
func splitStream(source string) (done, rest string) {
	doc := streamParser.Parse(goldmarktext.NewReader([]byte(source)))
	last := doc.LastChild()
	if last == nil {
		return "", source
	}
	var candidate ast.Node
	for n := last.PreviousSibling(); n != nil; n = n.PreviousSibling() {
		if _, ok := n.(*ast.Paragraph); ok {
			candidate = n
			break
		}
	}
	if candidate == nil {
		return "", source
	}
	end := streamParagraphEnd(candidate, source)
	if end <= 0 {
		return "", source
	}
	return source[:end], source[end:]
}

// streamParagraphEnd returns the offset just after the paragraph's last
// line, its newline included. A paragraph has no closing marker, so that
// line is where its source ends.
func streamParagraphEnd(n ast.Node, source string) int {
	lines := n.Lines()
	if lines == nil || lines.Len() == 0 {
		return -1
	}
	end := lines.At(lines.Len() - 1).Stop
	if end < len(source) && source[end] == '\n' {
		end++
	}
	return end
}

// userPrompt echoes the prompt the session runs.
func (r *renderer) userPrompt(text string) {
	r.write(ansiBold + "❯ " + text + ansiReset + "\n")
}

// steerPrompt echoes a line the person sent to the run that is
// working. The mark says the line rode the run instead of opening one:
// the model reads it at the run's next turn end.
func (r *renderer) steerPrompt(text string) {
	r.write(ansiBold + "» " + text + ansiReset + "\n")
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

// toolCall renders one tool call.
func (r *renderer) toolCall(tool, summary string) {
	tool = clipLine(sanitizeLine(tool), max(1, r.previewContentWidth()-4))
	line := ansiCyan + "⚙ " + tool + ansiReset
	if summary != "" {
		// The summary is one line of the model's own text: it reads
		// to the terminal's edge, and no further.
		budget := r.previewContentWidth() - ansi.StringWidth(tool) - 3
		if budget > 0 {
			line += " " + ansiDim + clipLine(sanitizeLine(firstLine(summary)), budget) + ansiReset
		}
	}
	r.write(line + "\n")
}

// toolResult renders one tool result.
func (r *renderer) toolResult(tool, outcome, text string, elided int) {
	p := previewResult(text, elided, r.previewContentWidth(), maxResultLines)
	var b strings.Builder
	if p.handle != "" {
		b.WriteString(resultIndent + ansiDim + p.handle + ansiReset + "\n")
	}
	for _, line := range p.body {
		b.WriteString(resultIndent + ansiDim + line + ansiReset + "\n")
	}
	if p.moreLines > 0 {
		b.WriteString(fmt.Sprintf("%s%s… +%d more lines%s\n", resultIndent, ansiDim, p.moreLines, ansiReset))
	}
	if p.elidedChars > 0 {
		b.WriteString(fmt.Sprintf("%s%s… %d characters elided%s\n", resultIndent, ansiDim, p.elidedChars, ansiReset))
	}
	if p.hint != "" {
		b.WriteString(resultIndent + ansiDim + p.hint + ansiReset + "\n")
	}
	if p.status != "" {
		b.WriteString(resultIndent + ansiYellow + p.status + ansiReset + "\n")
	}
	if outcome != "ok" && outcome != "" {
		outcome = sanitizeLine(outcome)
		name := clipLine(sanitizeLine(tool), max(1, r.previewContentWidth()-ansi.StringWidth(outcome)-3))
		b.WriteString(fmt.Sprintf("%s%s%s: %s%s\n", resultIndent, ansiRed, name, outcome, ansiReset))
	}
	r.write(b.String())
}

// previewContentWidth is the columns a tool result's line has after the
// transcript's indent, at the terminal's width now.
func (r *renderer) previewContentWidth() int {
	width := r.contentWidth()
	if width <= 0 {
		width = r.wrap
	}
	return max(1, width-ansi.StringWidth(resultIndent))
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
