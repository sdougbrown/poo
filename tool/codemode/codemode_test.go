package codemode_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
)

// fakeEngine records the runs the tool asked for and answers them.
type fakeEngine struct {
	opts   codemode.RunOptions
	result codemode.RunResult
	err    error
	runs   int
}

func (e *fakeEngine) Name() string { return "fake" }

func (e *fakeEngine) Run(ctx context.Context, opts codemode.RunOptions) (codemode.RunResult, error) {
	e.runs++
	e.opts = opts
	return e.result, e.err
}

// noopNested is a nested caller the tool tests pass; the fake engine
// never uses it.
type noopNested struct{}

func (noopNested) Call(ctx context.Context, parent, tool string, raw json.RawMessage) (poo.NestedResult, error) {
	return poo.NestedResult{}, nil
}

// env builds the environment a call runs in, with the nested seam.
func env(nested poo.NestedCaller, settings poo.ToolSettings) poo.ToolEnv {
	return poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: "/tmp"},
		Handle:     "r7",
		Nested:     nested,
		Settings:   settings,
	}
}

// TestTheToolRunsTheScriptOnItsEngine covers the wiring: the tool hands
// the engine the script, the seam and the parent handle, and shapes the
// result the model sees.
func TestTheToolRunsTheScriptOnItsEngine(t *testing.T) {
	engine := &fakeEngine{result: codemode.RunResult{
		Output: []string{"line one", "line two"},
		Value:  json.RawMessage(`{"answer": 42}`),
		Calls:  3,
	}}
	tool := toolcodemode.New(engine)
	prepared, err := tool.Prepare(json.RawMessage(`{"script":"print('hi')"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, env(noopNested{}, poo.ToolSettings{}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.IsError {
		t.Errorf("the result is an error: %s", res.Text)
	}
	for _, want := range []string{"3 call(s)", "line one", "line two", `{"answer": 42}`} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the result is %q, want it to name %q", res.Text, want)
		}
	}
	if engine.opts.Script != "print('hi')" {
		t.Errorf("the engine got the script %q", engine.opts.Script)
	}
	if engine.opts.Parent != "r7" {
		t.Errorf("the engine got the parent %q, want r7", engine.opts.Parent)
	}
	if engine.opts.Nested == nil {
		t.Error("the engine got no nested caller")
	}
}

// TestTheToolLimitsComeFromTheSettings covers the settings: an unset
// limit takes the session's, and the tool's own wins over both.
func TestTheToolLimitsComeFromTheSettings(t *testing.T) {
	engine := &fakeEngine{}
	tool := toolcodemode.New(engine)
	prepared, _ := tool.Prepare(json.RawMessage(`{"script":"x=1"}`), poo.PrepareEnv{})
	settings := poo.ToolSettings{CodemodeTimeoutS: 30, CodemodeScriptMaxBytes: 1000, CodemodeOutputCapChars: 500, CodemodeExecSteps: 999}
	if _, err := tool.Call(context.Background(), prepared, env(noopNested{}, settings)); err != nil {
		t.Fatalf("calling: %v", err)
	}
	if engine.opts.Limits != (codemode.Limits{TimeoutS: 30, ScriptMaxBytes: 1000, OutputCapChars: 500, ExecSteps: 999}) {
		t.Errorf("the limits are %+v, want the session's", engine.opts.Limits)
	}
	tool = toolcodemode.New(engine)
	tool.Limits = codemode.Limits{TimeoutS: 5}
	if _, err := tool.Call(context.Background(), prepared, env(noopNested{}, settings)); err != nil {
		t.Fatalf("calling: %v", err)
	}
	if engine.opts.Limits.TimeoutS != 5 {
		t.Errorf("the timeout is %d, want the tool's own 5", engine.opts.Limits.TimeoutS)
	}
	if engine.opts.Limits.ExecSteps != 999 {
		t.Errorf("the exec steps are %d, want the session's 999", engine.opts.Limits.ExecSteps)
	}
}

// TestTheToolNeedsTheSeam covers the job path: a background job's
// environment has no nested caller, so a script there is a fatal error.
func TestTheToolNeedsTheSeam(t *testing.T) {
	tool := toolcodemode.New(&fakeEngine{})
	prepared, _ := tool.Prepare(json.RawMessage(`{"script":"x=1"}`), poo.PrepareEnv{})
	_, err := tool.Call(context.Background(), prepared, poo.ToolEnv{Handle: "r7"})
	if err == nil {
		t.Fatal("the call ran without the seam")
	}
}

// TestTheToolReportsAnEngineFailure covers the engine's failures: the
// script that failed is a recoverable error to the model.
func TestTheToolReportsAnEngineFailure(t *testing.T) {
	engine := &fakeEngine{err: context.DeadlineExceeded}
	tool := toolcodemode.New(engine)
	prepared, _ := tool.Prepare(json.RawMessage(`{"script":"while True:\n    pass"}`), poo.PrepareEnv{})
	res, err := tool.Call(context.Background(), prepared, env(noopNested{}, poo.ToolSettings{}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if !res.IsError {
		t.Error("the failed run is not an error result")
	}
}

// TestTheToolCapsItsOutput covers the output cap: a script that prints
// past the budget is cut, with a note.
func TestTheToolCapsItsOutput(t *testing.T) {
	engine := &fakeEngine{result: codemode.RunResult{Output: []string{strings.Repeat("x", 200)}}}
	tool := toolcodemode.New(engine)
	prepared, _ := tool.Prepare(json.RawMessage(`{"script":"print('x')"} `), poo.PrepareEnv{})
	res, err := tool.Call(context.Background(), prepared, env(noopNested{}, poo.ToolSettings{ObservationCapChars: 100}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Text) > 200 {
		t.Errorf("the result is %d chars, want it capped", len(res.Text))
	}
	if !strings.Contains(res.Text, "cut at the context budget") {
		t.Errorf("the capped result does not say so: %q", res.Text)
	}
}

// TestTheToolValidatesItsArguments covers the validation: an empty
// script is refused before anything runs.
func TestTheToolValidatesItsArguments(t *testing.T) {
	tool := toolcodemode.New(&fakeEngine{})
	if _, err := tool.Prepare(json.RawMessage(`{}`), poo.PrepareEnv{}); err == nil {
		t.Error("an empty script prepared")
	}
	if _, err := tool.Prepare(json.RawMessage(`{"script": 42}`), poo.PrepareEnv{}); err == nil {
		t.Error("a non-string script prepared")
	}
}
