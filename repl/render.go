package repl

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldmarktext "github.com/yuin/goldmark/text"
	"golang.org/x/term"

	"github.com/sdougbrown/poo/wire/protocol"
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

// brailleFrames are the spinner's frames, shown in turn while the
// model writes or a tool runs.
var brailleFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// streamWriteDelay is how long a tool call runs before its spinner
// shows: a call that returns within it never gains a row.
const streamWriteDelay = 2 * time.Second

// maxResultLines is how many body lines of a tool result the
// transcript shows. A longer result keeps its head and says what was
// cut.
const maxResultLines = 8

// renderer writes the transcript: the rendered replies, the tool
// calls' decoration and the notices.
type renderer struct {
	// out is the terminal the transcript goes to.
	out io.Writer
	// layout is the terminal's pinned input area, when out is a
	// terminal and the session runs the bottom-anchored layout.
	layout *layout
	// term says out is a terminal.
	term bool
	// tr renders markdown for the terminal. It is nil for a writer
	// that is not a terminal, where markdown renders as plain text.
	tr *glamour.TermRenderer
	// newTR builds the markdown renderer at a width, so a terminal that
	// changes size renders the next message at the new one.
	newTR func(wrap int) (*glamour.TermRenderer, error)
	// transcript records the conversation when no terminal layout owns it.
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
	// hint says the writing hint stands on the terminal, where the
	// text will.
	hint bool
	// hintFrame counts the spinner frames the writing hint has shown,
	// so the run loop's frames advance it.
	hintFrame int
	// spinner is the running tool call's live row, nil when no tool is
	// running. The run loop's frames repaint it once the call has run
	// past streamWriteDelay, and toolResult takes it down.
	spinner *toolSpinner
}

// toolSpinner is a tool call the transcript is still waiting on. The
// run loop paints its row while it waits, after a delay that keeps a
// fast call's row from ever appearing.
type toolSpinner struct {
	// tool names the call.
	tool string
	// summary is the call's first summary line, when the event gave
	// one.
	summary string
	// started is when the call's event landed.
	started time.Time
}

// streamLive is one message being streamed. It holds the markdown a
// segment has not committed yet, and the segments a stalled block
// collected after the last row it could paint.
type streamLive struct {
	// id is the message the deltas belong to. A delta that names
	// another message ends the open block first, so two messages never
	// run their words together.
	id string
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
	// dirty says the source has changed since the last paint: the run
	// loop paints it on its next frame, or the flush paints it first.
	dirty bool
}

// streamSegment is one kind's markdown, waiting to be rendered.
type streamSegment struct {
	kind   string
	source string
}

// newRenderer builds the renderer for out.
//
// A terminal resolves its style once here, before anything reads stdin.
// Non-terminal writers get plain text without escapes and do not stream.
func newRenderer(out io.Writer) *renderer {
	r := &renderer{out: out, wrap: 80}
	r.term = termIsTerminal(out)
	if r.term {
		r.armPalette(func() string { return detectGlamourStyle(out) })
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
		if tr, err := r.newTR(r.wrap); err == nil {
			r.tr = tr
		}
	}
	return r
}

// armPalette settles the style the markdown renders in, and builds the
// builder every width change goes through.
//
// The query asks the terminal for OSC 11 and a cursor report. Those
// replies arrive on stdin while the line editor reads it. Asking once at
// construction lets resizes, rewraps, and stall recoveries reuse the
// answer.
func (r *renderer) armPalette(detect func() string) {
	r.newTR = termRenderer(detect())
}

// detectGlamourStyle resolves the standard style the transcript renders
// in from the output terminal's own palette.
func detectGlamourStyle(out io.Writer) string {
	if !termIsTerminal(out) {
		return styles.NoTTYStyle
	}
	if termenv.NewOutput(out).HasDarkBackground() {
		return styles.DarkStyle
	}
	return styles.LightStyle
}

// termRenderer builds the markdown renderer at a wrap width in one
// resolved style. It names the palette, never the question that picks
// it, so rebuilding it asks the terminal for nothing.
func termRenderer(style string) func(wrap int) (*glamour.TermRenderer, error) {
	return func(wrap int) (*glamour.TermRenderer, error) {
		return glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(wrap),
		)
	}
}

// rewrap lays future messages out at the terminal's new width.
func (r *renderer) rewrap(width int) bool {
	if width <= 4 || width == r.width {
		return false
	}
	return r.setWidth(width)
}

// setWidth moves the width, the wrap, and the markdown renderer
// together, so a render never lays out at one width and paints its rows
// at another.
func (r *renderer) setWidth(width int) bool {
	r.width = width
	r.wrap = width - 2
	if r.newTR == nil {
		return true
	}
	if tr, err := r.newTR(r.wrap); err == nil {
		r.tr = tr
	}
	return true
}

// write appends a chunk to the transcript and the terminal. A line the
// repl writes for itself — a steer's echo, a notice, a failure — ends
// the open block first. The screen and the transcript keep one order.
func (r *renderer) write(s string) {
	r.takeSpinner()
	if r.block.hint || r.live.kind != "" {
		r.finish()
	}
	r.emit(s)
}

// emit writes s without touching the stream: the stream's own flushes
// use it, so a write never re-enters the boundary that called it.
func (r *renderer) emit(s string) {
	if r.layout != nil {
		r.layout.write(s)
		return
	}
	r.transcript.WriteString(s)
	if r.term {
		// A terminal whose footer could not be built still reads rows:
		// every bare line feed gains its carriage return, and each row
		// starts at column zero, wherever the cursor stood.
		fmt.Fprint(r.out, "\r"+terminalLF(s))
		return
	}
	fmt.Fprint(r.out, s)
}

// terminalLF converts bare LF to CRLF without duplicating the carriage
// returns rows already carry.
func terminalLF(s string) string {
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

// paintPreviewBody paints one rendered body as the live preview.
func (r *renderer) paintPreviewBody(body string) {
	rows := textRows(body, r.width)
	if len(rows) == 0 {
		return
	}
	r.layout.paintPreview(rows)
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
	return r.layout != nil && r.width > 0
}

// hint shows the line that says work is happening, on a row of its own
// above the prompt, until the message's first delta arrives. The frame
// is the spinner's first braille cell; the run loop's frames advance
// it through paintHintFrame.
func (r *renderer) hint() {
	if !r.streams() || r.block.hint || r.block.started {
		return
	}
	r.layout.hint(r.writingHint())
	r.block.hint = true
}

// paintHintFrame advances the writing hint's braille spinner one frame.
// It paints nothing without a hint or after a width change; the old row
// wraps until the resize rebuilds the screen.
func (r *renderer) paintHintFrame() {
	if !r.streams() || !r.block.hint || r.stallIfResized() {
		return
	}
	r.block.hintFrame = (r.block.hintFrame + 1) % len(brailleFrames)
	r.layout.hint(r.writingHint())
}

func (r *renderer) writingHint() string {
	return ansiDim + string(brailleFrames[r.block.hintFrame]) + " writing" + ansiReset
}

// takeHint takes the writing hint back off the terminal.
func (r *renderer) takeHint() {
	if !r.block.hint {
		return
	}
	r.layout.clearPreview()
	r.block.hint = false
}

// streamDelta renders one delta of a message as it arrives. The id
// says which message the delta belongs to: one that names another
// message ends the open block first, so two messages never run their
// words together; the renderer owns the rule, beside the one for
// writes. The reply
// streams as markdown paragraphs, committed one block at a time. The
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
// The layout keeps the editor below the region, so the stream paints
// its rows above the footer and the prompt never shares a row with
// them.
func (r *renderer) streamDelta(id, kind, text string) {
	if id != "" {
		if r.live.kind != "" && id != r.live.id {
			r.finish()
		}
		r.live.id = id
	}

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
	if r.stallIfResized() {
		r.bufferDelta(kind, clean)
		return
	}
	first := r.live.kind != kind
	if first {
		r.switchKind(kind)
	}
	r.live.source += clean
	if first {
		// The first text of a kind lands at once; the frames that
		// follow coalesce until the run loop paints them.
		r.paintStream()
		r.live.dirty = false
		return
	}
	r.live.dirty = true
}

// paintPending paints the source a delta left dirty, once. The run loop
// calls it on its frame, and the flush calls it before it seals.
func (r *renderer) paintPending() {
	if !r.streams() || !r.live.dirty || r.stallIfResized() {
		return
	}
	r.paintStream()
	r.live.dirty = false
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

// drainStall clears the resize stall latch and assembles any parked
// segments and live preview rows at width.
func (r *renderer) drainStall(width int) (string, []string) {
	var committed strings.Builder
	for _, seg := range r.live.parked {
		committed.WriteString(r.chunkBody(seg.kind, seg.source))
	}
	r.live.parked = nil
	r.block.stalled = false
	r.live.dirty = false

	var rows []string
	switch {
	case r.block.hint:
		rows = []string{ansi.Truncate(r.writingHint(), width, "")}
	case r.live.kind != "":
		body, preview := r.streamBodies()
		committed.WriteString(body)
		rows = textRows(preview, width)
	}
	return committed.String(), rows
}

// resize lays out the open stream before rebuilding the screen so that
// replayed rows never have to scroll to make room for the preview.
func (r *renderer) resize(width, height, footer int) {
	r.rewrap(width)
	committed, rows := r.drainStall(width)
	r.layout.resize(width, height, footer, committed, rows)
}

// clearStall releases a resize stall when a settle cannot read a valid
// terminal size. Any parked segments and live preview paint at the last
// valid geometry so live updates do not stay frozen.
func (r *renderer) clearStall() {
	if !r.block.stalled {
		return
	}
	if !r.streams() {
		r.block.stalled = false
		r.live.dirty = false
		return
	}
	committed, rows := r.drainStall(r.width)
	if len(rows) > 0 {
		r.layout.paintStream(committed, rows)
		return
	}
	if committed != "" {
		r.layout.write(committed)
	}
}

// stalledNow reports that the terminal's size has changed under a block
// that is painting. The rows already painted match the old geometry, so
// painting stops until the resize lays them out again. When the width
// moves, wrap and markdown renderer move with it here.
func (r *renderer) stalledNow() bool {
	if r.layout != nil {
		if w, _, changed := r.layout.sizeChanged(); changed {
			if w > 4 && w != r.width {
				r.setWidth(w)
			}
			return true
		}
	}
	if r.liveWidth == nil {
		return false
	}
	width := r.liveWidth()
	if width <= 0 || width == r.width {
		return false
	}
	r.setWidth(width)
	return true
}

// stallIfResized reports whether live paints are paused for a pending
// resize, checking the terminal's current width and latching the stall
// when it moved.
func (r *renderer) stallIfResized() bool {
	if r.block.stalled {
		return true
	}
	if r.stalledNow() {
		r.block.stalled = true
		return true
	}
	return false
}

// stallForResize pauses live row paints until the screen rebuilds at
// the new size. Standing rows were wrapped at the old width, so another
// paint would stack a duplicate.
func (r *renderer) stallForResize() {
	if r.block.hint || r.block.spinner != nil || r.live.kind != "" {
		r.block.stalled = true
	}
}

// thinkingIntro opens a thinking segment. It is markdown in the
// segment's own source, so the preview and the committed block render
// the heading the same way.
const thinkingIntro = "· thinking ·\n\n"

// openSegment starts one kind's segment: the kind is set, the source
// starts empty, and a thinking segment opens with its heading. Both
// the live screen and the buffered paths open segments here, so the
// kinds read the same however the block is rendered.
func (r *renderer) openSegment(kind string) {
	r.live.kind = kind
	r.live.source = ""
	if kind == "thinking" {
		r.live.source = thinkingIntro
	}
}

// switchKind closes the segment the deltas were writing and opens one
// of another kind. The closed segment's preview is already its final
// rendering, so it is sealed rather than rendered again.
func (r *renderer) switchKind(kind string) {
	// The old segment's last tokens reach the screen before it seals.
	r.paintPending()
	if body := r.chunkBody(r.live.kind, r.live.source); body != "" {
		r.layout.commitPreview(body)
	} else {
		r.layout.clearPreview()
	}
	r.openSegment(kind)

	if body := r.chunkBody(r.live.kind, r.live.source); body != "" {
		r.paintPreviewBody(body)
	}
}

// paintStream commits completed paragraphs and replaces the preview
// with the tail that is still being written.
func (r *renderer) paintStream() {
	if r.live.kind == "" {
		return
	}
	committed, preview := r.streamBodies()
	r.layout.paintStream(committed, textRows(preview, r.width))
}

func (r *renderer) streamBodies() (committed, preview string) {
	done, rest := splitStream(r.live.source)
	r.live.source = rest
	return r.chunkBody(r.live.kind, done), r.chunkBody(r.live.kind, rest)
}

// bufferDelta adds a delta to the open segment without painting it. A
// sink that cannot stream renders the segments whole at the flush. A
// stalled block collects everything after the last row it painted.
func (r *renderer) bufferDelta(kind, text string) {
	if r.live.kind != kind {
		if r.live.kind != "" {
			r.live.parked = append(r.live.parked, streamSegment{kind: r.live.kind, source: r.live.source})
		}
		r.openSegment(kind)
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

// finish ends the streamed block. A pipe and a stalled block render
// what they collected. A live block seals the preview that already
// stands, so no row it painted moves. The block's rows are above the
// prompt and stay there; the next write starts below them.
func (r *renderer) finish() {
	r.takeHint()
	r.takeSpinner()
	r.paintPending()
	if !r.streams() {
		body := r.pendingBody()
		r.resetStream()
		if body != "" {
			r.emit(body)
		}
		return
	}
	if r.block.stalled {
		if body := r.pendingBody(); body != "" {
			r.layout.clearPreview()
			r.layout.write(body)
		}
		r.resetStream()
		return
	}
	body := r.chunkBody(r.live.kind, r.live.source)
	r.layout.commitPreview(body)
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

// chunkBody renders one markdown chunk the way the stream paints it.
// The leading blank glamour puts above a block is dropped: the row the
// chunk lands on is already the right place for it. A thinking chunk
// is dimmed whole.
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
// followed by the dim again. The renderer's own styling does not
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

// streamParser reads the block structure of the open source. It is
// the same goldmark the markdown renderer already depends on, so the
// stream commits on the boundaries the render will use.
var streamParser = goldmark.DefaultParser()

// splitStream splits the open markdown source after its last completed
// paragraph, when another top-level block follows it. A paragraph's
// last line has a known end. Everything above the split is complete,
// fences and lists included. The block after it stays pending whole.
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

// reply renders a reply the session already recorded, the way a stream's
// flush renders one: the markdown is laid out and written whole.
func (r *renderer) reply(text string) {
	if body := r.chunkBody("", text); body != "" {
		r.write(body)
	}
}

// markdown renders one markdown source for the terminal. What it hands
// back is what the terminal will obey, so it carries the styling the
// render drew and nothing else.
func (r *renderer) markdown(text string) string {
	if r.tr == nil {
		return filterRendered(strings.TrimRight(text, "\n")) + "\n"
	}
	out, err := r.tr.Render(text)
	if err != nil {
		// A source glamour refused is still the model's words: they
		// go out plain rather than not at all.
		return filterRendered(strings.TrimRight(text, "\n")) + "\n"
	}
	return filterRendered(out)
}

// filterRendered keeps the bytes of a rendered block a terminal should
// obey: the SGR sequences that carry the styling, and the text itself
// with its newlines and tabs. Every other escape sequence goes, and so
// does every C0 control, DEL and C1 control the text carries.
//
// The filter sits here, after rendering, because the renderer decodes
// character references in its text nodes: a source written &#27; holds
// no escape for the stream's strip to remove, and becomes one only when
// the reference is decoded. Reading the rendered bytes is also the one
// place both branches meet, so the plain render is filtered the same
// way.
func filterRendered(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	// The decoder gets no parser: one that collects parameters indexes
	// past its fixed capacity on a long sequence and panics, and the
	// filter reads none of what it would collect.
	var state byte
	for i := 0; i < len(s); {
		seq, _, n, next := ansi.DecodeSequence(s[i:], state, nil)
		state = next
		if n == 0 {
			// The decoder makes no progress past a byte that starts
			// no rune, and such a byte is no text.
			i++
			continue
		}
		i += n
		if keepsRendered(seq) {
			b.WriteString(seq)
		}
	}
	return b.String()
}

// keepsRendered reports whether one decoded sequence is something the
// terminal should obey.
func keepsRendered(seq string) bool {
	if len(seq) == 1 {
		// One byte: printable ASCII reads, and so do the newline and
		// the tab the render lays its rows out with. Every other C0
		// control, a raw C1 control byte and DEL do not.
		c := seq[0]
		return c == '\n' || c == '\t' || c >= 0x20 && c < 0x7f
	}
	if opensSequence(seq[0]) {
		// An escape sequence is the styling the render painted, or it
		// is not obeyed at all.
		return isSGR(seq)
	}
	if strings.ContainsFunc(seq, isC1Control) {
		// The same control a terminal obeys as a byte, written as the
		// code point that names it.
		return false
	}
	// Bytes that are no rune are no text either.
	return utf8.ValidString(seq)
}

// opensSequence reports whether c opens a control sequence. The parser
// reads the 7-bit escape and the 8-bit C1 introducers alike, and the
// raw introducer is as much a sequence to drop as the escape is.
func opensSequence(c byte) bool {
	if c == 0x1b {
		return true
	}
	switch c {
	case 0x90, 0x98, 0x9b, 0x9d, 0x9e, 0x9f: // DCS, SOS, CSI, OSC, PM, APC
		return true
	}
	return false
}

// isC1Control reports whether r is one of the C1 controls a terminal
// obeys, U+0080 through U+009F.
func isC1Control(r rune) bool {
	return r >= 0x80 && r <= 0x9f
}

// isSGR reports whether seq selects a graphic rendition: the colour and
// weight the render paints with. It is the one escape sequence the
// markdown renderer emits.
func isSGR(seq string) bool {
	if len(seq) < 3 || seq[0] != 0x1b || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return false
	}
	for i := 2; i < len(seq)-1; i++ {
		// Digits, ':' and ';' only. A private marker (< = > ?) or an
		// intermediate byte puts the sequence in another command space,
		// where 'm' is not styling: CSI > 4;2 m sets the keyboard mode.
		if seq[i] < '0' || seq[i] > ';' {
			return false
		}
	}
	return true
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

// armSpinner records the tool call the transcript is waiting on. Its
// row stands after streamWriteDelay, and stays until the result, or a
// write that ends the block, takes it down.
func (r *renderer) armSpinner(tool, summary string) {
	if !r.streams() || r.block.started || r.live.kind != "" {
		return
	}
	r.block.spinner = &toolSpinner{tool: tool, summary: summary, started: time.Now()}
}

// takeSpinner takes the running tool call's row back off the terminal.
// A block with content standing repaints its own rows, so the clear is
// its alone to make when nothing else stands.
func (r *renderer) takeSpinner() {
	if r.block.spinner == nil {
		return
	}
	r.block.spinner = nil
	if r.block.hint || r.block.started || r.live.kind != "" {
		return
	}
	r.layout.clearPreview()
}

// paintSpinner paints the running tool call's row, once the call has
// run past its delay: the braille frame advances with the wait, and a
// call past a second names the seconds it has run. It paints nothing
// while no spinner runs, the delay holds, or the block is stalled on a
// resize.
func (r *renderer) paintSpinner() {
	s := r.block.spinner
	if s == nil || !r.streams() || r.block.started || r.live.kind != "" || r.stallIfResized() {
		return
	}
	elapsed := time.Since(s.started)
	if elapsed < streamWriteDelay {
		return
	}
	tool := clipLine(sanitizeLine(s.tool), max(1, r.previewContentWidth()-4))
	row := tool
	suffix := ""
	if seconds := int(elapsed.Seconds()); seconds >= 1 {
		suffix = fmt.Sprintf(" · %ds", seconds)
	}
	if text := firstLine(s.summary); text != "" {
		budget := r.previewContentWidth() - ansi.StringWidth(tool) - 3 - ansi.StringWidth(suffix)
		if budget > 0 {
			row += " · " + clipLine(sanitizeLine(text), budget)
		}
	}
	row += suffix
	frame := brailleFrames[int(elapsed/(100*time.Millisecond))%len(brailleFrames)]
	r.layout.hint(ansiDim + string(frame) + " " + row + ansiReset)
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
//
// A run that stopped on the output limit writes the settle's text as a
// warning under the line, because the line alone reads as a stall.
func (r *renderer) settle(reason string, turns int, input, output int, text string) {
	line := ansiDim + fmt.Sprintf("✓ %s · %d turn(s) · %s in · %s out",
		reason, turns, shortTokens(input), shortTokens(output)) + ansiReset
	r.write(line + "\n")
	if reason == protocol.ReasonOutputTruncated && text != "" {
		r.warning(text)
	}
}

// notice writes a dim line that is not part of the conversation.
func (r *renderer) notice(format string, args ...any) {
	r.write(ansiDim + fmt.Sprintf(format, args...) + ansiReset + "\n")
}

// warning writes a warning the session reported.
//
// The text may carry names an endpoint chose, so it is made safe to print.
func (r *renderer) warning(text string) {
	r.write(ansiYellow + "⚠ " + sanitizeText(text) + ansiReset + "\n")
}

// failure writes a failure the repl could not get past.
func (r *renderer) failure(text string) {
	r.write(ansiRed + "✗ " + text + ansiReset + "\n")
}

// transcriptText returns the transcript so far.
func (r *renderer) transcriptText() string {
	if r.layout != nil {
		return r.layout.transcriptText()
	}
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
