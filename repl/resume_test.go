package repl

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sdougbrown/poo/wire/protocol"
)

// resumeRepl starts the repl against a fake server with the session id
// the options resume. The server's history and its load result are the
// test's; the transcript is what the repl wrote, escape sequences and
// all, so a test that reads the words strips them first.
func resumeRepl(t *testing.T, server *fakeServer, id string) string {
	t.Helper()
	stdin, stdout := pipeUp(t, server)
	var out bytes.Buffer
	code := Run(Options{
		WorkingDir: t.TempDir(),
		ResumeID:   id,
		In:         strings.NewReader("/quit\n"),
		Out:        &out,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	})
	if code != 0 {
		t.Fatalf("the repl exited %d, want 0", code)
	}
	return out.String()
}

// TestAResumedSessionLoadsInsteadOfCreating covers the reopen: the repl
// asks for the session the id names instead of starting one, and it
// renders the conversation the session already had, without the items
// the transcript has no place for.
func TestAResumedSessionLoadsInsteadOfCreating(t *testing.T) {
	server := &fakeServer{t: t}
	server.recoveredTail = true
	server.interrupted = []string{"r0"}
	server.history = []protocol.HistoryItem{
		{Seq: 1, Type: "run_started", RunID: "r0"},
		{Seq: 2, Type: "message", RunID: "r0", Role: "user", Text: "what is in the directory?"},
		{Seq: 3, Type: "message", RunID: "r0", Role: "assistant", Text: "Three files."},
		{Seq: 4, Type: "tool_call", RunID: "r0", Tool: "bash", Summary: "ls"},
		{Seq: 5, Type: "tool_result", RunID: "r0", Tool: "bash", Outcome: "ok", Text: "one\n"},
		{Seq: 6, Type: "permission", RunID: "r0", Tool: "bash", Decision: "allow"},
		{Seq: 7, Type: "compaction", RunID: "r0", Text: "summary"},
		{Seq: 8, Type: "run_settled", RunID: "r0", Reason: "end_turn"},
	}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	for _, want := range []string{
		"what is in the directory?",
		"Three files.",
		"ls",
		"one",
		"the view was compacted: summary",
		"the session was recovered",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the transcript does not hold %q:\n%s", want, drawn)
		}
	}
	// The run's boundary and the permission's answer are not the
	// conversation, so the transcript says nothing about them.
	for _, unwanted := range []string{"end_turn", "allow"} {
		if strings.Contains(drawn, unwanted) {
			t.Errorf("the transcript holds %q:\n%s", unwanted, drawn)
		}
	}

	// The user's line is what the session was asked, and the reply
	// answers it: the transcript reads in the order the session ran.
	asked := strings.Index(drawn, "what is in the directory?")
	answered := strings.Index(drawn, "Three files.")
	if asked < 0 || answered < 0 {
		t.Fatalf("the transcript does not hold both sides of the exchange:\n%s", drawn)
	}
	if asked > answered {
		t.Errorf("the reply renders before the line it answers:\n%s", drawn)
	}

	methods := server.methodsSoFar()
	if !slices.Contains(methods, protocol.MethodSessionLoad) {
		t.Errorf("the repl opened the session with %v, want session/load", methods)
	}
	if slices.Contains(methods, protocol.MethodSessionCreate) {
		t.Errorf("the repl created a session as well as loading one: %v", methods)
	}
}

// TestAResumedSessionShowsTheNewestHistory covers the cap: a session with
// more history than the repl shows says so, and renders the newest items
// rather than the oldest.
func TestAResumedSessionShowsTheNewestHistory(t *testing.T) {
	// The session holds historyItems items the repl shows and extra
	// ones the cap drops.
	const extra = 100
	total := historyItems + extra
	server := &fakeServer{t: t}
	for i := 1; i <= total; i++ {
		server.history = append(server.history, protocol.HistoryItem{
			Seq:  int64(i),
			Type: "message",
			Role: "user",
			Text: fmt.Sprintf("said %03d", i),
		})
	}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	if !strings.Contains(drawn, "older history is not shown") {
		t.Errorf("the transcript does not say older history is missing:\n%s", drawn)
	}
	// The newest history is rendered in the order it was recorded, once
	// each: a page that lands at the wrong end of the items reads as a
	// conversation that jumps back and forth.
	previous := 0
	for n := extra + 1; n <= total; n++ {
		item := fmt.Sprintf("said %03d", n)
		if got := strings.Count(drawn, item); got != 1 {
			t.Errorf("the transcript holds %q %d times, want once", item, got)
		}
		at := strings.Index(drawn, item)
		if at < previous {
			t.Fatalf("the transcript holds %q before the item recorded before it:\n%s", item, drawn)
		}
		previous = at
	}
	// The cap drops the oldest items whole: the transcript holds none of
	// them, rather than the tail of the item the cap cut.
	for n := 1; n <= extra; n++ {
		if item := fmt.Sprintf("said %03d", n); strings.Contains(drawn, item) {
			t.Errorf("the transcript holds the oldest item %q, which the cap drops", item)
		}
	}
}

// TestTheReplayCapsAPageThatOvershootsTheLimit covers the trim: a
// server that answers a page bigger than the one the repl asked for
// lands the running total past the cap, and the cap cuts the oldest
// items whole.
func TestTheReplayCapsAPageThatOvershootsTheLimit(t *testing.T) {
	// The session holds historyItems items the repl shows and extra
	// ones the cap drops, and the server's page holds more than the
	// one the repl asked for, so the second page lands the total past
	// the cap.
	const extra = 20
	total := historyItems + extra
	server := &fakeServer{t: t}
	server.historyPageSize = 300
	for i := 1; i <= total; i++ {
		server.history = append(server.history, protocol.HistoryItem{
			Seq:  int64(i),
			Type: "message",
			Role: "user",
			Text: fmt.Sprintf("said %03d", i),
		})
	}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	if !strings.Contains(drawn, "older history is not shown") {
		t.Errorf("the transcript does not say older history is missing:\n%s", drawn)
	}
	// The newest history is rendered in the order it was recorded, once
	// each: a page that lands at the wrong end of the items reads as a
	// conversation that jumps back and forth.
	previous := 0
	for n := extra + 1; n <= total; n++ {
		item := fmt.Sprintf("said %03d", n)
		if got := strings.Count(drawn, item); got != 1 {
			t.Errorf("the transcript holds %q %d times, want once", item, got)
		}
		at := strings.Index(drawn, item)
		if at < previous {
			t.Fatalf("the transcript holds %q before the item recorded before it:\n%s", item, drawn)
		}
		previous = at
	}
	// The cap drops the oldest items whole: the transcript holds none of
	// them, rather than the tail of the item the cap cut.
	for n := 1; n <= extra; n++ {
		if item := fmt.Sprintf("said %03d", n); strings.Contains(drawn, item) {
			t.Errorf("the transcript holds the oldest item %q, which the cap drops", item)
		}
	}
}

// TestAResumedSessionSanitizesTheRecordedText covers what a replay puts on
// the terminal: the text is the session's, so the escapes and the cursor
// controls it recorded are stripped, and the words stay.
func TestAResumedSessionSanitizesTheRecordedText(t *testing.T) {
	server := &fakeServer{t: t}
	server.history = []protocol.HistoryItem{
		{Seq: 1, Type: "message", Role: "user", Text: "\x1b[2Jwho\x07 put a clear in here"},
		{Seq: 2, Type: "message", Role: "assistant", Text: "\x1b[5;5Ha red reply"},
		{Seq: 3, Type: "compaction", Text: "\x1b[2Jsummary"},
	}

	drawn := resumeRepl(t, server, "s9")
	for _, unwanted := range []string{"\x1b[2J", "\x1b[5;5H", "\x07"} {
		if strings.Contains(drawn, unwanted) {
			t.Errorf("the transcript carries the recorded %q:\n%q", unwanted, drawn)
		}
	}
	for _, want := range []string{"who put a clear in here", "a red reply", "the view was compacted: summary"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the transcript does not hold %q:\n%q", want, drawn)
		}
	}
}
