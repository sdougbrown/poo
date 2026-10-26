package bwrap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
)

// TestTheSandboxCapturesTheStreamsApart covers the sandbox's side of the
// executor contract: a command that names a writer for stderr has the
// two streams captured separately, and one that does not keeps the
// interleaved stream.
func TestTheSandboxCapturesTheStreamsApart(t *testing.T) {
	executor := newExecutor(t, bwrap.Options{})
	ctx := context.Background()
	dir := t.TempDir()
	command := exec.Command{
		Argv: []string{"/bin/sh", "-c", "echo to-the-stdout; echo to-the-stderr 1>&2"},
		Dir:  dir,
	}

	t.Run("apart", func(t *testing.T) {
		var out, errOut strings.Builder
		command.Stderr = &errOut
		proc, err := executor.Start(ctx, command, &out)
		if err != nil {
			t.Fatalf("starting the command: %v", err)
		}
		if code, err := proc.Wait(); err != nil || code != 0 {
			t.Fatalf("the command exited %d: %v", code, err)
		}
		if got := out.String(); got != "to-the-stdout\n" {
			t.Errorf("stdout is %q, want only what the command wrote there", got)
		}
		if got := errOut.String(); got != "to-the-stderr\n" {
			t.Errorf("stderr is %q, want only what the command wrote there", got)
		}
	})

	t.Run("merged", func(t *testing.T) {
		var out strings.Builder
		command.Stderr = nil
		proc, err := executor.Start(ctx, command, &out)
		if err != nil {
			t.Fatalf("starting the command: %v", err)
		}
		if code, err := proc.Wait(); err != nil || code != 0 {
			t.Fatalf("the command exited %d: %v", code, err)
		}
		if got := out.String(); got != "to-the-stdout\nto-the-stderr\n" {
			t.Errorf("the merged stream is %q, want both lines", got)
		}
	})
}
