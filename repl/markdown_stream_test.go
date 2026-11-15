package repl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/wire/protocol"
)

// TestTheLiveStreamShowsTheMarkdownItWillSettleTo covers the jump the
// stream is for: the words on the screen while the message arrives are
// the words the render settles to, emphasis markers and all gone.
func TestTheLiveStreamShowsTheMarkdownItWillSettleTo(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	r.streamDelta("text", "I can check **both tools**.")

	drawn := scr.text()
	if strings.Contains(drawn, "**both tools**") {
		t.Fatalf("the live stream shows the markdown source:\n%s", drawn)
	}
	if !strings.Contains(drawn, "I can check both tools.") {
		t.Fatalf("the live stream lost the rendered words:\n%s", drawn)
	}
}

// TestTheLiveStreamRendersASplitFence covers the structural block: the
// backticks of a fence are never shown, and the code inside reads the
// way the rendered block reads, even before the fence closes.
func TestTheLiveStreamRendersASplitFence(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	r.streamDelta("text", "```go\nfunc main() {\n")
	r.streamDelta("text", "}\n```\n")

	drawn := scr.text()
	if strings.Contains(drawn, "```") {
		t.Fatalf("the live stream shows the fence markers:\n%s", drawn)
	}
	if rowOf(t, scr, "func main() {") < 0 {
		t.Fatalf("the live stream lost the code:\n%s", drawn)
	}
}

// TestThinkingStreamsAsDimMarkdown covers thinking while it arrives: it
// reads as dimmed markdown under its own heading, not as a hint that
// waits for the reply.
func TestThinkingStreamsAsDimMarkdown(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "thinking", "I will inspect **the source**."), "r1", state)

	raw := out.String()
	drawn := plain(raw)
	if !strings.Contains(drawn, "· thinking ·") {
		t.Fatalf("the thinking block has no heading:\n%q", drawn)
	}
	if strings.Contains(drawn, "**the source**") {
		t.Errorf("the thinking block shows the markdown source:\n%q", drawn)
	}
	if !strings.Contains(drawn, "I will inspect the source.") {
		t.Errorf("the thinking block lost the rendered words:\n%q", drawn)
	}
	if !strings.Contains(raw, ansiDim) {
		t.Errorf("the thinking block does not read dimmed:\n%q", raw)
	}
	// The settled transcript keeps the same dim markdown, once.
	r.finish()
	settled := plain(r.transcriptText())
	if n := strings.Count(settled, "I will inspect the source."); n != 1 {
		t.Errorf("the settled thinking reads %d time(s), want 1:\n%q", n, settled)
	}
}

// TestThinkingAndReplyKeepTheirArrivalOrder covers the two kinds of
// delta one message can carry: each piece reads where it arrived, so a
// thought that follows the reply does not jump back above it.
func TestThinkingAndReplyKeepTheirArrivalOrder(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "thinking", "first thought"), "r1", state)
	m.event(deltaEvent("m1", "text", "the answer"), "r1", state)
	m.event(deltaEvent("m1", "thinking", "second thought"), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	drawn := plain(r.transcriptText())
	first := strings.Index(drawn, "first thought")
	answer := strings.Index(drawn, "the answer")
	second := strings.Index(drawn, "second thought")
	if first < 0 || answer < 0 || second < 0 {
		t.Fatalf("the transcript is missing part of the message:\n%s", drawn)
	}
	if !(first < answer && answer < second) {
		t.Errorf("the transcript reads out of arrival order:\n%s", drawn)
	}
	if n := strings.Count(drawn, "· thinking ·"); n != 2 {
		t.Errorf("the transcript has %d thinking headings, want one per thought:\n%s", n, drawn)
	}
}

// TestACompletedParagraphKeepsItsRows covers the stability the chunking
// buys: once a paragraph's block is finished, the next one's arrival
// paints below it, and the settle does not move it.
func TestACompletedParagraphKeepsItsRows(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	r.streamDelta("text", "First paragraph is finished.\n\nSecond paragraph is still being typed")
	if rowOf(t, scr, "First paragraph is finished.") < 0 {
		t.Fatalf("the finished paragraph never showed:\n%s", scr.text())
	}
	first := rowOf(t, scr, "First paragraph is finished.")

	r.streamDelta("text", " and grows.")
	r.finish()
	drawn := scr.text()
	if got := rowOf(t, scr, "First paragraph is finished."); got != first {
		t.Errorf("the finished paragraph moved from row %d to %d:\n%s", first, got, drawn)
	}
	if got := countOf(t, scr, "First paragraph is finished."); got != 1 {
		t.Errorf("the finished paragraph reads %d time(s), want 1:\n%s", got, drawn)
	}
	if got := countOf(t, scr, "Second paragraph is still being"); got != 1 {
		t.Errorf("the second paragraph reads %d time(s), want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, "typed and grows.") < 0 {
		t.Errorf("the tail of the second paragraph never showed:\n%s", drawn)
	}
}

// TestASplitFenceCommitsAsOneBlock covers the structural block: the
// fence arrives in pieces, its code streams as code, and the settle
// leaves one rendered block with no fence markers on the screen.
func TestASplitFenceCommitsAsOneBlock(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	r.streamDelta("text", "```go\nfunc main() {")
	r.streamDelta("text", "\n}\n```\n\nAfter the fence.")
	r.finish()

	drawn := scr.text()
	if strings.Contains(drawn, "```") {
		t.Errorf("the fence markers showed:\n%s", drawn)
	}
	if got := countOf(t, scr, "func main() {"); got != 1 {
		t.Errorf("the code reads %d time(s), want 1:\n%s", got, drawn)
	}
	if rowOf(t, scr, "After the fence.") < 0 {
		t.Errorf("the paragraph after the fence never showed:\n%s", drawn)
	}
}

// TestAThinkingBlockEndsAboveAToolCall covers the ordering across the
// block's edge: the thought settles before the call decorates the
// transcript, and the reply that follows keeps its place.
func TestAThinkingBlockEndsAboveAToolCall(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "thinking", "weighing it"), "r1", state)
	m.event(deltaEvent("m1", "text", "Let me look."), "r1", state)
	m.event(eventOf("tool.call", map[string]any{"tool": "bash", "summary": "ls"}), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	drawn := plain(r.transcriptText())
	thought := strings.Index(drawn, "weighing it")
	answer := strings.Index(drawn, "Let me look.")
	call := strings.Index(drawn, "⚙ bash")
	if thought < 0 || answer < 0 || call < 0 {
		t.Fatalf("the transcript is missing part of the run:\n%s", drawn)
	}
	if !(thought < answer && answer < call) {
		t.Errorf("the run reads out of order:\n%s", drawn)
	}
	if n := strings.Count(drawn, "weighing it"); n != 1 {
		t.Errorf("the thought reads %d time(s), want 1:\n%s", n, drawn)
	}
}

// TestAnAskFlushesTheThinkingAboveIt covers the permission ask that
// arrives while thinking streams: the thought settles above the
// question, whole, and the question asks below it.
func TestAnAskFlushesTheThinkingAboveIt(t *testing.T) {
	var out bytes.Buffer
	m := &machine{r: aTerminal(t, &out, 40), status: newStatus("")}
	m.conn = &conn{events: make(chan protocol.Event, 8), asks: make(chan askRequest, 8)}
	m.runIDMu.Lock()
	m.runID = "r1"
	m.runIDMu.Unlock()
	m.state = &runState{}
	m.conn.events <- deltaEvent("m1", "thinking", "weighing it")
	in := &plainInput{write: func(string) {}}

	if _, done := m.handleAsk(askRequest{ask: protocol.AskParams{SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash"}}, in); done {
		t.Fatal("the ask ended the session")
	}
	drawn := plain(m.r.transcriptText())
	thought := strings.Index(drawn, "weighing it")
	asked := strings.Index(drawn, "permission")
	if thought < 0 || asked < 0 || thought > asked {
		t.Fatalf("the ask landed over the thinking:\n%s", drawn)
	}
	if n := strings.Count(drawn, "weighing it"); n != 1 {
		t.Errorf("the thought reads %d time(s), want 1:\n%s", n, drawn)
	}
}

// TestAThinkingParagraphDoesNotRepeatWhenTheReplyStarts covers the row
// the thinking block ends on: the preview is replaced whole when the
// reply's first delta switches kind, so the dim paragraph reads once and
// the reply starts below it.
func TestAThinkingParagraphDoesNotRepeatWhenTheReplyStarts(t *testing.T) {
	scr, r := aScreen(t, 100)
	r.userPrompt("look at both tools")
	r.hint()
	r.streamDelta("thinking", "I will inspect **the source** and use `printf`.\n\n")
	if got := countOf(t, scr, "I will inspect the source"); got != 1 {
		t.Fatalf("the thinking paragraph reads %d time(s), want 1:\n%s", got, scr.text())
	}

	r.streamDelta("text", "I can check **both tools**.\n\n")
	drawn := scr.text()
	if got := countOf(t, scr, "I will inspect the source"); got != 1 {
		t.Errorf("the thinking paragraph reads %d time(s) after the reply starts, want 1:\n%s", got, drawn)
	}
	if got := countOf(t, scr, "I can check both tools."); got != 1 {
		t.Errorf("the reply reads %d time(s), want 1:\n%s", got, drawn)
	}
	thought := rowOf(t, scr, "I will inspect the source")
	reply := rowOf(t, scr, "I can check both tools.")
	if thought < 0 || reply < 0 || thought > reply {
		t.Errorf("the thought and the reply are out of order:\n%s", drawn)
	}
}

// TestAStreamingFenceMatchesItsFinalRender covers the fence that
// follows a finished paragraph: the whole fence stays pending, so the
// code streams with the block's own indentation instead of the prose
// margin. The live row must equal the row the full render settles to.
func TestAStreamingFenceMatchesItsFinalRender(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	source := "Intro paragraph.\n\n```go\nfunc main() {\n"
	want := rowFrom(plain(r.markdown(source)), "func main() {")

	r.streamDelta("text", "Intro paragraph.\n\n```go\n")
	r.streamDelta("text", "func main() {\n")
	if got := renderedRow(t, r, scr.text(), "func main() {"); got != want {
		t.Fatalf("the live code row is %q, want the render's row %q:\n%s", got, want, scr.text())
	}
	r.finish()
	if got := renderedRow(t, r, scr.text(), "func main() {"); got != want {
		t.Errorf("the settled code row is %q, want the render's row %q:\n%s", got, want, scr.text())
	}
}

// TestAnEmptyFirstListItemKeepsItsNumbering covers a list whose first
// item is empty: the whole list stays pending, so its second item keeps
// the number the full render gives it instead of restarting at one.
func TestAnEmptyFirstListItemKeepsItsNumbering(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	source := "Intro paragraph.\n\n1.\n\n2. second item\n"
	want := rowFrom(plain(r.markdown(source)), "2. second item")

	r.streamDelta("text", "Intro paragraph.\n\n1.\n\n")
	r.streamDelta("text", "2. second item\n")
	drawn := scr.text()
	if got := renderedRow(t, r, drawn, "second item"); got != want {
		t.Fatalf("the live list row is %q, want the render's row %q:\n%s", got, want, drawn)
	}
	if strings.Contains(drawn, "1. second item") {
		t.Errorf("the list restarted its numbering:\n%s", drawn)
	}
	// The list is one block, so none of it is in the settled transcript
	// until the block ends: a split at the empty item would commit the
	// first item alone.
	if before := plain(r.transcriptText()); strings.Contains(before, "1.") {
		t.Errorf("the unfinished list committed before the settle:\n%s", before)
	}
	r.finish()
	if got := renderedRow(t, r, scr.text(), "second item"); got != want {
		t.Errorf("the settled list row is %q, want the render's row %q:\n%s", got, want, scr.text())
	}
}

// renderedRow returns the first row of a rendered text that shows want,
// with trailing spaces trimmed: the emulator's rows and the renderer's
// rows are compared the same way.
func renderedRow(t *testing.T, r *renderer, text, want string) string {
	t.Helper()
	return rowFrom(plain(text), want)
}

// rowFrom returns the first row of text that shows want.
func rowFrom(text, want string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return strings.TrimRight(line, " ")
		}
	}
	return ""
}

// TestAnUnrelatedLineEndsTheOpenBlockFirst covers the line the repl can
// write while a reply streams — a steer echo, a notice, a failure: the
// open block settles before it, so the screen and the transcript read
// the same order and no word is repeated or erased.
func TestAnUnrelatedLineEndsTheOpenBlockFirst(t *testing.T) {
	scr, r := aScreen(t, 40)
	r.userPrompt("go")
	scr.typeText("draft")

	r.streamDelta("text", "Half a reply")
	r.steerPrompt("steer this")
	r.notice("a notice")
	r.failure("a failure")
	r.streamDelta("text", " and the rest.")
	r.finish()

	drawn := scr.text()
	order := []string{"Half a reply", "steer this", "a notice", "a failure", "and the rest."}
	last := -1
	for _, want := range order {
		if n := countOf(t, scr, want); n != 1 {
			t.Fatalf("%q shows %d time(s), want 1:\n%s", want, n, drawn)
		}
		row := rowOf(t, scr, want)
		if row <= last {
			t.Errorf("%q landed above the line before it:\n%s", want, drawn)
		}
		last = row
	}
	if rowOf(t, scr, promptMark+"draft") < 0 {
		t.Errorf("the draft did not survive the lines:\n%s", drawn)
	}

	tr := plain(r.transcriptText())
	last = -1
	for _, want := range order {
		if n := strings.Count(tr, want); n != 1 {
			t.Fatalf("the transcript shows %q %d time(s), want 1:\n%s", want, n, tr)
		}
		at := strings.Index(tr, want)
		if at <= last {
			t.Errorf("the transcript reads %q out of order:\n%s", want, tr)
		}
		last = at
	}
}

// TestTwoMessageIDsReadAsSeparateParagraphs covers the message
// boundary: two replies with different ids are two blocks, not one word
// glued to the other.
func TestTwoMessageIDsReadAsSeparateParagraphs(t *testing.T) {
	var out bytes.Buffer
	r := aTerminal(t, &out, 40)
	m := &machine{r: r, status: newStatus("")}
	state := &runState{}

	m.event(deltaEvent("m1", "text", "alpha"), "r1", state)
	m.event(deltaEvent("m2", "text", "beta"), "r1", state)
	m.event(eventOf("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"}), "r1", state)

	drawn := plain(out.String())
	if strings.Contains(drawn, "alphabeta") {
		t.Fatalf("the two messages glued together:\n%s", drawn)
	}
	alpha := strings.Index(drawn, "alpha")
	beta := strings.Index(drawn, "beta")
	if alpha < 0 || beta < 0 || alpha > beta {
		t.Fatalf("the two messages do not read in order:\n%s", drawn)
	}
	tr := plain(r.transcriptText())
	if strings.Contains(tr, "alphabeta") {
		t.Fatalf("the transcript glued the two messages:\n%s", tr)
	}
}
