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

// lastPhysicalRow is the terminal's bottom row.
func lastPhysicalRow(term *terminal) string {
	rows := physicalRows(term)
	if len(rows) == 0 {
		return ""
	}
	return strings.TrimRight(rows[len(rows)-1], " ")
}

// cursorRow is the row the terminal's cursor stands on.
func cursorRow(term *terminal) int {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.vt.Cursor().Y
}

// waitBottom waits for the input cursor on the terminal's bottom row.
// The prompt, the draft or an ask all end there while the line is read.
func waitBottom(t *testing.T, term *terminal) {
	t.Helper()
	_, rows := term.vt.Size()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cursorRow(term) == rows-1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the input cursor is not on the bottom row; rows:\n%s", indexedRows(term))
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
	t.Fatalf("the screen never showed %q:\n%s", want, term.screen())
}

func TestTheInputHoldsTheBottomRowThroughARun(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)
	waitBottom(t, term)

	term.press("work\n")
	waitScreen(t, term, "Hello,")
	waitScreen(t, term, "world.")
	waitBottom(t, term)

	waitScreen(t, term, "allow bash?")
	// The ask's own line is the prompt the answer is typed behind.
	if !strings.Contains(lastPhysicalRow(term), "allow bash?") {
		t.Fatalf("the ask is not on the bottom row; rows:\n%s", indexedRows(term))
	}
	term.press("y\n")
	waitScreen(t, term, "file one")
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)

	// The reply and the tool result read above the input, in order.
	world, prompt := term.rowOf("world."), term.rowOf("💩❯")
	if world < 0 || prompt < 0 || world >= prompt {
		t.Fatalf("the reply is not above the input:\n%s", term.screen())
	}

	term.press("/quit\n")
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

	term.press("go\n")
	waitScreen(t, term, "result 9 row 2")
	waitBottom(t, term)

	// The newest result row is above the settle, which is above the
	// input; nothing of the transcript is below the input.
	result, settled, prompt := term.rowOf("result 9 row 2"), term.rowOf("✓ end_turn"), term.rowOf("💩❯")
	if result < 0 || settled < 0 || prompt < 0 || !(result < settled && settled < prompt) {
		t.Fatalf("the newest row, the settle and the prompt are out of order:\n%s", term.screen())
	}
	if prompt != len(physicalRows(term))-1 {
		t.Errorf("the prompt stands on row %d, want the bottom row:\n%s", prompt, indexedRows(term))
	}
	if term.rowOf("result 0 row 0") >= 0 {
		t.Errorf("the oldest row is still on the screen instead of the scrollback:\n%s", term.screen())
	}

	term.press("/quit\n")
	select {
	case <-code:
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
	term.press("work\n")
	close(first)
	waitScreen(t, term, "First chunk")
	term.press("draft")
	waitScreen(t, term, "💩❯ draft")
	if row, bottom := term.rowOf("💩❯ draft"), len(physicalRows(term))-1; row < bottom-1 {
		t.Fatalf("the draft stands on row %d, want the footer's rows (bottom %d):\n%s", row, bottom, term.screen())
	}
	term.press(" more")
	waitScreen(t, term, "💩❯ draft more")
	close(finish)
	waitScreen(t, term, "✓ end_turn")
	waitScreen(t, term, "💩❯ draft more")

	term.press("\n")
	if prompts := server.waitPrompts(t, 2); prompts[1] != "draft more" {
		t.Fatalf("the next prompt was %q, want the draft intact", prompts[1])
	}
	term.press("/quit\n")
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
			term.press("work\n")
			waitScreen(t, term, "held reply")
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
			waitScreen(t, term, "✓ end_turn")
			if n := strings.Count(term.screen(), "held reply"); n != 1 {
				t.Fatalf("held reply appears %d times after settle:\n%s", n, indexedRows(term))
			}
			if !backspace {
				waitScreen(t, term, "💩❯ x")
				term.press("\x7f")
			}
			waitBottom(t, term)
			term.press("/quit\n")
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

	term.press("work\n")
	waitScreen(t, term, "world.")
	waitBottomRows(t, term, 20)
	waitScreen(t, term, "✓ end_turn")
	waitBottomRows(t, term, 20)
	term.press("/quit\n")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

// waitBottomRows waits for the prompt to stand on the bottom row of a
// terminal that is rows tall.
func waitBottomRows(t *testing.T, term *terminal, rowsWant int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := term.vt.Size()
		if rows == rowsWant && cursorRow(term) == rowsWant-1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the input cursor is not on row %d; rows:\n%s", rowsWant, indexedRows(term))
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
	term.press("/less\n")
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

	term.press("/quit\n")
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

	term.press("/quit\n")
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
	// The draft wraps over the footer's rows, and its tail stands on the
	// last visible row above the spare one.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(term.screen(), strings.Repeat("x", 20)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rows := strings.Split(term.screen(), "\n")
	if !strings.Contains(rows[len(rows)-1], "xxxx") {
		t.Fatalf("the draft's tail is not in the footer:\n%s", indexedRows(term))
	}
	if len(physicalRows(term)) != 12 {
		t.Fatalf("the terminal changed size:\n%s", indexedRows(term))
	}

	term.press("\n")
	if prompts := server.waitPrompts(t, 1); prompts[0] != draft {
		t.Fatalf("the submitted line is %q, want the draft whole", prompts[0])
	}
	waitScreen(t, term, "❯ draft")
	waitBottom(t, term)
	term.press("/quit\n")
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
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         strings.NewReader(input),
		Out:        term.slave,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { code <- Run(opts) }()
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
	waitBottom(t, term)

	draft := "a draft " + strings.Repeat("d", 30)
	term.press(draft)
	waitScreen(t, term, "a draft")
	term.press("\x1b[Z")
	waitScreen(t, term, "🧠 off")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(term.screen(), strings.Repeat("d", 20)) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(term.screen(), strings.Repeat("d", 20)) {
		t.Fatalf("the prompt change lost the draft:\n%s", indexedRows(term))
	}
	if row := cursorRow(term); row < len(physicalRows(term))-2 {
		t.Errorf("the draft's cursor stands on row %d, want the footer's last rows:\n%s", row, indexedRows(term))
	}

	// A real Enter is CR: after the cycle's ESC put vim in normal mode,
	// LF is a vim key the editor swallows.
	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != draft {
		t.Fatalf("the submitted line is %q, want the draft whole", prompts[0])
	}
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)
	term.press("/quit\n")
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
	code := make(chan int, 1)
	opts := Options{
		WorkingDir:  t.TempDir(),
		HistoryFile: history,
		In:          term.slave,
		Out:         term.slave,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { code <- Run(opts) }()
	return term, code
}

func TestARecalledHistoryLineTakesTheFooter(t *testing.T) {
	history := filepath.Join(t.TempDir(), "history")
	recalled := "recalled " + strings.Repeat("h", 80)
	if err := os.WriteFile(history, []byte(recalled+"\n"), 0o600); err != nil {
		t.Fatalf("writing the history: %v", err)
	}
	server := &fakeServer{t: t}
	closed := make(chan struct{})
	close(closed)
	server.script = quietRun(closed)
	term, code := startTerminalReplWith(t, server, history)
	waitBottom(t, term)

	// vim's normal mode recalls the last line with k.
	term.press("\x1bk")
	waitScreen(t, term, strings.Repeat("h", 20))
	rows := len(physicalRows(term))
	if got := strings.Count(term.screen(), "recalled"); got != 1 {
		t.Fatalf("the recalled draft reads %d time(s) on the screen, want 1:\n%s", got, indexedRows(term))
	}
	head, tail := term.rowOf("recalled"), term.rowOf(strings.Repeat("h", 20))
	if head < rows-4 || tail < rows-3 || tail > rows-2 {
		t.Fatalf("the draft stands on rows %d..%d, want the footer's rows above %d:\n%s", head, tail, rows-4, indexedRows(term))
	}
	if row := cursorRow(term); row < rows-2 {
		t.Errorf("the recalled draft's cursor stands on row %d, want the footer's last rows:\n%s", row, indexedRows(term))
	}

	// A real Enter is CR: after the recall's ESC, vim is in normal mode.
	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != recalled {
		t.Fatalf("the submitted line is %q, want the recalled line whole", prompts[0])
	}
	waitScreen(t, term, "✓ end_turn")
	waitBottom(t, term)
	term.press("/quit\n")
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
	term.press("work\n")
	<-sent
	waitScreen(t, term, "preview row 29|")

	// Narrower and taller while the run holds: the preview was laid out
	// at forty columns and the footer at twelve rows.
	term.resize(30, 16)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("signalling the resize: %v", err)
	}
	waitBottomRows(t, term, 16)
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
	term.press("/quit\n")
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
