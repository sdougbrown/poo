package repl

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
)

// TestAMessageRendersItsMarkdown covers the replies: a code fence
// reads back as a highlighted block, and the text keeps its shape.
// TestExpandTabsPutsTheTerminalStopsIn covers the tab expansion the
// sanitizer runs: each tab lands on the next multiple of eight,
// counted from the start of its own line.
func TestExpandTabsPutsTheTerminalStopsIn(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"no tabs", "plain words", "plain words"},
		{"at line start", "\tx", "        x"},
		{"mid line", "ab\tx", "ab      x"},
		{"past the first stop", "a\tb\tc", "a       b       c"},
		{"the count restarts at a newline", "ab\ncd\tx", "ab\ncd      x"},
	}
	for _, c := range cases {
		if got := expandTabs(c.in); got != c.want {
			t.Errorf("%s: expandTabs(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestAMessageRendersItsMarkdown(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.write(r.markdown("# Heading\n\nA paragraph with `code`.\n\n```go\nfunc main() {}\n```\n"))

	rendered := out.String()
	for _, want := range []string{"Heading", "paragraph", "func main()"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the render lacks %q:\n%s", want, rendered)
		}
	}
}

// TestAMessageRendersPlainWithoutATerminal covers the notty path: a
// pipe reads the same words without the ANSI escapes the terminal
// gets.
func TestAMessageRendersPlainWithoutATerminal(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.write(r.markdown("# Heading\n\nBody text.\n"))

	rendered := out.String()
	if strings.Contains(rendered, "\x1b[") {
		t.Errorf("the render carries escapes for a pipe:\n%q", rendered)
	}
	if !strings.Contains(rendered, "Heading") || !strings.Contains(rendered, "Body text.") {
		t.Errorf("the render lacks the words:\n%q", rendered)
	}
}

// TestTheToolDecorationTruncatesLongResults covers the tool blocks: a
// long result keeps its head and says how much it cut.
func TestTheToolDecorationTruncatesLongResults(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolCall("bash", "ls -la\nmore of the summary")
	long := strings.Repeat("line\n", 20)
	r.toolResult("bash", "ok", long, 0)

	rendered := out.String()
	if !strings.Contains(rendered, "⚙ bash") || !strings.Contains(rendered, "ls -la") {
		t.Errorf("the tool call lacks its decoration:\n%s", rendered)
	}
	if strings.Contains(rendered, "more of the summary") {
		t.Errorf("the tool call kept the summary's tail:\n%s", rendered)
	}
	if !strings.Contains(rendered, "+12 more lines") {
		t.Errorf("the tool result does not say what it cut:\n%s", rendered)
	}
	// A short body with the server's own elision count reads the same
	// way: the wire counts characters, so the count says characters.
	r.toolResult("bash", "ok", "one line\n", 4)
	if !strings.Contains(out.String(), "4 characters elided") {
		t.Errorf("the elided count does not read:\n%s", out.String())
	}
}

// TestAFailedResultReadsAsAFailure covers a result that is not ok: it
// names the tool and the outcome.
func TestAFailedResultReadsAsAFailure(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.toolResult("bash", "error", "permission denied\n", 0)

	if !strings.Contains(out.String(), "bash: error") {
		t.Errorf("the failure does not name the outcome:\n%s", out.String())
	}
}

// TestTheSettleNamesTheAccounting covers the settle line: the reason,
// the turns and the tokens.
func TestTheSettleNamesTheAccounting(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.settle("end_turn", 3, 12300, 1100)

	rendered := out.String()
	for _, want := range []string{"end_turn", "3 turn(s)", "12.3k in", "1.1k out"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the settle lacks %q:\n%s", want, rendered)
		}
	}
}

// TestTheTranscriptMirrorsWhatWasWritten covers the pager's source:
// everything the renderer wrote is in the transcript, ANSI included.
func TestTheTranscriptMirrorsWhatWasWritten(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.notice("a notice")
	r.toolCall("read", "a file")

	if r.transcriptText() != out.String() {
		t.Error("the transcript does not mirror the output")
	}
}

// TestShortTokensReadsTheStatuslineWay covers the token counts.
func TestShortTokensReadsTheStatuslineWay(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{12300, "12.3k"},
		// The unit follows the number as it is written: 999_949 is
		// still 999.9k, and one token more rounds to a whole megabyte.
		{999_949, "999.9k"},
		{999_950, "1.0M"},
		{1_000_000, "1.0M"},
		{1_234_567, "1.2M"},
		{-1, "?"},
	} {
		if got := shortTokens(tc.n); got != tc.want {
			t.Errorf("shortTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestAStreamedSequenceNeverReachesTheTerminal covers the sanitizing:
// a delta that carries an escape sequence — an OSC that would rewrite
// the clipboard, a cursor that draws where the repl draws — prints as
// data, and a sequence split across deltas is stripped when its tail
// lands.
func TestAStreamedSequenceNeverReachesTheTerminal(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	r.userPrompt("say hi")

	r.streamDelta("m1", "text", "\x1b]52;c;"+"QUJD"+"\x07clipboard"+"\x1b[2Jcleared")
	out.Reset()
	r.streamDelta("m1", "text", "safe"+"\x1b[2J"+"text")
	r.paintPending()
	if strings.Contains(out.String(), "\x1b[2J") {
		t.Errorf("the terminal was told to erase:\n%q", out.String())
	}
	if !strings.Contains(plain(out.String()), "safetext") {
		t.Errorf("the data did not print:\n%q", plain(out.String()))
	}

	// A sequence split across deltas: the head is held pending, and
	// the tail strips the whole sequence when it lands. Glamour paints
	// its own 256-colour escapes, so the check names the sequence the
	// model sent rather than any escape at all.
	out.Reset()
	r.finish()
	r.streamDelta("m1", "text", "half \x1b[3")
	if strings.Contains(out.String(), "\x1b[31") {
		t.Errorf("the pending head leaked:\n%q", out.String())
	}
	r.streamDelta("m1", "text", "1mrest")
	r.paintPending()
	if strings.Contains(out.String(), "\x1b[31m") {
		t.Errorf("the split sequence reached the terminal:\n%q", out.String())
	}
	if !strings.Contains(plain(out.String()), "half rest") {
		t.Errorf("the data did not print around the split:\n%q", plain(out.String()))
	}
}

// TestASplitOSCStaysData covers the OSC branch of the sequence check: a
// sequence cut before its terminator is held, and the delta that
// completes it strips the whole sequence whether it ends with BEL or ST.
func TestASplitOSCStaysData(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	r.userPrompt("say hi")

	// Cut before the BEL: the head is held, not printed.
	r.streamDelta("m1", "text", "keep \x1b]0;window title")
	if strings.Contains(out.String(), "window title") {
		t.Errorf("the unfinished sequence printed:\n%q", out.String())
	}
	r.streamDelta("m1", "text", "\x07tail")
	r.paintPending()
	if strings.Contains(out.String(), "window title") {
		t.Errorf("the completed sequence reached the terminal:\n%q", out.String())
	}
	if !strings.Contains(plain(out.String()), "keep tail") {
		t.Errorf("the data around the sequence did not print:\n%q", plain(out.String()))
	}

	// The same with the ST terminator, cut between the ESC and the
	// backslash.
	out.Reset()
	r.finish()
	r.streamDelta("m1", "text", "keep \x1b]0;title\x1b")
	if strings.Contains(out.String(), "title") {
		t.Errorf("the unfinished ST sequence printed:\n%q", out.String())
	}
	r.streamDelta("m1", "text", "\\tail")
	r.paintPending()
	if strings.Contains(out.String(), "title") {
		t.Errorf("the ST sequence reached the terminal:\n%q", out.String())
	}
	if !strings.Contains(plain(out.String()), "keep tail") {
		t.Errorf("the data around the ST sequence did not print:\n%q", plain(out.String()))
	}
}

// TestAnEndlessEscapeIsDropped covers the bound on the held tail: a
// delta stream is under the server's control, so an escape that never
// terminates must not grow the buffer for the life of the run.
func TestAnEndlessEscapeIsDropped(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	r.userPrompt("say hi")

	r.streamDelta("m1", "text", "before \x1b]"+strings.Repeat("x", maxPending*4))
	if n := len(r.pending); n > maxPending {
		t.Fatalf("the held tail is %d bytes, want at most %d", n, maxPending)
	}
	r.streamDelta("m1", "text", "after")
	r.paintPending()
	if !strings.Contains(plain(out.String()), "after") {
		t.Errorf("the stream stopped after the endless escape:\n%q", plain(out.String()))
	}
}

// TestATerminalWithoutALayoutStillReadsRows covers a terminal whose
// footer could not be built: the transcript still reads rows — bare
// line feeds gain their carriage return, and each row starts at
// column zero — instead of staircasing up the screen.
func TestATerminalWithoutALayoutStillReadsRows(t *testing.T) {
	var out bytes.Buffer
	newTR := func(wrap int) (*glamour.TermRenderer, error) {
		return glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(wrap))
	}
	tr, err := newTR(78)
	if err != nil {
		t.Fatal(err)
	}
	r := &renderer{out: &out, wrap: 78, width: 80, term: true, newTR: newTR, tr: tr}

	r.userPrompt("look")

	drawn := out.String()
	for i := 0; i < len(drawn); i++ {
		if drawn[i] == '\n' && (i == 0 || drawn[i-1] != '\r') {
			t.Fatalf("a bare line feed staircased the transcript:\n%q", drawn)
		}
	}
	if !strings.HasPrefix(drawn, "\r") {
		t.Errorf("the row did not start at column zero:\n%q", drawn)
	}
	if !strings.Contains(drawn, "look") {
		t.Errorf("the prompt never read:\n%q", drawn)
	}
}

// nonStylingEscape returns the escape sequences in s that do not set a
// graphic rendition. Every one of them would tell the terminal to do
// something other than paint text.
func nonStylingEscape(s string) []string {
	var found []string
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= '0' && s[j] <= ';' {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j
				continue
			}
			found = append(found, s[i:min(j+1, len(s))])
			i = j
			continue
		}
		found = append(found, s[i:min(i+2, len(s))])
		i++
	}
	return found
}

// controlBytes returns the C0 controls, DEL and C1 controls in s — as a
// byte or as the UTF-8 code point that names it. A terminal obeys these
// rather than printing them. Newline, tab and the escape byte are not
// controls here: nonStylingEscape reads what the escape opens.
func controlBytes(s string) []string {
	var found []string
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		// A byte that is no rune is the raw form of a C1 control.
		stray := size == 1 && s[i] >= 0x80
		if stray || (r != '\n' && r != '\t' && r != 0x1b && unicode.IsControl(r)) {
			found = append(found, fmt.Sprintf("%q", s[i:i+size]))
		}
		i += size
	}
	return found
}

// TestCharacterReferencesDecodeToNoEscape covers the decode the renderer
// does after the stream's strip: an entity that names ESC or BEL must
// not reach the terminal as the byte it names, and the words around it
// still render.
func TestCharacterReferencesDecodeToNoEscape(t *testing.T) {
	r := aTerminal(t, io.Discard, 40)
	for _, tc := range []struct{ name, source string }{
		{"a reset", "before &#27;c after"},
		{"a clipboard write", "before &#x1b;]52;c;aGk=&#7; after"},
		// A private marker makes `m` another command: CSI > 4;2 m sets
		// the terminal's keyboard mode, and CSI ? 4 m asks it to reply.
		{"a keyboard mode in a code span", "before `&#27;[>4;2m` after"},
		{"a mode query", "before &#27;&#91;?4m after"},
		// The remaining private markers, written as entities: < and =.
		{"a private marker less than", "before &#27;[&lt;5m after"},
		{"a private marker equals", "before &#27;[&#61;5m after"},
		// More parameters than a parser holds.
		{"an overlong styling sequence", "before `&#27;[" + strings.Repeat("1;", 33) + "1m` after"},
	} {
		out := r.markdown(tc.source)
		if got := nonStylingEscape(out); len(got) > 0 {
			t.Errorf("%s: the render hands the terminal %q:\n%q", tc.name, got, out)
		}
		if got := controlBytes(out); len(got) > 0 {
			t.Errorf("%s: the render carries the control byte(s) %v:\n%q", tc.name, got, out)
		}
		for _, want := range []string{"before", "after"} {
			if !strings.Contains(plain(out), want) {
				t.Errorf("%s: the words around the escape did not render:\n%q", tc.name, plain(out))
			}
		}
	}
}

// TestControlBytesStayOutOfTheRenderedText covers the controls the strip
// leaves behind: a carriage return, a backspace, a bell, a raw C1 byte
// and the UTF-8 code point that names the same C1 control. Neither the
// rendered text nor the plain one prints them. The tab is the one control
// the filter keeps: it lays the rows out, so it survives where the others
// go.
func TestControlBytesStayOutOfTheRenderedText(t *testing.T) {
	source := "one\t\r two\b three\a four\u009b5m five\x9b6m six\u007f seven"
	for _, branch := range []struct {
		name string
		r    *renderer
	}{
		{"rendered", aTerminal(t, io.Discard, 40)},
		{"plain", newRenderer(io.Discard)},
	} {
		out := branch.r.markdown(source)
		if got := nonStylingEscape(out); len(got) > 0 {
			t.Errorf("%s: the text hands the terminal %q:\n%q", branch.name, got, out)
		}
		if got := controlBytes(out); len(got) > 0 {
			t.Errorf("%s: the text carries the control byte(s) %v:\n%q", branch.name, got, out)
		}
		if !strings.Contains(out, "\t") {
			t.Errorf("%s: the tab did not survive:\n%q", branch.name, out)
		}
		for _, want := range []string{"one", "seven"} {
			if !strings.Contains(plain(out), want) {
				t.Errorf("%s: the words around the controls did not render:\n%q", branch.name, plain(out))
			}
		}
	}
}

// TestTheStyledRenderKeepsItsStyling covers the other side of the filter:
// glamour paints with SGR sequences, and a render that carries nothing
// the filter drops comes back byte for byte — a heading, a fence and bold
// text keep the styling they were painted with.
func TestTheStyledRenderKeepsItsStyling(t *testing.T) {
	source := "# Heading\n\nA **bold** paragraph.\n\n```go\nfunc main() {}\n```\n"
	r := aTerminal(t, io.Discard, 40)
	out := r.markdown(source)

	painted, err := r.tr.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if out != painted {
		t.Errorf("the filter changed a render that carries only styling:\n got %q\nwant %q", out, painted)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("the render lost its styling:\n%q", out)
	}
}
