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
	server := &fakeServer{t: t}
	for i := 1; i <= historyItems+100; i++ {
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
	last := fmt.Sprintf("said %03d", historyItems+100)
	if !strings.Contains(drawn, last) {
		t.Errorf("the transcript does not hold the newest item %q", last)
	}
	oldest := fmt.Sprintf("said %03d", 1)
	if strings.Contains(drawn, oldest) {
		t.Errorf("the transcript holds the oldest item %q, which the cap drops", oldest)
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
