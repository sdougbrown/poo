package repl

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chzyer/readline"
	"github.com/sdougbrown/poo/wire/protocol"
)

// startRepl starts the repl against a fresh fake server. The script
// reads the input the person types and returns what the repl printed.
func startRepl(t *testing.T, input string) (string, *fakeServer) {
	return startReplAs(t, input, nil)
}

// startReplServer starts the repl against a fake server the test
// prepared: its fields — refusals, warnings, levels — are the script.
func startReplServer(t *testing.T, input string, server *fakeServer) (string, *fakeServer) {
	t.Helper()
	return startReplAs(t, input, server)
}

// startReplAs starts the repl against a fake server: the test's, when
// one was prepared, or a fresh one. The session's model, and the
// reasoning level it starts at, come from the server's answers.
func startReplAs(t *testing.T, input string, server *fakeServer) (string, *fakeServer) {
	t.Helper()
	if server == nil {
		server = &fakeServer{t: t}
	}
	stdin, stdout := pipeUp(t, server)

	var out bytes.Buffer
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         strings.NewReader(input),
		Out:        &out,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	code := Run(opts)
	if code != 0 {
		t.Fatalf("the repl exited %d, want 0", code)
	}
	return out.String(), server
}

// startTypedRepl starts the repl with input the test types a line at a
// time, so a decision is typed after the ask that wants it.
func startTypedRepl(t *testing.T, server *fakeServer) (out *syncBuffer, typeLine func(string), code <-chan int) {
	t.Helper()
	stdin, stdout := pipeUp(t, server)
	in, typed := io.Pipe()
	t.Cleanup(func() { typed.Close() })

	out = &syncBuffer{}
	done := make(chan int, 1)
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         in,
		Out:        out,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { done <- Run(opts) }()
	return out, func(line string) {
		t.Helper()
		if _, err := typed.Write([]byte(line)); err != nil {
			t.Errorf("typing %q: %v", line, err)
		}
	}, done
}

// syncBuffer is a transcript a test reads while the repl writes it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor waits for what the repl drew to say want.
func waitFor(t *testing.T, drawn interface{ String() string }, want string) {
	t.Helper()
	waitTimes(t, drawn, want, 1)
}

// waitTimes waits for what the repl drew to say want count times.
func waitTimes(t *testing.T, drawn interface{ String() string }, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(drawn.String(), want) >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the repl drew %q %d times, want %d:\n%s",
		want, strings.Count(drawn.String(), want), count, drawn.String())
}

// waitPrompts waits until the server has want prompts from the
// session, and returns them.
func (s *fakeServer) waitPrompts(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		prompts := append([]string(nil), s.prompts...)
		s.mu.Unlock()
		if len(prompts) >= want {
			return prompts
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("the server got %d prompts, want %d: %v", len(s.prompts), want, s.prompts)
	return nil
}

// waitCancels waits until the session's cancels number want, and
// returns them.
func (s *fakeServer) waitCancels(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		cancels := append([]string(nil), s.cancels...)
		s.mu.Unlock()
		if len(cancels) >= want {
			return cancels
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("the server got %d cancels, want %d: %v", len(s.cancels), want, s.cancels)
	return nil
}

func TestUsageUpdatesTheStatus(t *testing.T) {
	m := &machine{status: &status{contextLimit: 262144}}
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"input_tokens": 12300}}, "r1", &runState{})
	if prompt := m.status.prompt(); !strings.Contains(prompt, "ctx 12.3k/262.1k") {
		t.Fatalf("the prompt lacks the run's usage: %s", prompt)
	}
}

func TestCtrlCClearsWhitespace(t *testing.T) {
	m := &machine{}
	if code, done := m.line(lineResult{line: " ", err: readline.ErrInterrupt}, nil); done {
		t.Fatalf("Ctrl-C exited %d instead of clearing the typed space", code)
	}
}

// TestARunRendersTheReplyTheToolsAndTheSettle covers one prompt's
// whole ride: the deltas render as markdown, the tool call and its
// result decorate the transcript, and the settle names the outcome
// and the accounting.
func TestARunRendersTheReplyTheToolsAndTheSettle(t *testing.T) {
	out, server := startRepl(t, "say hello\n/quit\n")

	for _, want := range []string{
		"❯ say hello",
		"· thinking ·",
		"hmm",
		"Hello, world.",
		"⚙ bash",
		"file one",
		"file two",
		"end_turn",
		"2 turn(s)",
		// The settle's output side, from the fake's 1100 output tokens:
		// a regression in decoding them would print "0 out".
		"1.1k out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	// The statusline names the model and the window the usage filled.
	if !strings.Contains(out, "gemma4") {
		t.Errorf("the statusline names no model:\n%s", out)
	}
	if !strings.Contains(out, "12.3k/262.1k") {
		t.Errorf("the statusline shows no context fill:\n%s", out)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.prompts) != 1 || server.prompts[0] != "say hello" {
		t.Errorf("the server got the prompts %v, want one \"say hello\"", server.prompts)
	}
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestAnAskWaitsForTheDecisionItWasGiven covers the permission ask:
// the client answers it on the wire, and the answer is what the person
// typed.
func TestAnAskWaitsForTheDecisionItWasGiven(t *testing.T) {
	// The server answers the switch with a name its catalog does not
	// carry, the way a normalising server would.
	out, typeLine, code := startTypedRepl(t, &fakeServer{t: t})
	typeLine("rm the lot\n")
	waitFor(t, out, "allow bash?")
	typeLine("y\n")
	waitFor(t, out, "→ allow")
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	if got := <-code; got != 0 {
		t.Fatalf("the repl exited %d, want 0", got)
	}
	if !strings.Contains(out.String(), "→ allow") {
		t.Errorf("the transcript shows no decision:\n%s", out.String())
	}
}

func TestAnAskShowsEveryLineOfItsSummary(t *testing.T) {
	server := &fakeServer{t: t, askSummary: "echo first\necho second"}
	out, typeLine, code := startTypedRepl(t, server)
	typeLine("run this\n")
	// Both lines of the summary reach the transcript, not just the last.
	waitFor(t, out, "echo first")
	waitFor(t, out, "echo second")
	typeLine("y\n")
	waitFor(t, out, "→ allow")
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	<-code
}

func TestAnEmptyAnswerDeniesTheAsk(t *testing.T) {
	server := &fakeServer{t: t}
	out, typeLine, done := startTypedRepl(t, server)
	typeLine("run this\n")
	waitFor(t, out, "allow bash?")
	typeLine("\n")
	waitFor(t, out, "→ deny")
	waitFor(t, out, "end_turn")
	typeLine("/quit\n")
	if code := <-done; code != 0 {
		t.Fatalf("the repl exited %d, want 0", code)
	}
}

// TestAnUnknownAnswerDeniesTheAsk covers the safe side of the ask:
// anything that is not a yes refuses the call.
func TestAnUnknownAnswerDeniesTheAsk(t *testing.T) {
	// The server answers the switch with a name its catalog does not
	// carry, the way a normalising server would.
	out, typeLine, code := startTypedRepl(t, &fakeServer{t: t})
	typeLine("rm the lot\n")
	waitFor(t, out, "allow bash?")
	typeLine("no idea\n")
	waitFor(t, out, "→ deny")
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	<-code
}

// TestTheCommandsDoWhatTheirHelpSays covers the slash commands the
// session answers without the server: the help lists them, and an
// unknown one is a prompt, not a failure.
func TestTheCommandsDoWhatTheirHelpSays(t *testing.T) {
	out, _ := startRepl(t, "/help\n/quit\n")

	for _, want := range []string{"/quit", "/less", "/model", "/thinking", "Esc", "/help"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help lacks %q:\n%s", want, out)
		}
	}
}

// TestAnUnknownCommandIsAPromptNotAFailure covers a line the repl does
// not answer to: it goes to the session like any other prompt, and the
// failure the server makes of it reads without ending the session.
func TestAnUnknownCommandIsAPromptNotAFailure(t *testing.T) {
	out, server := startRepl(t, "/frobnicate\n/quit\n")

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.prompts) != 1 || server.prompts[0] != "/frobnicate" {
		t.Errorf("the server got the prompts %v, want one \"/frobnicate\"", server.prompts)
	}
	if strings.Contains(out, "✗") {
		t.Errorf("the unknown command failed the session:\n%s", out)
	}
}

// TestACancelWithNothingRunningSendsNothing covers /cancel at an idle
// prompt: there is no run to stop, so no call goes out, and the
// session stays open.
func TestACancelWithNothingRunningSendsNothing(t *testing.T) {
	out, server := startRepl(t, "/cancel\n/quit\n")

	if strings.Contains(out, "✗") {
		t.Errorf("the cancel failed the session:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.cancels) != 0 {
		t.Errorf("the server got the cancels %v, want none", server.cancels)
	}
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestACancelNamesTheRunItStops covers the wire shape the cancel rides
// on: run/cancel names the run, because the server refuses a cancel
// that does not say what to stop.
func TestACancelNamesTheRunItStops(t *testing.T) {
	server := &fakeServer{t: t}
	stdin, stdout := pipeUp(t, server)
	conn := startConn(stdout, stdin)
	m := &machine{conn: conn, sessionID: "s1", runID: "r1", r: newRenderer(&bytes.Buffer{})}

	m.cancelRun("r1")

	deadline := time.After(5 * time.Second)
	for {
		server.mu.Lock()
		n := len(server.cancels)
		server.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the cancel never reached the server")
		case <-time.After(10 * time.Millisecond):
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.cancels[0] != "r1" {
		t.Errorf("the cancel named %q, want r1", server.cancels[0])
	}
}

// TestAnInputThatEndsWithoutAQuitStillExitsCleanly covers the EOF path:
// a session whose input ends ends cleanly, with no failure read.
func TestAnInputThatEndsWithoutAQuitStillExitsCleanly(t *testing.T) {
	out, _ := startRepl(t, "say hello\n")

	if strings.Contains(out, "✗") {
		t.Errorf("the end of input read as a failure:\n%s", out)
	}
}

// TestARefusedPromptFailsTheLineNotTheSession covers a prompt the
// server refused: the refusal is a failure line in the transcript, and
// the repl keeps going.
func TestARefusedPromptFailsTheLineNotTheSession(t *testing.T) {
	out, server := startReplServer(t, "say hello\n/quit\n", &fakeServer{t: t, refusePrompt: true})

	if !strings.Contains(out, "✗") || !strings.Contains(out, "busy") {
		t.Errorf("the refusal did not read in the transcript:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestALessWithoutAPagerFailsTheLineNotTheSession covers /less with no
// less on the path: the failure is a line in the transcript, and the
// session goes on.
func TestALessWithoutAPagerFailsTheLineNotTheSession(t *testing.T) {
	t.Setenv("PATH", "")
	out, _ := startRepl(t, "/less\n/quit\n")

	if !strings.Contains(out, "✗") {
		t.Errorf("the missing pager read no failure:\n%s", out)
	}
}

// TestALineTypedDuringARunIsNotLostSilently covers the Enter pressed
// while a turn works: the line is not sent as the next prompt, and the
// transcript says so — with the text and the key that stops the turn —
// instead of discarding it.
func TestALineTypedDuringARunIsNotLostSilently(t *testing.T) {
	var out bytes.Buffer
	m := &machine{r: newRenderer(&out), status: newStatus("")}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	in := &plainInput{write: func(string) {}}

	if code, done := m.line(lineResult{line: "a second thought"}, in); done {
		t.Fatalf("the line ended the session (code %d)", code)
	}
	drawn := out.String()
	if !strings.Contains(drawn, "a second thought") {
		t.Errorf("the typed line is not on screen:\n%s", drawn)
	}
	if !strings.Contains(drawn, "Esc stops it") {
		t.Errorf("nothing says which key stops the turn:\n%s", drawn)
	}
}

// TestAModelSwitchTakesTheNewModelAndItsWindow covers `/model <name>`:
// the session's chat role moves on the wire, and the statusline names
// the new model and its window.
func TestAModelSwitchTakesTheNewModelAndItsWindow(t *testing.T) {
	// The run after the switch gives the statusline a token count to
	// fill the new window against.
	out, server := startRepl(t, "/model qwen\nsay hi\n/quit\n")

	if !strings.Contains(out, "model → qwen") {
		t.Errorf("the transcript shows no switch:\n%s", out)
	}
	if !strings.Contains(out, "ctx 12.3k/131.1k") {
		t.Errorf("the statusline does not fill the new model's window:\n%s", out)
	}
	// The statusline is written again once the switch lands, and that
	// write names the new model beside the new window: a switch that
	// kept the old name would render gemma4 over qwen's window.
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ctx 12.3k/131.1k") && strings.Contains(line, "qwen") {
			found = true
		}
	}
	if !found {
		t.Errorf("no statusline names qwen beside its window:\n%s", out)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
	}
}

// TestAModelSwitchTheCatalogCannotDescribe covers a switch the server
// accepts for a model the client's catalog does not hold: the session
// moves, and the statusline keeps the window it had rather than
// inventing one.
func TestAModelSwitchTheCatalogCannotDescribe(t *testing.T) {
	// The server answers the switch with a name its catalog does not
	// carry, the way a normalising server would.
	out, typeLine, code := startTypedRepl(t, &fakeServer{t: t, replyAs: "hidden"})
	typeLine("say hi\n")
	waitFor(t, out, "allow bash?")
	typeLine("y\n")
	waitFor(t, out, "end_turn")
	typeLine("/model qwen\n")
	waitFor(t, out, "model → hidden")
	typeLine("/quit\n")
	<-code

	plain := ansi.Strip(out.String())
	// The window the run filled stays: there is no entry to read a new
	// one from, and the old model's must not be presented as the new
	// model's guess.
	found := false
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "│ hidden") && strings.Contains(line, "262.1k") {
			found = true
		}
	}
	if !found {
		t.Errorf("no statusline names hidden beside the window it kept:\n%s", plain)
	}
}

// TestAModelSwitchToAnUnknownNameIsRefused covers a switch the catalog
// cannot resolve: the refusal reads in the transcript, and the session
// keeps the model it had.
func TestAModelSwitchToAnUnknownNameIsRefused(t *testing.T) {
	out, server := startRepl(t, "/model nope\n/quit\n")

	if !strings.Contains(out, "✗") {
		t.Errorf("the transcript shows no refusal:\n%s", out)
	}
	// The statusline still names the model the wire session kept: a
	// refusal that clobbered the client's copy would print the name it
	// never switched to.
	plain := ansi.Strip(out)
	if strings.Contains(plain, "│ nope") {
		t.Errorf("the refused switch took the statusline's model:\n%s", plain)
	}
	if !strings.Contains(plain, "│ gemma4") {
		t.Errorf("the statusline lost the model the session kept:\n%s", plain)
	}
	// The refusal is the matcher's to word: no catalog entry matches
	// the fragment. A failure line that renders anything else — or
	// nothing — is the failure this test watches for.
	if !strings.Contains(out, "no model matches \"nope\"") {
		t.Errorf("the refusal does not name the catalog miss:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("the server took the switch %v", server.switches)
	}
}

// TestAFragmentThatMatchesOneModelSwitchesToIt covers the short form:
// a name nobody types in full is a fragment, and the one model it matches
// is the switch.
func TestAFragmentThatMatchesOneModelSwitchesToIt(t *testing.T) {
	// The run after the switch gives the statusline a token count to
	// fill the new model's window against.
	out, server := startRepl(t, "/model qwe\nsay hi\n/quit\n")

	if !strings.Contains(out, "model → qwen") {
		t.Errorf("the fragment did not switch:\n%s", out)
	}
	if !strings.Contains(out, "ctx 12.3k/131.1k") {
		t.Errorf("the statusline does not fill the new model's window:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
	}
}

// gemmaFleet is a catalog whose names share the fragment "gemma": the
// session's own model and one it does not run.
func gemmaFleet(t *testing.T) *fakeServer {
	return &fakeServer{
		t: t,
		models: []fakeModel{
			{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off", "low", "high"}},
			{name: "gemma-3-27b", modelID: "google/gemma-3-27b", window: 131072, levels: []string{"off"}},
		},
	}
}

// TestAFragmentThatMatchesSeveralListsThemRanked covers the other side:
// the matches read as a numbered list, best first, the session's own model
// marked, and the session does not switch until a number picks one.
func TestAFragmentThatMatchesSeveralListsThemRanked(t *testing.T) {
	out, server := startReplServer(t, "/model gemma\n/quit\n", gemmaFleet(t))

	for _, want := range []string{
		"1. gemma-3-27b · google/gemma-3-27b",
		"2. gemma4 ← · gemma4",
		"/model <number> picks one of these",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("a listing switched the model: %v", server.switches)
	}
}

// TestANumberPicksFromTheRankedList covers the pick: the number names a
// model off the list, and the session takes that model and its window.
func TestANumberPicksFromTheRankedList(t *testing.T) {
	out, server := startReplServer(t, "/model gemma\n/model 1\nsay hi\n/quit\n", gemmaFleet(t))

	if !strings.Contains(out, "model → gemma-3-27b") {
		t.Errorf("the numbered pick switched nothing:\n%s", out)
	}
	if !strings.Contains(out, "ctx 12.3k/131.1k") {
		t.Errorf("the statusline does not fill the picked model's window:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "gemma-3-27b" {
		t.Errorf("the server got the switches %v, want one to gemma-3-27b", server.switches)
	}
}

// TestTheCatalogListIsNumberedAndPickable covers `/model` bare: the
// catalog reads numbered in name order, and a number takes the model off
// that list.
func TestTheCatalogListIsNumberedAndPickable(t *testing.T) {
	out, server := startRepl(t, "/model\n/model 3\n/quit\n")

	for _, want := range []string{
		"1. gemma4 ← · gemma4",
		"2. mystery · mystery",
		"3. qwen · qwen",
		"model → qwen",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
	}
}

// TestANumberWithNoListSaysWhereTheListIs covers a pick before anything
// was listed: the repl says so and switches nothing.
func TestANumberWithNoListSaysWhereTheListIs(t *testing.T) {
	out, server := startRepl(t, "/model 2\n/quit\n")

	if !strings.Contains(out, "✗") || !strings.Contains(out, "there is no /model list to pick 2 from") {
		t.Errorf("the transcript does not say there is no list:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("the server took the switch %v", server.switches)
	}
}

// TestANumberTheListCannotHoldSaysSo covers the marks that name no
// model: a number past the end of the list, and the digit that no list
// numbers from.
func TestANumberTheListCannotHoldSaysSo(t *testing.T) {
	out, server := startRepl(t, "/model\n/model 9\n/model 0\n/quit\n")

	// Both marks name no model of the three the list holds: 9 is past
	// its end, and 0 is no mark at all.
	const refusal = "✗ the last /model list has 3 model(s) — /model <number> takes 1-3"
	if n := strings.Count(out, refusal); n != 2 {
		t.Errorf("the transcript says %q %d time(s), want 2:\n%s", refusal, n, out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("the server took the switch %v", server.switches)
	}
}

// TestAFailedSearchDropsTheListItReplaced covers the stale list: a
// search that matches nothing clears the listing it was going to
// replace, so a number after it has nothing to pick from.
func TestAFailedSearchDropsTheListItReplaced(t *testing.T) {
	out, server := startRepl(t, "/model\n/model zzzz\n/model 1\n/quit\n")

	if !strings.Contains(out, "no model matches") {
		t.Fatalf("the search did not fail:\n%s", out)
	}
	if !strings.Contains(out, "there is no /model list to pick 1 from") {
		t.Errorf("the stale list still answered the pick:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("the server took the switch %v", server.switches)
	}
}

// TestAFragmentNoNameHoldsListsNothing covers the empty answer: the repl
// says nothing matches rather than offering the whole catalog.
func TestAFragmentNoNameHoldsListsNothing(t *testing.T) {
	out, _ := startRepl(t, "/model zzz\n/quit\n")

	if !strings.Contains(out, "✗") || !strings.Contains(out, "no model matches") {
		t.Errorf("the transcript does not say nothing matched:\n%s", out)
	}
	if strings.Contains(out, "qwen") {
		t.Errorf("a fragment that matched nothing listed the catalog:\n%s", out)
	}
}

// TestAnExactNameSwitchesWithoutAList covers the names scripts and muscle
// memory use: a fragment that names a catalog entry is the switch, even
// when other entries hold it as a fragment too.
func TestAnExactNameSwitchesWithoutAList(t *testing.T) {
	out, server := startReplServer(t, "/model qwen\n/quit\n", &fakeServer{
		t: t,
		models: []fakeModel{
			{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off", "low", "high"}},
			{name: "qwen", modelID: "qwen", window: 131072, levels: []string{"off", "medium"}},
			{name: "qwen-coder", modelID: "qwen-coder", window: 131072, levels: []string{"off"}},
		},
	})

	if !strings.Contains(out, "model → qwen") {
		t.Errorf("the exact name did not switch:\n%s", out)
	}
	if strings.Contains(out, "picks one of these") {
		t.Errorf("the exact name listed candidates instead of switching:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
	}
}

// TestTheThinkingCommandCyclesTheModelSLevels covers `/thinking` with no
// level: it climbs the chat model's own levels in their order, the first
// one when the session runs at none, and the statusline names the level it
// reached.
func TestTheThinkingCommandCyclesTheModelSLevels(t *testing.T) {
	out, server := startRepl(t, "/thinking\n/thinking\n/thinking\n/quit\n")

	for _, want := range []string{"thinking → off", "thinking → low", "thinking → high"} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "🧠 high") {
		t.Errorf("the statusline names no level:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if got := strings.Join(server.levels, ","); got != "off,low,high" {
		t.Errorf("the server got the levels %q, want off,low,high", got)
	}
}

// TestTheThinkingCycleWrapsAtTheEndOfTheList covers the last level: the
// next one is the first again.
func TestTheThinkingCycleWrapsAtTheEndOfTheList(t *testing.T) {
	out, _ := startRepl(t, "/thinking high\n/thinking\n/quit\n")

	if !strings.Contains(out, "thinking → high") {
		t.Errorf("the named level did not take:\n%s", out)
	}
	if !strings.Contains(out, "thinking → off") {
		t.Errorf("the cycle did not wrap:\n%s", out)
	}
}

// TestANamedThinkingLevelIsSet covers `/thinking <level>`: the level goes
// to the server as it was named, and the statusline shows what the session
// runs at.
func TestANamedThinkingLevelIsSet(t *testing.T) {
	out, server := startRepl(t, "/thinking High\n/quit\n")

	if !strings.Contains(out, "thinking → high") {
		t.Errorf("the transcript shows no level:\n%s", out)
	}
	if !strings.Contains(out, "🧠 high") {
		t.Errorf("the statusline names no level:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.levels) != 1 || server.levels[0] != "high" {
		t.Errorf("the server got the levels %v, want one \"high\"", server.levels)
	}
}

// TestAThinkingLevelTheModelDoesNotTakeIsRefused covers the server's
// refusal: the level stays where it was, and the message names the levels
// the model does take.
func TestAThinkingLevelTheModelDoesNotTakeIsRefused(t *testing.T) {
	out, server := startRepl(t, "/thinking medium\n/quit\n")

	if !strings.Contains(out, "✗") {
		t.Errorf("the transcript shows no refusal:\n%s", out)
	}
	if !strings.Contains(out, "off, low, high") {
		t.Errorf("the refusal does not name the model's levels:\n%s", out)
	}
	if strings.Contains(out, "🧠") {
		t.Errorf("the statusline names a level the session does not run at:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.levels) != 0 {
		t.Errorf("the server took the level %v", server.levels)
	}
}

// TestAModelSwitchTakesTheLevelTheWireReports covers the level across a
// switch: a level the new model does not take is gone, and the transcript
// says where the session runs now.
func TestAModelSwitchTakesTheLevelTheWireReports(t *testing.T) {
	out, _ := startRepl(t, "/thinking high\n/model qwen\n/quit\n")

	if !strings.Contains(out, "model → qwen") {
		t.Errorf("the transcript shows no switch:\n%s", out)
	}
	if !strings.Contains(out, "thinking → off") {
		t.Errorf("the switch did not report the level it left the session at:\n%s", out)
	}
	if !strings.Contains(out, "🧠 off") {
		t.Errorf("the statusline names no level after the switch:\n%s", out)
	}
}

// TestAModelSwitchFallsBackToTheLevelTheModelTakes covers a move to a
// model that cannot be told off: the level the session ran at is one
// the new model does not take, and the session lands at the lowest
// level the model declares.
func TestAModelSwitchFallsBackToTheLevelTheModelTakes(t *testing.T) {
	out, _ := startRepl(t, "/thinking off\n/model mystery\n/quit\n")

	if !strings.Contains(out, "model → mystery") {
		t.Errorf("the transcript shows no switch:\n%s", out)
	}
	if !strings.Contains(out, "thinking → low") {
		t.Errorf("the switch did not fall back to the lowest declared level:\n%s", out)
	}
}

// TestTheStatuslineNamesTheLevelTheSessionOpenedAt covers the read-back:
// a session that runs at a level says so before anything is typed.
func TestTheStatuslineNamesTheLevelTheSessionOpenedAt(t *testing.T) {
	out, _ := startReplAs(t, "/quit\n", &fakeServer{t: t, level: "high"})

	if !strings.Contains(out, "🧠 high") {
		t.Errorf("the statusline names no level:\n%s", out)
	}
}

// TestTheLevellessModelClimbsTheProtocolLevels covers a model that
// declares no levels: the cycle falls back to the protocol's canonical
// order, and wraps from the most thinking back to off.
func TestTheLevellessModelClimbsTheProtocolLevels(t *testing.T) {
	m := &machine{
		r:      newRenderer(io.Discard),
		status: newStatus(""),
		catalog: map[string]protocol.ModelInfo{
			"plain": {Name: "plain"},
		},
	}
	m.status.model = "plain"

	if got, ok, _ := m.nextThinkingLevel(); !ok || got != "off" {
		t.Errorf("the first level is %q (ok %v), want off", got, ok)
	}
	m.status.level = "off"
	if got, ok, _ := m.nextThinkingLevel(); !ok || got != "minimal" {
		t.Errorf("the level after off is %q (ok %v), want minimal", got, ok)
	}
	m.status.level = "xhigh"
	if got, ok, _ := m.nextThinkingLevel(); !ok || got != "off" {
		t.Errorf("the level after xhigh is %q (ok %v), want off", got, ok)
	}
}

// TestTheBacktabFilterCyclesTheLevel covers the key binding: the editor
// hands Shift+Tab to the filter one rune at a time, and the run that
// completes it moves the level while the others pass through.
func TestTheBacktabFilterCyclesTheLevel(t *testing.T) {
	server := &fakeServer{t: t}
	stdin, stdout := pipeUp(t, server)
	m := &machine{
		out:       io.Discard,
		r:         newRenderer(io.Discard),
		conn:      startConn(stdout, stdin),
		sessionID: "s1",
		status:    newStatus(""),
		catalog: map[string]protocol.ModelInfo{
			"gemma4": {Name: "gemma4", ReasoningLevels: []string{"off", "low", "high"}},
		},
	}
	m.status.model = "gemma4"
	tab := &backtab{m: m}

	// A rune that is not the sequence passes through and starts over.
	if r, process := tab.filter('a'); r != 'a' || !process {
		t.Errorf("the filter ate the rune %q (process %v)", r, process)
	}
	// The ESC is the vim mode's own key, and it passes on.
	if r, process := tab.filter(readline.CharEsc); r != readline.CharEsc || !process {
		t.Errorf("the filter ate the ESC (process %v)", process)
	}
	// An escape that starts no sequence aborts: the next rune is a key
	// again, and the sequence still works after it.
	if r, process := tab.filter('x'); r != 'x' || !process {
		t.Errorf("the rune after an aborted escape was eaten (process %v)", process)
	}
	if r, process := tab.filter(readline.CharEsc); r != readline.CharEsc || !process {
		t.Errorf("the filter ate the ESC of the retried sequence (process %v)", process)
	}
	// The '[' and the 'Z' are the sequence, and neither reaches the vim
	// handler.
	if _, process := tab.filter(readline.CharEscapeEx); process {
		t.Error("the filter passed the '[' of a Shift+Tab on")
	}
	if _, process := tab.filter('Z'); process {
		t.Error("the filter passed the 'Z' of a Shift+Tab on")
	}
	if m.status.level != "off" {
		t.Errorf("the cycle left the level at %q, want off", m.status.level)
	}

	// The sequence completes once: the next rune is a key again.
	if r, process := tab.filter('b'); r != 'b' || !process {
		t.Errorf("the rune after the cycle was eaten (process %v)", process)
	}
}

// TestTheShiftTabBindingReachesTheCycle covers the binding end to end: a
// terminal sends Shift+Tab as ESC, '[', 'Z', the editor reads the three and
// hands them to the filter, and the filter cycles the level. The editor's
// own escape decoding is what must not see them, so the test drives the
// real editor over a pipe.
func TestTheShiftTabBindingReachesTheCycle(t *testing.T) {
	server := &fakeServer{t: t}
	in, out := pipeUp(t, server)
	// The editor writes the statusline from its own goroutine, so the test
	// reads the screen through the same lock it writes behind.
	screen := &syncBuffer{}
	m := &machine{
		out:       screen,
		r:         newRenderer(screen),
		conn:      startConn(out, in),
		sessionID: "s1",
		status:    newStatus(""),
		catalog: map[string]protocol.ModelInfo{
			"gemma4": {Name: "gemma4", ReasoningLevels: []string{"off", "low", "high"}},
		},
	}
	m.status.model = "gemma4"
	tab := &backtab{m: m}

	stdin, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("opening the pipe: %v", err)
	}
	cfg := m.lineConfig(tab)
	cfg.Stdin = stdin
	cfg.Stdout = screen
	cfg.FuncIsTerminal = func() bool { return true }
	cfg.FuncMakeRaw = func() error { return nil }
	cfg.FuncExitRaw = func() error { return nil }
	rl, err := readline.NewEx(cfg)
	if err != nil {
		t.Fatalf("starting the editor: %v", err)
	}
	tab.rl = rl
	// The editor's read loop is released by the end of the pipe, which is
	// why the writer closes before the editor does.
	defer rl.Close()
	defer writer.Close()

	go func() {
		writer.Write([]byte("\x1b[Z\r"))
	}()
	// The line is read in the test's own goroutine, so a sequence the
	// editor never hands over fails the test instead of hanging it.
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := rl.Readline()
		done <- result{line, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("reading the line: %v", got.err)
		}
		if got.line != "" {
			t.Errorf("the line is %q, want the sequence to leave no text", got.line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the editor did not return a line")
	}
	if m.status.level != "off" {
		t.Errorf("the level is %q, want off", m.status.level)
	}
	// The prompt was set again, so the statusline the next line is read
	// behind names the level the cycle reached.
	if !strings.Contains(screen.String(), "🧠 off") {
		t.Errorf("the statusline names no level:\n%s", screen.String())
	}
}

// TestAModelWithoutALevelCycleSaysWhatToType covers the models a level
// cycle cannot climb: one whose reasoning is a token budget, and one
// whose reasoning cannot be moved. The repl says what to type instead of
// proposing a level the server refuses on every press, and sends no
// configure at all.
func TestAModelWithoutALevelCycleSaysWhatToType(t *testing.T) {
	for _, tc := range []struct {
		model string
		mode  string
		want  string
	}{
		{"budgeter", "budget", "/thinking <tokens>"},
		{"nothing", "none", "does not control its reasoning"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			server := &fakeServer{t: t, models: []fakeModel{{
				name: tc.model, modelID: tc.model, window: 8192, mode: tc.mode,
			}}}
			out, server := startReplServer(t, "/model "+tc.model+"\n/thinking\n/quit\n", server)
			if !strings.Contains(out, tc.want) {
				t.Errorf("the transcript lacks %q:\n%s", tc.want, out)
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if len(server.levels) != 0 {
				t.Errorf("the repl sent the levels %v, want none", server.levels)
			}
		})
	}
}

// TestANumberThatNamesAModelSwitchesToIt covers the fallthrough: a bare
// number with no list to read is still a catalog name when the catalog
// holds one by that name, so a model called "2" stays reachable.
func TestANumberThatNamesAModelSwitchesToIt(t *testing.T) {
	server := &fakeServer{t: t, chat: "gemma4", models: []fakeModel{
		{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off"}},
		{name: "2", modelID: "2", window: 8192, levels: []string{"off"}},
	}}
	out, server := startReplServer(t, "/model 2\n/quit\n", server)
	if !strings.Contains(out, "model → 2") {
		t.Errorf("the numeric name did not switch:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "2" {
		t.Errorf("the server got the switches %v, want one to \"2\"", server.switches)
	}
}

// TestAnOutOfRangeNumberThatNamesAModelSwitches covers the number that
// is both past the last listing and a catalog name: the listing cannot
// hold it, so the name wins and the switch happens.
func TestAnOutOfRangeNumberThatNamesAModelSwitches(t *testing.T) {
	server := &fakeServer{t: t, chat: "gemma4", models: []fakeModel{
		{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off"}},
		{name: "qwen", modelID: "qwen", window: 131072, levels: []string{"off"}},
		{name: "3", modelID: "3", window: 8192, levels: []string{"off"}},
	}}
	// "gemma" lists one model, so the number 3 is past that list's end
	// and is read as the catalog name it is.
	out, server := startReplServer(t, "/model gemma\n/model 3\n/quit\n", server)
	if !strings.Contains(out, "model → 3") {
		t.Errorf("the out-of-range number did not switch by name:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	// The session's own model is configured at start; the switch to the
	// numeric name is the last one.
	if got := server.switches; len(got) == 0 || got[len(got)-1] != "3" {
		t.Errorf("the server got the switches %v, want the last to be \"3\"", got)
	}
}
