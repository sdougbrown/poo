package repl

import (
	"bytes"
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
	return &renderer{out: out, sink: out, tr: tr, wrap: width - 2, width: width, editor: true}
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
	// Each delta paints a sealed row of its own: the first lands where
	// the prompt stood, the second climbs back up to it and rewrites
	// it, and the prompt's redraw takes a fresh line below either way.
	want := "\r" + ansiBold + "❯ say hello" + ansiReset + "\n" +
		"\r" + ansi.EraseDisplay(0) + "Hello, " + "\r\n" +
		"\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0) + "Hello, world." + "\r\n"
	if got := out.String(); got != want {
		t.Fatalf("the streamed text is %q, want %q", got, want)
	}
	// The transcript holds the words once, without the cursor work.
	if got, want := r.transcriptText(), ansiBold+"❯ say hello"+ansiReset+"\nHello, world."; got != want {
		t.Fatalf("the transcript is %q, want %q", got, want)
	}

	r.replace(r.markdown("Hello, world."))
	// The render takes the block's place: the cursor climbs over the
	// row the block filled, everything below comes off, and the raw
	// text leaves the transcript with it.
	want = "\r" + ansiBold + "❯ say hello" + ansiReset + "\n" +
		"\r" + ansi.EraseDisplay(0) + "Hello, " + "\r\n" +
		"\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0) + "Hello, world." + "\r\n" +
		"\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0) +
		strings.ReplaceAll(strings.TrimLeft(r.markdown("Hello, world."), "\n"), "\n", "\r\n")
	if got := out.String(); got != want {
		t.Errorf("the flush left %q, want %q", got, want)
	}
	if got, want := r.transcriptText(), ansiBold+"❯ say hello"+ansiReset+"\n"+r.markdown("Hello, world."); got != want {
		t.Errorf("the transcript reads %q, want the rendered message in the raw text's place", got)
	}
}

// TestTheStreamHardWrapsAtTheWidth covers the row arithmetic the
// streaming paints by: rows of at most width columns, each newline
// ending a row, and a wrapped block repainting only its partial row.
func TestTheStreamHardWrapsAtTheWidth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		width int
		rows  []string
	}{
		{"one line", "hello", 10, []string{"hello"}},
		{"a wrap", "hello there", 10, []string{"hello ther", "e"}},
		{"a line that fills the width", "0123456789", 10, []string{"0123456789"}},
		{"a line that fills the width and goes on", "0123456789x", 10, []string{"0123456789", "x"}},
		{"two lines", "hello\nthere", 10, []string{"hello", "there"}},
		{"a blank line", "hello\n\nthere", 10, []string{"hello", "", "there"}},
		{"a trailing newline", "hello\n", 10, []string{"hello", ""}},
		{"nothing at all", "", 10, []string{""}},
		{"wide runes that fill the width", "日本語", 6, []string{"日本語"}},
		{"wide runes that wrap", "日本語", 5, []string{"日本", "語"}},
	} {
		if got := hardRows(tc.text, tc.width); strings.Join(got, "|") != strings.Join(tc.rows, "|") {
			t.Errorf("%s: hardRows(%q, %d) = %q, want %q", tc.name, tc.text, tc.width, got, tc.rows)
		}
	}

	// A block that wraps: the first delta commits its full first row
	// and paints the rest as the partial row; the second repaints the
	// partial row only, and the flush climbs over the rows the block
	// and the prompt filled.
	var out bytes.Buffer
	r := aTerminal(t, &out, 10)
	r.userPrompt("wrap it")
	r.streamText("one two three four")
	r.streamText("\nlast")
	want := "\r" + ansiBold + "❯ wrap it" + ansiReset + "\n" +
		"\r" + ansi.EraseDisplay(0) + "one two th" + "\r\n" + "ree four" + "\r\n" +
		"\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0) + "ree four" + "\r\n" + "last" + "\r\n"
	if got := out.String(); got != want {
		t.Fatalf("the wrapped stream read %q, want %q", got, want)
	}

	r.replace("")
	want += "\r" + ansi.CursorUp(3) + ansi.EraseDisplay(0)
	if got := out.String(); got != want {
		t.Errorf("the flush left %q, want %q", got, want)
	}
}

// TestTheWritingHintStandsUntilTheTextArrives covers the hint: it
// holds a sealed row until the message's first text takes it over, once.
func TestTheWritingHintStandsUntilTheTextArrives(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	hint := ansiDim + "… writing" + ansiReset

	r.hint()
	r.hint()
	if got := out.String(); got != "\r"+hint+"\r\n" {
		t.Errorf("the hint reads %q, want %q once", got, "\r"+hint+"\r\n")
	}
	// The hint is the terminal's, not part of the conversation.
	if got := r.transcriptText(); got != "" {
		t.Errorf("the hint is in the transcript: %q", got)
	}

	r.streamText("Hi")
	want := "\r" + hint + "\r\n" +
		"\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0) +
		"\r" + ansi.EraseDisplay(0) + "Hi" + "\r\n"
	if got, want := out.String(), want; got != want {
		t.Errorf("the first text left %q, want %q", got, want)
	}
	// The text has the row now: no hint goes back on top of it.
	r.hint()
	if got, want := out.String(), want; got != want {
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
	want := "\r" + ansiDim + "… writing" + ansiReset + "\r\n" + "\r" + ansi.CursorUp(1) + ansi.EraseDisplay(0)
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
	r.replace("")
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

	// The bytes read in the order the person saw them: the reply so
	// far, the call, then the reply the decision released.
	drawn := out.String()
	dfirst := strings.Index(drawn, "Let me look.")
	dcall := strings.Index(drawn, "⚙ bash")
	dsecond := strings.Index(drawn, "All done.")
	if dfirst < 0 || dcall < 0 || dsecond < 0 {
		t.Fatalf("the terminal is missing part of the run:\n%q", drawn)
	}
	if !(dfirst < dcall && dcall < dsecond) {
		t.Errorf("the terminal read out of order:\n%q", drawn)
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

// TestAnAskRendersTheDeltasThatArrivedBeforeIt covers the ask that
// races the deltas: the reader hands events and asks to the main loop
// in whatever order the select finds ready, so the ask's own handler
// drains the events that reached the connection first. The reply so
// far renders whole, above the question — never split by it.
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
	tr := m.r.transcriptText()
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
	tr := m.r.transcriptText()
	settled := strings.Index(tr, "end_turn")
	asked := strings.Index(tr, "permission")
	if settled < 0 || asked < 0 || settled > asked {
		t.Fatalf("the ask landed over the settled run:\n%s", tr)
	}
}

// TestATerminalWithoutEditorRendersOnce covers the fallback: a terminal
// the editor could not take streams nothing, and the message renders
// once at the flush — the raw text is never on the terminal to erase.
func TestATerminalWithoutEditorRendersOnce(t *testing.T) {
	var out bytes.Buffer
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("notty"),
		glamour.WithWordWrap(38),
	)
	if err != nil {
		t.Fatalf("building the markdown renderer: %v", err)
	}
	r := &renderer{out: &out, sink: &out, tr: tr, wrap: 38, width: 40}

	r.userPrompt("say hello")
	r.streamText("Hello, ")
	r.streamText("world.")
	if got := out.String(); got != ansiBold+"❯ say hello"+ansiReset+"\n" {
		t.Fatalf("the terminal was written before the flush:\n%q", out.String())
	}

	r.replace(r.markdown("Hello, world."))
	want := ansiBold + "❯ say hello" + ansiReset + "\n" + r.markdown("Hello, world.")
	if got := out.String(); got != want {
		t.Errorf("the render is %q, want %q", got, want)
	}
	if got := r.transcriptText(); got != want {
		t.Errorf("the transcript reads %q, want %q", got, want)
	}
}
