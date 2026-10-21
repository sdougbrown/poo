package bash

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// result is what a command produced.
type result struct {
	// returncode is the command's exit status, nil when the process was
	// killed by a signal.
	returncode *int
	// timedOut reports that the command hit its timeout.
	timedOut bool
	// cancelled reports that the run's context ended the command.
	cancelled bool
	// output is the captured output, truncated to the capture cap.
	output string
}

// run executes a command in its own process group and collects its output.
//
// The command gets a fresh shell, a null stdin and the given environment.
// stdout and stderr share one pipe, so the output arrives interleaved.
//
// Whatever is still alive in the command's process group when the command
// ends is killed before the call returns, so no process outlives the call.
func run(ctx context.Context, command, dir string, env []string, timeout time.Duration) (result, error) {
	cmd := exec.Command("/bin/bash", "-c", command)
	cmd.Dir = dir
	// A nil Stdin makes the process read from the null device.
	cmd.Stdin = nil
	cmd.Env = env
	// The command runs in its own process group, so the whole group can be
	// signalled at once.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	pr, pw, err := os.Pipe()
	if err != nil {
		return result{}, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return result{}, err
	}
	// The parent's copy of the write end is closed, so the pipe reaches
	// EOF once every writer is gone.
	pw.Close()

	pgid := cmd.Process.Pid

	buf := &cappedBuffer{limit: CaptureCap}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(buf, pr)
	}()

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var res result
	// exited reports whether the command's result has been collected.
	exited := false

	select {
	case <-waited:
		// The shell exited on its own. Whatever it left behind in the
		// group is still killed below.
		exited = true
	case <-timer.C:
		res.timedOut = true
		terminateGroup(pgid)
	case <-ctx.Done():
		res.cancelled = true
		terminateGroup(pgid)
	}

	// Wait for the command, so the exit status is known before the
	// observation is built.
	if !exited {
		select {
		case <-waited:
		case <-time.After(KillGrace):
			killGroup(pgid, syscall.SIGKILL)
			<-waited
		}
	}

	// Nothing may outlive the call: SIGTERM the group, then SIGKILL it
	// once the grace period is up.
	terminateGroup(pgid)

	// A descendant may still hold the pipe open. Wait for the drain, then
	// close the pipe so the goroutine cannot hang.
	select {
	case <-drained:
	case <-time.After(KillGrace):
		killGroup(pgid, syscall.SIGKILL)
		pr.Close()
		<-drained
	}
	pr.Close()

	res.output = buf.String()
	if cmd.ProcessState != nil {
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			res.returncode = &code
		}
	}
	return res, nil
}

// terminateGroup kills a process group and returns when the group is gone.
//
// The group gets SIGTERM, then SIGKILL after the grace period. The wait is
// bounded, so the call cannot hang on a process that ignores SIGTERM.
func terminateGroup(pgid int) {
	killGroup(pgid, syscall.SIGTERM)
	deadline := time.Now().Add(KillGrace)
	for time.Now().Before(deadline) {
		if !groupAlive(pgid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	killGroup(pgid, syscall.SIGKILL)
}

// groupAlive reports whether any process is left in the group.
func groupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	// Signal 0 checks for existence without disturbing the process.
	return syscall.Kill(-pgid, syscall.Signal(0)) == nil
}

// killGroup signals a whole process group. A group that is already gone is
// not an error.
func killGroup(pgid int, sig syscall.Signal) {
	if pgid <= 0 {
		return
	}
	// A negative pid signals the process group.
	_ = syscall.Kill(-pgid, sig)
}

// cappedBuffer collects output up to a limit and discards the rest.
//
// Write always reports the whole input as written, so the reader draining
// the pipe never blocks on a full buffer.
type cappedBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

// Write appends to the buffer until it is full, then discards.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < b.limit {
		n := b.limit - len(b.buf)
		if n > len(p) {
			n = len(p)
		}
		b.buf = append(b.buf, p[:n]...)
	}
	return len(p), nil
}

// String returns the captured output.
//
// A cut that lands in the middle of a character drops the partial one.
// Bytes the command printed are otherwise kept as they are, valid UTF-8
// or not.
func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < b.limit {
		return string(b.buf)
	}
	return trimCutRune(string(b.buf))
}

// trimCutRune drops the start of a multi-byte rune that a cut left
// incomplete at the end of a string.
func trimCutRune(s string) string {
	// A rune is at most utf8.UTFMax bytes, so its start is within the
	// last few bytes.
	for i := len(s) - 1; i >= 0 && i >= len(s)-utf8.UTFMax; i-- {
		if utf8.RuneStart(s[i]) {
			if s[i] >= utf8.RuneSelf && !utf8.FullRuneInString(s[i:]) {
				return s[:i]
			}
			return s
		}
	}
	return s
}

// ChildEnv returns the environment for the child process.
//
// A nil base means the parent's own environment. The scrubbed names are
// removed from whichever base is used, so an API key never reaches the
// shell.
func ChildEnv(base []string, scrub []string) []string {
	if len(scrub) == 0 && base == nil {
		return nil
	}
	if base == nil {
		base = os.Environ()
	}
	if len(scrub) == 0 {
		return base
	}
	drop := make(map[string]bool, len(scrub))
	for _, name := range scrub {
		drop[name] = true
	}
	out := make([]string, 0, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if drop[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
