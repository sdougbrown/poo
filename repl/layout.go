package repl

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// The cursor and screen controls the layout emits. They are standard
// DEC/ANSI sequences, so the terminal keeps the transcript in its own
// scrollback. The region is DECSTBM, the rows are CUP, and the scrolls
// are SU.
const (
	seqSaveCursor    = "\x1b7"   // DECSC
	seqRestoreCursor = "\x1b8"   // DECRC
	seqEraseLine     = "\x1b[2K" // EL
	seqEraseTail     = "\x1b[K"  // EL 0
	seqEraseBelow    = "\x1b[J"  // ED 0
	seqResetRegion   = "\x1b[r"  // DECSTBM full screen
)

// footerChrome is the rows the layout owns below the editor's rows:
// the rule above the statusline, and the statusline row itself. Both
// stay pinned to the screen's bottom edge.
const footerChrome = 2

// layout pins the line editor to the bottom of the terminal.
//
// The transcript scrolls in a region above a footer the editor owns, and
// the footer — statusline, prompt and draft — stays where the person is
// looking.
//
// The layout owns the terminal's geometry and every write to it, each
// operation one write under the lock so a redraw never splits a row.
type layout struct {
	mu  sync.Mutex
	out io.Writer
	// getSize reads the terminal's current size when resize tracking is
	// active. A frame or write after a resize checks here first, because
	// a wide row wraps and scrolls. Nil when no resize signals exist.
	getSize func() (int, int, bool)

	// transcript records committed text in screen order, including stderr.
	transcript bytes.Buffer

	// width and height are the terminal's size in cells.
	width, height int
	// footer is the rows reserved at the screen's bottom: the editor's
	// rows plus the chrome's two.
	footer int
	// last is the row the last committed transcript row was written
	// on. The next row goes just below it; a row below the region
	// scrolls the region first. It is zero before the first row.
	last int
	// preview is how many rows a live stream has painted under last,
	// the rows the next delta erases and paints again.
	preview int
	// previewRows holds the latest rendering, including a clipped head.
	previewRows []string
	// status is the statusline text the footer's bottom row carries.
	// It is empty until the session names its first parts.
	status string
	// chromeStale says the chrome rows need a repaint. An enter or a
	// resize sets it; the next footer sizing paints them again.
	chromeStale bool
	// active says the region is set and the footer is reserved.
	active bool
}

// termSize reads the terminal's dimensions and checks that they fit a
// layout with a scroll region above the footer.
func termSize(fd int) (int, int, bool) {
	w, h, err := term.GetSize(fd)
	return w, h, err == nil && w >= 2 && h >= 4
}

// newLayout builds the layout for a terminal. A writer that is not a
// terminal has no layout: the transcript is a plain stream, and any
// editor draws wherever the stream ends.
func newLayout(out io.Writer) *layout {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return nil
	}
	fd := int(f.Fd())
	w, h, ok := termSize(fd)
	if !ok {
		return nil
	}
	l := &layout{out: out, width: w, height: h, footer: 1 + footerChrome}
	if hasResizeSignals {
		l.getSize = func() (int, int, bool) { return termSize(fd) }
	}
	l.last = l.regionBottom() - 1
	return l
}

// size is the terminal's live size when resize tracking is active.
// Without a reader, the layout keeps the size it started with.
func (l *layout) size() (int, int, bool) {
	if l.getSize == nil {
		return 0, 0, false
	}
	return l.getSize()
}

// regionBottom is the region's last row, one above the footer.
func (l *layout) regionBottom() int { return l.height - l.footer }

// footerTop is the footer's first row.
func (l *layout) footerTop() int { return l.regionBottom() + 1 }

// raw is the writer the editor draws through. Its own escape sequences
// are left alone, but it shares the layout's lock. A redraw never splits
// a transcript write.
func (l *layout) raw() io.Writer { return layoutWriter{l} }

// layoutWriter is the editor's view of the terminal.
type layoutWriter struct{ l *layout }

func (w layoutWriter) Write(p []byte) (int, error) {
	w.l.mu.Lock()
	defer w.l.mu.Unlock()
	return w.l.out.Write(p)
}

// enter reserves the footer and puts the cursor at the editor's
// prompt row.
func (l *layout) enter() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active {
		return
	}
	l.active = true
	l.chromeStale = true
	l.emit(l.regionSeq() + l.footerSeq())
}

// leave restores the whole screen to the terminal and leaves the
// cursor where a shell prompt belongs: the footer's first row.
func (l *layout) leave() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leaveLocked()
}

func (l *layout) leaveLocked() {
	if !l.active {
		return
	}
	var b strings.Builder
	l.clearPreviewLocked(&b)
	l.active = false
	b.WriteString(seqResetRegion + l.footerSeq())
	l.emit(b.String())
}

// regionSeq sets the scroll region to the rows above the footer. The
// terminal homes the cursor, so the caller places it afterwards.
func (l *layout) regionSeq() string {
	return fmt.Sprintf("\x1b[1;%dr", l.regionBottom())
}

// footerSeq clears the footer and puts the cursor at its first row.
func (l *layout) footerSeq() string {
	return fmt.Sprintf("\x1b[%d;1H", l.footerTop()) + seqEraseBelow
}

// emit writes one composed sequence to the terminal.
func (l *layout) emit(s string) {
	if s != "" {
		fmt.Fprint(l.out, s)
	}
}

// write appends transcript text to the region. Each line is laid out in
// rows no wider than the terminal. A row the layout writes is a row the
// terminal shows, so nothing wraps behind its back.
//
// A write from outside the run loop — a server's diagnostic — can land
// while a preview stands on the rows it would take. The standing rows
// move down: they are cleared here and repainted below the new text
// from the cache, so the geometry and the cache stay the layout's own.
func (l *layout) write(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.transcript.WriteString(s)
	if !l.active {
		l.emit(terminalLF(s))
		return
	}
	if _, _, changed := l.sizeChangedLocked(); changed {
		// The size moved under the standing rows. The text is already in
		// l.transcript: lay the tail out at the live size now, because a
		// rebuild the debounce would run may never follow — the write's
		// caller can be the seal of a stream the resize stalled, and no
		// winch comes after it. The tail repaint recovers the rows a
		// scroll pushed out of the pane, and the footer clamps to the
		// pane the resize left behind.
		if w, h, ok := l.size(); ok {
			l.width, l.height = w, h
		}
		if l.footer > l.height-1 {
			l.footer = l.height - 1
		}
		preview := l.previewRows
		l.preview, l.previewRows = 0, nil
		l.rebuildLocked(preview)
		return
	}
	rows := textRows(s, l.width)
	if len(rows) == 0 {
		return
	}
	var b strings.Builder
	pending := l.previewRows
	if l.preview > 0 {
		l.clearPreviewLocked(&b)
	}
	b.WriteString(seqSaveCursor + l.rowsSeq(rows) + seqRestoreCursor)
	l.paintPreviewLocked(&b, pending)
	l.emit(b.String())
}

// wrapRows lays one line out as rows no wider than the terminal. Rows
// break at word boundaries: a word the wrap point would split reads as
// two fragments, and a marker the model streamed — a bar-terminated
// word — counts as neither fragment. A token longer than the width has
// no boundary to break at, so the wrap leaves it whole and the hard
// pass splits it, keeping every byte visible.
func wrapRows(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := ansi.Wordwrap(expandTabs(text), width, "")
	return strings.Split(ansi.Hardwrap(words, width, true), "\n")
}

// textRows lays one transcript text out in rows. Each line wraps at the
// width; a trailing newline ends a line rather than adding a blank row.
func textRows(s string, width int) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	var rows []string
	for _, line := range strings.Split(s, "\n") {
		rows = append(rows, wrapRows(line, width)...)
	}
	return rows
}

// rowsSeq writes rows under the last transcript row, scrolling the
// region one row each time a row falls below it. Every row is written
// inside the region, so a write longer than the screen never reaches the
// footer.
func (l *layout) rowsSeq(rows []string) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	row := l.last + 1
	for _, text := range rows {
		if row > l.regionBottom() {
			b.WriteString(l.scrollSeq(1))
			row = l.regionBottom()
		}
		b.WriteString(fmt.Sprintf("\x1b[%d;1H", row))
		b.WriteString("\r" + seqEraseLine)
		b.WriteString(text)
		row++
	}
	l.last = row - 1
	return b.String()
}

// scrollSeq scrolls the region up by n rows, keeping the transcript
// bottom-anchored and pushing what leaves the top into the terminal's
// own scrollback.
func (l *layout) scrollSeq(n int) string {
	if n <= 0 {
		return ""
	}
	if room := l.regionBottom() - 1; n > room+1 {
		n = room + 1
	}
	l.last -= n
	if l.last < 0 {
		l.last = 0
	}
	return fmt.Sprintf("\x1b[%dS", n)
}

// paintStream replaces the preview in the same write that commits any
// completed paragraphs. Pending rows must be erased before they can scroll.
func (l *layout) paintStream(committed string, preview []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.transcript.WriteString(committed)
	var b strings.Builder
	if rows := textRows(committed, l.width); len(rows) > 0 {
		l.clearPreviewLocked(&b)
		b.WriteString(seqSaveCursor + l.rowsSeq(rows) + seqRestoreCursor)
	}
	l.paintPreviewLocked(&b, preview)
	l.emit(b.String())
}

// paintPreview replaces the pending rows below the committed transcript.
func (l *layout) paintPreview(rows []string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	n := l.paintPreviewLocked(&b, rows)
	l.emit(b.String())
	return n
}

// paintPreviewLocked writes the rows' paint into b and caches them as
// the standing preview, returning the rows it stands on. The caller
// emits the builder, so a clear and a paint can share one write.
func (l *layout) paintPreviewLocked(b *strings.Builder, rows []string) int {
	if !l.active {
		l.previewRows = rows
		return 0
	}
	// Remove the old preview before a scroll so pending lines never
	// enter history. A repaint in place instead overwrites them.
	scroll := min(l.last, max(0, len(rows)-(l.regionBottom()-l.last)))
	if scroll > 0 {
		l.clearPreviewLocked(b)
	}
	n := min(len(rows), l.regionBottom()-l.last+scroll)
	visible := rows[len(rows)-n:]
	old := l.previewRows[len(l.previewRows)-l.preview:]
	l.previewRows, l.preview = rows, n
	if scroll == 0 && slices.Equal(visible, old) {
		return n
	}
	b.WriteString(seqSaveCursor)
	b.WriteString(l.scrollSeq(scroll))
	// Skip the unchanged plain prefix. Styled rows can set attributes
	// that later rows inherit, so play them from the first styled row.
	start := 0
	for start < min(len(old), n) && visible[start] == old[start] && !strings.ContainsRune(visible[start], '\x1b') {
		start++
	}
	for i, text := range visible[start:] {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", l.last+1+start+i))
		b.WriteString(text)
		if ansi.StringWidth(text) < l.width {
			b.WriteString(seqEraseTail)
		}
	}
	for i := n; i < len(old); i++ {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", l.last+1+i))
		b.WriteString(seqEraseLine)
	}
	b.WriteString(seqRestoreCursor)
	return n
}

// hint paints the writing hint on the row the stream's text will take.
func (l *layout) hint(text string) {
	l.paintPreview([]string{ansi.Truncate(text, l.width, "")})
}

// clearPreview takes the live preview back off the region, leaving the
// row it started on for the next paint.
func (l *layout) clearPreview() {
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	l.clearPreviewLocked(&b)
	l.emit(b.String())
}

// clearPreviewLocked writes the erases that take the standing preview
// off the region into b, and empties the cache. The caller emits the
// builder, so a clear that shares a frame with a paint lands as one
// write and the terminal never shows the rows erased.
func (l *layout) clearPreviewLocked(b *strings.Builder) {
	l.previewRows = nil
	if !l.active || l.preview <= 0 {
		return
	}
	top := l.last + 1
	rows := l.preview
	l.preview = 0
	b.WriteString(seqSaveCursor)
	for row := top; row < top+rows && row <= l.regionBottom(); row++ {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", row))
		b.WriteString(seqEraseLine)
	}
	b.WriteString(seqRestoreCursor)
}

// sealPreview keeps the rows a live preview painted: the stream ended
// with them showing their final rendering, so the next write starts
// below them.
func (l *layout) sealPreview() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sealPreviewLocked()
}

// sealPreviewLocked seals the standing preview without the lock: the
// committed boundary moves past the rows, which stay as they stand.
func (l *layout) sealPreviewLocked() {
	l.last += l.preview
	if l.last > l.regionBottom() {
		l.last = l.regionBottom()
	}
	l.preview = 0
	l.previewRows = nil
}

// commitPreview seals a fitting rendering or writes the full clipped body.
// Geometry and the decision stay under the same lock as footer changes.
func (l *layout) commitPreview(body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.transcript.WriteString(body)
	rows := textRows(body, l.width)
	if len(rows) > l.preview {
		var b strings.Builder
		l.clearPreviewLocked(&b)
		b.WriteString(seqSaveCursor + l.rowsSeq(rows) + seqRestoreCursor)
		l.emit(b.String())
	} else {
		l.sealPreviewLocked()
	}
}

// sizeChanged reports whether the terminal's live size differs from the
// dimensions this layout last rebuilt at.
func (l *layout) sizeChanged() (int, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sizeChangedLocked()
}

func (l *layout) sizeChangedLocked() (int, int, bool) {
	w, h, ok := l.size()
	if !ok || (w == l.width && h == l.height) {
		return 0, 0, false
	}
	return w, h, true
}

// prepare gets the footer ready for the editor's frame: the rows the
// draft fills plus its spare, no stray rows, and the cursor anchored at
// the footer's first row — the origin the editor's frame renders from.
// It runs before every frame, on the editor's own goroutine, and it is
// the only writer of cursor-geometry sequences there: transcript writes
// save and restore the cursor, so they interleave safely with the
// anchored frame.
//
// A frame that lands after a terminal resize and before layout.resize
// paints nothing but the cursor. Every row it would write uses the old
// width. On a narrower pane the rule wraps and scrolls the region, and
// a repainted preview stacks a mangled copy of itself. The resize that
// follows rebuilds the screen at the new size.
func (l *layout) prepare(footer int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active {
		return
	}
	if _, h, changed := l.sizeChangedLocked(); changed {
		l.chromeStale = true
		l.emit(fmt.Sprintf("\x1b[%d;1H", min(l.footerTop(), h)))
		return
	}
	l.ensureLocked(footer)
	l.anchorLocked()
}

// setStatus stores the statusline and repaints the footer's bottom row
// when the footer stands. Its changes never touch the editor's frame;
// the rule above it repaints with the rest of the chrome.
func (l *layout) setStatus(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status == s {
		return
	}
	l.status = s
	if !l.active {
		return
	}
	if _, _, changed := l.sizeChangedLocked(); changed {
		l.chromeStale = true
		return
	}
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", l.height))
	b.WriteString(seqEraseLine + l.statusRow())
	b.WriteString(seqRestoreCursor)
	l.emit(b.String())
}

// statusRow is the statusline text clipped to the terminal's width. A
// wider line would wrap the bottom row, and the wrap scrolls the
// pinned footer off the screen.
func (l *layout) statusRow() string {
	return ansi.Truncate(l.status, l.width, "")
}

// paintChromeLocked writes the chrome rows into b: the rule and the
// statusline. They are the footer's bottom two rows, pinned to the
// screen's bottom edge, so a draft that grows moves them never.
func (l *layout) paintChromeLocked(b *strings.Builder) {
	rule := ansiDim + strings.Repeat("─", l.width) + ansiReset
	b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", l.height-1))
	b.WriteString(seqEraseLine + rule)
	b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", l.height))
	b.WriteString(seqEraseLine + l.statusRow())
}

// ensure makes the footer the given number of rows, without touching the
// cursor: a redraw the editor computes from where it stands sizes the
// footer this way. The caller keeps one spare row while a draft is
// typed, so the keystroke that wraps has somewhere to go.
//
// Growing scrolls the region first, so the last transcript rows move up
// into the scrollback and the cursor keeps its offset.
func (l *layout) ensure(footer int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ensureLocked(footer)
}

// clampFooter leaves at least one row for the transcript.
func (l *layout) clampFooter(rows int) int {
	return min(l.height-1, max(1, rows)+footerChrome)
}

// ensureLocked is the sizing half of a frame's preparation. It never
// moves the cursor by a delta: the frame's anchor decides where the
// cursor reads from, and a delta computed from the engine's row model
// is the arithmetic that stranded it.
func (l *layout) ensureLocked(footer int) bool {
	total := l.clampFooter(footer)
	if total == l.footer && !l.chromeStale {
		// The editor's accepted-line handling erases below its input,
		// and that wipe takes the chrome rows with it. Every
		// preparation paints them again, the way the frames keep the
		// prompt standing.
		var b strings.Builder
		b.WriteString(seqSaveCursor)
		l.paintChromeLocked(&b)
		b.WriteString(seqRestoreCursor)
		l.emit(b.String())
		return false
	}
	rows := l.previewRows
	var b strings.Builder
	l.clearPreviewLocked(&b)
	b.WriteString(seqSaveCursor)
	b.WriteString(l.footerSeq())
	if total > l.footer {
		// Only committed rows scroll. A pending preview row that left
		// the region is written again when the stream repaints, and
		// the footer's growth covers the rest.
		k := total - l.footer
		if committed := l.last - (l.regionTop() - 1); k > committed {
			k = committed
		}
		if k > 0 {
			b.WriteString(l.scrollSeq(k))
		}
	}
	l.footer = total
	b.WriteString(l.regionSeq())
	b.WriteString(l.footerSeq())
	b.WriteString(seqRestoreCursor)
	if l.last > l.regionBottom() {
		l.last = l.regionBottom()
	}
	if l.last < l.regionTop()-1 {
		l.last = l.regionTop() - 1
	}
	l.active = true
	l.paintChromeLocked(&b)
	l.chromeStale = false
	l.paintPreviewLocked(&b, rows)
	l.emit(b.String())
	return true
}

// anchor finishes a frame's preparation on its own: no rows under the
// committed boundary, the cursor on the footer's first row.
func (l *layout) anchor() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active {
		return
	}
	l.anchorLocked()
}

// anchorLocked is the anchoring half of a frame's preparation. Rows
// under the committed boundary belong to no stream: a paint that
// outgrew the footer left its rows there, and the editor's frame starts
// from the committed boundary again. The standing preview is not one of
// the strays; it is repainted from the cache. The anchor itself only
// positions the cursor: an erase below the footer's first row on every
// frame would open a window where a reader sees the draft gone, between
// this write and the frame that refills it. The erase rides the footer's
// size change instead, where the vacated rows actually exist.
func (l *layout) anchorLocked() {
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	pending := l.previewRows
	l.preview = 0
	l.previewRows = nil
	for row := l.last + 1; row <= l.regionBottom(); row++ {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", row))
		b.WriteString(seqEraseLine)
	}
	b.WriteString(fmt.Sprintf("\x1b[%d;1H", l.footerTop()))
	l.paintPreviewLocked(&b, pending)
	l.emit(b.String())
}

// cols is the terminal's width now.
func (l *layout) cols() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.width
}

// regionTop is the region's first row.
func (l *layout) regionTop() int { return 1 }

// resize rebuilds the visible tail: the terminal can move or reflow its
// old rows before notifying us. Paint in place rather than scrolling the
// replay into native history. The editor redraws at the new footer.
func (l *layout) resize(width, height, footer int, committed string, preview []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.transcript.WriteString(committed)
	l.width, l.height = width, height
	if footer == 0 {
		footer = l.footer - footerChrome
	}
	l.footer = l.clampFooter(footer)
	l.preview, l.previewRows = 0, nil
	if !l.active {
		return
	}
	l.rebuildLocked(preview)
}

// rebuildLocked repaints the region's tail: the transcript's committed
// rows laid out at the layout's current width, the standing preview over
// them, and the chrome, ending with the cursor anchored at the footer.
// The caller holds l.mu, with l.footer already clamped for the live
// height and the preview cache cleared for the rows it passes.
func (l *layout) rebuildLocked(preview []string) {
	bottom := l.regionBottom()
	rows := transcriptTail(l.transcript.Bytes(), l.width, bottom-len(preview))
	l.last = bottom - min(len(preview), bottom) - len(rows)
	var b strings.Builder
	b.WriteString(l.regionSeq())
	b.WriteString("\x1b[H" + seqEraseBelow)
	b.WriteString(l.rowsSeq(rows))
	l.paintPreviewLocked(&b, preview)
	l.paintChromeLocked(&b)
	l.chromeStale = false
	b.WriteString(fmt.Sprintf("\x1b[%d;1H", l.footerTop()))
	l.emit(b.String())
}

func (l *layout) transcriptText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.transcript.String()
}

// Walk back only as far as needed to fill the viewport, not all the way
// to the start of a long session. A single logical line can fill it.
func transcriptTail(transcript []byte, width, room int) []string {
	if room <= 0 || len(transcript) == 0 {
		return nil
	}
	var groups [][]string
	end := len(bytes.TrimSuffix(transcript, []byte{'\n'}))
	count := 0
	for {
		start := bytes.LastIndexByte(transcript[:end], '\n') + 1
		group := wrapRows(string(transcript[start:end]), width)
		groups = append(groups, group[max(0, len(group)-(room-count)):])
		count += len(groups[len(groups)-1])
		if start == 0 || count == room {
			break
		}
		end = start - 1
	}
	rows := make([]string, 0, count)
	for i := len(groups) - 1; i >= 0; i-- {
		rows = append(rows, groups[i]...)
	}
	return rows
}

// suspend gives the whole terminal to a program that wants it, the
// pager: the region comes off and the footer is cleared.
func (l *layout) suspend() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leaveLocked()
}

// resume takes the terminal back after a suspended program.
func (l *layout) resume() {
	l.enter()
}
