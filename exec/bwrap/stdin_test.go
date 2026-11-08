package bwrap_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	osexec "os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
)

func runSandboxStdin(t *testing.T, executor *bwrap.Executor, c exec.Command) (int, string, error) {
	t.Helper()
	var out bytes.Buffer
	proc, err := executor.Start(context.Background(), c, &out)
	if err != nil {
		t.Fatalf("starting the sandboxed command: %v", err)
	}
	code, waitErr := proc.Wait()
	return code, out.String(), waitErr
}

// TestSandboxNilStdinIsTheNullDevice covers absent input in the sandbox: the
// command reads the null device, not an open pipe.
func TestSandboxNilStdinIsTheNullDevice(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	code, out, err := runSandboxStdin(t, executor, exec.Command{
		Argv: []string{"/bin/sh", "-c", "readlink /proc/self/fd/0"},
		Dir:  t.TempDir(),
	})
	if err != nil || code != 0 {
		t.Fatalf("the command exited %d: %v", code, err)
	}
	if got := strings.TrimSpace(out); got != "/dev/null" {
		t.Errorf("stdin is %q, want the null device", got)
	}
}

// TestSandboxEmptyStdinIsAnOpenPipe covers present-but-empty input in the
// sandbox: the command gets a pipe that reaches EOF at once.
func TestSandboxEmptyStdinIsAnOpenPipe(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	empty := ""
	code, out, err := runSandboxStdin(t, executor, exec.Command{
		Argv:  []string{"/bin/sh", "-c", "readlink /proc/self/fd/0"},
		Dir:   t.TempDir(),
		Stdin: &empty,
	})
	if err != nil || code != 0 {
		t.Fatalf("the command exited %d: %v", code, err)
	}
	if got := strings.TrimSpace(out); !strings.HasPrefix(got, "pipe:") {
		t.Errorf("stdin is %q, want a pipe", got)
	}
}

// TestSandboxDeliversStdinExactly covers the sandbox's input path: every byte
// arrives, including newlines and characters outside ASCII, and a large
// payload crosses the sandbox intact.
func TestSandboxDeliversStdinExactly(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
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
			code, out, err := runSandboxStdin(t, executor, exec.Command{
				Argv:  []string{"/bin/cat"},
				Dir:   t.TempDir(),
				Stdin: &tc.input,
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

// TestSandboxReportsStdinTheCommandDidNotRead covers a sandboxed command that
// exits successfully without taking its input: the call does not hang and the
// incomplete delivery is its own error.
func TestSandboxReportsStdinTheCommandDidNotRead(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	input := strings.Repeat("secret", 1<<18)
	start := time.Now()
	code, _, err := runSandboxStdin(t, executor, exec.Command{
		Argv:  []string{"/bin/sh", "-c", "exit 0"},
		Dir:   t.TempDir(),
		Stdin: &input,
	})
	elapsed := time.Since(start)
	if code != 0 {
		t.Fatalf("the command exited %d", code)
	}
	if !errors.Is(err, exec.ErrStdinIncomplete) {
		t.Fatalf("the wait error is %v, want an incomplete stdin", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Error("the wait error leaked the input")
	}
	if elapsed > 5*time.Second {
		t.Errorf("the call took %s waiting for input the command never read", elapsed)
	}
}

// TestSandboxReportsIncompleteStdinAlongsideAFailedChild covers the sandbox's
// error contract: a failed command keeps its exit error and code, and the
// input it did not take is still observable as ErrStdinIncomplete.
func TestSandboxReportsIncompleteStdinAlongsideAFailedChild(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	input := strings.Repeat("secret", 1<<18)
	code, _, err := runSandboxStdin(t, executor, exec.Command{
		Argv:  []string{"/bin/sh", "-c", "exit 3"},
		Dir:   t.TempDir(),
		Stdin: &input,
	})
	if code != 3 {
		t.Fatalf("the command exited %d, want 3", code)
	}
	if err == nil {
		t.Fatal("a failed command returned no error")
	}
	if !errors.Is(err, exec.ErrStdinIncomplete) {
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

// TestSandboxCancellationDoesNotWaitForStdin covers the context ending while
// a sandboxed command is not reading its input: the call stops promptly and
// the cancellation is the outcome, not an input error.
func TestSandboxCancellationDoesNotWaitForStdin(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	input := strings.Repeat("x", 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := executor.Start(ctx, exec.Command{
		Argv:  []string{"/bin/sh", "-c", "sleep 30"},
		Dir:   t.TempDir(),
		Stdin: &input,
	}, io.Discard)
	if err != nil {
		cancel()
		t.Fatalf("starting the sandboxed command: %v", err)
	}
	// Let the command reach its own run before the context ends: the
	// immediate case races the sandbox's startup, which is not what this
	// test covers.
	time.Sleep(100 * time.Millisecond)
	cancel()
	start := time.Now()
	code, waitErr := proc.Wait()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation waited %s for unread input", elapsed)
	}
	if !proc.Stopped() {
		t.Error("the command was not reported as stopped")
	}
	if code >= 0 {
		t.Errorf("a stopped command has code %d, want none", code)
	}
	if errors.Is(waitErr, exec.ErrStdinIncomplete) {
		t.Error("the input error replaced the cancellation")
	}
}
