package starlark_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/codemode/starlark"
)

// fakeNested records the batches a script hands the seam and answers
// them.
type fakeNested struct {
	mu      sync.Mutex
	batches [][]poo.NestedCall
	// deny is the tool name whose calls are refused.
	deny string
	// block is how long every batch takes.
	block time.Duration
}

func (f *fakeNested) Call(ctx context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
	f.mu.Lock()
	f.batches = append(f.batches, append([]poo.NestedCall(nil), calls...))
	deny := f.deny
	block := f.block
	f.mu.Unlock()
	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	results := make([]poo.NestedResult, len(calls))
	for i, call := range calls {
		results[i] = poo.NestedResult{Handle: "rX", Tool: call.Tool, Outcome: poo.OutcomeOK, Text: "[rX · " + call.Tool + "]\nthe result"}
		if deny != "" && call.Tool == deny {
			results[i] = poo.NestedResult{Handle: "rX", Tool: call.Tool, Outcome: poo.OutcomeDenied, Text: "[rX · " + call.Tool + "]\nrefused", IsError: true}
		}
	}
	return results, nil
}

// handed returns the batches the seam received, in order.
func (f *fakeNested) handed() [][]poo.NestedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]poo.NestedCall(nil), f.batches...)
}

func run(t *testing.T, script string, nested poo.NestedCaller, limits codemode.Limits) (codemode.RunResult, error) {
	t.Helper()
	return starlark.New().Run(context.Background(), codemode.RunOptions{
		Script: script,
		Nested: nested,
		Limits: limits,
	})
}

// TestAScriptCallsTools covers the headline: the script's calls go
// through the seam with JSON arguments, and the result comes back as
// data.
func TestAScriptCallsTools(t *testing.T) {
	nested := &fakeNested{}
	res, err := run(t, `
r = call("read", {"path": "notes.txt"})
exit({"saw": r["text"], "outcome": r["outcome"], "ok": not r["is_error"]})
`, nested, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	var value struct {
		Saw     string `json:"saw"`
		Outcome string `json:"outcome"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(res.Value, &value); err != nil {
		t.Fatalf("the exit value is not JSON: %v", err)
	}
	if value.Saw != "[rX · read]\nthe result" {
		t.Errorf("the script saw the text %q", value.Saw)
	}
	if value.Outcome != "ok" || !value.OK {
		t.Errorf("the script saw outcome %q ok %v, want ok true", value.Outcome, value.OK)
	}
	made := nested.handed()
	if len(made) != 1 {
		t.Fatalf("the script handed the seam %d batches, want one", len(made))
	}
	if len(made[0]) != 1 || made[0][0].Tool != "read" {
		t.Fatalf("the script asked for %v, want a batch of one read call", made[0])
	}
	if string(made[0][0].Arguments) != `{"path":"notes.txt"}` {
		t.Errorf("the call's arguments are %s, want the marshalled dict", made[0][0].Arguments)
	}
	if res.Calls != 1 {
		t.Errorf("the run counted %d calls, want 1", res.Calls)
	}
}

// TestAScriptSeesADenialAsData covers refusals: the script gets the
// denial as a result and carries on.
func TestAScriptSeesADenialAsData(t *testing.T) {
	nested := &fakeNested{deny: "bash"}
	res, err := run(t, `
first = call("bash", {"command": "rm -rf /"})
second = call("read", {"path": "x"})
exit({"first": first["outcome"], "second": second["outcome"]})
`, nested, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if !strings.Contains(string(res.Value), `"first":"denied"`) || !strings.Contains(string(res.Value), `"second":"ok"`) {
		t.Errorf("the exit value is %s, want the denial followed by the ok call", res.Value)
	}
}

// TestAScriptPrints covers the output: print lands in the result's
// output, in order.
func TestAScriptPrints(t *testing.T) {
	res, err := run(t, `
print("one", 2)
print("three")
`, &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Output) != 2 || res.Output[0] != "one 2" || res.Output[1] != "three" {
		t.Errorf("the output is %q, want the two printed lines", res.Output)
	}
}

// TestAPrintCutKeepsTheTextValid covers the print cap: the cut lands on
// a rune boundary, so what the model reads is valid text.
func TestAPrintCutKeepsTheTextValid(t *testing.T) {
	res, err := run(t, `print("日本語")`, &fakeNested{}, codemode.Limits{OutputCapChars: 8})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Output) != 1 {
		t.Fatalf("the output is %q, want one line", res.Output)
	}
	if !utf8.ValidString(res.Output[0]) {
		t.Errorf("the capped line is not valid UTF-8: %q", res.Output[0])
	}
	if !strings.HasSuffix(res.Output[0], "…") {
		t.Errorf("the capped line does not say it was cut: %q", res.Output[0])
	}
}

// TestAScriptWithoutExitReturnsItsPrints covers the exit-less script:
// it is a result, not a failure.
func TestAScriptWithoutExitReturnsItsPrints(t *testing.T) {
	res, err := run(t, `print("done")`, &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Value != nil {
		t.Errorf("the exit value is %s, want none", res.Value)
	}
}

// TestAnExitWithoutAValueReturnsNothing covers the bare exit: the run
// stopped where the script said, and it has no value to report.
func TestAnExitWithoutAValueReturnsNothing(t *testing.T) {
	res, err := run(t, `
print("done")
exit()
`, &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Value != nil {
		t.Errorf("the exit value is %s, want none", res.Value)
	}
	if len(res.Output) != 1 || res.Output[0] != "done" {
		t.Errorf("the output is %q, want the line the script printed", res.Output)
	}
}

// TestTheStepBudgetStopsALoop covers the exec-steps cap: a script that
// loops forever is stopped, and the run fails.
func TestTheStepBudgetStopsALoop(t *testing.T) {
	_, err := run(t, `
while True:
    x = 1
`, &fakeNested{}, codemode.Limits{ExecSteps: 1000})
	if err == nil {
		t.Fatal("the infinite loop ran to completion")
	}
}

// TestTheTimeoutStopsAWait covers the wall-clock limit: a script whose
// tool call outlives the timeout is stopped.
func TestTheTimeoutStopsAWait(t *testing.T) {
	nested := &fakeNested{block: 5 * time.Second}
	_, err := run(t, `r = call("slow", {})`, nested, codemode.Limits{
		TimeoutS: 1,
	})
	if err == nil {
		t.Fatal("the script outlived its timeout")
	}
}

// TestTheScriptSizeLimit covers the size cap: an over-long script is
// refused before it runs.
func TestTheScriptSizeLimit(t *testing.T) {
	long := "# " + strings.Repeat("x", 300) + "\n"
	_, err := run(t, long, &fakeNested{}, codemode.Limits{ScriptMaxBytes: 256})
	if err == nil {
		t.Fatal("the over-long script ran")
	}
}

// TestANegativeLimitIsRefused covers a limit the session got wrong: a
// negative one would wrap or shrink a bound, so the run refuses it
// instead of reading it as the engine's default.
func TestANegativeLimitIsRefused(t *testing.T) {
	_, err := run(t, `exit(1)`, &fakeNested{}, codemode.Limits{ExecSteps: -1})
	if err == nil {
		t.Fatal("the run took a negative limit")
	}
	if !strings.Contains(err.Error(), "the codemode limit exec_steps is negative") {
		t.Errorf("the failure is %q, want it to name the negative limit", err)
	}
}

// TestGatherHandsTheSeamOneBatch covers the fan-out: the whole list is
// one batch, in the list's order, and the results come back in the same
// order.
func TestGatherHandsTheSeamOneBatch(t *testing.T) {
	nested := &fakeNested{}
	res, err := run(t, `
rs = gather([("read", {"path": "a"}), ("read", {"path": "b"}), ("grep", {"pattern": "c"})])
exit([r["tool"] for r in rs])
`, nested, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if string(res.Value) != `["read","read","grep"]` {
		t.Errorf("the exit value is %s, want the results in the list's order", res.Value)
	}
	made := nested.handed()
	if len(made) != 1 {
		t.Fatalf("the gather handed the seam %d batches, want one", len(made))
	}
	want := []string{"read", "read", "grep"}
	if len(made[0]) != len(want) {
		t.Fatalf("the batch holds %d calls, want %d", len(made[0]), len(want))
	}
	for i, name := range want {
		if made[0][i].Tool != name {
			t.Errorf("the batch's call %d is %q, want %q", i, made[0][i].Tool, name)
		}
	}
	if res.Calls != 3 {
		t.Errorf("the run counted %d calls, want the 3 results it got", res.Calls)
	}
}

// TestGatherRefusesAWidthItCannotRun covers the fan-out's bound: a
// gather as wide as the bound runs, and one call wider is a script
// error, not a stampede.
func TestGatherRefusesAWidthItCannotRun(t *testing.T) {
	gatherOf := func(width int) string {
		calls := make([]string, width)
		for i := range calls {
			calls[i] = `("read", {"path": "a"})`
		}
		return "rs = gather([" + strings.Join(calls, ", ") + "])\nexit(len(rs))"
	}
	res, err := run(t, gatherOf(starlark.MaxGatherWidth), &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("a gather of %d failed: %v", starlark.MaxGatherWidth, err)
	}
	if string(res.Value) != strconv.Itoa(starlark.MaxGatherWidth) {
		t.Errorf("the gather came back with %s results, want %d", res.Value, starlark.MaxGatherWidth)
	}
	if _, err := run(t, gatherOf(starlark.MaxGatherWidth+1), &fakeNested{}, codemode.Limits{}); err == nil {
		t.Fatalf("the gather of %d ran, want it refused", starlark.MaxGatherWidth+1)
	}
}

// TestTheSandboxHasNoPowers covers the sandbox's shape: the scripts
// reach nothing but the tools.
func TestTheSandboxHasNoPowers(t *testing.T) {
	for _, script := range []string{
		`open("x")`,
		`import("os")`,
		`load("other.star")`,
	} {
		if _, err := run(t, script, &fakeNested{}, codemode.Limits{}); err == nil {
			t.Errorf("the script %q ran, want it refused", script)
		}
	}
}
