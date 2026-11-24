package repl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/reeflective/readline"
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
	mu        sync.Mutex
	buf       bytes.Buffer
	changedCh chan struct{}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changedCh != nil {
		close(b.changedCh)
		b.changedCh = nil
	}
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// changed returns the buffer's signal: the next write closes it, so a
// wait ends on the write that landed the text instead of a poll. A
// wait takes the channel, then checks, then blocks: any write after
// the check closes the channel the wait holds, so no write is missed.
func (b *syncBuffer) changed() chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changedCh == nil {
		b.changedCh = make(chan struct{})
	}
	return b.changedCh
}

// drawnBuffer is what the drawn-output waits block on: the buffer's
// own signal closes on each write. Both the repl's in-memory
// transcript and the pty terminal's drawn stream speak it.
type drawnBuffer interface {
	String() string
	changed() chan struct{}
}

// waitFor waits for what the repl drew to say want. The wait blocks
// on the buffer's own signal, so the wait ends on the draw that landed
// the text instead of a poll.
func waitFor(t *testing.T, drawn drawnBuffer, want string) {
	t.Helper()
	waitTimes(t, drawn, want, 1)
}

// waitTimes waits for what the repl drew to say want count times.
func waitTimes(t *testing.T, drawn drawnBuffer, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		changed := drawn.changed()
		if got := strings.Count(drawn.String(), want); got >= count {
			return
		}
		select {
		case <-changed:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("the repl drew %q %d times, want %d:\n%s", want,
				strings.Count(drawn.String(), want), count, drawn.String())
		}
	}
}

// promptsSoFar returns the prompts the session has received so far.
func (s *fakeServer) promptsSoFar() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.prompts...)
}

// waitPrompts waits until the server has want prompts from the
// session, and returns them. The wait blocks on the server's own
// signal, so the wait ends on the prompt's arrival instead of a poll.
func (s *fakeServer) waitPrompts(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		changed := s.changed()
		s.mu.Lock()
		prompts := append([]string(nil), s.prompts...)
		s.mu.Unlock()
		if len(prompts) >= want {
			return prompts
		}
		select {
		case <-changed:
		case <-deadline:
			s.mu.Lock()
			defer s.mu.Unlock()
			t.Fatalf("the server got %d prompts, want %d: %v", len(s.prompts), want, s.prompts)
		}
	}
}

// waitMessages waits until the server has want run/message texts from
// the session, and returns them. The wait blocks on the server's own
// signal, so the wait ends on the message's arrival instead of a poll.
//
// The transcript's steer echo is written before the request goes out,
// so a test that waits for the echo has not yet waited for the send.
func (s *fakeServer) waitMessages(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		changed := s.changed()
		s.mu.Lock()
		messages := append([]string(nil), s.messages...)
		s.mu.Unlock()
		if len(messages) >= want {
			return messages
		}
		select {
		case <-changed:
		case <-deadline:
			s.mu.Lock()
			defer s.mu.Unlock()
			t.Fatalf("the server got %d messages, want %d: %v", len(s.messages), want, s.messages)
		}
	}
}

// waitCancels waits until the session's cancels number want, and
// returns them. The wait blocks on the server's own signal, so the
// wait ends on the cancel's arrival instead of a poll.
func (s *fakeServer) waitCancels(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		changed := s.changed()
		s.mu.Lock()
		cancels := append([]string(nil), s.cancels...)
		s.mu.Unlock()
		if len(cancels) >= want {
			return cancels
		}
		select {
		case <-changed:
		case <-deadline:
			s.mu.Lock()
			defer s.mu.Unlock()
			t.Fatalf("the server got %d cancels, want %d: %v", len(s.cancels), want, s.cancels)
		}
	}
}

func TestUsageUpdatesTheStatus(t *testing.T) {
	m := &machine{status: &status{promptLimit: 262144}}
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"run_id": "r1", "input_tokens": 12300}}, "r1", &runState{})
	if line := m.status.line(); !strings.Contains(line, "ctx 12.3k/262.1k") {
		t.Fatalf("the statusline lacks the run's usage: %s", line)
	}
}

// TestTheSettleDoesNotStealTheStatusline covers the run.settled feed:
// the settle's usage is the run's total, the sum of every request, and
// a statusline that read it for the snapshot would show a context
// fuller than the window. The snapshot comes from the usage events;
// the settle leaves it alone.
func TestTheSettleDoesNotStealTheStatusline(t *testing.T) {
	var out bytes.Buffer
	m := &machine{status: &status{promptLimit: 262144}, r: newRenderer(&out)}
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"run_id": "r1", "input_tokens": 12300}}, "r1", &runState{})
	m.event(protocol.Event{
		Type: "run.settled",
		Payload: map[string]any{"run_id": "r1", "reason": "end_turn", "turns": 2,
			"detail": map[string]any{
				"usage": map[string]any{"input_tokens": 20300, "output_tokens": 1100},
			}},
	}, "r1", &runState{})
	if line := m.status.line(); !strings.Contains(line, "ctx 12.3k/262.1k") {
		t.Errorf("the statusline does not hold the last request's fill: %s", line)
	}
}

// TestAnotherRunsUsageLeavesTheStatuslineAlone covers the filter: a
// background job's request rides the session's own connection, and its
// count is not the conversation's fill.
func TestAnotherRunsUsageLeavesTheStatuslineAlone(t *testing.T) {
	var out bytes.Buffer
	m := &machine{status: &status{promptLimit: 262144}, r: newRenderer(&out)}
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"run_id": "r1", "input_tokens": 12300}}, "r1", &runState{})
	m.event(protocol.Event{Type: "usage", Payload: map[string]any{"run_id": "job-7", "input_tokens": 99000}}, "r1", &runState{})
	if line := m.status.line(); !strings.Contains(line, "ctx 12.3k/262.1k") {
		t.Errorf("another run's request took the statusline: %s", line)
	}
}

func TestCtrlCClearsWhitespace(t *testing.T) {
	m := &machine{r: newRenderer(&bytes.Buffer{})}
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
		"Hello,",
		"world.",
		"⚙ bash",
		"file one",
		"file two",
		"end_turn",
		"2 turn(s)",
		// The settle's input side, from the fake's 20300-token run
		// total: feeding it the last request's 12300 instead would
		// print "12.3k in".
		"20.3k in",
		// The settle's output side, from the fake's 1100 output tokens:
		// a regression in decoding them would print "0 out".
		"1.1k out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	// The three deltas read in the order they arrived: the reply's two
	// halves keep their place around the thinking between them.
	hello := strings.Index(out, "Hello,")
	hmm := strings.Index(out, "hmm")
	world := strings.Index(out, "world.")
	if hello < 0 || hmm < 0 || world < 0 || !(hello < hmm && hmm < world) {
		t.Errorf("the deltas read out of arrival order:\n%s", out)
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

// TestALearnedOutputLimitIsShownAsAWarning covers the event the session
// emits when an endpoint rejects its max_tokens: the repl prints the
// session's own message, the way it prints the warnings a session
// reports at creation.
func TestALearnedOutputLimitIsShownAsAWarning(t *testing.T) {
	const message = "the endpoint rejected max_tokens 32768 and asks for 16384 from now on; set models.big.max_output to keep it."
	server := &fakeServer{t: t, script: func(s *fakeServer) {
		s.event("turn.started", map[string]any{"run_id": "r1", "turn": 1})
		s.event("model.output_limit", map[string]any{
			"run_id": "r1", "turn": 1, "request": 1,
			"previous": 32768, "limit": 16384,
			"setting": "models.big.max_output", "message": message,
		})
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Hello.",
		})
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn", "turns": 1})
	}}
	out, _ := startReplServer(t, "say hello\n/quit\n", server)
	if !strings.Contains(out, "⚠ "+message) {
		t.Errorf("the transcript does not warn about the learned limit:\n%s", out)
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

// TestALineTypedDuringARunReachesTheServerAsASteer covers the whole
// ride: the line typed while the run works goes out as run/message, the
// server records it, and the transcript echoes it as a steer — the
// mark that says the line rode the run instead of opening one.
func TestALineTypedDuringARunReachesTheServerAsASteer(t *testing.T) {
	hold := make(chan struct{})
	server := &fakeServer{t: t, script: quietRun(hold)}
	out, typeLine, code := startTypedRepl(t, server)

	typeLine("work on it\n")
	// The tool result is the first thing the run renders whole: it is
	// the anchor that says the run is working.
	waitFor(t, out, "a find")
	typeLine("also check the tests\n")
	waitFor(t, out, "» also check the tests")

	sent := server.waitMessages(t, 1)
	if len(sent) != 1 || sent[0] != "also check the tests" {
		t.Fatalf("the server got %v, want one steer", sent)
	}

	close(hold)
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	if got := <-code; got != 0 {
		t.Fatalf("the repl exited %d, want 0", got)
	}
	if !strings.Contains(out.String(), "» also check the tests") {
		t.Errorf("the steer is not in the transcript:\n%s", out.String())
	}
}

// TestARefusedSteerFailsTheLineNotTheSession covers a run/message the
// server refused — a run that cannot take the message: the refusal is
// a failure line, and the session goes on.
func TestARefusedSteerFailsTheLineNotTheSession(t *testing.T) {
	hold := make(chan struct{})
	server := &fakeServer{t: t, refuseMessage: true, script: quietRun(hold)}
	out, typeLine, code := startTypedRepl(t, server)

	typeLine("work on it\n")
	waitFor(t, out, "a find")
	typeLine("a line too late\n")
	waitFor(t, out, "» a line too late")
	waitFor(t, out, "✗")

	close(hold)
	waitFor(t, out, "✓ end_turn")
	typeLine("/quit\n")
	if got := <-code; got != 0 {
		t.Fatalf("the repl exited %d, want 0", got)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.closed {
		t.Error("the repl left the session open")
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

func TestAnAskPromptSanitizesTheTool(t *testing.T) {
	got := askPrompt(protocol.AskParams{Tool: "bash\x1b]52;c;bm90\x07\r\x00"})
	for _, bad := range []string{"\x1b]", "\r", "\x00"} {
		if strings.Contains(got, bad) {
			t.Errorf("the prompt shows a control %q:\n%q", bad, got)
		}
	}
	if !strings.Contains(got, "bash") {
		t.Errorf("the prompt lost the tool:\n%q", got)
	}
}

func TestAnAskSanitizesTheToolAndSummaryItShows(t *testing.T) {
	out := &bytes.Buffer{}
	var prompt string
	m := &machine{r: newRenderer(out), status: newStatus("")}
	in := &plainInput{write: func(s string) { prompt = s }}

	m.openAsk(askRequest{ask: protocol.AskParams{
		Tool:    "bash\x1b]52;c;bm90\x07\r\x00",
		Summary: "echo first\r\x00\necho second",
	}}, in)

	for name, got := range map[string]string{"body": out.String(), "prompt": prompt} {
		for _, bad := range []string{"\x1b]", "\r", "\x00"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s shows a control %q:\n%q", name, bad, got)
			}
		}
	}
	if !strings.Contains(out.String(), "bash") {
		t.Errorf("the body lost the tool:\n%q", out.String())
	}
	if !strings.Contains(out.String(), "echo first") || !strings.Contains(out.String(), "echo second") {
		t.Errorf("the body lost the summary lines:\n%q", out.String())
	}
	if !strings.Contains(prompt, "bash") {
		t.Errorf("the prompt lost the tool:\n%q", prompt)
	}
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

// lineWriter records the lines the client writes, one per call: the
// test reads them back and answers over the conn's demux, the way the
// server would.
type lineWriter struct {
	lines chan string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.lines <- string(p)
	return len(p), nil
}

// TestALineTypedDuringARunSteersTheRun covers the Enter pressed while
// a turn works: the line goes to the run as a steer — run/message, mode
// steer — and reaches the model at the run's next turn end. A settle
// that dropped it names the text the person wrote, not the input's id.
func TestALineTypedDuringARunSteersTheRun(t *testing.T) {
	var out bytes.Buffer
	lw := &lineWriter{lines: make(chan string, 8)}
	m := &machine{r: newRenderer(&out), status: newStatus(""), sessionID: "s1"}
	m.conn = &conn{
		w:       lw,
		pending: map[int]chan rpcResult{},
		events:  make(chan protocol.Event, 8),
		asks:    make(chan askRequest, 8),
	}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	in := &plainInput{write: func(string) {}}

	// The server's answer to the steer: an input id, the way the wire
	// names an accepted input.
	go func() {
		raw := <-lw.lines
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			t.Errorf("the steer was not one request line: %v", err)
			return
		}
		if req.Method != protocol.MethodRunMessage {
			t.Errorf("the line went out as %q, want run/message", req.Method)
			return
		}
		var params protocol.MessageParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Errorf("the steer's params did not decode: %v", err)
			return
		}
		if params.Text != "a second thought" || params.Mode != "steer" || params.SessionID != "s1" {
			t.Errorf("the steer's params were %+v", params)
		}
		m.conn.line([]byte(fmt.Sprintf(`{"id":%d,"result":{"input_id":"i1"}}`, req.ID)))
	}()

	if code, done := m.line(lineResult{line: "a second thought"}, in); done {
		t.Fatalf("the line ended the session (code %d)", code)
	}
	drawn := out.String()
	if !strings.Contains(drawn, "» a second thought") {
		t.Errorf("the typed line is not on screen as a steer:\n%s", drawn)
	}

	// A settle that reports the input undelivered says what it was.
	m.event(protocol.Event{
		Type: "run.settled",
		Payload: map[string]any{"run_id": "r1", "reason": "cancelled", "turns": 1,
			"detail": map[string]any{
				"undelivered_input": []any{map[string]any{"input_id": "i1", "mode": "steer"}},
			}},
	}, "r1", &runState{})
	if !strings.Contains(out.String(), `"a second thought" was delivered`) {
		t.Errorf("the undelivered steer did not read back as its text:\n%s", out.String())
	}
}

// TestASlashCommandTypedDuringARunWaitsForTheNextPrompt covers a
// command typed while the run works: it does not steer, and the
// transcript says the line was not sent and which key stops the turn.
func TestASlashCommandTypedDuringARunWaitsForTheNextPrompt(t *testing.T) {
	sent := make(chan string, 1)
	m, out, in := steering(t, sent)

	if code, done := m.line(lineResult{line: "/model qwen"}, in); done {
		t.Fatalf("the line ended the session (code %d)", code)
	}
	if drawn := out.String(); !strings.Contains(drawn, "was not sent") {
		t.Errorf("the command went out instead of waiting:\n%s", drawn)
	}
	select {
	case raw := <-sent:
		t.Errorf("the command went out as a request: %s", raw)
	default:
	}
}

// TestALineTheDispatcherWouldRunAsAPromptSteers covers the shared
// reading of a command: a slash line the dispatcher would send as a
// prompt steers the working run, rather than being refused here and
// prompted there.
func TestALineTheDispatcherWouldRunAsAPromptSteers(t *testing.T) {
	sent := make(chan string, 1)
	m, out, in := steering(t, sent)

	if code, done := m.line(lineResult{line: "/remind me tomorrow"}, in); done {
		t.Fatalf("the line ended the session (code %d)", code)
	}
	if drawn := out.String(); strings.Contains(drawn, "was not sent") {
		t.Errorf("an unknown slash line was refused instead of steered:\n%s", drawn)
	}
	select {
	case raw := <-sent:
		if !strings.Contains(raw, `"/remind me tomorrow"`) {
			t.Errorf("the steer carried something else: %s", raw)
		}
	default:
		t.Error("the line did not reach the run")
	}
}

// TestAnUndeliveredInputTheReplNeverSentNamesItsId covers the settle's
// fallback: an input the client did not accept here has no text to read
// back, so the notice names the id the server reported.
func TestAnUndeliveredInputTheReplNeverSentNamesItsId(t *testing.T) {
	out := &bytes.Buffer{}
	m := &machine{r: newRenderer(out), status: newStatus(""), sessionID: "s1"}
	m.event(protocol.Event{
		Type: "run.settled",
		Payload: map[string]any{"run_id": "r1", "reason": "cancelled", "turns": 1,
			"detail": map[string]any{
				"undelivered_input": []any{map[string]any{"input_id": "elsewhere-7", "mode": "steer"}},
			}},
	}, "r1", &runState{})
	if !strings.Contains(out.String(), "elsewhere-7") {
		t.Errorf("the notice does not name the input:\n%s", out.String())
	}
}

// steering returns a machine with a run active, its transcript, and the
// input the tests submit. Every line the client writes is answered the
// way the server would, and the raw line is handed to sent: a test that
// expects no request gets nothing, and one that expects a request reads
// it there.
func steering(t *testing.T, sent chan string) (*machine, *bytes.Buffer, input) {
	t.Helper()
	out := &bytes.Buffer{}
	lw := &lineWriter{lines: make(chan string, 8)}
	m := &machine{r: newRenderer(out), status: newStatus(""), sessionID: "s1"}
	m.conn = &conn{
		w:       lw,
		pending: map[int]chan rpcResult{},
		events:  make(chan protocol.Event, 8),
		asks:    make(chan askRequest, 8),
	}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	// The line is recorded before the answer goes back: the test reads
	// sent once m.line returns, and the call returns as soon as the
	// answer lands.
	go func() {
		for {
			select {
			case raw := <-lw.lines:
				var req struct {
					ID int `json:"id"`
				}
				if err := json.Unmarshal([]byte(raw), &req); err != nil {
					t.Errorf("the line was not one request: %v", err)
					return
				}
				sent <- raw
				m.conn.line([]byte(fmt.Sprintf(`{"id":%d,"result":{"input_id":"i1"}}`, req.ID)))
			case <-stop:
				return
			}
		}
	}()
	return m, out, &plainInput{write: func(string) {}}
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
	// A footerless host rides the statusline in the prompt: the switch
	// names qwen beside the window the run filled, and a switch that
	// kept the old model's name or window would read them here.
	found := false
	for _, line := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Contains(line, "ctx 12.3k/131.1k") && strings.Contains(line, "qwen") {
			found = true
		}
	}
	if !found {
		t.Errorf("no prompt names qwen beside its window:\n%s", out)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.switches) != 1 || server.switches[0] != "qwen" {
		t.Errorf("the server got the switches %v, want one to qwen", server.switches)
	}
}

// TestTheStatuslineBoundsTheFillByTheInputCap covers an endpoint that takes a
// smaller prompt than its window: the request gate and the compaction measure
// against the prompt the endpoint accepts, so the line shows that bound, not
// the window's whole 400k.
func TestTheStatuslineBoundsTheFillByTheInputCap(t *testing.T) {
	server := &fakeServer{t: t, models: []fakeModel{
		{name: "gemma4", modelID: "gemma4", window: 400000, input: 272000, levels: []string{"off", "low"}},
	}}
	out, _ := startReplServer(t, "say hi\n/quit\n", server)

	if line := ansi.Strip(out); !strings.Contains(line, "ctx 12.3k/272.0k") {
		t.Errorf("the statusline does not bound the fill by the 272000-token input cap:\n%s", out)
	}
}

// TestAModelSwitchTheCatalogCannotDescribe covers a switch the server
// accepts for a model the client's catalog does not hold: the session
// moves, and the transcript says the name the wire answered with. The
// window it kept is the pty variant's to pin:
// TestAModelSwitchTheCatalogCannotDescribeKeepsTheWindow.
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

}

// TestAModelSwitchToAnUnknownNameIsRefused covers a switch the catalog
// cannot resolve: the refusal reads in the transcript, and the session
// keeps the model it had.
func TestAModelSwitchToAnUnknownNameIsRefused(t *testing.T) {
	out, server := startRepl(t, "/model nope\n/quit\n")

	if !strings.Contains(out, "✗") {
		t.Errorf("the transcript shows no refusal:\n%s", out)
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

// TestTheStatuslineNamesTheLevelTheSessionOpenedAt covers the level's
// own rendering: 🧠 <level> in the status the footer's bottom row
// carries. The session-open read-back from the wire is not exercised
// here.
func TestTheStatuslineNamesTheLevelTheSessionOpenedAt(t *testing.T) {
	s := &status{level: "high"}
	if line := s.line(); !strings.Contains(line, "🧠 high") {
		t.Errorf("the statusline names no level:\n%s", line)
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
// is past a listing the session is holding and is also a catalog name:
// the list cannot hold it, so the name wins and the switch happens.
func TestAnOutOfRangeNumberThatNamesAModelSwitches(t *testing.T) {
	server := &fakeServer{t: t, chat: "gemma4", models: []fakeModel{
		{name: "gemma4", modelID: "gemma4", window: 262144, levels: []string{"off"}},
		{name: "gemma-mini", modelID: "gemma-mini", window: 131072, levels: []string{"off"}},
		{name: "3", modelID: "3", window: 8192, levels: []string{"off"}},
	}}
	// "gemma" matches two models, so it lists them; the number 3 is past
	// that two-model list and is read as the catalog name it is.
	out, server := startReplServer(t, "/model gemma\n/model 3\n/quit\n", server)
	for _, want := range []string{". gemma-mini", ". gemma4", "picks one of these", "model → 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the transcript lacks %q:\n%s", want, out)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if got := server.switches; len(got) == 0 || got[len(got)-1] != "3" {
		t.Errorf("the server got the switches %v, want the last to be \"3\"", got)
	}
}
