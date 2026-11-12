package repl

import (
	"io"
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
// markdown a test reads plain, the width the stream lays out at, and the
// editor's writer as the sink. The three test harnesses share it so a
// field added to the streaming shape is set in one place.
func aStreamingRenderer(t *testing.T, out, sink io.Writer, width int) *renderer {
	t.Helper()
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
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

// TestTheScreenKeepsTheStreamWhileTheDraftTypes covers the regression
// the raw byte tests could not see: the streamed text shares the
// prompt's row with the draft, and the editor's redraw erased it. The
// sealed rows must keep the words on the screen while the person types,
// contiguous, with the draft intact below.
func TestTheScreenKeepsTheStreamWhileTheDraftTypes(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("say hello")

	// The thinking delta draws the hint; the hint must survive typing
	// too, on its sealed row.
	r.hint()
	if rowOf(t, scr, "… writing") < 0 {
		t.Fatalf("the hint never showed:\n%s", scr.text())
	}
	scr.typeText("wa")
	if rowOf(t, scr, "… writing") < 0 {
		t.Fatalf("typing took the hint off the screen:\n%s", scr.text())
	}

	r.streamText("Hello, ")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("the first chunk never showed:\n%s", scr.text())
	}
	scr.typeText("tch")
	if rowOf(t, scr, "Hello,") < 0 {
		t.Fatalf("typing took the streamed words off the screen:\n%s", scr.text())
	}

	r.streamText("world.")
	// The paragraph reads as one line, once, with the draft intact
	// below it.
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

	// The flush takes the raw text off and renders the message in its
	// place; the draft keeps its place below.
	r.replace(r.markdown("Hello, world."))
	drawn := scr.text()
	if got := countOf(t, scr, "Hello, world."); got != 1 {
		t.Errorf("the rendered message reads %d time(s) on the screen, want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, promptMark+"watch") < 0 {
		t.Errorf("the draft did not survive the flush:\n%s", drawn)
	}
	// Nothing shows twice: the raw text is gone from the screen, the
	// render stands where it was.
	if n := strings.Count(drawn, "Hello,"); n != 1 {
		t.Errorf("the words show %d time(s) on the screen, want 1:\n%s", n, drawn)
	}
}

// TestTheScreenKeepsThePriorLines covers the rows above the block: a
// transcript line the repl printed before the run stays where it was
// while the stream and the flush repaint below it.
func TestTheScreenKeepsThePriorLines(t *testing.T) {
	scr, r := aScreen(t, 30)
	r.userPrompt("say hello")
	r.streamText("a fairly long streaming line that wraps past the width")
	scr.typeText("draft")
	r.replace(r.markdown("done"))
	drawn := scr.text()
	if !strings.Contains(drawn, "❯ say hello") {
		t.Errorf("the echoed prompt above the run was lost:\n%s", drawn)
	}
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft was lost under the flush:\n%s", drawn)
	}
	if got := countOf(t, scr, "done"); got != 1 {
		t.Errorf("the render shows %d time(s), want 1:\n%s", got, drawn)
	}
}

func TestStreamingTabsKeepsThePreviousLines(t *testing.T) {
	scr, r := aScreen(t, 20)
	r.userPrompt("history")
	r.streamText("1234567890\tABCDEFG")
	scr.typeText("draft")
	r.streamText("HIJ")
	if n := strings.Count(scr.text(), "1234567890"); n != 1 {
		t.Fatalf("tabbed output appeared %d times:\n%s", n, scr.text())
	}
	r.replace(r.markdown("done"))
	if !strings.Contains(scr.text(), "❯ history") || rowOf(t, scr, promptMark+"draft") < 0 {
		t.Fatalf("tabbed output corrupted the screen:\n%s", scr.text())
	}
}

// TestTheScreenHardWrapMatchesTheTerminal covers the width: a row the
// stream commits is a row the terminal wraps the same way, so the words
// land on one line each and nothing doubles up.
func TestTheScreenHardWrapMatchesTheTerminal(t *testing.T) {
	scr, r := aScreen(t, 20)
	r.userPrompt("go")
	long := "words that run well past the width of the terminal window and keep going"
	r.streamText(long)
	drawn := scr.text()
	if got := countOf(t, scr, "words that run well"); got != 1 {
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

// TestTheStreamStartsAtColumnZero covers the start of the block: the
// editor's cursor stands after the prompt it drew and no hint stood
// before the text, so the rows the stream paints must begin at column
// zero — or the row arithmetic counts one row short and the flush
// leaves the stream's first row on the screen.
func TestTheStreamStartsAtColumnZero(t *testing.T) {
	scr, r := aQuietScreen(t, 40)
	// The editor drew the prompt; the cursor stands after it, and no
	// thinking delta drew a hint before the text arrived.
	scr.typeText("")

	r.streamText("one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen")
	if got := countOf(t, scr, "one two three"); got != 1 {
		t.Fatalf("the stream shows %d time(s), want 1:\n%s", got, scr.text())
	}
	scr.typeText(" draft")
	r.streamText(" more words here to finish the paragraph off nicely indeed yes")
	r.replace(r.markdown("done"))
	// The flush leaves the cursor on a fresh row; the editor's next
	// draw — the prompt the run loop sets when the run ends — repaints
	// the prompt and the draft below the render.
	scr.typeText("")

	drawn := scr.text()
	if got := countOf(t, scr, "one two three"); got != 0 {
		t.Errorf("the stream's rows stayed on the screen (%d):\n%s", got, drawn)
	}
	if got := countOf(t, scr, "done"); got != 1 {
		t.Errorf("the render shows %d time(s), want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, promptMark+" draft") < 0 {
		t.Errorf("the draft was lost under the flush:\n%s", drawn)
	}
	done, draft := rowOf(t, scr, "done"), rowOf(t, scr, promptMark+" draft")
	if done < 0 || draft < 0 || done > draft {
		t.Errorf("the render and the draft are out of order:\n%s", drawn)
	}
}

// TestTheFlushKeepsTheScreenWhenTheBlockOutgrowsIt covers a block
// taller than the screen: the rows the flush climbs stop at the screen
// top, the erase cannot reach above the block's visible extent, and the
// flush leaves the render, the draft and no raw text behind.
func TestTheFlushKeepsTheScreenWhenTheBlockOutgrowsIt(t *testing.T) {
	scr, r := aScreen(t, 30)
	r.userPrompt("go")
	var stream strings.Builder
	for i := 0; i < 30; i++ {
		stream.WriteString("line 30 of a stream taller than the screen\n")
	}
	r.streamText(stream.String())
	scr.typeText("draft")
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Fatalf("the draft was lost under the tall stream:\n%s", scr.text())
	}

	r.replace(r.markdown("done"))
	drawn := scr.text()
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft did not survive the flush:\n%s", drawn)
	}
	if got := countOf(t, scr, "line 30"); got != 0 {
		t.Errorf("the raw stream rows stayed on the screen (%d), want 0:\n%s", got, drawn)
	}
	if got := countOf(t, scr, "done"); got != 1 {
		t.Errorf("the render shows %d time(s), want 1:\n%s", got, drawn)
	}
	for i, line := range strings.Split(drawn, "\n") {
		if n := len([]rune(line)); n > 30 {
			t.Errorf("row %d is %d columns wide, want at most 30: %q", i, n, line)
		}
	}
}

// TestAResizeMidBlockStopsThePainting covers the width the rows are
// counted at: a terminal that changes width while a block streams keeps
// its old rows on the screen, the painting stops, and the render
// replaces them at the new width instead of climbing to the wrong row.
func TestAResizeMidBlockStopsThePainting(t *testing.T) {
	scr, r := aScreen(t, 40)
	width := 40
	r.liveWidth = func() int { return width }
	r.userPrompt("go")
	r.streamText("words that wrap over more than one row at forty columns wide")

	// The terminal narrows; the next delta must not paint at the old
	// width.
	width = 20
	before := scr.text()
	r.streamText(" and more words arrive after the resize")
	if got := scr.text(); got != before {
		t.Errorf("the stream kept painting after the resize:\n%s", got)
	}

	r.replace(r.markdown("done"))
	drawn := scr.text()
	if got := countOf(t, scr, "done"); got != 1 {
		t.Errorf("the render shows %d time(s), want 1:\n%s", got, drawn)
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
	r.streamText("still streaming")
	if rowOf(t, scr, "still streaming") < 0 {
		t.Fatalf("the stream stopped when the width could not be read:\n%s", scr.text())
	}
	if r.width != 40 {
		t.Errorf("the width became %d, want it left at 40", r.width)
	}
}
