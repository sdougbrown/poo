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
	code, out := resumeReplExit(t, server, id)
	if code != 0 {
		t.Fatalf("the repl exited %d, want 0", code)
	}
	return out
}

// resumeReplExit is resumeRepl's exit-reading twin: it returns the code
// the repl took and what it wrote, for the cases where a non-zero exit
// is the point.
func resumeReplExit(t *testing.T, server *fakeServer, id string) (int, string) {
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
	return code, out.String()
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
		{Seq: 4, Type: "tool_call", RunID: "r0", Tool: "bash", Summary: "ls", Origin: "model"},
		{Seq: 5, Type: "tool_result", RunID: "r0", Tool: "bash", Outcome: "ok", Text: "one\n", Origin: "tool"},
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

// TestAResumedSessionWhoseLoadIsRefusedExits covers the refusal: a
// server that refuses the load fails the start, and the repl exits
// non-zero without falling through to creating a fresh session.
func TestAResumedSessionWhoseLoadIsRefusedExits(t *testing.T) {
	server := &fakeServer{t: t, refuseLoad: true}

	code, drawn := resumeReplExit(t, server, "s9")
	if code == 0 {
		t.Fatalf("the repl exited 0, want non-zero: the load refusal fell through to a session:\n%s", drawn)
	}
	// The failure is the start's, with the server's answer.
	if !strings.Contains(drawn, "the session's load is not readable") {
		t.Errorf("the transcript does not hold the refusal:\n%s", drawn)
	}
	// The refusal is the end of the line: the repl does not fall
	// through to creating a fresh session for the id.
	if methods := server.methodsSoFar(); slices.Contains(methods, protocol.MethodSessionCreate) {
		t.Errorf("the repl created a session after the load refused: %v", methods)
	}
}

// TestAResumedSessionWithNoHistoryShowsNothing covers the boundary: a
// logged session with zero runs has no scrollback to draw: the greeting
// is there, the transcript holds no history items, and the cap's notice
// is absent.
func TestAResumedSessionWithNoHistoryShowsNothing(t *testing.T) {
	server := &fakeServer{t: t}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	if !strings.Contains(drawn, "poo repl — /help for the commands") {
		t.Errorf("the transcript does not hold the greeting:\n%s", drawn)
	}
	if strings.Contains(drawn, "older history is not shown") {
		t.Errorf("the transcript says older history is not shown, with no history:\n%s", drawn)
	}
	// The transcript holds no history items: what is drawn is the
	// greeting and the prompt, nothing else. A history item's line
	// would be the rest.
	for _, line := range strings.Split(drawn, "\n") {
		if line == "" ||
			strings.Contains(line, "poo repl — /help for the commands") ||
			strings.Contains(line, "💩❯ ") {
			continue
		}
		t.Errorf("the transcript holds a line the empty history does not draw: %q:\n%s", line, drawn)
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
	// The load recovered nothing: the recovery's notice is absent.
	if strings.Contains(drawn, "the session was recovered") {
		t.Errorf("the transcript holds the recovery's notice, with nothing recovered:\n%q", drawn)
	}
}

// TestAReplayedReplyCarriesNoDecodedEscape covers the reply's path: a
// recorded reply whose text carries character references decodes to
// real escapes inside the renderer, so the reply the replay draws goes
// through the same filter the stream's flush does, and neither the
// escape nor the control byte reaches the terminal.
func TestAReplayedReplyCarriesNoDecodedEscape(t *testing.T) {
	server := &fakeServer{t: t}
	server.history = []protocol.HistoryItem{
		{Seq: 1, Type: "message", Role: "assistant", Text: "one&#27;c two\r three&#7;"},
	}

	drawn := resumeRepl(t, server, "s9")
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(plain(drawn), want) {
			t.Errorf("the transcript does not hold %q:\n%q", want, drawn)
		}
	}
	if got := nonStylingEscape(drawn); len(got) > 0 {
		t.Errorf("the replay hands the terminal %q:\n%q", got, drawn)
	}
	if got := controlBytes(drawn); len(got) > 0 {
		t.Errorf("the replay carries the control byte(s) %v:\n%q", got, drawn)
	}
}

// TestTheReplayWarnsWhenTheHistoryWillNotBeRead covers the replay's
// failure: a server that refuses the history is a warning in the
// transcript, and the repl settles to the prompt rather than paging
// forever.
func TestTheReplayWarnsWhenTheHistoryWillNotBeRead(t *testing.T) {
	server := &fakeServer{t: t}
	server.refuseHistory = true
	server.history = []protocol.HistoryItem{
		{Seq: 1, Type: "message", Role: "user", Text: "a line the replay cannot draw"},
	}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	// The warning says the history could not be read, with the
	// server's answer.
	for _, want := range []string{"the session's history could not be read", "the session's history is not readable"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the transcript does not hold %q:\n%s", want, drawn)
		}
	}
	// The repl gave up paging: the history it could not read is not
	// drawn, and the greeting is there, so the repl reached the
	// prompt.
	if strings.Contains(drawn, "a line the replay cannot draw") {
		t.Errorf("the transcript holds the history the server refused:\n%s", drawn)
	}
	if !strings.Contains(drawn, "poo repl — /help for the commands") {
		t.Errorf("the transcript does not hold the greeting: the repl did not settle:\n%s", drawn)
	}
}

// TestTheReplayDrawsEachOriginTheWayTheLiveTranscriptDoes covers the
// rendering a replayed user-role message gets: a line the person typed is
// the prompt's mark, a line that rode the run is the steer's, and a line
// the live transcript never drew — a job's result, a feed's message, a
// hook's step or a notice the session wrote — is not drawn at all.
func TestTheReplayDrawsEachOriginTheWayTheLiveTranscriptDoes(t *testing.T) {
	server := &fakeServer{t: t}
	server.history = []protocol.HistoryItem{
		{Seq: 1, Type: "message", Role: "user", Origin: "user", Text: "what is in the directory?"},
		{Seq: 2, Type: "message", Role: "user", Origin: "steer", Text: "and the hidden ones"},
		{Seq: 3, Type: "message", Role: "user", Origin: "interrupt", Text: "stop, wrong directory"},
		{Seq: 4, Type: "message", Role: "user", Origin: "follow_up", Text: "then summarize it"},
		{Seq: 5, Type: "message", Role: "user", Origin: "job", Text: "the job's result"},
		{Seq: 6, Type: "message", Role: "user", Origin: "feed:ticker", Text: "a feed's message"},
		{Seq: 7, Type: "message", Role: "user", Origin: "hook:cleanup", Text: "a step's output"},
		// A notice the session wrote into the model's view is not a line
		// anyone typed: the live transcript draws none of them, and the
		// replay draws none either.
		{Seq: 8, Type: "message", Role: "user", Origin: "notice", Text: "the context window is nearly full"},
		// The reply's origin is the model's, and the reply is drawn
		// whatever it is: the transcript shows the model's lines.
		{Seq: 9, Type: "message", Role: "assistant", Origin: "model", Text: "Three files."},
		// A record older than the origin says nothing about who put the
		// message there, and it reads as the prompt it always did.
		{Seq: 10, Type: "message", Role: "user", Text: "a line from an older log"},
	}

	drawn := ansi.Strip(resumeRepl(t, server, "s9"))
	for _, want := range []string{
		"❯ what is in the directory?",
		"» and the hidden ones",
		"» stop, wrong directory",
		"» then summarize it",
		"❯ a line from an older log",
		"Three files.",
		"poo repl — /help for the commands",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the transcript does not hold %q:\n%s", want, drawn)
		}
	}
	// Nothing the live transcript does not draw is drawn, and no line
	// is echoed as the prompt when nobody typed it.
	for _, unwanted := range []string{"the job's result", "a feed's message", "a step's output", "the context window is nearly full"} {
		if strings.Contains(drawn, unwanted) {
			t.Errorf("the transcript holds %q, which the live path does not draw:\n%s", unwanted, drawn)
		}
	}
	// The reply to a job's result is the model's, and is drawn: the live
	// transcript loses the cause of that reply in the same way.
	if strings.Index(drawn, "Three files.") < strings.Index(drawn, "then summarize it") {
		t.Errorf("the reply renders before the lines it answers:\n%s", drawn)
	}
}

// TestTheReplayEchoesASteerTheWayTheLiveTranscriptDoes covers the
// parity: a steer drawn by the live path and a steer drawn by a replay
// of the record it left are the same line, mark, styling and all.
func TestTheReplayEchoesASteerTheWayTheLiveTranscriptDoes(t *testing.T) {
	// The live run: the line typed while the run works is a steer.
	hold := make(chan struct{})
	server := &fakeServer{t: t, script: quietRun(hold)}
	out, typeLine, code := startTypedRepl(t, server)
	typeLine("work on it\n")
	waitFor(t, out, "a find")
	typeLine("also check the tests\n")
	waitFor(t, out, "» also check the tests")
	close(hold)
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	if got := <-code; got != 0 {
		t.Fatalf("the repl exited %d, want 0", got)
	}

	// The replay: the same line, as the record the steer left.
	resumed := &fakeServer{t: t}
	resumed.history = []protocol.HistoryItem{
		{Seq: 1, Type: "message", Role: "user", Origin: "steer", Text: "also check the tests"},
	}
	drawn := resumeRepl(t, resumed, "s9")

	live, ok := echoLine(out.String(), "also check the tests")
	if !ok {
		t.Fatalf("the live transcript does not echo the steer:\n%q", out.String())
	}
	replayed, ok := echoLine(drawn, "also check the tests")
	if !ok {
		t.Fatalf("the replay does not echo the steer:\n%q", drawn)
	}
	if live != replayed {
		t.Errorf("the live echo is %q, and the replay's is %q", live, replayed)
	}
}

// echoLine returns the one line of the transcript that holds the text,
// escape sequences and all.
func echoLine(transcript, text string) (string, bool) {
	found := ""
	for _, line := range strings.Split(transcript, "\n") {
		if !strings.Contains(line, text) {
			continue
		}
		if found != "" {
			return "", false
		}
		found = line
	}
	return found, found != ""
}
