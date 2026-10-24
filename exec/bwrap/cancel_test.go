package bwrap_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo/exec"
)

// TestAnAlreadyCancelledSandboxStartRunsNothing covers the executor's
// promise: a command whose context is over does not get a sandbox, so
// nothing of it runs, not even for the moment it takes to stop it.
func TestAnAlreadyCancelledSandboxStartRunsNothing(t *testing.T) {
	executor, err := newReviewExecutor(t)
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proc, err := executor.Start(ctx, exec.Command{
		Argv: []string{"/bin/sh", "-c", `printf executed > "$1"`, "probe", marker},
		Dir:  dir,
	}, io.Discard)
	if err == nil {
		proc.Kill()
		_, _ = proc.Wait()
		t.Fatalf("the executor started a sandbox for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want a cancellation", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the cancelled command ran and wrote %s", marker)
	}
}
