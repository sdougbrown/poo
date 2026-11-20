package repl

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/chubin/vt10x"
)

// showPrompt draws the prompt the way the editor does: its own row,
// erased first, from the footer's anchored origin.
func showPrompt(l *layout, prompt string) {
	l.anchor()
	l.raw().Write([]byte(prompt))
}

// mark is the prompt mark as the screen shows it: the harness trims
// the trailing space a terminal keeps at the end of a row.
var mark = strings.TrimRight(promptMark, " ")

// screenRows is the screen's rows, trailing blanks dropped.
func screenRows(vt vt10x.Terminal) []string {
	return strings.Split(trimScreen(vt.String()), "\n")
}

// lastRow is the bottom row a text stands on, -1 when it does not show.
func lastRow(vt vt10x.Terminal, want string) int {
	row := -1
	for i, line := range screenRows(vt) {
		if strings.Contains(line, want) {
			row = i
		}
	}
	return row
}

func TestTheLayoutKeepsTheTranscriptAboveTheFooter(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)

	l.write("first line\n")
	l.write("second line\n")
	l.write("third line\n")

	if got, want := lastRow(vt, mark), len(screenRows(vt))-1; got != want {
		t.Fatalf("the prompt stands on row %d, want the last row %d:\n%s", got, want, vt.String())
	}
	rows := screenRows(vt)
	first, second, third := lastRow(vt, "first line"), lastRow(vt, "second line"), lastRow(vt, "third line")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("the transcript lost a line:\n%s", vt.String())
	}
	if !(first < second && second < third && third < lastRow(vt, mark)) {
		t.Fatalf("the transcript and the prompt are out of order:\n%s", vt.String())
	}
	if n := strings.Count(strings.Join(rows, "\n"), "first line"); n != 1 {
		t.Errorf("the first line shows %d time(s), want 1", n)
	}
}

func TestTheTranscriptScrollsUnderLongOutputAndTheFooterHolds(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)
	promptRow := lastRow(vt, mark)

	for i := 0; i < 40; i++ {
		l.write("line " + itoa(i) + "\n")
	}
	if got := lastRow(vt, mark); got != promptRow {
		t.Fatalf("the prompt moved from row %d to %d as output arrived:\n%s", promptRow, got, vt.String())
	}
	drawn := strings.Join(screenRows(vt), "\n")
	if !strings.Contains(drawn, "line 39") {
		t.Errorf("the newest line is not on the screen:\n%s", vt.String())
	}
	if strings.Contains(drawn, "line 0 ") || strings.Contains(drawn, "line 0\n") {
		t.Errorf("the oldest line is still on the screen instead of the scrollback:\n%s", vt.String())
	}
	// The newest transcript row stands directly above the prompt.
	rows := screenRows(vt)
	if want, got := "line 39", rows[len(rows)-2]; !strings.Contains(got, want) {
		t.Errorf("the row above the prompt is %q, want %q:\n%s", got, want, vt.String())
	}
}

func TestTheStreamedBlockStaysAboveTheFooter(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)
	promptRow := lastRow(vt, mark)

	l.hint("… writing")
	if lastRow(vt, "… writing") < 0 {
		t.Fatalf("the hint never showed:\n%s", vt.String())
	}
	l.clearPreview()
	l.paintPreview([]string{"Hello, world."})
	if lastRow(vt, "… writing") >= 0 {
		t.Errorf("the text left the hint standing:\n%s", vt.String())
	}
	if got := lastRow(vt, "Hello, world."); got < 0 || got >= promptRow {
		t.Fatalf("the streamed text is not above the prompt:\n%s", vt.String())
	}

	// The next delta repaints the preview in place, once.
	l.clearPreview()
	l.paintPreview([]string{"Hello, world. Again."})
	drawn := strings.Join(screenRows(vt), "\n")
	if n := strings.Count(drawn, "Hello, world. Again."); n != 1 {
		t.Errorf("the continued block reads %d time(s), want 1:\n%s", n, vt.String())
	}

	// The flush seals the preview where it stands and writes the
	// settle below it.
	l.sealPreview()
	l.write("settled\n")
	if got := lastRow(vt, mark); got != promptRow {
		t.Errorf("the flush moved the prompt to row %d, want %d:\n%s", got, promptRow, vt.String())
	}
	drawn = strings.Join(screenRows(vt), "\n")
	if n := strings.Count(drawn, "Hello, world. Again."); n != 1 {
		t.Errorf("the render reads %d time(s) on the screen, want 1:\n%s", n, vt.String())
	}
}

// TestAWriteUnderAStandingPreviewKeepsThePreview covers a write from
// outside the run loop — a server's diagnostic — that lands while a
// preview stands on the rows it would take: the new text is written and
// the preview is repainted below it, with the cache still matching the
// screen.
func TestAWriteUnderAStandingPreviewKeepsThePreview(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 24))
	l := aLayout(t, vt, 40, 24)
	l.write("committed row\n")
	l.paintPreview([]string{"a standing preview"})

	l.write("a diagnostic\n")

	drawn := trimScreen(vt.String())
	for _, want := range []string{"committed row", "a standing preview", "a diagnostic"} {
		if n := strings.Count(drawn, want); n != 1 {
			t.Errorf("%q shows %d time(s), want 1:\n%s", want, n, drawn)
		}
	}
	said := strings.Index(drawn, "a diagnostic")
	preview := strings.Index(drawn, "a standing preview")
	if said < 0 || preview < 0 || said > preview {
		t.Errorf("the diagnostic did not land above the preview:\n%s", drawn)
	}
	if l.preview <= 0 || len(l.previewRows) == 0 || !strings.Contains(l.previewRows[0], "a standing preview") {
		t.Errorf("the preview cache no longer matches the screen: preview=%d rows=%v", l.preview, l.previewRows)
	}
}

// TestClearingUnderTheLastRowKeepsAStandingPreview covers the draft
// paint that outgrew the footer: the stray rows under the committed
// rows are cleared, and the standing preview is repainted from the
// cache rather than cleared with them.
func TestClearingUnderTheLastRowKeepsAStandingPreview(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 24))
	l := aLayout(t, vt, 40, 24)
	l.write("committed row\n")
	l.paintPreview([]string{"a standing preview"})

	l.anchor()

	if l.preview <= 0 || len(l.previewRows) == 0 || !strings.Contains(l.previewRows[0], "a standing preview") {
		t.Fatalf("the standing preview lost its cache: preview=%d rows=%v", l.preview, l.previewRows)
	}
	drawn := trimScreen(vt.String())
	for _, want := range []string{"committed row", "a standing preview"} {
		if n := strings.Count(drawn, want); n != 1 {
			t.Errorf("%q shows %d time(s), want 1:\n%s", want, n, drawn)
		}
	}
}

func TestTheFooterGrowsForAWrappedDraftAndShrinksBack(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(20, 12))
	l := aLayout(t, vt, 20, 12)
	showPrompt(l, promptMark)
	l.write("a transcript line\n")

	// A draft that wrapped over three rows: the footer takes those rows
	// plus the prompt, and the transcript moves up rather than away.
	if !l.ensure(4) {
		t.Fatal("the footer did not grow for the draft")
	}
	showPrompt(l, promptMark)
	promptRow, transcriptRow := lastRow(vt, mark), lastRow(vt, "a transcript line")
	if transcriptRow < 0 {
		t.Fatalf("growing the footer lost the transcript:\n%s", vt.String())
	}
	if !(transcriptRow < promptRow) {
		t.Fatalf("the prompt moved above the transcript: prompt %d, transcript %d\n%s", promptRow, transcriptRow, vt.String())
	}

	// The line is submitted: the footer goes back to the prompt's row,
	// and the prompt stands just above the chrome's rule.
	if !l.ensure(1) {
		t.Fatal("the footer did not shrink back")
	}
	showPrompt(l, promptMark)
	rule := lastRow(vt, "───")
	if got, want := lastRow(vt, mark), rule-1; rule < 0 || got != want {
		t.Fatalf("the prompt stands on row %d, want row %d just above the rule:\n%s", got, want, vt.String())
	}
	if lastRow(vt, "a transcript line") < 0 {
		t.Errorf("shrinking the footer lost the transcript:\n%s", vt.String())
	}
}

// TestAStatuslineWiderThanTheScreenClipsToTheRow covers a narrow
// terminal: a statusline longer than the width would wrap the bottom
// row, and the wrap scrolls the screen — the pinned footer rides off
// the bottom edge and the transcript's rows desync.
func TestAStatuslineWiderThanTheScreenClipsToTheRow(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(12, 12))
	l := aLayout(t, vt, 12, 12)
	// The payload is the real dim-wrapped shape the statusline paints:
	// the clip must not leave the terminal styling unbalanced.
	s := &status{dir: "/home/me/Code/a-really-long-folder-name", model: "gemma4", promptLimit: 262144}
	s.setTokens(12300)
	l.setStatus(s.line())
	l.ensure(1)

	rows := screenRows(vt)
	last := rows[len(rows)-1]
	if !strings.Contains(last, "a-really-l") || strings.Contains(last, "gemma4") {
		t.Errorf("the bottom row is not the clipped statusline: %q", last)
	}
	// The clip leaves the dim balanced: the row keeps its opening dim
	// and ends with a reset, so the frames after it do not inherit the
	// styling.
	if row := l.statusRow(); !strings.HasPrefix(row, ansiDim) || !strings.HasSuffix(row, ansiReset) {
		t.Errorf("the clipped row's styling is unbalanced: %q", row)
	}
	if rule := lastRow(vt, "───"); rule != len(rows)-2 {
		t.Errorf("the rule stands on row %d, want the row above the statusline:\n%s", rule, vt.String())
	}
}

func TestTheLayoutLeavesTheWholeScreenOnExit(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)
	l.write("a transcript line\n")

	l.leave()
	// The footer is the shell's again: a line typed at the cursor scrolls
	// the whole screen, so the transcript the layout held leaves it.
	l.raw().Write([]byte("after the repl\n\n\n\n\n\n\n\n\n\n\n"))
	drawn := strings.Join(screenRows(vt), "\n")
	if strings.Contains(drawn, "a transcript line") {
		t.Errorf("the screen still holds the layout's transcript:\n%s", vt.String())
	}
	if !strings.Contains(drawn, "after the repl") {
		t.Errorf("the line written after the repl scrolled away:\n%s", vt.String())
	}
}

// itoa is fmt.Sprint without the import in the tests' way.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestAHugeWriteNeverReachesTheFooter(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)

	var lines strings.Builder
	for i := 0; i < 120; i++ {
		lines.WriteString("row " + itoa(i) + "\n")
	}
	l.write(lines.String())

	rows := screenRows(vt)
	if got, want := lastRow(vt, mark), len(rows)-1; got != want {
		t.Fatalf("the prompt stands on row %d, want the bottom row %d:\n%s", got, want, vt.String())
	}
	if !strings.Contains(rows[len(rows)-2], "row 119") {
		t.Errorf("the newest row is not directly above the prompt:\n%s", vt.String())
	}
	if strings.Contains(rows[len(rows)-1], "row ") {
		t.Errorf("a transcript row overwrote the footer:\n%s", vt.String())
	}
	if strings.Contains(strings.Join(rows, "\n"), "row 0 ") || strings.Contains(strings.Join(rows, "\n"), "row 0\n") {
		t.Errorf("the oldest row is still on the screen:\n%s", vt.String())
	}
}

func TestAStreamTallerThanTheScreenKeepsItsTailInTheRegion(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)

	var text strings.Builder
	for i := 0; i < 40; i++ {
		if i > 0 {
			text.WriteString("\n")
		}
		text.WriteString("stream row " + itoa(i))
	}
	l.paintPreview(strings.Split(text.String(), "\n"))

	rows := screenRows(vt)
	if got, want := lastRow(vt, mark), len(rows)-1; got != want {
		t.Fatalf("the prompt stands on row %d, want the bottom row %d:\n%s", got, want, vt.String())
	}
	if !strings.Contains(rows[len(rows)-2], "stream row 39") {
		t.Errorf("the stream's tail is not directly above the prompt:\n%s", vt.String())
	}

	l.sealPreview()
	l.write("done\n")
	rows = screenRows(vt)
	if got, want := lastRow(vt, mark), len(rows)-1; got != want {
		t.Errorf("the seal moved the prompt to row %d, want %d:\n%s", got, want, vt.String())
	}
	drawn := strings.Join(rows, "\n")
	if n := strings.Count(drawn, "stream row 39"); n != 1 {
		t.Errorf("the sealed stream reads %d time(s), want 1:\n%s", n, vt.String())
	}
	if !strings.Contains(drawn, "done") {
		t.Errorf("the write after the seal landed nowhere:\n%s", vt.String())
	}
}

func TestTheLayoutAndTheEditorShareOneTerminal(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	l := aLayout(t, vt, 40, 12)
	showPrompt(l, promptMark)

	var wg sync.WaitGroup
	for _, work := range []func(i int){
		func(i int) { l.raw().Write([]byte("\r\x1b[J" + promptMark + "draft" + itoa(i))) },
		func(i int) { l.write("transcript row " + itoa(i) + "\n") },
		func(i int) {
			if i%2 == 0 {
				l.ensure(2)
			} else {
				l.ensure(1)
			}
		},
		func(i int) { l.paintPreview([]string{"preview row " + itoa(i)}) },
	} {
		wg.Add(1)
		go func(work func(int)) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				work(i)
			}
		}(work)
	}
	wg.Wait()

	// The terminal is still usable: the editor's prompt takes the
	// footer, and nothing of the transcript landed in it.
	l.clearPreview()
	l.ensure(1)
	showPrompt(l, promptMark)
	rows := screenRows(vt)
	rule := lastRow(vt, "───")
	if got, want := lastRow(vt, mark), rule-1; rule < 0 || got != want {
		t.Fatalf("the prompt stands on row %d, want row %d just above the rule:\n%s", got, want, vt.String())
	}
	if strings.Contains(rows[len(rows)-1], "transcript row") {
		t.Errorf("a transcript row overwrote the footer:\n%s", vt.String())
	}
}

// recordingScreen is an emulator that keeps the bytes it was sent, so a
// test can read the cursor work as well as the screen.
type recordingScreen struct {
	vt  vt10x.Terminal
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recordingScreen) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.buf.Write(p)
	r.mu.Unlock()
	return r.vt.Write(p)
}

// take returns the bytes written since the last call.
func (r *recordingScreen) take() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.buf.String()
	r.buf.Reset()
	return s
}

// scrollRows counts the rows the sequences scroll, the rows that leave
// the region for the terminal's history.
func scrollRows(seq string) int {
	total := 0
	for i := 0; i+2 < len(seq); i++ {
		if seq[i] != '\x1b' || seq[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(seq) && seq[j] >= '0' && seq[j] <= '9' {
			j++
		}
		if j < len(seq) && seq[j] == 'S' && j > i+2 {
			if n, err := strconv.Atoi(seq[i+2 : j]); err == nil {
				total += n
			}
			i = j
		}
	}
	return total
}

// previewRows returns rows numbered "preview 00|" and up. The bar ends
// the token, so a count of one row does not count another row's prefix.
func previewRows(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("preview %02d|", i)
	}
	return rows
}

func TestATallPreviewNeverScrollsPendingRows(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	rec := &recordingScreen{vt: vt}
	l := aLayout(t, rec, 40, 12)
	showPrompt(l, promptMark)

	// Fill the region with committed rows: the preview has nowhere to
	// stand without taking room from them.
	var committed strings.Builder
	for i := 0; i < 12; i++ {
		committed.WriteString("committed " + itoa(i) + "\n")
	}
	l.write(committed.String())
	if l.last != l.regionBottom() {
		t.Fatalf("the committed rows end on %d, want the region bottom %d", l.last, l.regionBottom())
	}
	before := l.last
	rec.take()

	rows := previewRows(24)
	l.paintPreview(rows)
	if got := scrollRows(rec.take()); got == 0 || got > before {
		t.Fatalf("the first preview scrolled %d rows, want 1..%d committed rows", got, before)
	}
	shown := strings.Join(screenRows(vt), "\n")
	if !strings.Contains(shown, "preview 23|") {
		t.Fatalf("the preview's tail never showed:\n%s", vt.String())
	}
	if strings.Contains(shown, "preview 00|") {
		t.Fatalf("the clipped head of the preview showed:\n%s", vt.String())
	}

	// A second paint of the same oversized preview has no committed
	// rows left to take: it must not scroll at all.
	l.clearPreview()
	l.paintPreview(rows)
	if got := scrollRows(rec.take()); got != 0 {
		t.Fatalf("the repaint scrolled %d pending rows into history, want 0", got)
	}

	// The commit writes the whole body once; the rows the preview never
	// showed reach the history with it, exactly once.
	l.clearPreview()
	commit := strings.Repeat(strings.Join(previewRows(24), "\n")+"\n", 1)
	l.write(commit)
	raw := rec.take()
	for _, row := range []string{"preview 00|", "preview 06|", "preview 12|"} {
		if n := strings.Count(raw, row); n != 1 {
			t.Errorf("the clipped row %q entered the terminal %d time(s), want 1", row, n)
		}
	}
	if row := lastRow(vt, mark); row != len(screenRows(vt))-1 {
		t.Errorf("the commit moved the prompt to row %d, want the bottom row", row)
	}
	if !strings.Contains(strings.Join(screenRows(vt), "\n"), "preview 23|") {
		t.Errorf("the commit's tail is not above the prompt:\n%s", vt.String())
	}
}

func TestFooterGrowthNeverScrollsPendingRows(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 12))
	rec := &recordingScreen{vt: vt}
	l := aLayout(t, rec, 40, 12)
	showPrompt(l, promptMark)

	var committed strings.Builder
	for i := 0; i < 12; i++ {
		committed.WriteString("committed " + itoa(i) + "\n")
	}
	l.write(committed.String())
	l.paintPreview(previewRows(3))
	// Committed rows are still visible above the preview.
	left := l.last
	if left < 4 {
		t.Fatalf("the preview left %d committed rows, want at least 4", left)
	}
	rec.take()

	l.ensure(4)
	if got := scrollRows(rec.take()); got > left {
		t.Fatalf("the footer's growth scrolled %d rows, more than the %d committed ones", got, left)
	}
	// The preview is repainted after the growth, and still does not
	// scroll pending rows.
	l.clearPreview()
	l.paintPreview(previewRows(3))
	if got := scrollRows(rec.take()); got != 0 {
		t.Errorf("the repaint after growth scrolled %d rows, want 0", got)
	}
}
