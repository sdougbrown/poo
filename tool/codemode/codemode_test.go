package codemode_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/internal/caps"
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

func (noopNested) Call(ctx context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
	return make([]poo.NestedResult, len(calls)), nil
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

// prepare validates a script for a call.
func prepare(t *testing.T, script string) poo.Prepared {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"script": script})
	if err != nil {
		t.Fatalf("marshalling the arguments: %v", err)
	}
	prepared, err := toolcodemode.New(&fakeEngine{}).Prepare(raw, poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	return prepared
}

// TestTheToolRunsTheScriptOnItsEngine covers the wiring: the tool hands
// the engine the script and the seam, and shapes the result the model
// sees.
func TestTheToolRunsTheScriptOnItsEngine(t *testing.T) {
	engine := &fakeEngine{result: codemode.RunResult{
		Output: []string{"line one", "line two"},
		Value:  json.RawMessage(`{"answer": 42}`),
		Calls:  3,
	}}
	tool := toolcodemode.New(engine)
	res, err := tool.Call(context.Background(), prepare(t, "print('hi')"), env(noopNested{}, poo.ToolSettings{}))
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
	if engine.opts.Nested == nil {
		t.Error("the engine got no nested caller")
	}
}

// TestTheToolLimitsComeFromTheSettings covers the settings: an unset
// limit takes the session's, and the tool's own wins over both.
func TestTheToolLimitsComeFromTheSettings(t *testing.T) {
	engine := &fakeEngine{}
	tool := toolcodemode.New(engine)
	prepared := prepare(t, "x=1")
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

// TestTheToolWithoutTheSeamIsARecoverableError covers the nesting
// depth: a script that calls codemode gets an error result it can look
// at, not a failure that ends the run.
func TestTheToolWithoutTheSeamIsARecoverableError(t *testing.T) {
	tool := toolcodemode.New(&fakeEngine{})
	res, err := tool.Call(context.Background(), prepare(t, "x=1"), poo.ToolEnv{Handle: "r7"})
	if err != nil {
		t.Fatalf("the call failed: %v", err)
	}
	if !res.IsError {
		t.Error("the call without a seam is not an error result")
	}
	if res.Text != "codemode runs only as a call the model makes: a nested call or a background job has no tools to call" {
		t.Errorf("the result is %q, want the depth-limit message", res.Text)
	}
}

// TestTheToolKeepsWhatAFailedScriptDid covers the failed script: the
// result still says what the script did, so a script that timed out
// after an edit tells the model the edit happened.
func TestTheToolKeepsWhatAFailedScriptDid(t *testing.T) {
	engine := &fakeEngine{
		result: codemode.RunResult{Output: []string{"edited notes.txt"}, Calls: 2},
		err:    context.DeadlineExceeded,
	}
	tool := toolcodemode.New(engine)
	res, err := tool.Call(context.Background(), prepare(t, "while True:\n    pass"), env(noopNested{}, poo.ToolSettings{}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if !res.IsError {
		t.Error("the failed run is not an error result")
	}
	count := strings.Index(res.Text, "2 call(s)")
	printed := strings.Index(res.Text, "edited notes.txt")
	failure := strings.Index(res.Text, context.DeadlineExceeded.Error())
	for name, at := range map[string]int{"the call count": count, "the printed line": printed, "the failure": failure} {
		if at < 0 {
			t.Fatalf("the result is %q, want it to name %s", res.Text, name)
		}
	}
	if !(count < printed && printed < failure) {
		t.Errorf("the result is %q, want the count, then the printed lines, then the failure", res.Text)
	}
}

// TestTheToolCapsItsOutput covers the output cap: a script that prints
// past the budget is cut, with a note, and the cut keeps the text valid.
func TestTheToolCapsItsOutput(t *testing.T) {
	engine := &fakeEngine{result: codemode.RunResult{Output: []string{strings.Repeat("日本語", 20)}}}
	tool := toolcodemode.New(engine)
	res, err := tool.Call(context.Background(), prepare(t, "print('x')"), env(noopNested{}, poo.ToolSettings{ObservationCapChars: 100}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Text) > 200 {
		t.Errorf("the result is %d chars, want it capped", len(res.Text))
	}
	if !strings.Contains(res.Text, "cut at the context budget") {
		t.Errorf("the capped result does not say so: %q", res.Text)
	}
	if !utf8.ValidString(res.Text) {
		t.Errorf("the capped result is not valid UTF-8: %q", res.Text)
	}
}

// TestTheToolCapsItsOutputWithoutASetting covers the fallback: a
// session that sets no observation budget still caps the script's
// output, at the bound the sibling tools fall back to.
func TestTheToolCapsItsOutputWithoutASetting(t *testing.T) {
	engine := &fakeEngine{result: codemode.RunResult{Output: []string{strings.Repeat("x", 2*caps.ObservationMax)}}}
	tool := toolcodemode.New(engine)
	res, err := tool.Call(context.Background(), prepare(t, "print('x')"), env(noopNested{}, poo.ToolSettings{}))
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if len(res.Text) > caps.ObservationMax+100 {
		t.Errorf("the result is %d chars, want it capped at %d", len(res.Text), caps.ObservationMax)
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
