//go:build linux

package repl

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sdougbrown/poo/wire/protocol"
)

// The pty ioctls a terminal pair needs. Linux's syscall package names
// neither, so they are here.
const (
	ioctlPtyUnlock = 0x40045431 // TIOCSPTLCK: 0 unlocks the slave
	ioctlPtyNumber = 0x80045430 // TIOCGPTN: the slave's number
)

// terminal is a real pty. The repl holds the slave, the way a program
// holds its terminal, and the test holds the master: it types the keys
// and reads what was drawn.
type terminal struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	mu     sync.Mutex
	drawn  strings.Builder
}

// openTerminal opens a terminal pair and drains what is drawn on it.
func openTerminal(t *testing.T) *terminal {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening a pty: %v", err)
	}
	unlock := int32(0)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlPtyUnlock, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		t.Fatalf("unlocking the pty: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), ioctlPtyNumber, uintptr(unsafe.Pointer(&number))); errno != 0 {
		master.Close()
		t.Fatalf("naming the pty: %v", errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("opening the pty's slave: %v", err)
	}

	term := &terminal{t: t, master: master, slave: slave}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	go term.drain()
	return term
}

// drain holds what the repl drew.
func (term *terminal) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := term.master.Read(buf)
		if n > 0 {
			term.mu.Lock()
			term.drawn.Write(buf[:n])
			term.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (term *terminal) String() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.drawn.String()
}

// press types keys the way a keyboard sends them.
func (term *terminal) press(keys string) {
	term.t.Helper()
	if _, err := term.master.WriteString(keys); err != nil {
		term.t.Fatalf("typing %q: %v", keys, err)
	}
}

// startTerminalRepl runs a repl on the terminal. The exit code arrives
// on the returned channel once the repl leaves.
func startTerminalRepl(t *testing.T, server *fakeServer) (*terminal, <-chan int) {
	t.Helper()
	term := openTerminal(t)
	stdin, stdout := pipeUp(t, server)

	code := make(chan int, 1)
	opts := Options{
		WorkingDir: t.TempDir(),
		In:         term.slave,
		Out:        term.slave,
		StartServer: func(args []string) (io.WriteCloser, io.ReadCloser, func() error, error) {
			return stdin, stdout, func() error { return nil }, nil
		},
	}
	go func() { code <- Run(opts) }()
	return term, code
}

func TestStreamingPreservesDraftBetweenDeltas(t *testing.T) {
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
	server.waitPrompts(t, 1)
	term.press("draft")
	close(first)
	waitFor(t, term, "First chunk")
	term.press(" more")
	close(finish)
	waitFor(t, term, "✓ end_turn")
	term.press("\n")
	if prompts := server.waitPrompts(t, 2); prompts[1] != "draft more" {
		t.Fatalf("the next prompt was %q, want the draft intact", prompts[1])
	}
	term.press("\x03")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not exit")
	}
}

func TestStreamingBeforePermissionAsk(t *testing.T) {
	ask := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Let me look. "})
		<-ask
		id := s.request(protocol.MethodPermissionAsk, protocol.AskParams{SessionID: "s1", RunID: "r1", AskID: "a1", Tool: "bash", Summary: "ls -la"})
		s.waitDecision(id)
		s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "All done."})
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	term.press("look\n")
	waitFor(t, term, "Let me look.")
	close(ask)
	waitFor(t, term, "allow bash?")
	term.press("y\n")
	waitFor(t, term, "All done.")
	waitFor(t, term, "✓ end_turn")
	term.press("/quit\n")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not quit")
	}
	out := term.String()
	if !(strings.Index(out, "Let me look.") < strings.Index(out, "permission") && strings.Index(out, "permission") < strings.Index(out, "All done.")) {
		t.Fatalf("the reply and ask are out of order: %q", out)
	}
}

func TestStreamingSurvivesServerDisconnect(t *testing.T) {
	disconnect := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{"run_id": "r1", "message_id": "m1", "kind": "text", "text": "Half a sentence"})
		<-disconnect
		s.hangUp()
	}
	term, code := startTerminalRepl(t, server)
	term.press("go on\n")
	waitFor(t, term, "Half a sentence")
	close(disconnect)
	select {
	case got := <-code:
		if got != 1 {
			t.Fatalf("the repl exited %d, want 1", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not exit after disconnect")
	}
}

func TestShiftTabCyclesThinkingAtThePrompt(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("\x1b[Z")
	waitFor(t, term, "🧠 off")
	term.press("\x03")
	select {
	case got := <-code:
		if got != 130 {
			t.Fatalf("the repl exited %d, want 130", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not quit")
	}
}

// TestEscapeStopsTheRunAndTheSessionGoesOn covers Escape over a real
// terminal: the run it stops settles cancelled, the server stays up,
// and the next prompt runs.
func TestEscapeStopsTheRunAndTheSessionGoesOn(t *testing.T) {
	server := &fakeServer{t: t, holdRun: true}
	term, code := startTerminalRepl(t, server)

	term.press("work\n")
	waitTimes(t, term, "world.", 1)
	term.press("\x1b")
	server.waitCancels(t, 1)
	waitFor(t, term, "✓ cancelled")

	term.press("next\n")
	if prompts := server.waitPrompts(t, 2); prompts[1] != "next" {
		t.Fatalf("the session got %v, want \"next\" after the cancel", prompts)
	}
	waitTimes(t, term, "world.", 3)
	term.press("\x1b")
	server.waitCancels(t, 2)
	waitTimes(t, term, "✓ cancelled", 2)
	term.press("/quit\n")

	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d, want 0", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl never left after /quit; it drew:\n%s", term.String())
	}
}

// TestAnArrowKeyIsNotAnEscape covers the keys that begin with an escape
// byte: an arrow must not stop the run, and the escape that stands
// alone must.
func TestAnArrowKeyIsNotAnEscape(t *testing.T) {
	server := &fakeServer{t: t, holdRun: true}
	term, _ := startTerminalRepl(t, server)

	term.press("work\n")
	waitTimes(t, term, "world.", 1)
	term.press("\x1b[A")
	time.Sleep(4 * escWindow)

	server.mu.Lock()
	cancels := append([]string(nil), server.cancels...)
	server.mu.Unlock()
	if len(cancels) != 0 {
		t.Fatalf("an arrow key cancelled the run: %v", cancels)
	}

	term.press("\x1b")
	server.waitCancels(t, 1)
}

// TestCtrlCClearsTheLineThenLeavesTheSession covers Ctrl-C: a line it
// clears is not sent, the session goes on, and an empty prompt is the
// person leaving.
func TestCtrlCClearsTheLineThenLeavesTheSession(t *testing.T) {
	server := &fakeServer{t: t, holdRun: true}
	term, code := startTerminalRepl(t, server)

	term.press("half typed")
	waitFor(t, term, "half typed")
	term.press("\x03")
	waitFor(t, term, "^C")

	term.press("hello\n")
	if prompts := server.waitPrompts(t, 1); prompts[0] != "hello" {
		t.Fatalf("the session got %v, want one \"hello\"", prompts)
	}

	// Nothing is typed now, so the next Ctrl-C is the person leaving.
	term.press("\x03")
	select {
	case got := <-code:
		if got != 130 {
			t.Fatalf("the repl exited %d, want 130", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repl never left on an empty Ctrl-C")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if !server.closed {
		t.Error("the repl left the session open")
	}
}

// TestAPermissionAskTakesTheAnswerTypedBehindIt covers the ask over a
// terminal: the decision is the y typed behind it, and the run settles.
func TestAPermissionAskTakesTheAnswerTypedBehindIt(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)

	term.press("rm the lot\n")
	waitFor(t, term, "allow bash?")
	term.press("y\n")
	waitFor(t, term, "→ allow")
	waitFor(t, term, "✓ end_turn")
	term.press("/quit\n")

	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d, want 0", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl never left after /quit; it drew:\n%s", term.String())
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.decisions) != 1 || server.decisions[0] != "allow" {
		t.Errorf("the server got the decisions %v, want one allow", server.decisions)
	}
}

// TestAForeignSettleDoesNotEndTheRun covers the guard on the settle:
// a settle naming a run that is not the session's is ignored, so the
// run stays live and Escape still has it to stop.
func TestAForeignSettleDoesNotEndTheRun(t *testing.T) {
	server := &fakeServer{t: t, holdRun: true, staleSettle: true, staleSettled: make(chan struct{})}
	term, code := startTerminalRepl(t, server)

	term.press("work\n")
	server.waitPrompts(t, 1)
	<-server.staleSettled
	// The stale settle has gone out; the run it named is not this
	// one, so the client still holds a run for Escape to stop.
	term.press("\x1b")
	if got := server.waitCancels(t, 1); got[0] != "r1" {
		t.Fatalf("the cancel named %q, want the live run r1", got[0])
	}
	waitFor(t, term, "✓ cancelled")
	term.press("/quit\n")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d, want 0", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl never left; it drew:\n%s", term.String())
	}
}
