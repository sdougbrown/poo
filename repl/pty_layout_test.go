//go:build linux

package repl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sdougbrown/poo/wire/protocol"
)

// physicalRows is every row the terminal shows, blanks included.
func physicalRows(term *terminal) []string {
	term.mu.Lock()
	raw := term.vt.String()
	_, rows := term.vt.Size()
	term.mu.Unlock()
	return strings.Split(raw, "\n")[:rows]
}

// cursorRow is the row the terminal's cursor stands on.
func cursorRow(term *terminal) int {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.vt.Cursor().Y
}

// waitBottom waits for the input cursor on the prompt's own row, with
// an empty line being read: exactly one live prompt, the cursor on it,
// no stranded copy below. Wrapped drafts read their cursor elsewhere;
// those tests assert the draft's own position instead.
func waitBottom(t *testing.T, term *terminal) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		screen := physicalRows(term)
		promptRow := -1
		for i, r := range screen {
			if strings.Contains(r, promptMark) || strings.Contains(r, "allow bash?") {
				promptRow = i
			}
		}
		if promptRow >= 0 && cursorRow(term) == promptRow {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the input cursor is not on the prompt's row; rows:\n%s", indexedRows(term))
}

// indexedRows is the terminal's rows with their numbers, for a failure
// that has to say where the prompt stood.
func indexedRows(term *terminal) string {
	var b strings.Builder
	for i, row := range physicalRows(term) {
		b.WriteString(itoa(i) + "|" + strings.TrimRight(row, " ") + "|\n")
	}
	return b.String()
}

// waitScreen waits for the terminal's screen to show want on a row.
func waitScreen(t *testing.T, term *terminal, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if term.rowOf(want) >= 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "DECODED\n%s\nRAW\n%q\n", term.screen(), term.String())
	t.Fatalf("the screen never showed %q", want)
}

func TestTheInputHoldsTheBottomRowThroughARun(t *testing.T) {
	// The run's deltas wait for the test, so each row the reply paints
	// is caught before the next one scrolls it.
	sawHello := make(chan struct{})
	sawWorld := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("run.started", map[string]any{"run_id": "r1"})
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Hello, ",
		})
		<-sawHello
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "thinking", "text": "hmm",
		})
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "world.",
		})
		<-sawWorld
		s.event("usage", map[string]any{
			"run_id": "r1", "turn": 1, "request": 1,
			"input_tokens": 8000, "output_tokens": 500,
		})
		s.event("turn.ended", map[string]any{"run_id": "r1", "turn": 1})
		s.event("turn.started", map[string]any{"run_id": "r1", "turn": 2})
		s.event("tool.call", map[string]any{
			"run_id": "r1", "handle": "h1", "tool": "bash", "summary": "ls -la",
		})
		askID := s.request(protocol.MethodPermissionAsk, protocol.AskParams{
			SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash", Summary: "ls -la",
			Class: "side_effecting", Reason: "policy",
		})
		s.waitDecision(askID)
		s.event("tool.result", map[string]any{
			"run_id": "r1", "handle": "h1", "tool": "bash", "outcome": "ok",
			"text": "file one\nfile two\n",
		})
		s.event("turn.ended", map[string]any{"run_id": "r1", "turn": 2})
		s.event("run.settled", map[string]any{
			"run_id": "r1", "reason": "end_turn", "turns": 2,
			"detail": map[string]any{
				"usage": map[string]any{"input_tokens": 20300, "output_tokens": 1100},
			},
		})
	}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)

	term.press("work\r")
	waitScreen(t, term, "Hello,")
	close(sawHello)
	waitScreen(t, term, "world.")
	close(sawWorld)

	waitScreen(t, term, "allow bash?")
	term.press("y\r")
	waitScreen(t, term, "file one")
	waitScreen(t, term, "✓ end_turn")

	// The transcript's last rows read above the input, and the input
	// stands in the footer's own rows: the mark above the rule and the
	// statusline row at the screen's bottom edge. The earlier reply rows
	// have scrolled into the terminal's history by now.
	settle, prompt, rule := term.rowOf("✓ end_turn"), term.rowOf("💩❯"), term.rowOf("───")
	if settle < 0 || prompt < 0 || settle >= prompt {
		t.Fatalf("the run's rows are not above the input:\n%s", term.screen())
	}
	if rule < 0 || rule <= prompt {
		t.Fatalf("the footer's rule is not below the input:\n%s", term.screen())
	}
	// The footer's bottom row carries the statusline: the model the
	// session opened with, and the window the run's usage filled.
	rows := strings.Split(term.screen(), "\n")
	last := rows[len(rows)-1]
	if !strings.Contains(last, "gemma4") || !strings.Contains(last, "ctx ") || !strings.Contains(last, "/262.1k") {
		t.Errorf("the footer's statusline lacks the model and the fill: %q", last)
	}

	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestLongOutputScrollsAboveTheFooter(t *testing.T) {
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.mu.Lock()
		n := len(s.prompts)
		s.mu.Unlock()
		if n > 1 {
			s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
			return
		}
		// Ten results of three rows each: more rows than the screen,
		// and under the transcript's own cap on one result.
		for i := 0; i < 10; i++ {
			s.event("tool.call", map[string]any{
				"run_id": "r1", "handle": "h1", "tool": "bash", "summary": "row " + itoa(i),
			})
			var text strings.Builder
			for j := 0; j < 3; j++ {
				text.WriteString("result " + itoa(i) + " row " + itoa(j) + "\n")
			}
			s.event("tool.result", map[string]any{
				"run_id": "r1", "handle": "h1", "tool": "bash", "outcome": "ok",
				"text": text.String(),
			})
		}
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	term.press("go\r")
	waitScreen(t, term, "result 9 row 2")
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)

	// The newest result row is above the settle, which is above the
	// input; nothing of the transcript is below the input.
	result, settled, prompt := term.rowOf("result 9 row 2"), term.rowOf("✓ end_turn"), term.rowOf("💩❯")
	if result < 0 || settled < 0 || prompt < 0 || !(result < settled && settled < prompt) {
		t.Fatalf("the newest row, the settle and the prompt are out of order:\n%s", term.screen())
	}
	// The prompt reads within the footer: its line row and the spare
	// row the editor's erases spend under it, above the chrome's rule.
	rule := term.rowOf("───")
	if rule < 0 || prompt > rule-1 {
		t.Errorf("the prompt stands on row %d, want the footer's rows above the rule:\n%s", prompt, indexedRows(term))
	}
	if term.rowOf("result 0 row 0") >= 0 {
		t.Errorf("the oldest row is still on the screen instead of the scrollback:\n%s", term.screen())
	}

	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestAModelSwitchPaintsTheFooterStatusline(t *testing.T) {
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	term.press("/model qwen\r")
	waitScreen(t, term, "model → qwen")
	term.press("say hi\r")
	waitScreen(t, term, "✓ end_turn")

	// The footer's bottom row names the model the switch took, beside
	// the window the run's usage filled: a switch that kept the old
	// name or window would read them here.
	rows := strings.Split(term.screen(), "\n")
	last := rows[len(rows)-1]
	if !strings.Contains(last, "qwen") || !strings.Contains(last, "/131.1k") {
		t.Errorf("the footer's statusline does not name qwen beside its window: %q", last)
	}

	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestTheDraftSurvivesTheStreamingAndSettlesInTheFooter(t *testing.T) {
	first := make(chan struct{})
	finish := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.mu.Lock()
		n := len(s.prompts)
		s.mu.Unlock()
		if n > 1 {
			s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
			return
		}
		<-first
		s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "First chunk "})
		<-finish
		s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "and second.\n"})
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("work\r")
	close(first)
	// The stream paints when the block settles: a draft typed over the
	// held deltas rides in the editor, and the settle renders the reply.
	term.press("draft")
	term.press(" more")
	close(finish)
	waitScreen(t, term, "First chunk and second.")
	waitScreen(t, term, "✓ end_turn")
	// A single-line draft stays on the prompt's own row: the cursor
	// reads after it on that row, and no footer row grew for the typed
	// line.
	waitScreen(t, term, "draft more")
	waitBottom(t, term)
	if row := term.rowOf("draft more"); row != cursorRow(term) {
		t.Fatalf("the typed draft stands on row %d with the cursor on %d, want the prompt's own row:\n%s", row, cursorRow(term), indexedRows(term))
	}

	term.press("\r")
	if prompts := server.waitPrompts(t, 2); prompts[1] != "draft more" {
		t.Fatalf("the next prompt was %q, want the draft intact", prompts[1])
	}
	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestHeldReplySurvivesFirstDraftKeyAndBackspace(t *testing.T) {
	for _, backspace := range []bool{false, true} {
		t.Run(fmt.Sprint("backspace=", backspace), func(t *testing.T) {
			finish := make(chan struct{})
			server := &fakeServer{t: t}
			server.script = func(s *fakeServer) {
				s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "held reply"})
				<-finish
				s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
			}
			term, code := startTerminalRepl(t, server)
			waitFor(t, term, promptMark)
			term.press("work\r")
			term.press("x")
			waitScreen(t, term, "💩❯ x")
			if backspace {
				term.press("\x7f")
				deadline := time.Now().Add(10 * time.Second)
				for strings.Contains(term.screen(), "💩❯ x") && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if strings.Contains(term.screen(), "💩❯ x") {
					t.Fatal("backspace did not clear the draft")
				}
			}
			close(finish)
			waitScreen(t, term, "held reply")
			waitScreen(t, term, "✓ end_turn")
			if n := strings.Count(term.screen(), "held reply"); n != 1 {
				t.Fatalf("held reply appears %d times after settle:\n%s", n, indexedRows(term))
			}
			if !backspace {
				waitScreen(t, term, "💩❯ x")
				term.press("\x7f")
			}
			term.press("/quit\r")
			select {
			case got := <-code:
				if got != 0 {
					t.Fatalf("repl exited %d", got)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("repl did not quit")
			}
		})
	}
}

func TestAResizeMovesTheInputToTheNewBottomRow(t *testing.T) {
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	term.resize(60, 20)
	// The terminal's own size change signals the foreground process
	// group. A pty with no controlling terminal has no one to signal, so
	// the test sends the signal itself.
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("signalling the resize: %v", err)
	}
	waitBottomRows(t, term, 20)

	// The submit waits for its own echo: an Enter that lands while the
	// winch redraw is still running can clear the draft without
	// submitting, and the echo is what says the line was taken.
	pressSubmitted(t, term, "work")
	waitScreen(t, term, "world.")
	waitBottomRows(t, term, 20)
	waitScreen(t, term, "✓ end_turn")
	waitBottomRows(t, term, 20)
	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

// echoOnScreen says whether the transcript's echo of a submitted line
// stands on the screen: the row reads "❯ work" and carries no prompt
// mark, so the draft's own "💩❯ work" row does not count.
func echoOnScreen(term *terminal, text string) bool {
	for _, line := range strings.Split(term.screen(), "\n") {
		if strings.Contains(line, "❯ "+text) && !strings.Contains(line, promptMark) {
			return true
		}
	}
	return false
}

// waitEcho waits for the transcript's echo of a submitted line.
func waitEcho(t *testing.T, term *terminal, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if echoOnScreen(term, text) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the echo of %q never read:\n%s", text, term.screen())
}

// pressSubmitted types a line and waits for the machine's echo of it. An
// Enter that lands mid-winch-redraw can clear the draft without
// submitting, so the echo says whether the line was taken; until it
// shows, the line is typed again — the cleared draft is empty, so the
// retyped line is whole.
func pressSubmitted(t *testing.T, term *terminal, text string) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		term.press(text + "\r")
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if echoOnScreen(term, text) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatalf("the line %q never took; the screen reads:\n%s", text, term.screen())
}

// waitBottomRows waits for the prompt to stand on the footer's line row
// of a terminal that is rows tall: the bottom row holds the spare the
// editor's erases spend.
func waitBottomRows(t *testing.T, term *terminal, rowsWant int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := term.vt.Size()
		// The input cursor reads on the prompt's row, inside the footer's
		// own rows above the chrome: the rule and the statusline's row
		// sit under it at the screen's bottom edge.
		if rows == rowsWant && cursorRow(term) >= rowsWant-4 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the input cursor is not on the footer's rows above row %d; rows:\n%s", rowsWant, indexedRows(term))
}

// TestAnInactiveLayoutDoesNotDoubleTheCarriageReturns covers the
// plain writes of a layout the repl left: a line that already ends in
// CRLF does not gain a second carriage return.
func TestAnInactiveLayoutDoesNotDoubleTheCarriageReturns(t *testing.T) {
	var record bytes.Buffer
	l := &layout{out: &record, width: 40, height: 12, footer: 1}

	l.write("a\r\nb\r\n")

	drawn := record.String()
	if strings.Contains(drawn, "\r\r") {
		t.Fatalf("a CRLF line gained a second carriage return:\n%q", drawn)
	}
	if !strings.Contains(drawn, "a\r\nb\r\n") {
		t.Fatalf("the plain write read wrong:\n%q", drawn)
	}
}

// TestATooSmallTerminalHasNoLayout covers the size guard of the real
// constructor: a terminal that cannot hold a region and a footer
// builds no layout, and the full one does.
func TestATooSmallTerminalHasNoLayout(t *testing.T) {
	term := openTerminal(t)
	setSize(t, term.master, 80, 3)
	if l := newLayout(term.slave); l != nil {
		t.Fatalf("a 3-row terminal built a layout")
	}
	setSize(t, term.master, 80, 24)
	l := newLayout(term.slave)
	if l == nil {
		t.Fatal("a full terminal built no layout")
	}
	if l.height != 24 {
		t.Fatalf("the layout read %d rows, want 24", l.height)
	}
}

// setSize gives the pty its geometry, the way a terminal emulator
// would.
func setSize(t *testing.T, f *os.File, cols, rows int) {
	t.Helper()
	ws := struct{ row, col, x, y uint16 }{uint16(rows), uint16(cols), 0, 0}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioctlWinsize, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		t.Fatalf("sizing the pty: %v", errno)
	}
}

func TestThePagerTakesAndReturnsTheTerminal(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("no less on the path")
	}
	// -X keeps less on the primary screen, so the region handoff is in
	// the stream the test reads rather than hidden by an alt buffer.
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("LESS", "-X")
	server := &fakeServer{t: t}
	// A line that reaches the server by mistake settles rather than
	// leaving a goroutine waiting on an ask the test never answers.
	server.script = func(s *fakeServer) {
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	// The bytes after this point hold the pager's turn at the terminal.
	start := len(term.String())
	term.press("/less\r")
	// less's own end marker says it is reading the terminal; a pager
	// that did not start leaves the prompt standing instead. The
	// forty-column status line cuts the marker's last glyph.
	waitScreen(t, term, "(END")
	if term.rowOf("💩❯") >= 0 {
		t.Fatalf("the pager did not take the terminal:\n%s", term.screen())
	}
	term.press("q")
	// The statusline is not in the transcript the pager shows: its
	// return says the pager left and the session's own screen is back.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && term.rowOf("⌂") < 0 {
		time.Sleep(5 * time.Millisecond)
	}
	waitBottom(t, term)

	// The region came off before the pager drew, and went back after:
	// the reset precedes less's own end marker, and a region set follows
	// it.
	seq := term.String()[start:]
	reset := strings.Index(seq, seqResetRegion)
	pager := strings.Index(seq, "(END")
	if reset < 0 || pager < 0 || reset > pager {
		t.Fatalf("the region was not reset before the pager drew (reset %d, pager %d):\n%q", reset, pager, seq)
	}
	region := regexp.MustCompile(`\x1b\[1;[0-9]+r`).FindStringIndex(seq[pager:])
	if region == nil {
		t.Fatalf("the region was not reserved again after the pager:\n%q", seq[pager:])
	}
	if strings.Contains(seq, "\x1b[?1049") {
		t.Errorf("less used an alt buffer despite -X; the primary-screen witness is weaker")
	}

	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit after the pager; it drew:\n%s", term.screen())
	}
}

func TestTheLayoutRestoresTheTerminalOnExit(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)
	waitScreen(t, term, "poo repl")

	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}

	// The shell's turn: a line at the cursor scrolls the whole screen.
	if _, err := term.slave.WriteString(strings.Repeat("\n", 12) + "after the repl\n"); err != nil {
		t.Fatalf("writing after the repl: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		screen := term.screen()
		if strings.Contains(screen, "after the repl") && !strings.Contains(screen, "poo repl") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the screen did not scroll after the repl:\n%s", term.screen())
}

var _ = protocol.MethodRunCancel

func TestAMultilineDraftStaysInTheFooter(t *testing.T) {
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	draft := "draft " + strings.Repeat("x", 76)
	term.press(draft)
	waitScreen(t, term, "💩❯ draft")
	// The draft wraps within the footer's rows, and its tail stands
	// above the chrome's rule, never below the footer's own rows.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(term.screen(), strings.Repeat("x", 20)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rows := strings.Split(term.screen(), "\n")
	rule, tail := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "───") {
			rule = i
		}
		if strings.Contains(row, "xxxx") {
			tail = i
		}
	}
	if tail < 0 || rule < 0 || tail >= rule {
		t.Fatalf("the draft's tail is not in the footer above the rule:\n%s", indexedRows(term))
	}
	if len(physicalRows(term)) != 12 {
		t.Fatalf("the terminal changed size:\n%s", indexedRows(term))
	}

	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != draft {
		t.Fatalf("the submitted line is %q, want the draft whole", prompts[0])
	}
	// The echo of a two-row draft scrolls fast on a twelve-row terminal,
	// so the send is what the wait asserts.
	waitBottom(t, term)
	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

// startPipedRepl runs the repl with its input from a pipe and its output
// on the terminal, the way a redirect does.
func startPipedRepl(t *testing.T, server *fakeServer, input string) (*terminal, <-chan int) {
	t.Helper()
	term := openTerminal(t)
	stdin, stdout := pipeUp(t, server)

	code := make(chan int, 1)
	exited := make(chan struct{})
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         strings.NewReader(input),
		Out:        term.slave,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { code <- Run(opts); close(exited) }()
	// endReplCleanup owns the teardown contract: interrupts until the
	// repl leaves, then the wait that holds the terminal until Run has
	// returned. It runs after the interrupts and before the stdio
	// restore (cleanups unwind last registered first).
	t.Cleanup(func() { endReplCleanup(t, term, exited) })
	return term, code
}

func TestAPipedInputKeepsThePlainTranscript(t *testing.T) {
	server := &fakeServer{t: t}
	sent := make(chan struct{})
	release := make(chan struct{})
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Hello, world.",
		})
		close(sent)
		<-release
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startPipedRepl(t, server, "say hello\n")
	waitScreen(t, term, "❯ say hello")
	<-sent
	// A run held open: a stream shows the words now; a pipe waits for
	// the settle.
	time.Sleep(150 * time.Millisecond)
	streamed := strings.Contains(term.screen(), "Hello, world.")
	close(release)
	if streamed {
		t.Errorf("the reply streamed with a pipe for its input:\n%s", term.screen())
	}

	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not finish; it drew:\n%s", term.screen())
	}
	// The repl's last write reaches the terminal a moment after Run
	// returns.
	waitScreen(t, term, "Hello, world.")
	if got := strings.Count(term.screen(), "Hello, world."); got != 1 {
		t.Errorf("the reply reads %d time(s), want 1:\n%s", got, term.screen())
	}
	if rows := strings.Split(term.screen(), "\n"); !strings.Contains(rows[0], "poo repl") {
		t.Errorf("the transcript does not start at the top row:\n%s", indexedRows(term))
	}
}

func TestAPromptChangeKeepsATypedDraft(t *testing.T) {
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	waitBottom(t, term)

	draft := "a draft " + strings.Repeat("d", 30)
	term.press(draft)
	waitScreen(t, term, "a draft")
	term.press("\x1b[Z")
	waitScreen(t, term, "🧠 off")
	// The draft and its cursor settle together: the frame that shows
	// the tail also lands the cursor under it, and a read between the
	// two sees the cursor still on the prompt's row, inside the
	// footer's rows above the chrome.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		screen := term.screen()
		if strings.Contains(screen, strings.Repeat("d", 20)) && cursorRow(term) >= len(physicalRows(term))-4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(term.screen(), strings.Repeat("d", 20)) {
		t.Fatalf("the prompt change lost the draft:\n%s", indexedRows(term))
	}
	if row := cursorRow(term); row < len(physicalRows(term))-4 {
		t.Errorf("the draft's cursor stands on row %d, want the footer's rows above the chrome:\n%s", row, indexedRows(term))
	}

	// A real Enter is CR: after the cycle's ESC put vim in normal mode,
	// LF is a vim key the editor swallows.
	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != draft {
		t.Fatalf("the submitted line is %q, want the draft whole", prompts[0])
	}
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)
	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

// startTerminalReplWith runs a repl on a terminal with the options a
// test needs, the history file above all.
func startTerminalReplWith(t *testing.T, server *fakeServer, history string) (*terminal, <-chan int) {
	t.Helper()
	term := openTerminal(t)
	stdin, stdout := pipeUp(t, server)
	restore := takeOverStdio(t, term.slave)
	t.Cleanup(restore)
	code := make(chan int, 1)
	exited := make(chan struct{})
	opts := Options{
		WorkingDir:  t.TempDir(),
		HistoryFile: history,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { code <- Run(opts); close(exited) }()
	// endReplCleanup owns the teardown contract: interrupts until the
	// repl leaves, then the wait that holds the terminal until Run has
	// returned. It runs after the interrupts and before the stdio
	// restore (cleanups unwind last registered first).
	t.Cleanup(func() { endReplCleanup(t, term, exited) })
	return term, code
}

func TestARecalledHistoryLineTakesTheFooter(t *testing.T) {
	history := filepath.Join(t.TempDir(), "history")
	recalled := "recalled " + strings.Repeat("h", 80)
	// The history is a file of JSON lines, one entry each.
	line := fmt.Sprintf(`{"block": %q}`, recalled+"\n")
	if err := os.WriteFile(history, []byte(line), 0o600); err != nil {
		t.Fatalf("writing the history: %v", err)
	}
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalReplWith(t, server, history)
	waitBottom(t, term)

	// vim's normal mode recalls the last line with k: the Escape lands
	// first, so the command keymap is the one the k reads in.
	term.press("\x1b")
	time.Sleep(50 * time.Millisecond)
	term.press("k")
	waitScreen(t, term, strings.Repeat("h", 20))
	rows := len(physicalRows(term))
	if got := strings.Count(term.screen(), "recalled"); got != 1 {
		t.Fatalf("the recalled draft reads %d time(s) on the screen, want 1:\n%s", got, indexedRows(term))
	}
	// The recalled block keeps the newline its storage carries: the
	// draft fills three rows (the prompt's indent rides on the first),
	// the cursor reads on the row the trailing newline opens, and the
	// spare row and the chrome's two sit under that. The wrapped tail
	// is whole.
	head, tail := term.rowOf("recalled"), term.rowOf(strings.Repeat("h", 20))
	if head < rows-7 || head > rows-6 {
		t.Fatalf("the draft's head stands on row %d, want the footer's top rows:\n%s", head, indexedRows(term))
	}
	if tail != head+1 {
		t.Fatalf("the draft's wrapped tail stands on row %d, want the row under the head:\n%s", tail, indexedRows(term))
	}
	if row := cursorRow(term); row < head+2 || row > head+3 {
		t.Errorf("the recalled draft's cursor stands on row %d, want the rows under the draft:\n%s", row, indexedRows(term))
	}

	// A real Enter is CR: after the recall's ESC, vim is in normal mode.
	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != recalled {
		t.Fatalf("the submitted line is %q, want the recalled line whole", prompts[0])
	}
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)
	// The recall left the editor in vim's command keymap; `i` returns
	// to insert before the quit command reads.
	term.press("i")
	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestAResizeDuringAHeldPreviewKeepsTheInputAtTheBottom(t *testing.T) {
	sent := make(chan struct{})
	release := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		var block strings.Builder
		block.WriteString("```text\n")
		for i := 0; i < 30; i++ {
			block.WriteString("preview row " + itoa(i) + "|\n")
		}
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": block.String(),
		})
		close(sent)
		<-release
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text",
			"text": "```\n\nAfter the block.\n\nEverything is done.",
		})
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)
	term.press("work\r")
	<-sent
	waitScreen(t, term, "preview row 29|")

	// Narrower and taller while the run holds: the preview was laid out
	// at forty columns and the footer at twelve rows.
	term.resize(30, 16)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("signalling the resize: %v", err)
	}
	waitBottomRows(t, term, 16)
	// The held preview comes back when the resize response repaints the
	// region at the new width; the frame that anchors the response may
	// land first.
	waitScreen(t, term, "preview row")
	if term.rowOf("preview row") < 0 {
		t.Errorf("the resize took the held preview off the screen:\n%s", indexedRows(term))
	}

	close(release)
	waitScreen(t, term, "✓ end_turn")
	waitScreen(t, term, "After the block.")
	waitBottomRows(t, term, 16)

	// The rows the preview clipped reach the terminal once, at the
	// commit. The resize cleared the stale preview instead of letting
	// the commit write it beside itself.
	raw := term.String()
	for _, marker := range []string{"preview row 0|", "preview row 10|"} {
		if n := strings.Count(raw, marker); n != 1 {
			t.Errorf("the clipped row %q entered the terminal %d time(s), want 1", marker, n)
		}
	}
	term.press("/quit\r")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

func TestAStartupThatFailsLeavesTheTerminalWhole(t *testing.T) {
	term := openTerminal(t)
	code := make(chan int, 1)
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         term.slave,
		Out:        term.slave,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return nil, nil, nil, errors.New("no server to start")
		},
	}
	go func() { code <- Run(opts) }()
	select {
	case got := <-code:
		if got != 1 {
			t.Fatalf("the repl exited %d, want 1", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not fail; it drew:\n%s", term.screen())
	}
	waitScreen(t, term, "no server to start")
	if region := regexp.MustCompile(`\x1b\[1;[0-9]+r`).FindString(term.String()); region != "" {
		t.Errorf("a handshake that never opened reserved the region %q", region)
	}
	if term.rowOf("💩❯") >= 0 {
		t.Errorf("a handshake that never opened drew a prompt:\n%s", term.screen())
	}
}
