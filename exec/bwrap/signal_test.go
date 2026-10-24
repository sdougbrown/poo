package bwrap_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/tool/bash"
)

// TestASignalDeathIsReportedTheSameWayByBothExecutors covers the plan's
// envelope: returncode is null when the process was killed, whichever
// executor ran it.
func TestASignalDeathIsReportedTheSameWayByBothExecutors(t *testing.T) {
	sandbox, err := newReviewExecutor(t)
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	for _, c := range []struct {
		name string
		exec exec.Executor
	}{
		{"local", bash.NewLocal()},
		{"bwrap", sandbox},
	} {
		t.Run(c.name, func(t *testing.T) {
			tool := bash.New(bash.WithExecutor(c.exec))
			dir := t.TempDir()
			raw, err := json.Marshal(map[string]string{"command": "kill -9 $$"})
			if err != nil {
				t.Fatalf("marshalling: %v", err)
			}
			prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
			if err != nil {
				t.Fatalf("preparing: %v", err)
			}
			res, err := tool.Call(t.Context(), prepared, poo.ToolEnv{
				PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
				Env:        os.Environ(),
			})
			if err != nil {
				t.Fatalf("calling: %v", err)
			}
			t.Logf("%s: text=%s isError=%v", c.name, res.Text, res.IsError)
			var obs map[string]any
			if err := json.Unmarshal(res.Structured, &obs); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if rc := obs["returncode"]; rc != nil {
				t.Errorf("the %s executor reports returncode %v, want null for a killed process", c.name, rc)
			}
			if !res.IsError {
				t.Errorf("the %s executor does not report the killed command as an error", c.name)
			}
		})
	}
}

var _ = bwrap.CodeUnavailable
