package bash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/tool/bash"
)

// TestTheExecutorCapturesTheStreamsApart covers the contract a tool needs
// when the two streams are read differently: a command that names a
// writer for stderr has them captured separately, and one that does not
// keeps the interleaved stream.
func TestTheExecutorCapturesTheStreamsApart(t *testing.T) {
	ctx := context.Background()
	command := bash.Command{
		Argv: []string{"/bin/sh", "-c", "echo to-the-stdout; echo to-the-stderr 1>&2"},
		Env:  []string{"PATH=/usr/bin:/bin"},
	}

	t.Run("apart", func(t *testing.T) {
		var out, errOut strings.Builder
		command.Stderr = &errOut
		proc, err := bash.NewLocal().Start(ctx, command, &out)
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
		proc, err := bash.NewLocal().Start(ctx, command, &out)
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
