package repl

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/chubin/vt10x"
)

// The screen tests run the renderer against a terminal emulator, so a
// regression is caught by what the screen shows, not by the bytes that
// went past. The emulator stands in for the terminal; a thin writer
// stands in for the line editor, redrawing the prompt and the draft the
// way readline's Refresh does around every write.

// aStreamingRenderer builds the renderer a terminal session gets: the
// markdown a terminal shows, at the width the stream lays out at, and
// the layout that writes the terminal. The harnesses share it so a
// field added to the streaming shape is set in one place. The dark
// style is what the emulator's rows show once the escapes are gone.
func aStreamingRenderer(t *testing.T, out io.Writer, width int, l *layout) *renderer {
	t.Helper()
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width-2),
	)
	if err != nil {
		t.Fatalf("building the markdown renderer: %v", err)
	}
	return &renderer{out: out, tr: tr, wrap: width - 2, width: width, layout: l}
}

// screenHarness is the editor around an emulator. The renderer writes
// through the layout, and typeText redraws the prompt and the draft in
// the footer the way readline's Refresh does.
type screenHarness struct {
	vt     vt10x.Terminal
	layout *layout
	prompt string
	draft  string
}

// paint draws the prompt and the draft from the footer's top row,
// erasing the footer first.
func (s *screenHarness) paint() {
	rows := promptRows(s.prompt, []rune(s.draft), s.layout.width)
	if s.draft != "" {
		// A draft keeps a spare row so the keystroke that wraps it has
		// somewhere to go.
		rows++
	}
	s.layout.ensure(rows)
	s.layout.place(0)
	s.layout.raw().Write([]byte("\x1b[J" + s.prompt + s.draft))
}

func (s *screenHarness) typeText(text string) {
	s.draft += text
	s.paint()
}

// text returns what the screen shows: the rows, trimmed, with the
// trailing empties dropped.
func (s *screenHarness) text() string {
	return trimScreen(s.vt.String())
}

// trimScreen trims the emulator's rows the way both harnesses read
// them: trailing whitespace and trailing empty rows dropped.
func trimScreen(raw string) string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// aLayout is the layout over a writer, the way a terminal session has
// one: it starts with the prompt's row reserved.
func aLayout(t *testing.T, out io.Writer, cols, rows int) *layout {
	t.Helper()
	l := &layout{out: out, width: cols, height: rows, footer: 1}
	l.last = l.regionBottom() - 1
	l.enter()
	return l
}

func aScreen(t *testing.T, width int) (*screenHarness, *renderer) {
	t.Helper()
	vt := vt10x.New(vt10x.WithSize(width, 24))
	l := aLayout(t, vt, width, 24)
	scr := &screenHarness{vt: vt, layout: l, prompt: promptMark}
	r := aStreamingRenderer(t, vt, width, l)
	scr.paint()
	return scr, r
}

// screen is what the row helpers read: either harness's view of the
// emulator.
type screen interface{ text() string }

// rowOf returns the row a text stands on, or -1 when it does not show.
func rowOf(t *testing.T, scr screen, want string) int {
	t.Helper()
	for i, line := range strings.Split(scr.text(), "\n") {
		if strings.Contains(line, want) {
			return i
		}
	}
	return -1
}

// countOf counts the rows a text shows on: garbling repeats, an erase
// that took words away leaves fewer than were painted.
func countOf(t *testing.T, scr screen, want string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(scr.text(), "\n") {
		if strings.Contains(line, want) {
			n++
		}
	}
	return n
}

func rowStandsAlone(drawn, want string) bool {
	for _, line := range strings.Split(drawn, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func TestTheScreenKeepsTheStreamWhileTheDraftTypes(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("say hello")

	// The hint stands on its own sealed row and survives typing too.
	r.hint()
	if rowOf(t, scr, "… writing") < 0 {
		t.Fatalf("the hint never showed:\n%s", scr.text())
	}
	scr.typeText("wa")
	if rowOf(t, scr, "… writing") < 0 {
		t.Fatalf("typing took the hint off the screen:\n%s", scr.text())
	}

	r.streamDelta("m1", "text", "Hello, ")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("the first chunk never showed:\n%s", scr.text())
	}
	scr.typeText("tch")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("typing took the streamed words off the screen:\n%s", scr.text())
	}

	r.streamDelta("m1", "text", "world.")
	r.paintPending()
	// The paragraph reads once, with the draft intact below it, and the
	// hint has given way to the text.
	if got := countOf(t, scr, "Hello, world."); got != 1 {
		t.Fatalf("the streamed text reads %d time(s) on the screen, want 1:\n%s", got, scr.text())
	}
	if rowOf(t, scr, "… writing") >= 0 {
		t.Errorf("the hint stood over the text:\n%s", scr.text())
	}
	if rowOf(t, scr, promptMark+"watch") < 0 {
		t.Fatalf("the draft did not survive under the stream:\n%s", scr.text())
	}
	prompt := rowOf(t, scr, promptMark)
	if stream := rowOf(t, scr, "Hello, world."); stream < 0 || stream >= prompt {
		t.Fatalf("the stream and the prompt are out of order:\n%s", scr.text())
	}

	// The settle seals the paragraph where it stands; the words do not
	// move or double, and the draft keeps its place below.
	r.finish()
	drawn := scr.text()
	if got := countOf(t, scr, "Hello, world."); got != 1 {
		t.Errorf("the settled message reads %d time(s) on the screen, want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, promptMark+"watch") < 0 {
		t.Errorf("the draft did not survive the settle:\n%s", drawn)
	}
	if n := strings.Count(drawn, "Hello,"); n != 1 {
		t.Errorf("the words show %d time(s) on the screen, want 1:\n%s", n, drawn)
	}
}

func TestTheScreenKeepsThePriorLines(t *testing.T) {
	scr, r := aScreen(t, 30)
	r.userPrompt("say hello")
	r.streamDelta("m1", "text", "a fairly long streaming line that wraps past the width")
	scr.typeText("draft")
	r.finish()
	drawn := scr.text()
	if !strings.Contains(drawn, "❯ say hello") {
		t.Errorf("the echoed prompt above the run was lost:\n%s", drawn)
	}
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft was lost under the settle:\n%s", drawn)
	}
	if got := countOf(t, scr, "a fairly long streaming"); got != 1 {
		t.Errorf("the stream shows %d time(s), want 1:\n%s", got, drawn)
	}
}

func TestTheScreenHardWrapMatchesTheTerminal(t *testing.T) {
	scr, r := aScreen(t, 20)
	r.userPrompt("go")
	long := "words that run well past the width of the terminal window and keep going"
	r.streamDelta("m1", "text", long)
	drawn := scr.text()
	if got := countOf(t, scr, "words that run"); got != 1 {
		t.Errorf("the wrapped stream reads %d time(s), want 1:\n%s", got, drawn)
	}
	for _, word := range []string{"terminal", "window", "going"} {
		if !strings.Contains(drawn, word) {
			t.Errorf("the screen lost %q of the wrapped stream:\n%s", word, drawn)
		}
	}
	if rowOf(t, scr, strings.TrimRight(promptMark, " ")) < 0 {
		t.Errorf("the prompt was lost under the wrapped stream:\n%s", drawn)
	}
	// Every row the block painted is at most the terminal's width:
	// nothing the terminal had to wrap on its own twice.
	for i, line := range strings.Split(drawn, "\n") {
		if n := len([]rune(line)); n > 20 {
			t.Errorf("row %d is %d columns wide, want at most 20: %q", i, n, line)
		}
	}
}

func TestTheStreamStartsAtColumnZero(t *testing.T) {
	scr, r := aScreen(t, 40)
	// The editor drew the prompt; the cursor stands after it, and no
	// hint stood before the text arrived.
	scr.typeText("")

	r.streamDelta("m1", "text", "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen")
	if got := countOf(t, scr, "one two three"); got != 1 {
		t.Fatalf("the stream shows %d time(s), want 1:\n%s", got, scr.text())
	}
	scr.typeText(" draft")
	r.streamDelta("m1", "text", " more words here to finish the paragraph off nicely indeed yes")
	r.finish()
	scr.typeText("")

	drawn := scr.text()
	if got := countOf(t, scr, "one two three"); got != 1 {
		t.Errorf("the settled paragraph reads %d time(s), want 1:\n%s", got, drawn)
	}
	if got := countOf(t, scr, "paragraph off nicely"); got != 1 {
		t.Errorf("the stream's tail reads %d time(s), want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, promptMark+" draft") < 0 {
		t.Errorf("the draft was lost under the settle:\n%s", drawn)
	}
	last := rowOf(t, scr, "paragraph off nicely")
	draft := rowOf(t, scr, promptMark+" draft")
	if last < 0 || draft < 0 || last > draft {
		t.Errorf("the paragraph and the draft are out of order:\n%s", drawn)
	}
}

// TestTheFlushKeepsTheScreenWhenTheBlockOutgrowsIt covers a block
// taller than the screen: the settle leaves the paragraph, the draft and
// no duplicate of any line behind.
func TestTheFlushKeepsTheScreenWhenTheBlockOutgrowsIt(t *testing.T) {
	scr, r := aScreen(t, 30)
	r.userPrompt("go")
	var stream strings.Builder
	for i := 0; i < 10; i++ {
		stream.WriteString("line ")
		stream.WriteString(strconv.Itoa(i))
		stream.WriteString("\n\n")
	}
	r.streamDelta("m1", "text", stream.String())
	scr.typeText("draft")
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Fatalf("the draft was lost under the tall stream:\n%s", scr.text())
	}

	r.finish()
	drawn := scr.text()
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft did not survive the settle:\n%s", drawn)
	}
	for _, line := range []string{"line 0", "line 5", "line 9"} {
		if got := countOf(t, scr, line); got != 1 {
			t.Errorf("%q shows %d time(s), want 1:\n%s", line, got, drawn)
		}
	}
	for i, line := range strings.Split(drawn, "\n") {
		if n := len([]rune(line)); n > 30 {
			t.Errorf("row %d is %d columns wide, want at most 30: %q", i, n, line)
		}
	}
}

func TestAResizeMidBlockStopsThePainting(t *testing.T) {
	scr, r := aScreen(t, 40)
	width := 40
	r.liveWidth = func() int { return width }
	r.userPrompt("go")
	r.streamDelta("m1", "text", "words that wrap over more than one row at forty columns wide")

	// The terminal narrows; the next delta must not paint at the old
	// width.
	width = 20
	scr.layout.width = 20
	before := scr.text()
	r.streamDelta("m1", "text", " and more words arrive after the resize")
	if got := scr.text(); got != before {
		t.Errorf("the stream kept painting after the resize:\n%s", got)
	}

	r.finish()
	drawn := scr.text()
	if got := strings.Count(drawn, "resize"); got != 1 {
		t.Errorf("the settled text reads %d time(s), want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, strings.TrimRight(promptMark, " ")) < 0 {
		t.Errorf("the prompt was lost after the resize:\n%s", drawn)
	}
}

// TestAWidthThatCannotBeReadKeepsStreaming covers the size query that
// fails: the terminal still streams, at the width the markdown wraps
// at, instead of the stream quietly turning itself off.
func TestAWidthThatCannotBeReadKeepsStreaming(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.liveWidth = func() int { return -1 }
	r.userPrompt("go")
	r.streamDelta("m1", "text", "still streaming")
	if rowOf(t, scr, "still streaming") < 0 {
		t.Fatalf("the stream stopped when the width could not be read:\n%s", scr.text())
	}
	if r.width != 40 {
		t.Errorf("the width became %d, want it left at 40", r.width)
	}
}

func TestAMultilineToolResultStartsEachRowAtColumnZero(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("read a file")
	r.toolCall("read", "a file")
	r.toolResult("read", "ok", "func main() {\n    fmt.Println(\"hi\")\n}\nsecond line of output", 0)
	r.notice("the read finished")
	scr.typeText("draft")
	drawn := scr.text()

	for _, w := range []string{
		"  func main() {",
		"      fmt.Println(\"hi\")",
		"  }",
		"  second line of output",
		"the read finished",
	} {
		if !rowStandsAlone(drawn, w) {
			t.Errorf("the screen lacks the row %q standing at column zero:\n%s", w, drawn)
		}
	}
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft did not survive under the multiline result:\n%s", drawn)
	}
}

func TestAMultilineNoticeStartsEachRowAtColumnZero(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("think about it")
	scr.typeText("draft")
	r.notice("step one\nstep two\nstep three")
	drawn := scr.text()

	for _, w := range []string{"step one", "step two", "step three"} {
		if !rowStandsAlone(drawn, w) {
			t.Errorf("the screen lacks the row %q standing at column zero:\n%s", w, drawn)
		}
	}
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft did not survive the multiline notice:\n%s", drawn)
	}
}

func TestATallLivePreviewCommitsEachRowOnce(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 24))
	rec := &recordingScreen{vt: vt}
	l := aLayout(t, rec, 40, 24)
	scr := &screenHarness{vt: vt, layout: l, prompt: promptMark}
	r := aStreamingRenderer(t, rec, 40, l)
	scr.paint()
	r.userPrompt("go")

	var fence strings.Builder
	fence.WriteString("```\n")
	for i := 0; i < 100; i++ {
		fence.WriteString("line " + strconv.Itoa(i) + "|\n")
	}
	fence.WriteString("```\n")
	r.streamDelta("text", fence.String())
	// A second block keeps the fence pending and repaints the preview.
	var more strings.Builder
	more.WriteString("\n```\n")
	for i := 0; i < 20; i++ {
		more.WriteString("tail " + strconv.Itoa(i) + "|\n")
	}
	more.WriteString("```\n")
	r.streamDelta("text", more.String())

	// The first paint shows the fence's tail, the repaint shows the tail
	// of both blocks: the fence's head is never on the screen.
	raw := rec.take()
	for _, row := range []string{"line 0|", "line 20|", "line 40|"} {
		if n := strings.Count(raw, row); n != 0 {
			t.Errorf("the clipped row %q was written %d time(s) before the commit, want 0", row, n)
		}
	}

	r.finish()
	scr.paint()
	raw = rec.take()
	for _, row := range []string{"line 0|", "line 20|", "line 40|"} {
		if n := strings.Count(raw, row); n != 1 {
			t.Errorf("the clipped row %q was written %d time(s) at the commit, want 1", row, n)
		}
	}
	for _, row := range []string{"line 0|", "line 20|", "line 40|"} {
		if n := strings.Count(r.transcriptText(), row); n != 1 {
			t.Errorf("the clipped row %q reads %d time(s) in the transcript, want 1", row, n)
		}
	}
	if !strings.Contains(strings.Join(screenRows(vt), "\n"), "tail 19|") {
		t.Errorf("the committed tail is not on the screen:\n%s", vt.String())
	}
	if row := vt.Cursor().Y; row != 23 {
		t.Errorf("the prompt stands on row %d, want the bottom row 23:\n%s", row, indexedScreen(vt))
	}
}

// indexedScreen is the emulator's rows with their numbers.
func indexedScreen(vt vt10x.Terminal) string {
	var b strings.Builder
	for i, row := range screenRows(vt) {
		b.WriteString(strconv.Itoa(i) + "|" + row + "|\n")
	}
	return b.String()
}

func TestAFenceSplitAcrossDeltasCommitsItsRowsOnce(t *testing.T) {
	vt := vt10x.New(vt10x.WithSize(40, 24))
	rec := &recordingScreen{vt: vt}
	l := aLayout(t, rec, 40, 24)
	scr := &screenHarness{vt: vt, layout: l, prompt: promptMark}
	r := aStreamingRenderer(t, rec, 40, l)
	scr.paint()
	r.userPrompt("go")

	fence := func(from, to int) string {
		var b strings.Builder
		for i := from; i < to; i++ {
			b.WriteString("SCROLLROW" + fmt.Sprintf("%03d", i) + "|\n")
		}
		return b.String()
	}
	// The block's head never reaches the region. The first chunk's tail
	// and the tail of both chunks together both start below it.
	rows := []string{"SCROLLROW000|", "SCROLLROW005|", "SCROLLROW010|"}

	r.streamDelta("text", "```text\n"+fence(0, 40))
	r.streamDelta("text", fence(40, 80))
	raw := rec.take()
	for _, row := range rows {
		if n := strings.Count(raw, row); n != 0 {
			t.Errorf("the pending row %q was written %d time(s) before the commit, want 0", row, n)
		}
	}

	// The closing fence and two paragraphs; the coalesced frame holds
	// them until the flush, whose first paragraph makes the block safe
	// to commit.
	r.streamDelta("text", "```\n\nThe tall block is finished.\n\nEverything is done.")
	if raw := rec.take(); strings.Count(raw, rows[0]) != 0 {
		t.Errorf("the coalesced frame wrote the pending block before the flush")
	}

	r.finish()
	scr.paint()
	raw = rec.take()
	for _, row := range rows {
		if n := strings.Count(raw, row); n != 1 {
			t.Errorf("the committed row %q was written %d time(s) at the flush, want 1", row, n)
		}
	}
	for _, row := range rows {
		if n := strings.Count(r.transcriptText(), row); n != 1 {
			t.Errorf("the row %q reads %d time(s) in the transcript, want 1", row, n)
		}
	}
}
