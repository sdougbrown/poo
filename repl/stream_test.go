package repl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
	"github.com/sdougbrown/poo/wire/protocol"
)

// aTerminal is a renderer that draws on out the way it draws on a
// terminal width columns wide.
//
// newRenderer reaches that state by finding a terminal behind the
// writer, which a test has no pty for; the fields it would set are the
// ones set here, so the streaming path is the same code either way. The
// markdown renderer is the plain style, so a test reads the words of a
// render without the palette the auto style picks.
func aTerminal(t *testing.T, out io.Writer, width int) *renderer {
	t.Helper()
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(width-2),
	)
	if err != nil {
		t.Fatalf("building the markdown renderer: %v", err)
	}
	return &renderer{out: out, tr: tr, wrap: width - 2, width: width}
}

// liveMachine is a machine whose renderer draws on out as if out were a
// terminal width columns wide, with the fake server on the other end of
// its wire. The server's script must be set before this call.
func liveMachine(t *testing.T, out io.Writer, server *fakeServer, width int) (*machine, *plainInput) {
	t.Helper()
	stdin, stdout := pipeUp(t, server)
	m := &machine{
		out:       out,
		r:         aTerminal(t, out, width),
		conn:      startConn(stdout, stdin),
		sessionID: "s1",
		status:    newStatus(""),
	}
	return m, &plainInput{scanner: bufio.NewScanner(strings.NewReader("y\n")), write: m.r.write}
}

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

// TestAReplyStreamsRawAndTheRenderTakesItsPlace covers the streaming
// the repl is for: the reply prints as it arrives, raw, and the markdown
// it renders to takes its place when the message completes.
func TestAReplyStreamsRawAndTheRenderTakesItsPlace(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	r.userPrompt("say hello")

	r.streamText("Hello, ")
	r.streamText("world.")
	// The terminal has the raw deltas, and the transcript has what the
	// terminal shows.
	if got, want := out.String(), ansiBold+"❯ say hello"+ansiReset+"\nHello, world."; got != want {
		t.Fatalf("the streamed text is %q, want %q", got, want)
	}
	if got := r.transcriptText(); got != out.String() {
		t.Fatalf("the transcript is %q, want %q", got, out.String())
	}

	r.erase()
	// The cursor went back to the block's first line and everything
	// below it came off. The prompt above the block stayed.
	want := ansiBold + "❯ say hello" + ansiReset + "\nHello, world.\r" + ansi.EraseDisplay(0)
	if got := out.String(); got != want {
		t.Errorf("the erase left %q, want %q", got, want)
	}
	// The raw text is out of the transcript too, so the render replaces
	// it rather than following it.
	if got := r.transcriptText(); got != ansiBold+"❯ say hello"+ansiReset+"\n" {
		t.Errorf("the transcript reads %q after the erase, want the prompt alone", got)
	}

	r.message("Hello, world.")
	if got, want := r.transcriptText(), ansiBold+"❯ say hello"+ansiReset+"\n"+r.markdown("Hello, world."); got != want {
		t.Errorf("the transcript reads %q, want the rendered message in the raw text's place", got)
	}
}

// TestTheEraseCountsTheLinesTheTerminalWrapped covers the erase's
// arithmetic. A raw block the terminal wrapped over several lines comes
// off whole, and the line above it stays.
func TestTheEraseCountsTheLinesTheTerminalWrapped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		width int
		lines int
	}{
		{"one line", "hello", 10, 1},
		{"a wrap", "hello there", 10, 2},
		{"a line that fills the width", "0123456789", 10, 1},
		{"a line that fills the width and goes on", "0123456789x", 10, 2},
		{"two lines", "hello\nthere", 10, 2},
		{"a blank line", "hello\n\nthere", 10, 3},
		{"a trailing newline", "hello\n", 10, 2},
		{"nothing at all", "", 10, 1},
		{"wide runes that fill the width", "日本語", 6, 1},
		{"wide runes that wrap", "日本語", 5, 2},
	} {
		if got := wrappedLines(tc.text, tc.width); got != tc.lines {
			t.Errorf("%s: wrappedLines(%q, %d) = %d, want %d", tc.name, tc.text, tc.width, got, tc.lines)
		}
	}

	// A block of three lines - one wrapped and one after a newline -
	// takes two cursor moves to leave.
	var out bytes.Buffer
	r := aTerminal(t, &out, 10)
	r.userPrompt("wrap it")
	r.streamText("one two three four")
	r.streamText("\nlast")
	r.erase()
	want := ansiBold + "❯ wrap it" + ansiReset + "\none two three four\nlast\r" + ansi.CursorUp(2) + ansi.EraseDisplay(0)
	if got := out.String(); got != want {
		t.Errorf("the erase left %q, want %q", got, want)
	}
}

// TestTheWritingHintStandsUntilTheTextArrives covers the hint: it holds
// the line until the message's first text takes it over, once.
func TestTheWritingHintStandsUntilTheTextArrives(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	hint := ansiDim + "… writing" + ansiReset

	r.hint()
	r.hint()
	if got := out.String(); got != hint {
		t.Errorf("the hint reads %q, want %q once", got, hint)
	}
	// The hint is the terminal's, not part of the conversation.
	if got := r.transcriptText(); got != "" {
		t.Errorf("the hint is in the transcript: %q", got)
	}

	r.streamText("Hi")
	if got, want := out.String(), hint+"\r"+ansi.EraseLine(0)+"Hi"; got != want {
		t.Errorf("the first text left %q, want %q", got, want)
	}
	// The text has the line now: no hint goes back on top of it.
	r.hint()
	if got, want := out.String(), hint+"\r"+ansi.EraseLine(0)+"Hi"; got != want {
		t.Errorf("the hint came back over the text: %q", got)
	}
}

// TestTheHintComesOffAtTheFlush covers a message that never streams: its
// thinking arrived and its text did not, so the hint is what the flush
// has to take back before the render lands on that line.
func TestTheHintComesOffAtTheFlush(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{out: &out, r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "thinking", "weighing it"), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	// The hint was taken back where it stood, and the thinking block
	// renders from that spot.
	want := ansiDim + "… writing" + ansiReset + "\r" + ansi.EraseLine(0)
	if got := out.String(); !strings.HasPrefix(got, want) {
		t.Fatalf("the flush left %q, want it to start with %q", got, want)
	}
	if got := r.transcriptText(); !strings.HasPrefix(got, ansiDim+"· thinking ·\n") {
		t.Errorf("the thinking block does not open the transcript: %q", got)
	}
}

// TestAPipeStreamsNothing covers the writer that is not a terminal: the
// deltas stay buffered, and the message renders whole when it completes,
// which is what it always did.
func TestAPipeStreamsNothing(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(&out)

	r.hint()
	r.streamText("Hello, ")
	r.streamText("world.")
	r.erase()
	if got := out.String(); got != "" {
		t.Errorf("a pipe was written %q, want nothing", got)
	}
	if got := r.transcriptText(); got != "" {
		t.Errorf("a pipe's transcript reads %q, want nothing", got)
	}

	r.message("Hello, world.")
	if got := out.String(); !strings.Contains(got, "Hello, world.") {
		t.Errorf("the message did not render at the flush: %q", got)
	}
	if got := r.transcriptText(); got != out.String() {
		t.Errorf("the transcript is %q, want %q", got, out.String())
	}
}

// TestAToolCallRendersTheStreamedMessageAboveItself covers the
// interleaving: the message a tool call interrupts renders as markdown
// above the call, and the reply that follows streams as a block of its
// own.
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

	// One erase per flush: the tool call's, and the settle's.
	if n := strings.Count(out.String(), ansi.EraseDisplay(0)); n != 2 {
		t.Errorf("the stream was erased %d time(s), want 2:\n%q", n, out.String())
	}
	transcript := r.transcriptText()
	first := strings.Index(transcript, "Let me look.")
	call := strings.Index(transcript, "⚙ bash")
	second := strings.Index(transcript, "All done.")
	if first < 0 || call < 0 || second < 0 {
		t.Fatalf("the transcript is missing part of the run:\n%s", transcript)
	}
	if !(first < call && call < second) {
		t.Errorf("the transcript reads out of order:\n%s", transcript)
	}
	// The first message is rendered where it streamed, not kept beside
	// its own raw text.
	if n := strings.Count(transcript, "Let me look."); n != 1 {
		t.Errorf("the first message reads %d time(s) in the transcript, want 1:\n%s", n, transcript)
	}
}

// TestTwoMessagesShareOneStreamedBlock covers a reply that streams in
// two messages with nothing rendered between them: the block they both
// wrote comes off once, and both messages render where its raw text was.
func TestTwoMessagesShareOneStreamedBlock(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{out: &out, r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "text", "First."), "r1", state)
	m.event(deltaEvent("m2", "text", " Second."), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	// One block was on the terminal, so one erase takes it off.
	if n := strings.Count(out.String(), ansi.EraseDisplay(0)); n != 1 {
		t.Errorf("the stream was erased %d time(s), want 1:\n%q", n, out.String())
	}
	transcript := r.transcriptText()
	first := strings.Index(transcript, "First.")
	second := strings.Index(transcript, "Second.")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the two messages do not read in order:\n%s", transcript)
	}
	if n := strings.Count(transcript, "First."); n != 1 {
		t.Errorf("the first message reads %d time(s), want 1:\n%s", n, transcript)
	}
}

// TestThinkingBuffersWhileTheTextStreams covers the split between the
// two kinds: thinking reaches the terminal dimmed at the flush, and only
// the reply streams as it arrives.
func TestThinkingBuffersWhileTheTextStreams(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{out: &out, r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "thinking", "weighing it"), "r1", state)
	if strings.Contains(out.String(), "weighing it") {
		t.Errorf("a thinking delta streamed:\n%q", out.String())
	}
	m.event(deltaEvent("m1", "text", "The answer."), "r1", state)
	if !strings.Contains(out.String(), "The answer.") {
		t.Errorf("the reply did not stream:\n%q", out.String())
	}
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	transcript := r.transcriptText()
	think := strings.Index(transcript, "weighing it")
	text := strings.Index(transcript, "The answer.")
	if think < 0 || text < 0 {
		t.Fatalf("the transcript is missing part of the message:\n%s", transcript)
	}
	if think > text {
		t.Errorf("the thinking block landed after the message:\n%s", transcript)
	}
	if !strings.Contains(transcript, ansiDim+"· thinking ·\n") {
		t.Errorf("the thinking block does not read dimmed:\n%s", transcript)
	}
}

// TestAMidMessageAskRendersTheReplyItInterrupts covers a question that
// arrives while the reply is still streaming: the reply so far renders
// above the question, and the deltas the answer releases stream in a
// block of their own.
func TestAMidMessageAskRendersTheReplyItInterrupts(t *testing.T) {
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Let me look. ",
		})
		askID := s.request(protocol.MethodPermissionAsk, protocol.AskParams{
			SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash", Summary: "ls -la",
		})
		s.waitDecision(askID)
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "All done.",
		})
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	var out bytes.Buffer
	m, in := liveMachine(t, &out, server, 40)
	if err := m.runPrompt("look", in); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	// The block comes off before the question goes on, so the question
	// is not written inside a message and no half-block is left behind.
	erase := strings.Index(out.String(), ansi.EraseDisplay(0))
	ask := strings.Index(out.String(), "permission")
	if erase < 0 || ask < 0 || erase > ask {
		t.Fatalf("the question landed at %d and the erase at %d:\n%q", ask, erase, out.String())
	}

	transcript := m.r.transcriptText()
	for _, want := range []string{"Let me look.", "→ allow", "All done."} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("the transcript lacks %q:\n%s", want, transcript)
		}
	}
	rest := strings.Index(transcript, "All done.")
	if strings.Index(transcript, "→ allow") > rest {
		t.Errorf("the answer the question released landed above it:\n%s", transcript)
	}
}

// TestARunTheServerDroppedStillErasesTheStream covers the drain loop's
// failure exit: a server that dies mid-message still leaves the streamed
// text erased, with the rendered message in its place.
func TestARunTheServerDroppedStillErasesTheStream(t *testing.T) {
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Half a sent",
		})
		s.hangUp()
	}
	var out bytes.Buffer
	m, in := liveMachine(t, &out, server, 40)
	if err := m.runPrompt("go on", in); !errors.Is(err, errServerClosed) {
		t.Fatalf("the run reported %v, want the closed connection", err)
	}

	if !strings.Contains(out.String(), ansi.EraseDisplay(0)) {
		t.Errorf("the streamed text stayed on the terminal:\n%q", out.String())
	}
	transcript := m.r.transcriptText()
	if n := strings.Count(transcript, "Half a sent"); n != 1 {
		t.Errorf("the interrupted reply reads %d time(s) in the transcript, want 1:\n%s", n, transcript)
	}
}
