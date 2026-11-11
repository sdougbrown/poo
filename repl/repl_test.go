package repl

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chzyer/readline"
	"github.com/sdougbrown/poo/wire/protocol"
)

// startRepl starts the repl against a fresh fake server. The script
// reads the input the person types and returns what the repl printed.
func startRepl(t *testing.T, input string) (string, *fakeServer) {
	return startReplServer(t, input, &fakeServer{t: t})
}

// startReplServer starts the repl against a server the test prepared.
func startReplServer(t *testing.T, input string, server *fakeServer) (string, *fakeServer) {
	t.Helper()
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
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"input_tokens": 12300}}, &runState{}, &streamView{})
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

	for _, want := range []string{"/quit", "/less", "/model", "Esc", "/help"} {
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

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
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
	// The refusal is the server's to word: the catalog has no such
	// model. A failure line that renders anything else — or nothing —
	// is the failure this test watches for.
	if !strings.Contains(out, "not in the catalog") {
		t.Errorf("the refusal does not name the catalog miss:\n%s", out)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 0 {
		t.Errorf("the server took the switch %v", server.switches)
	}
}
