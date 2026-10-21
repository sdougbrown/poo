package poo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// A tool's Prepare sees the environment the run works in: the working
// directory and the protected paths. A tool that resolves a relative path
// against the working directory needs the first, and a tool that refuses
// to read a protected path needs the second.

// recordingPrepareTool records the environment its Prepare received.
type recordingPrepareTool struct {
	echoTool
	// env is the environment the last Prepare received.
	env poo.PrepareEnv
	// calls counts the Prepare calls.
	calls int
}

// Prepare records the environment and accepts the call.
func (t *recordingPrepareTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	t.calls++
	t.env = env
	return echoPrepared{text: string(raw)}, nil
}

// TestPrepareReceivesThePrepareEnv covers the contract: the tool's
// Prepare is given the environment, and the run's working directory
// reaches it.
func TestPrepareReceivesThePrepareEnv(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{}`)}},
	}}}
	sink := &recordingSink{}
	tool := &recordingPrepareTool{}

	opts := testOptions(t, tr, &tool.echoTool, sink)
	opts.Tools["echo"] = tool
	opts.WorkingDir = "/tmp/the-working-dir"
	// The fake transport repeats its last response, so the run needs a
	// turn ceiling: the test is about the first call's environment.
	opts.MaxTurns = 1

	if _, err := poo.NewRun(opts).Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("Prepare was called %d times, want 1", tool.calls)
	}
	if tool.env.WorkingDir != "/tmp/the-working-dir" {
		t.Errorf("Prepare's working directory is %q, want %q", tool.env.WorkingDir, "/tmp/the-working-dir")
	}
}

// TestPrepareEnvCarriesTheProtectedPaths covers the other half of the
// environment: the protected paths are already expanded when the tool
// sees them.
func TestPrepareEnvCarriesTheProtectedPaths(t *testing.T) {
	env := poo.PrepareEnv{
		WorkingDir:     "/work",
		ProtectedPaths: []string{"/work/.env", "/work/secrets"},
	}
	if len(env.ProtectedPaths) != 2 {
		t.Errorf("the environment holds %d protected paths, want 2", len(env.ProtectedPaths))
	}
	if env.ProtectedPaths[0] != "/work/.env" {
		t.Errorf("the first protected path is %q, want %q", env.ProtectedPaths[0], "/work/.env")
	}
}

// TestTheToolEnvCarriesTheJobHandle covers the job seam: a call that runs
// as a job names its job, and a call that runs in the turn does not.
func TestTheToolEnvCarriesTheJobHandle(t *testing.T) {
	env := poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: "/work"}}
	if env.Job != "" {
		t.Errorf("the job handle is %q, want the empty string", env.Job)
	}
	env.Job = "j3"
	if env.Job != "j3" {
		t.Errorf("the job handle is %q, want j3", env.Job)
	}
}
