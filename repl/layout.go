package repl

import (
	"fmt"
	"io"
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
	seqEraseBelow    = "\x1b[J"  // ED 0
	seqResetRegion   = "\x1b[r"  // DECSTBM full screen
)

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
	fd  int

	// width and height are the terminal's size in cells.
	width, height int
	// footer is the rows reserved at the bottom for the editor.
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
	// active says the region is set and the footer is reserved.
	active bool
}

// newLayout builds the layout for a terminal. A writer that is not a
// terminal has no layout: the transcript is a plain stream, and any
// editor draws wherever the stream ends.
func newLayout(out io.Writer) *layout {
	f, ok := out.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return nil
	}
	l := &layout{out: out, fd: int(f.Fd()), footer: 1}
	if !l.readSize() || l.height < 4 || l.width < 2 {
		return nil
	}
	l.last = l.regionBottom() - 1
	return l
}

// readSize takes the terminal's current size. A terminal that will not
// say keeps the size it had.
func (l *layout) readSize() bool {
	w, h, err := term.GetSize(l.fd)
	if err != nil || w < 2 || h < 4 {
		return false
	}
	l.width, l.height = w, h
	return true
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
	l.active = false
	l.emit(seqResetRegion + l.footerSeq())
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
	if !l.active {
		l.emit(strings.ReplaceAll(s, "\n", "\r\n"))
		return
	}
	rows := textRows(s, l.width)
	if len(rows) == 0 {
		return
	}
	pending := l.previewRows
	if l.preview > 0 {
		l.clearPreviewLocked()
	}
	l.emit(seqSaveCursor + l.rowsSeq(rows) + seqRestoreCursor)
	l.paintPreviewLocked(pending)
}

// wrapRows lays one line out as rows no wider than the terminal. A row
// the layout writes is a row the terminal shows, so nothing wraps
// behind its back.
func wrapRows(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	return strings.Split(ansi.Hardwrap(expandTabs(text), width, true), "\n")
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

// paintPreview paints the rows a live stream has not committed and
// returns the rows it shows. They stand under the last committed row and
// are repainted as the stream grows.
//
// The region scrolls only over committed rows, and a taller preview
// shows its visible tail until the commit.
func (l *layout) paintPreview(rows []string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearPreviewLocked()
	return l.paintPreviewLocked(rows)
}

func (l *layout) paintPreviewLocked(rows []string) int {
	l.previewRows = rows
	if !l.active || len(rows) == 0 {
		return 0
	}
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	if extra := len(rows) - (l.regionBottom() - l.last); extra > 0 {
		// Room comes from what is above the preview: the committed rows
		// scroll into history, and nothing else.
		committed := l.last - (l.regionTop() - 1)
		if committed > 0 {
			k := extra
			if k > committed {
				k = committed
			}
			b.WriteString(l.scrollSeq(k))
		}
	}
	// The tail the region can show, oldest rows first.
	room := l.regionBottom() - l.last
	start := len(rows) - room
	if start < 0 {
		start = 0
	}
	row := l.last + 1
	for _, text := range rows[start:] {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H", row))
		b.WriteString("\r" + seqEraseLine)
		b.WriteString(text)
		row++
	}
	l.preview = row - 1 - l.last
	if l.preview < 0 {
		l.preview = 0
	}
	b.WriteString(seqRestoreCursor)
	l.emit(b.String())
	return l.preview
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
	l.clearPreviewLocked()
}

func (l *layout) clearPreviewLocked() {
	l.previewRows = nil
	if !l.active || l.preview <= 0 {
		return
	}
	top := l.last + 1
	rows := l.preview
	l.preview = 0
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	for row := top; row < top+rows && row <= l.regionBottom(); row++ {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", row))
		b.WriteString(seqEraseLine)
	}
	b.WriteString(seqRestoreCursor)
	l.emit(b.String())
}

// sealPreview keeps the rows a live preview painted: the stream ended
// with them showing their final rendering, so the next write starts
// below them.
func (l *layout) sealPreview() {
	l.mu.Lock()
	defer l.mu.Unlock()
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
	rows := textRows(body, l.width)
	if len(rows) > l.preview {
		l.clearPreviewLocked()
		l.emit(seqSaveCursor + l.rowsSeq(rows) + seqRestoreCursor)
	} else {
		l.last += l.preview
		l.preview = 0
		l.previewRows = nil
	}
}

// ensure makes the footer the given number of rows. The caller keeps one
// spare row while a draft is typed, so the keystroke that wraps has
// somewhere to go.
//
// Growing scrolls the region first, so the last transcript rows move up
// into the scrollback and the cursor keeps its offset.
func (l *layout) ensure(footer int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if footer < 1 {
		footer = 1
	}
	if room := l.height - 2; footer > room {
		footer = room
	}
	if footer == l.footer {
		return false
	}
	rows := l.previewRows
	l.clearPreviewLocked()
	delta := footer - l.footer
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	b.WriteString(l.footerSeq())
	if delta > 0 {
		// Only committed rows scroll. A pending preview row that left the
		// region is written again when the stream repaints, and the
		// footer's growth covers the rest.
		k := delta
		if committed := l.last - (l.regionTop() - 1); k > committed {
			k = committed
		}
		if k > 0 {
			b.WriteString(l.scrollSeq(k))
		}
	}
	l.footer = footer
	b.WriteString(l.regionSeq())
	b.WriteString(l.footerSeq())
	b.WriteString(seqRestoreCursor)
	if delta > 0 {
		b.WriteString(ansi.CursorUp(delta))
	} else {
		b.WriteString(ansi.CursorDown(-delta))
	}
	if l.last > l.regionBottom() {
		l.last = l.regionBottom()
	}
	if l.last < l.regionTop()-1 {
		l.last = l.regionTop() - 1
	}
	l.active = true
	l.emit(b.String())
	l.paintPreviewLocked(rows)
	return true
}

// footerRows is the rows reserved for the editor now.
func (l *layout) footerRows() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.footer
}

// clearBelowLast blanks the region under the committed rows. A paint
// that outgrew the footer left rows there that belong to no stream, and
// the editor's next draw starts from the committed boundary again. The
// standing preview is not one of the strays: it is repainted from the
// cache once the region is clear.
func (l *layout) clearBelowLast() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active || l.last >= l.regionBottom() {
		return
	}
	pending := l.previewRows
	var b strings.Builder
	b.WriteString(seqSaveCursor)
	for row := l.last + 1; row <= l.regionBottom(); row++ {
		b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", row))
		b.WriteString(seqEraseLine)
	}
	b.WriteString(seqRestoreCursor)
	l.preview = 0
	l.previewRows = nil
	l.emit(b.String())
	l.paintPreviewLocked(pending)
}

// cols is the terminal's width now.
func (l *layout) cols() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.width
}

// regionTop is the region's first row.
func (l *layout) regionTop() int { return 1 }

// place puts the cursor at the given row of the footer. It is for a
// redraw the editor cannot compute from where it stands: a prompt that
// wraps differently, or a terminal that just changed size.
func (l *layout) place(row int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active {
		return
	}
	if row < 0 {
		row = 0
	}
	if row > l.footer-1 {
		row = l.footer - 1
	}
	l.emit(fmt.Sprintf("\x1b[%d;1H", l.footerTop()+row))
}

// resize takes the terminal's new size. A transcript row the terminal
// held keeps the row it was on, as far as the new region reaches. The
// caller redraws the prompt at the new bottom.
func (l *layout) resize() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, h := l.width, l.height
	if !l.readSize() || (w == l.width && h == l.height) {
		l.width, l.height = w, h
		return false
	}
	if l.footer > l.height-2 {
		l.footer = l.height - 2
	}
	if l.footer < 1 {
		l.footer = 1
	}
	if l.last > l.regionBottom() {
		l.last = l.regionBottom()
	}
	l.preview = 0
	l.previewRows = nil
	if l.active {
		var b strings.Builder
		b.WriteString(l.regionSeq())
		b.WriteString(l.footerSeq())
		// The rows under the committed boundary held a preview laid out
		// for the old width. They are stale now.
		if l.last < l.regionBottom() {
			for row := l.last + 1; row <= l.regionBottom(); row++ {
				b.WriteString(fmt.Sprintf("\x1b[%d;1H\r", row))
				b.WriteString(seqEraseLine)
			}
		}
		l.emit(b.String())
	}
	return true
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
