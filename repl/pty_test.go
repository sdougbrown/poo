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
	// The hint is drawn by the first event, which the repl only reads
	// once it knows the run it belongs to.
	waitTimes(t, term, "… writing", 1)
	term.press("\x1b")
	server.waitCancels(t, 1)
	waitFor(t, term, "✓ cancelled")

	term.press("next\n")
	if prompts := server.waitPrompts(t, 2); prompts[1] != "next" {
		t.Fatalf("the session got %v, want \"next\" after the cancel", prompts)
	}
	waitTimes(t, term, "… writing", 2)
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
	waitTimes(t, term, "… writing", 1)
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
