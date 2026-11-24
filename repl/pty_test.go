//go:build linux

package repl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/chubin/vt10x"
	"github.com/reeflective/readline"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The pty ioctls a terminal pair needs. Linux's syscall package names
// neither, so they are here.
const (
	ioctlPtyUnlock = 0x40045431 // TIOCSPTLCK: 0 unlocks the slave
	ioctlWinsize   = 0x5414     // TIOCSWINSZ: 0 sets the pty's size
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
	// vt reads the same bytes the terminal is sent, so a test asserts
	// the screen the person sees instead of the stream that drew it.
	vt vt10x.Terminal
	// changed is the draw signal: the next batch of drawn bytes closes
	// it, so a wait ends on the draw instead of a poll. Nil until a
	// wait asks for it.
	changedCh chan struct{}
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
	term.resize(40, 12)
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	term.vt = vt10x.New(vt10x.WithSize(40, 12))
	go term.drain()
	return term
}

// resize gives the terminal a window size, the way a terminal
// emulator's own window does.
func (term *terminal) resize(cols, rows int) {
	term.t.Helper()
	// The emulator is ready before the pty tells the repl. The signal can
	// arrive during the ioctl, and the redraw must land on the new
	// screen.
	if term.vt != nil {
		term.vt.Resize(cols, rows)
		term.mu.Lock()
		term.signalChanged()
		term.mu.Unlock()
	}
	ws := struct {
		Row, Col, Xpixel, Ypixel uint16
	}{Row: uint16(rows), Col: uint16(cols)}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, term.master.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		term.t.Fatalf("sizing the pty: %v", errno)
	}
}

// drain holds what the repl drew.
func (term *terminal) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := term.master.Read(buf)
		if n > 0 {
			term.mu.Lock()
			term.drawn.Write(buf[:n])
			if term.vt != nil {
				term.vt.Write(buf[:n])
			}
			term.signalChanged()
			term.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// changed returns the draw signal: the next batch of drawn bytes
// closes it. A wait takes the channel, then checks, then blocks: any
// draw after the check closes the channel the wait holds, so no draw
// is missed.
func (term *terminal) changed() chan struct{} {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.changedCh == nil {
		term.changedCh = make(chan struct{})
	}
	return term.changedCh
}

// signalChanged closes the draw signal. The caller holds mu.
func (term *terminal) signalChanged() {
	if term.changedCh != nil {
		close(term.changedCh)
		term.changedCh = nil
	}
}

func (term *terminal) String() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.drawn.String()
}

// screen is what the terminal shows: the rows, trimmed.
func (term *terminal) screen() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return trimScreen(term.vt.String())
}

// rowOf returns the last row a text stands on, or -1.
func (term *terminal) rowOf(want string) int {
	row := -1
	for i, line := range strings.Split(term.screen(), "\n") {
		if strings.Contains(line, want) {
			row = i
		}
	}
	return row
}

// press types keys the way a keyboard sends them.
func (term *terminal) press(keys string) {
	term.t.Helper()
	if _, err := term.master.WriteString(keys); err != nil {
		term.t.Fatalf("typing %q: %v", keys, err)
	}
}

// takeOverStdio dups the pty slave over the process's own stdin and
// stdout, and returns the function that puts the saved fds back. The
// editor captured the stream objects at init; the fds are what those
// objects actually read and write, so the dup is what aims them at the
// terminal the test owns.
func takeOverStdio(t *testing.T, slave *os.File) (restore func()) {
	t.Helper()
	savedIn, err := syscall.Dup(0)
	if err != nil {
		t.Fatalf("duping stdin: %v", err)
	}
	savedOut, err := syscall.Dup(1)
	if err != nil {
		syscall.Close(savedIn)
		t.Fatalf("duping stdout: %v", err)
	}
	if err := syscall.Dup2(int(slave.Fd()), 0); err != nil {
		syscall.Close(savedIn)
		syscall.Close(savedOut)
		t.Fatalf("duping the terminal over stdin: %v", err)
	}
	if err := syscall.Dup2(int(slave.Fd()), 1); err != nil {
		syscall.Dup2(savedIn, 0)
		syscall.Close(savedIn)
		syscall.Close(savedOut)
		t.Fatalf("duping the terminal over stdout: %v", err)
	}
	// The editor's terminal queries read stderr by default, which is
	// this process's test log, not the terminal under test. The query
	// override points them at the slave, so the engine sizes and probes
	// the same terminal its frames render on.
	readline.SetQueryFile(slave)
	return func() {
		syscall.Dup2(savedIn, 0)
		syscall.Dup2(savedOut, 1)
		syscall.Close(savedIn)
		syscall.Close(savedOut)
		readline.SetQueryFile(nil)
	}
}

// endReplCleanup ends a test's repl. The failure dump goes to stderr —
// stdout is the terminal under test. The interrupts repeat until the
// repl actually leaves. The first clears a draft, the second leaves an
// empty prompt, and one the editor consumed mid-sequence asks for
// another. The loop is bounded, so a repl that will not leave fails
// here instead of reading the next test's keystrokes forever. After
// the interrupts, the wait holds the terminal until Run has returned:
// a repl still running holds the editor on fd 0 and reads the next
// test's keystrokes.
func endReplCleanup(t *testing.T, term *terminal, exited <-chan struct{}) {
	t.Helper()
	if t.Failed() {
		fmt.Fprintf(os.Stderr, "DECODED\n%s\nRAW\n%q\n", term.screen(), term.String())
	}
	for i := 0; i < 5; i++ {
		select {
		case <-exited:
			return
		case <-time.After(300 * time.Millisecond):
		}
		term.press("\x03")
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Error("the repl did not leave within 10s")
	}
}

// startTerminalRepl runs a repl on the terminal. The exit code arrives
// on the returned channel once the repl leaves.
func startTerminalRepl(t *testing.T, server *fakeServer) (*terminal, <-chan int) {
	t.Helper()
	term := openTerminal(t)
	stdin, stdout := pipeUp(t, server)

	// The editor is hardwired to the process's own stdio: it reads keys
	// from fd 0 and renders to fd 1, and it captured the stream objects
	// when the editor's package initialized. The terminal's slave
	// therefore stands in for the process's own fds 0 and 1 while the
	// repl runs; the saved fds come back after, so a failure never
	// leaves the harness reading itself. The repl keeps the default
	// In and Out, which are the very streams the editor holds.
	restore := takeOverStdio(t, term.slave)
	t.Cleanup(restore)

	code := make(chan int, 1)
	exited := make(chan struct{})
	// The working dir's basename rides the statusline's first segment,
	// so it stays short and explicit: a TempDir's name is the
	// toolchain's to choose, and a wide one would clip the segments the
	// footer tests read.
	dir := filepath.Join(t.TempDir(), "poo")
	os.MkdirAll(dir, 0o700)
	opts := Options{
		WorkingDir: dir,
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
	waitFor(t, term, promptMark)
	term.press("work\r")
	server.waitPrompts(t, 1)
	// The stream paints when the block settles: the draft typed over the
	// held deltas rides in the editor, and the settle renders the reply.
	term.press("draft")
	term.press(" more")
	close(first)
	close(finish)
	waitFor(t, term, "First chunk")
	waitFor(t, term, "✓ end_turn")
	term.press("\r")
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
	waitFor(t, term, promptMark)
	// The stream paints when the block settles, so the reply's words and
	// the ask's answer meet in the transcript, in the order they rode.
	term.press("look\r")
	close(ask)
	waitScreen(t, term, "allow bash?")
	term.press("y\r")
	// The reply's words render styled in pieces, so the waits read the
	// screen the terminal decoded, not the raw bytes.
	waitScreen(t, term, "All done.")
	waitScreen(t, term, "✓ end_turn")
	waitScreen(t, term, "Let me look.")
	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not quit")
	}
	out := term.screen()
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
	waitFor(t, term, promptMark)
	term.press("go on\r")
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

// TestCloseLeavesNoReaderInTheTerminal covers the read/close handshake
// the session's exit rests on. The close returns only once the read it
// aborted has left the terminal. Run returns behind the close, and
// whatever tears the terminal down after — a test closing its pty —
// races a reader still inside. A read that starts after the close is
// the interrupt, without entering the terminal at all. Every wait the
// test holds is bounded: a broken handshake must cost the test its
// report, not the binary its exit.
func TestCloseLeavesNoReaderInTheTerminal(t *testing.T) {
	term := openTerminal(t)
	restore := takeOverStdio(t, term.slave)
	t.Cleanup(restore)

	m := &machine{in: os.Stdin, out: os.Stdout, r: &renderer{}}
	i := newEditor(m)

	// The reader parks after its read ends, before the handshake
	// reports the read left. A close that waits the read out cannot
	// return while the read still holds the terminal.
	parked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	closed := make(chan struct{})
	// The cleanup releases a reader a failure may have left parked,
	// and closes behind a deadline: the close's own wait has none.
	// A close the deadline catches is given the pty's end — the
	// master goes and the wake tells the editor's loop to look, so
	// the reader reads EIO and leaves while fd 0 still names the
	// pty, and the close behind it finishes before the stdio
	// restore below points fd 0 elsewhere. A close wedged past both
	// stays parked in the handshake's wait, touching no file.
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			term.master.Close()
			i.rl.Keys.RequestRefresh()
			select {
			case <-closed:
			case <-time.After(10 * time.Second):
			}
		}
	})
	remove := i.SetEditorRaceHooks(&editorRaceHooks{
		AfterReadline: func() { close(parked); <-release },
	})
	done := make(chan struct{})
	go func() { defer close(done); i.read() }()
	// The prompt on the terminal says the read holds it.
	waitFor(t, term, promptMark)

	returned := make(chan bool, 1)
	go func() {
		i.close()
		// The handshake's own invariant, at the instant the close
		// returned: the read it aborted has left the terminal. The
		// buffered send lets a close that returns after the test's
		// end report without touching the test itself.
		i.readMu.Lock()
		held := i.readHeld
		i.readMu.Unlock()
		returned <- held
		close(closed)
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		t.Fatal("the read the close aborted never left the terminal")
	}
	// The hooks belong to the first read alone; a read that starts
	// after the close must not reach them.
	remove()
	// A close that waits the read out cannot return while it is
	// held, however loaded the machine. The window is generous
	// against a close that polls instead of waiting, and the report
	// the close goroutine carries below catches a return in the gap
	// between this window and the read's departure.
	select {
	case <-closed:
		t.Fatal("the close returned while the read it aborted still held the terminal")
	case <-time.After(2 * time.Second):
	}
	// The read may leave now; the close behind it must return.
	releaseOnce.Do(func() { close(release) })
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the close never returned once the read had left")
	}
	// The close's own report, at the instant it returned: the read
	// it aborted had left the terminal.
	if held := <-returned; held {
		t.Fatal("the close returned while the read it aborted still held the terminal")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the read the close aborted never returned")
	}

	// A read that starts after the close is the interrupt, without
	// entering the terminal.
	after := make(chan error, 1)
	go func() { _, err := i.read(); after <- err }()
	select {
	case err := <-after:
		if !errors.Is(err, readline.ErrInterrupt) {
			t.Fatalf("a read after the close returned %v, want the interrupt", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a read after the close never returned: it entered the terminal the close had closed")
	}
}

// TestTwoClosesWaitTheSameReadOut covers the read's end waking every
// close that waits it out: one wake must not leave a second close
// parked on the read's departure, for the close's own contract says a
// second close finds the gate shut and the reader gone.
func TestTwoClosesWaitTheSameReadOut(t *testing.T) {
	term := openTerminal(t)
	restore := takeOverStdio(t, term.slave)
	t.Cleanup(restore)

	m := &machine{in: os.Stdin, out: os.Stdout, r: &renderer{}}
	i := newEditor(m)

	parked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	first, second := make(chan struct{}), make(chan struct{})
	// The cleanup joins both closes behind a deadline, the way the
	// handshake test's does, and a close the deadline catches gets
	// the pty's end: no close outlives the stdio restore below to put
	// this pty's termios on a foreign fd.
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		for _, done := range []chan struct{}{first, second} {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				term.master.Close()
				i.rl.Keys.RequestRefresh()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
			}
		}
	})
	waits := make(chan struct{}, 2)
	i.SetEditorRaceHooks(&editorRaceHooks{
		AfterReadline: func() { close(parked); <-release },
		BeforeWait:    func() { waits <- struct{}{} },
	})
	go func() { i.read() }()
	// The prompt on the terminal says the read holds it.
	waitFor(t, term, promptMark)

	go func() { i.close(); close(first) }()
	go func() { i.close(); close(second) }()
	// Both closes must be parked in the handshake's wait before the
	// read leaves — the hook's own signal is the receipt — or the
	// wake this test exists to pin races past the wait entirely.
	for n := 0; n < 2; n++ {
		select {
		case <-waits:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of two closes reached the wait", n)
		}
	}
	// The read may leave now; both closes behind it must return.
	releaseOnce.Do(func() { close(release) })
	for _, done := range []chan struct{}{first, second} {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("a close waiting the held read out never returned")
		}
	}
}

// TestAReadThatHasNotEnteredStillLeavesOnTheClose covers the window
// before the read enters the editor: the close has already sent its
// abort when the read starts. The abort latches in the editor's key
// engine, so the read ends on its own — without the latch, the read
// would park in the terminal and the close would wait a departure that
// never comes, wedging Run's exit.
func TestAReadThatHasNotEnteredStillLeavesOnTheClose(t *testing.T) {
	term := openTerminal(t)
	restore := takeOverStdio(t, term.slave)
	t.Cleanup(restore)

	m := &machine{in: os.Stdin, out: os.Stdout, r: &renderer{}}
	i := newEditor(m)

	claimed := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	closed := make(chan struct{})
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			term.master.Close()
			i.rl.Keys.RequestRefresh()
			select {
			case <-closed:
			case <-time.After(10 * time.Second):
			}
		}
	})
	waits := make(chan struct{}, 1)
	i.SetEditorRaceHooks(&editorRaceHooks{
		BeforeReadline: func() { close(claimed); <-release },
		BeforeWait:     func() { waits <- struct{}{} },
	})
	go func() { i.read() }()
	// The read has claimed the handshake but not entered the editor.
	<-claimed

	go func() { i.close(); close(closed) }()
	// The close must be parked in the handshake's wait — its abort is
	// out — before the read enters the editor behind it. The hook's
	// own signal is the receipt, so the wait ends on the close instead
	// of a poll.
	select {
	case <-waits:
	case <-time.After(10 * time.Second):
		t.Fatal("the close never reached the wait")
	}
	// The read enters behind the abort now, and must leave on its own.
	releaseOnce.Do(func() { close(release) })
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the close never returned: a read that started behind the abort did not leave")
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
	waitFor(t, term, promptMark)
	term.press("work\r")
	// The run must be on before the escape. An escape that arrives while
	// the editor still holds the draft is one the editor eats under
	// load. The run the server opened is the receipt for the line, so
	// the keys after it reach the run. The held deltas paint as the
	// stream's preview, and they can scroll the echo out of the region,
	// so the prompt is what the wait asserts.
	if prompts := server.waitPrompts(t, 1); prompts[0] != "work" {
		t.Fatalf("the session got %v, want \"work\"", prompts)
	}
	term.press("\x1b")
	server.waitCancels(t, 1)
	waitFor(t, term, "✓ cancelled")
	waitTimes(t, term, "world.", 1)

	term.press("next\r")
	// The held deltas paint as the stream's preview while the run
	// holds, so the second run's cancel comes before its reply's words
	// settle; the prompt the server received is the receipt.
	if prompts := server.waitPrompts(t, 2); prompts[1] != "next" {
		t.Fatalf("the session got %v, want \"next\" after the cancel", prompts)
	}
	term.press("\x1b")
	server.waitCancels(t, 2)
	waitTimes(t, term, "✓ cancelled", 2)
	waitTimes(t, term, "world.", 2)
	term.press("/quit\r")

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
	waitFor(t, term, promptMark)
	term.press("work\r")
	// The run must be on before the keys. An escape that arrives while
	// the editor still holds the draft is one the editor eats under
	// load. The run the server opened is the receipt for the line, so
	// the keys after it reach the run.
	if prompts := server.waitPrompts(t, 1); prompts[0] != "work" {
		t.Fatalf("the session got %v, want \"work\"", prompts)
	}
	// The editor dispatches a bare escape once its key buffer holds no
	// more keys, and an arrow the moment its last byte lands, so a poll
	// through the dispatch window is enough to tell them apart: an
	// editor that reads the arrow's escape as Escape cancels inside it.
	term.press("\x1b[B")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		server.mu.Lock()
		cancels := len(server.cancels)
		server.mu.Unlock()
		if cancels != 0 {
			t.Fatalf("an arrow key cancelled the run: %d cancel(s)", cancels)
		}
		time.Sleep(10 * time.Millisecond)
	}

	server.mu.Lock()
	cancels := append([]string(nil), server.cancels...)
	server.mu.Unlock()
	if len(cancels) != 0 {
		t.Fatalf("an arrow key cancelled the run: %v", cancels)
	}
	// The held deltas paint when the run settles, so the cancel is what
	// brings the reply's words onto the screen.
	term.press("\x1b")
	server.waitCancels(t, 1)
	waitTimes(t, term, "world.", 1)

	term.press("\x1b")
	server.waitCancels(t, 1)
}

// TestCtrlCClearsTheLineThenLeavesTheSession covers Ctrl-C: a line it
// clears is not sent, the session goes on, and an empty prompt is the
// person leaving.
func TestCtrlCClearsTheLineThenLeavesTheSession(t *testing.T) {
	server := &fakeServer{t: t, holdRun: true}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("half typed")
	waitFor(t, term, "half typed")
	term.press("\x03")
	waitFor(t, term, "^C")

	term.press("hello\r")
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

// TestCtrlJBreaksTheDraftAndEnterSubmitsItWhole covers the line break:
// Ctrl-J puts a line feed into the draft instead of submitting it, and
// the Enter after submits the draft whole, newlines inside.
func TestCtrlJBreaksTheDraftAndEnterSubmitsItWhole(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)

	term.press("first line")
	// Ctrl-J is the line feed byte: the draft grows a line instead of
	// the prompt going.
	term.press("\x0a")
	term.press("second line")
	waitScreen(t, term, "second line")
	if prompts := server.promptsSoFar(); len(prompts) != 0 {
		t.Fatalf("the session got %v before the Enter, want nothing sent", prompts)
	}

	term.press("\r")
	if prompts := server.waitPrompts(t, 1); prompts[0] != "first line\nsecond line" {
		t.Fatalf("the submitted draft is %q, want both lines whole", prompts[0])
	}

	// The session's own run asks about its tool; the y answers it and
	// the settle closes the run the quit leaves behind.
	waitScreen(t, term, "allow bash?")
	term.press("y\r")
	waitScreen(t, term, "✓ end_turn")

	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d, want 0", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl did not quit; it drew:\n%s", term.screen())
	}
}

// TestAPermissionAskTakesTheAnswerTypedBehindIt covers the ask over a
// terminal: the decision is the y typed behind it, and the run settles.
func TestAPermissionAskTakesTheAnswerTypedBehindIt(t *testing.T) {
	server := &fakeServer{t: t}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("rm the lot\r")
	waitFor(t, term, "allow bash?")
	term.press("y\r")
	waitFor(t, term, "→ allow")
	waitFor(t, term, "✓ end_turn")
	term.press("/quit\r")

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
	waitFor(t, term, promptMark)
	term.press("work\r")
	server.waitPrompts(t, 1)
	<-server.staleSettled
	// The stale settle has gone out; the run it named is not this
	// one, so the client still holds a run for Escape to stop.
	term.press("\x1b")
	if got := server.waitCancels(t, 1); got[0] != "r1" {
		t.Fatalf("the cancel named %q, want the live run r1", got[0])
	}
	waitFor(t, term, "✓ cancelled")
	term.press("/quit\r")
	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d, want 0", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl never left; it drew:\n%s", term.String())
	}
}

// TestTheFramePaintsAQuietDeltaWithoutAnotherEvent covers the coalesced
// frame over a real terminal: the run is held open after a later delta,
// and the delta still reaches the screen before another event or the
// settle, because the run loop's frame paints it.
func TestTheFramePaintsAQuietDeltaWithoutAnotherEvent(t *testing.T) {
	later := make(chan struct{})
	hold := make(chan struct{})
	server := &fakeServer{t: t}
	server.script = func(s *fakeServer) {
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "First chunk ",
		})
		<-later
		s.event("message.delta", map[string]any{
			"run_id": "r1", "message_id": "m1", "kind": "text", "text": "SECONDCHUNK",
		})
		<-hold
		s.event("run.settled", map[string]any{"run_id": "r1", "reason": "end_turn"})
	}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("work\r")
	// The reply's words render styled in pieces, so the waits read the
	// screen the terminal decoded, not the raw bytes.
	waitScreen(t, term, "First chunk")
	close(later)
	// No delta and no settle can paint it: the frame is the only writer.
	waitScreen(t, term, "SECONDCHUNK")
	close(hold)
	waitFor(t, term, "✓ end_turn")
	term.press("/quit\r")

	select {
	case got := <-code:
		if got != 0 {
			t.Fatalf("the repl exited %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("the repl never left after /quit; it drew:\n%s", term.String())
	}
}

// TestARefusedPromptTakesTheHintBack covers a run the server refuses
// before its first delta: the hint the run showed comes off the
// terminal, and the failure reads alone.
func TestARefusedPromptTakesTheHintBack(t *testing.T) {
	server := &fakeServer{t: t, refusePrompt: true}
	term, code := startTerminalRepl(t, server)
	waitFor(t, term, promptMark)
	term.press("go\r")
	waitFor(t, term, "busy")
	// The failure and the redraw the hint's removal drives settle in
	// the drawn stream before the screen reads whole.
	time.Sleep(200 * time.Millisecond)
	emu := vt10x.New(vt10x.WithSize(80, 24))
	emu.Write([]byte(term.String()))
	if strings.Contains(emu.String(), "writing") {
		t.Errorf("the hint stood over the refusal:\n%s", emu.String())
	}
	if !strings.Contains(emu.String(), "✗") {
		t.Errorf("the refusal never read:\n%s", emu.String())
	}
	term.press("\x03")
	select {
	case <-code:
	case <-time.After(10 * time.Second):
		t.Fatal("the repl did not exit")
	}
}
