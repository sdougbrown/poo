package repl

import (
	"bytes"
	"strings"
	"testing"
)

// TestAMessageRendersItsMarkdown covers the replies: a code fence
// reads back as a highlighted block, and the text keeps its shape.
func TestAMessageRendersItsMarkdown(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)
	r.message("# Heading\n\nA paragraph with `code`.\n\n```go\nfunc main() {}\n```\n")

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
	r.message("# Heading\n\nBody text.\n")

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
	// way: the lines the server cut count toward the total.
	r.toolResult("bash", "ok", "one line\n", 4)
	if !strings.Contains(out.String(), "+4 more lines") {
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
		{1_000_000, "1.0M"},
		{1_234_567, "1.2M"},
	} {
		if got := shortTokens(tc.n); got != tc.want {
			t.Errorf("shortTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
