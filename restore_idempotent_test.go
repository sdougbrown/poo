package poo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// idempotentTool is a tool that declares itself idempotent.
type idempotentTool struct {
	echoTool
	// idempotent is the tool's idempotent flag.
	idempotent bool
}

// Spec describes the tool with its idempotent flag.
func (t *idempotentTool) Spec() poo.ToolSpec {
	spec := t.echoTool.Spec()
	spec.Idempotent = t.idempotent
	return spec
}

// TestTheRestoreRejectsALostIdempotentFlag covers the compatibility
// check: a recorded tool that was idempotent and is not any more does
// not fit.
func TestTheRestoreRejectsALostIdempotentFlag(t *testing.T) {
	created := header("s1")
	created.Tools[0].Idempotent = true
	records := []poo.Record{created}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Tools["echo"] = &idempotentTool{echoTool: echoTool{}}
	_, _, err := poo.Restore(opts, records)
	if err == nil {
		t.Fatal("the restore accepted a tool that lost its idempotent flag")
	}
	var incompatible *poo.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("the restore returned %T, want an incompatible error", err)
	}
	if len(incompatible.Differences) != 1 {
		t.Fatalf("the error names %d differences, want 1: %v", len(incompatible.Differences), incompatible.Differences)
	}
	if !strings.Contains(incompatible.Differences[0], "echo") {
		t.Errorf("the difference does not name the tool: %q", incompatible.Differences[0])
	}
	if !strings.Contains(incompatible.Differences[0], "idempotent") {
		t.Errorf("the difference does not name the flag: %q", incompatible.Differences[0])
	}
}

// TestTheRestoreKeepsAGainedIdempotentFlag covers the asymmetric rule:
// a record written before the tool declared itself idempotent says
// nothing about it, so gaining the flag is not a difference. An absent
// flag and a false flag are the same on the wire.
func TestTheRestoreKeepsAGainedIdempotentFlag(t *testing.T) {
	records := []poo.Record{header("s1")}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Tools["echo"] = &idempotentTool{echoTool: echoTool{}, idempotent: true}
	session, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
}
