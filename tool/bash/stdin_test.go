package bash_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pooexec "github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/tool/bash"
)

// stdinPtr returns a pointer to the input, the way a plan that binds input
// carries it.
func stdinPtr(s string) *string { return &s }

// runLocal starts a command on the local executor and returns its exit code,
// the wait error and what it wrote.
func runLocal(t *testing.T, c pooexec.Command) (int, error, string) {
	t.Helper()
	var out bytes.Buffer
	proc, err := bash.NewLocal().Start(context.Background(), c, &out)
	if err != nil {
		t.Fatalf("starting the command: %v", err)
	}
	code, waitErr := proc.Wait()
	return code, waitErr, out.String()
}

// TestLocalNilStdinIsTheNullDevice covers the absent input: a command with no
// bound stdin reads the null device, not an open pipe.
func TestLocalNilStdinIsTheNullDevice(t *testing.T) {
	code, err, out := runLocal(t, pooexec.Command{
		Argv: []string{"/bin/sh", "-c", "readlink /proc/self/fd/0"},
	})
	if err != nil || code != 0 {
		t.Fatalf("the command exited %d: %v", code, err)
	}
	if got := strings.TrimSpace(out); got != "/dev/null" {
		t.Errorf("stdin is %q, want the null device", got)
	}
}

// TestLocalEmptyStdinIsAnOpenPipe covers present-but-empty input: the command
// gets a pipe that reaches EOF at once, not the null device.
func TestLocalEmptyStdinIsAnOpenPipe(t *testing.T) {
	code, err, out := runLocal(t, pooexec.Command{
		Argv:  []string{"/bin/sh", "-c", "readlink /proc/self/fd/0"},
		Stdin: stdinPtr(""),
	})
	if err != nil || code != 0 {
		t.Fatalf("the command exited %d: %v", code, err)
	}
	if got := strings.TrimSpace(out); !strings.HasPrefix(got, "pipe:") {
		t.Errorf("stdin is %q, want a pipe", got)
	}
}

// TestLocalDeliversStdinExactly covers the bound input: every byte arrives,
// including newlines and characters outside ASCII, and a large payload is not
// cut short.
func TestLocalDeliversStdinExactly(t *testing.T) {
	large := strings.Repeat("x", 1<<20)
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"text", "secret\n雪\n"},
		{"large", large},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err, out := runLocal(t, pooexec.Command{
				Argv:  []string{"/bin/cat"},
				Stdin: stdinPtr(tc.input),
			})
			if err != nil || code != 0 {
				t.Fatalf("the command exited %d: %v", code, err)
			}
			if out != tc.input {
				t.Errorf("the command read %d bytes, want %d", len(out), len(tc.input))
			}
		})
	}
}

// TestLocalReportsStdinTheCommandDidNotRead covers a command that exits
// successfully without taking its input: the call does not hang and the
// incomplete delivery is its own error.
func TestLocalReportsStdinTheCommandDidNotRead(t *testing.T) {
	input := strings.Repeat("secret", 1<<18)
	start := time.Now()
	code, err := startLocal(t, pooexec.Command{
		Argv:  []string{"/bin/sh", "-c", "exit 0"},
		Stdin: &input,
	})
	elapsed := time.Since(start)
	if code != 0 {
		t.Fatalf("the command exited %d", code)
	}
	if !errors.Is(err, pooexec.ErrStdinIncomplete) {
		t.Fatalf("the wait error is %v, want an incomplete stdin", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Error("the wait error leaked the input")
	}
	if elapsed > 3*time.Second {
		t.Errorf("the call took %s waiting for input the command never read", elapsed)
	}
}

// TestLocalReportsIncompleteStdinAlongsideAFailedChild covers the error
// contract: a failed command keeps its exit error and code, and the input it
// did not take is still observable as ErrStdinIncomplete.
func TestLocalReportsIncompleteStdinAlongsideAFailedChild(t *testing.T) {
	input := strings.Repeat("secret", 1<<18)
	code, err := startLocal(t, pooexec.Command{
		Argv:  []string{"/bin/sh", "-c", "exit 3"},
		Stdin: &input,
	})
	if code != 3 {
		t.Fatalf("the command exited %d, want 3", code)
	}
	if err == nil {
		t.Fatal("a failed command returned no error")
	}
	if !errors.Is(err, pooexec.ErrStdinIncomplete) {
		t.Errorf("the wait error %v does not report the incomplete input", err)
	}
	var exitErr *osexec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the wait error %v lost the child's exit error", err)
	}
	if got := exitErr.ExitCode(); got != 3 {
		t.Errorf("the exit error has code %d, want 3", got)
	}
}

// TestLocalCancellationDoesNotWaitForStdin covers the context ending while a
// command is not reading its input: the call stops promptly and the
// cancellation is the outcome, not an input error.
func TestLocalCancellationDoesNotWaitForStdin(t *testing.T) {
	input := strings.Repeat("x", 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := bash.NewLocal().Start(ctx, pooexec.Command{
		Argv:  []string{"/bin/sh", "-c", "sleep 30"},
		Stdin: &input,
	}, io.Discard)
	if err != nil {
		t.Fatalf("starting the command: %v", err)
	}
	cancel()
	start := time.Now()
	code, waitErr := proc.Wait()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("cancellation waited %s for unread input", elapsed)
	}
	if !proc.Stopped() {
		t.Error("the command was not reported as stopped")
	}
	if code >= 0 {
		t.Errorf("a stopped command has code %d, want none", code)
	}
	if errors.Is(waitErr, pooexec.ErrStdinIncomplete) {
		t.Error("the input error replaced the cancellation")
	}
}

// TestLocalEscapedReaderDoesNotHoldWait covers a descendant that left the
// process group while holding the input pipe: it cannot hold Wait, and the
// input that no command reads is reported as incomplete.
func TestLocalEscapedReaderDoesNotHoldWait(t *testing.T) {
	if _, err := osexec.LookPath("setsid"); err != nil {
		t.Skip("skip: setsid is not installed")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	input := strings.Repeat("x", 1<<20)
	script := `setsid /bin/sleep 30 <&0 >/dev/null 2>&1 & echo $! > "$1"; exit 0`
	t.Cleanup(func() { killEscaped(t, pidFile) })
	start := time.Now()
	code, err := startLocal(t, pooexec.Command{
		Argv:  []string{"/bin/sh", "-c", script, "probe", pidFile},
		Stdin: &input,
	})
	elapsed := time.Since(start)
	if code != 0 {
		t.Fatalf("the command exited %d: %v", code, err)
	}
	if !errors.Is(err, pooexec.ErrStdinIncomplete) {
		t.Fatalf("the wait error is %v, want an incomplete stdin", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("the call waited %s for an escaped reader", elapsed)
	}
}

// TestLocalStartFailureReleasesStdin covers a command that never starts: the
// start reports the failure instead of leaving the input delivery behind.
func TestLocalStartFailureReleasesStdin(t *testing.T) {
	input := strings.Repeat("x", 1<<20)
	start := time.Now()
	proc, err := bash.NewLocal().Start(context.Background(), pooexec.Command{
		Argv:  []string{"/nonexistent/poo-stdin-test"},
		Stdin: &input,
	}, io.Discard)
	if err == nil {
		proc.Kill()
		_, _ = proc.Wait()
		t.Fatal("a missing binary started")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the failed start waited %s on its input", elapsed)
	}
}

// startLocal runs a command on the local executor and returns its exit code
// and wait error.
func startLocal(t *testing.T, c pooexec.Command) (int, error) {
	t.Helper()
	proc, err := bash.NewLocal().Start(context.Background(), c, io.Discard)
	if err != nil {
		t.Fatalf("starting the command: %v", err)
	}
	return proc.Wait()
}

// killEscaped stops the process whose pid the command wrote, so a test that
// deliberately escapes the group does not leave it behind.
func killEscaped(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = process.Kill()
}
