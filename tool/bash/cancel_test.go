package bash_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestAnAlreadyCancelledLocalStartRunsNothing covers the executor's
// promise: a command whose context is over is not started, so nothing of
// it runs, not even for the moment it takes to stop it.
func TestAnAlreadyCancelledLocalStartRunsNothing(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proc, err := bash.NewLocal().Start(ctx, exec.Command{
		Argv: []string{"/bin/sh", "-c", `printf executed > "$1"`, "probe", marker},
		Dir:  dir,
	}, io.Discard)
	if err == nil {
		proc.Kill()
		_, _ = proc.Wait()
		t.Fatalf("the executor started a command for a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want a cancellation", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the cancelled command ran and wrote %s", marker)
	}
}

// startedExecutor counts the commands an executor was asked to start.
type startedExecutor struct {
	exec.Executor
	starts atomic.Int64
}

// Start counts the command and hands it to the executor it wraps.
func (s *startedExecutor) Start(ctx context.Context, c exec.Command, out io.Writer) (exec.Process, error) {
	s.starts.Add(1)
	return s.Executor.Start(ctx, c, out)
}

// TestAnAlreadyCancelledCallIsReportedAsCancelled covers the tool: a call
// made with a context that has already ended starts nothing and is the
// cancel observation, not an error the run has to classify.
func TestAnAlreadyCancelledCallIsReportedAsCancelled(t *testing.T) {
	dir := t.TempDir()
	executor := &startedExecutor{Executor: bash.NewLocal()}
	tool := &bash.Tool{Timeout: 30 * time.Second, Executor: executor}
	prepared, err := tool.Prepare([]byte(`{"command":"printf executed > marker"}`), poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := tool.Call(ctx, prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if got := executor.starts.Load(); got != 0 {
		t.Errorf("the executor was asked to start %d commands for a cancelled call", got)
	}
	if !res.IsError {
		t.Errorf("a cancelled call is not an error: %s", res.Text)
	}
	obs := structured(t, res)
	if timedOut, _ := obs["timed_out"].(bool); timedOut {
		t.Errorf("a cancelled call reported a timeout: %s", res.Text)
	}
	if rc, ok := obs["returncode"]; ok && rc != nil {
		t.Errorf("returncode is %v, want null for a cancelled call", rc)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); err == nil {
		t.Errorf("the cancelled call ran the command")
	}
}
