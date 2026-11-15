package repl

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sdougbrown/poo/wire/protocol"
)

// aTerminal is a renderer that draws on out the way it draws on a
// terminal width columns wide.
//
// newRenderer reaches that state by finding a terminal behind the
// writer, which a test has no pty for; the fields it would set are the
// ones set here, so the streaming path is the same code either way.
func aTerminal(t *testing.T, out io.Writer, width int) *renderer {
	t.Helper()
	return aStreamingRenderer(t, out, out, width)
}

// plain returns a rendered string's words, without the style escapes: a
// test reads what the screen shows, not how it is painted.
func plain(s string) string { return ansi.Strip(s) }

// deltaEvent is one message delta, the way the server sends it.
func deltaEvent(id, kind, text string) protocol.Event {
	return protocol.Event{Type: "message.delta", Payload: map[string]any{
		"run_id": "r1", "message_id": id, "kind": kind, "text": text,
	}}
}

// eventOf is one event with the payload a test gave it.
func eventOf(name string, payload map[string]any) protocol.Event {
	return protocol.Event{Type: name, Payload: payload}
}

// TestTheWritingHintStandsUntilTheFirstDeltaArrives covers the hint: it
// holds a sealed row from the prompt's echo until the message starts,
// once.
func TestTheWritingHintStandsUntilTheFirstDeltaArrives(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.hint()
	r.hint()
	if got := countOf(t, scr, "… writing"); got != 1 {
		t.Fatalf("the hint shows %d time(s), want 1 once:\n%s", got, scr.text())
	}

	r.streamDelta("text", "Hi")
	if rowOf(t, scr, "… writing") >= 0 {
		t.Errorf("the hint stayed on the terminal after the text arrived:\n%s", scr.text())
	}
	if rowOf(t, scr, "Hi") < 0 {
		t.Fatalf("the streamed text never showed:\n%s", scr.text())
	}
	// Nothing is in the transcript until the block settles: the
	// preview is the terminal's, not the conversation's.
	if got := r.transcriptText(); got != "" {
		t.Errorf("the preview entered the transcript: %q", got)
	}
	r.finish()
	if !strings.Contains(plain(r.transcriptText()), "Hi") {
		t.Errorf("the settled text is not in the transcript: %q", r.transcriptText())
	}
}

// TestTheHintComesOffAtTheFlush covers a run that says nothing: the
// hint is what the flush has to take back before the settle line lands
// on that row.
func TestTheHintComesOffAtTheFlush(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.hint()
	if rowOf(t, scr, "… writing") < 0 {
		t.Fatalf("the hint never showed:\n%s", scr.text())
	}
	r.finish()
	if rowOf(t, scr, "… writing") >= 0 {
		t.Errorf("the flush left the hint on the screen:\n%s", scr.text())
	}
	if got := r.transcriptText(); got != "" {
		t.Errorf("the empty run wrote %q to the transcript", got)
	}
}

// TestAPipeStreamsNothing covers the writer that is not a terminal: the
// deltas stay buffered, and the message renders whole when it settles.
func TestAPipeStreamsNothing(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)

	r.hint()
	r.streamDelta("text", "Hello, ")
	r.streamDelta("text", "world.")
	if got := out.String(); got != "" {
		t.Errorf("a pipe was written %q, want nothing", got)
	}
	if got := r.transcriptText(); got != "" {
		t.Errorf("a pipe's transcript reads %q, want nothing", got)
	}

	r.finish()
	if got := out.String(); !strings.Contains(got, "Hello, world.") {
		t.Errorf("the message did not render at the flush: %q", got)
	}
	if got := r.transcriptText(); got != out.String() {
		t.Errorf("the transcript is %q, want %q", got, out.String())
	}
}

// TestAPipeStripsTheEscapesAModelSends covers the pipe's sanitizing: a
// terminal is not the only reader that must never receive a model's
// control sequences.
func TestAPipeStripsTheEscapesAModelSends(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)

	r.streamDelta("text", "safe\x1b[2Jtext")
	r.finish()

	if strings.Contains(out.String(), "\x1b[2J") {
		t.Errorf("a model's erase reached the pipe: %q", out.String())
	}
	if !strings.Contains(out.String(), "safetext") {
		t.Errorf("the words around the sequence did not render: %q", out.String())
	}
}

// TestAToolCallRendersTheStreamedMessageAboveItself covers the
// interleaving: the message a tool call interrupts is sealed above the
// call, and the reply that follows streams as a block of its own.
func TestAToolCallRendersTheStreamedMessageAboveItself(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{out: &out, r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "text", "Let me look. "), "r1", state)
	m.event(eventOf("tool.call", map[string]any{"tool": "bash", "summary": "ls -la"}), "r1", state)
	m.event(deltaEvent("m2", "text", "All done."), "r1", state)
	if done := m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state); !done {
		t.Fatal("the settle did not end the run")
	}

	drawn := plain(out.String())
	dfirst := strings.Index(drawn, "Let me look.")
	dcall := strings.Index(drawn, "⚙ bash")
	dsecond := strings.Index(drawn, "All done.")
	if dfirst < 0 || dcall < 0 || dsecond < 0 {
		t.Fatalf("the terminal is missing part of the run:\n%q", drawn)
	}
	if !(dfirst < dcall && dcall < dsecond) {
		t.Errorf("the terminal read out of order:\n%q", drawn)
	}
	transcript := plain(r.transcriptText())
	first := strings.Index(transcript, "Let me look.")
	call := strings.Index(transcript, "⚙ bash")
	second := strings.Index(transcript, "All done.")
	if first < 0 || call < 0 || second < 0 {
		t.Fatalf("the transcript is missing part of the run:\n%s", transcript)
	}
	if !(first < call && call < second) {
		t.Errorf("the transcript reads out of order:\n%s", transcript)
	}
	if n := strings.Count(transcript, "Let me look."); n != 1 {
		t.Errorf("the first message reads %d time(s) in the transcript, want 1:\n%s", n, transcript)
	}
}

// TestTwoMessagesReadInOrder covers a reply that arrives in two
// messages with nothing rendered between them: each message keeps its
// place, once.
func TestTwoMessagesReadInOrder(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{out: &out, r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "text", "First."), "r1", state)
	m.event(deltaEvent("m2", "text", " Second."), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	transcript := plain(r.transcriptText())
	first := strings.Index(transcript, "First.")
	second := strings.Index(transcript, "Second.")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the two messages do not read in order:\n%s", transcript)
	}
	if n := strings.Count(transcript, "First."); n != 1 {
		t.Errorf("the first message reads %d time(s), want 1:\n%s", n, transcript)
	}
}

// TestAnAskRendersTheDeltasThatArrivedBeforeIt covers the ask that
// races the deltas: the ask's handler drains the events that reached
// the connection first, so the reply so far settles above the question,
// never split by it.
func TestAnAskRendersTheDeltasThatArrivedBeforeIt(t *testing.T) {
	var out bytes.Buffer
	m := &machine{r: newRenderer(&out), status: newStatus("")}
	m.conn = &conn{events: make(chan protocol.Event, 8), asks: make(chan askRequest, 8)}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	m.state = &runState{}
	m.conn.events <- deltaEvent("m1", "text", "Hello, ")
	m.conn.events <- deltaEvent("m1", "text", "world.")
	in := &plainInput{write: func(string) {}}

	if code, done := m.handleAsk(askRequest{ask: protocol.AskParams{SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash"}}, in); done {
		t.Fatalf("the ask ended the session (code %d)", code)
	}
	tr := plain(m.r.transcriptText())
	whole := strings.Index(tr, "Hello, world.")
	asked := strings.Index(tr, "permission")
	if whole < 0 || asked < 0 || whole > asked {
		t.Fatalf("the ask did not render the deltas before it:\n%s", tr)
	}
}

// TestAnAskAfterTheSettleEndsTheRunAndStillOpens covers an ask that
// follows its run's settle: the drained settle ends the run the usual
// way, and the ask still opens behind its prompt.
func TestAnAskAfterTheSettleEndsTheRunAndStillOpens(t *testing.T) {
	var out bytes.Buffer
	m := &machine{r: newRenderer(&out), status: newStatus("")}
	m.conn = &conn{events: make(chan protocol.Event, 8), asks: make(chan askRequest, 8)}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	m.state = &runState{}
	m.conn.events <- deltaEvent("m1", "text", "Hello, ")
	m.conn.events <- eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	in := &plainInput{write: func(string) {}}

	if _, done := m.handleAsk(askRequest{ask: protocol.AskParams{SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash"}}, in); done {
		t.Fatalf("the ask ended the session")
	}
	if m.activeRun() != "" || m.state != nil {
		t.Fatalf("the settled run did not end: run %q, state %v", m.activeRun(), m.state)
	}
	if m.ask == nil {
		t.Fatalf("the ask did not open")
	}
	tr := plain(m.r.transcriptText())
	settled := strings.Index(tr, "end_turn")
	asked := strings.Index(tr, "permission")
	if settled < 0 || asked < 0 || settled > asked {
		t.Fatalf("the ask landed over the settled run:\n%s", tr)
	}
}

// TestATerminalWithoutEditorRendersOnce covers the fallback: a terminal
// the editor could not take streams nothing, and the message renders
// once when it settles.
func TestATerminalWithoutEditorRendersOnce(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	r.editor = false

	r.userPrompt("say hello")
	r.streamDelta("text", "Hello, ")
	r.streamDelta("text", "world.")
	if got := out.String(); got != ansiBold+"❯ say hello"+ansiReset+"\n" {
		t.Fatalf("the terminal was written before the flush:\n%q", out.String())
	}

	r.finish()
	if got := strings.Count(plain(out.String()), "Hello, world."); got != 1 {
		t.Errorf("the render reads %d time(s), want 1:\n%q", got, out.String())
	}
	if got := plain(r.transcriptText()); !strings.Contains(got, "Hello, world.") {
		t.Errorf("the transcript lost the message: %q", got)
	}
}

// TestASessionThatEndsMidMessageRendersTheReply covers the flush on the
// way out: a reply the deltas had buffered is rendered when the session
// ends, so a run cut off mid-message leaves its words on the terminal.
func TestASessionThatEndsMidMessageRendersTheReply(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{r: r, status: newStatus("")}
	m.state = &runState{}
	m.r.streamDelta("text", "Half a sentence")

	m.flushPending()

	if n := strings.Count(plain(r.transcriptText()), "Half a sentence"); n != 1 {
		t.Fatalf("the buffered reply reads %d time(s) in the transcript, want 1:\n%s", n, r.transcriptText())
	}
	if row := plain(out.String()); !strings.Contains(row, "Half a sentence") {
		t.Errorf("the flush drew nothing:\n%q", row)
	}
}

// TestAServerThatDiesMidMessageEndsTheSession covers the loop's exit on
// a lost server: the session ends with the failure code rather than
// waiting on a wire nothing will answer.
func TestAServerThatDiesMidMessageEndsTheSession(t *testing.T) {
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Half a sentence",
		})
		s.hangUp()
	}
	stdin, stdout := pipeUp(t, server)
	code := Run(Options{
		WorkingDir: t.TempDir(),
		In:         strings.NewReader("go on\n"),
		Out:        &bytes.Buffer{},
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	})
	if code != 1 {
		t.Fatalf("the repl exited %d, want 1 after the server went away", code)
	}
}

// TestAnAskWhoseWireDiesEndsTheSession covers the drain loop's closed
// channel: a server that goes away while a permission ask is open ends
// the session with the failure code instead of blocking on a channel
// nothing will write again.
func TestAnAskWhoseWireDiesEndsTheSession(t *testing.T) {
	m := &machine{r: newRenderer(&bytes.Buffer{}), status: newStatus("")}
	m.conn = &conn{events: make(chan protocol.Event), asks: make(chan askRequest)}
	close(m.conn.events)
	in := &plainInput{write: func(string) {}}

	code, done := m.handleAsk(askRequest{ask: protocol.AskParams{Tool: "bash"}}, in)
	if !done || code != 1 {
		t.Fatalf("the closed wire gave (%d, %v), want (1, true)", code, done)
	}
}
