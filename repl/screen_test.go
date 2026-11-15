package repl

import (
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
// markdown a terminal shows, at the width the stream lays out at. The
// editor's writer is the sink. The three test harnesses share it so a
// field added to the streaming shape is set in one place. The dark
// style is the one a light-on-dark terminal picks, and it is what the
// emulator's rows show once the escapes are gone.
func aStreamingRenderer(t *testing.T, out, sink io.Writer, width int) *renderer {
	t.Helper()
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width-2),
	)
	if err != nil {
		t.Fatalf("building the markdown renderer: %v", err)
	}
	return &renderer{out: out, sink: sink, tr: tr, wrap: width - 2, width: width, editor: true}
}

// screenHarness is the editor around an emulator: every write the
// renderer sends is wrapped in the erase of the prompt's own row and
// the redraw of the prompt and draft below it. typeText plays
// keystrokes: an erase and a redraw, no payload.
type screenHarness struct {
	vt    vt10x.Terminal
	draft string
}

func (s *screenHarness) Write(p []byte) (int, error) {
	s.vt.Write([]byte("\x1b[J\x1b[2K\r"))
	n, err := s.vt.Write(p)
	s.vt.Write([]byte(promptMark + s.draft))
	return n, err
}

func (s *screenHarness) typeText(text string) {
	s.vt.Write([]byte("\x1b[J\x1b[2K\r"))
	s.draft += text
	s.vt.Write([]byte(promptMark + s.draft))
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

func aScreen(t *testing.T, width int) (*screenHarness, *renderer) {
	t.Helper()
	vt := vt10x.New(vt10x.WithSize(width, 24))
	scr := &screenHarness{vt: vt}
	return scr, aStreamingRenderer(t, vt, scr, width)
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

	r.streamDelta("text", "Hello, ")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("the first chunk never showed:\n%s", scr.text())
	}
	scr.typeText("tch")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("typing took the streamed words off the screen:\n%s", scr.text())
	}

	r.streamDelta("text", "world.")
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
	r.streamDelta("text", "a fairly long streaming line that wraps past the width")
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
	r.streamDelta("text", long)
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

// quietHarness is the editor between keystrokes: the renderer's writes
// reach the terminal untouched, the way readline passes them through,
// and a keystroke redraws the prompt and the draft at the cursor's row.
type quietHarness struct {
	vt    vt10x.Terminal
	draft string
}

func (s *quietHarness) Write(p []byte) (int, error) { return s.vt.Write(p) }

func (s *quietHarness) typeText(text string) {
	s.vt.Write([]byte("\r\x1b[2K" + promptMark + s.draft))
	s.draft += text
	s.vt.Write([]byte("\r\x1b[2K" + promptMark + s.draft))
}

// text returns what the screen shows, the screenHarness's way.
func (s *quietHarness) text() string {
	return trimScreen(s.vt.String())
}

func aQuietScreen(t *testing.T, width int) (*quietHarness, *renderer) {
	t.Helper()
	vt := vt10x.New(vt10x.WithSize(width, 24))
	scr := &quietHarness{vt: vt}
	return scr, aStreamingRenderer(t, vt, scr, width)
}

func TestTheStreamStartsAtColumnZero(t *testing.T) {
	scr, r := aQuietScreen(t, 40)
	// The editor drew the prompt; the cursor stands after it, and no
	// hint stood before the text arrived.
	scr.typeText("")

	r.streamDelta("text", "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen")
	if got := countOf(t, scr, "one two three"); got != 1 {
		t.Fatalf("the stream shows %d time(s), want 1:\n%s", got, scr.text())
	}
	scr.typeText(" draft")
	r.streamDelta("text", " more words here to finish the paragraph off nicely indeed yes")
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
	r.streamDelta("text", stream.String())
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
	r.streamDelta("text", "words that wrap over more than one row at forty columns wide")

	// The terminal narrows; the next delta must not paint at the old
	// width.
	width = 20
	before := scr.text()
	r.streamDelta("text", " and more words arrive after the resize")
	if got := scr.text(); got != before {
		t.Errorf("the stream kept painting after the resize:\n%s", got)
	}

	r.finish()
	drawn := scr.text()
	if got := countOf(t, scr, "after the resize"); got != 1 {
		t.Errorf("the settle reads %d time(s), want 1:\n%s", got, drawn)
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
	r.streamDelta("text", "still streaming")
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
