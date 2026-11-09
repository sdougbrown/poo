package starlark_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/codemode/starlark"
)

// fakeNested records the calls a script makes and answers them.
type fakeNested struct {
	mu    sync.Mutex
	calls []nestedCall
	// deny is the tool name whose calls are refused.
	deny string
	// block is how long every call takes.
	block time.Duration
}

type nestedCall struct {
	parent string
	tool   string
	raw    json.RawMessage
}

func (f *fakeNested) Call(ctx context.Context, parent, tool string, raw json.RawMessage) (poo.NestedResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, nestedCall{parent: parent, tool: tool, raw: raw})
	deny := f.deny == tool
	block := f.block
	f.mu.Unlock()
	if block > 0 {
		select {
		case <-time.After(block):
		case <-ctx.Done():
			return poo.NestedResult{}, ctx.Err()
		}
	}
	if deny {
		return poo.NestedResult{Handle: "rX", Tool: tool, Outcome: poo.OutcomeDenied, Text: "[rX · " + tool + "]\nrefused", IsError: true}, nil
	}
	return poo.NestedResult{Handle: "rX", Tool: tool, Outcome: poo.OutcomeOK, Text: "[rX · " + tool + "]\nthe result"}, nil
}

func (f *fakeNested) made() []nestedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]nestedCall(nil), f.calls...)
}

func run(t *testing.T, script string, nested poo.NestedCaller, limits codemode.Limits) (codemode.RunResult, error) {
	t.Helper()
	return starlark.New().Run(context.Background(), codemode.RunOptions{
		Script: script,
		Nested: nested,
		Parent: "r1",
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
	made := nested.made()
	if len(made) != 1 || made[0].tool != "read" {
		t.Fatalf("the script made %v, want one read call", made)
	}
	if made[0].parent != "r1" {
		t.Errorf("the call's parent is %q, want r1", made[0].parent)
	}
	if string(made[0].raw) != `{"path":"notes.txt"}` {
		t.Errorf("the call's arguments are %s, want the marshalled dict", made[0].raw)
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
	start := time.Now()
	_, err := run(t, `r = call("slow", {})`, nested, codemode.Limits{
		TimeoutS: 1,
	})
	if err == nil {
		t.Fatal("the script outlived its timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the run took %s, want it cut at the timeout", time.Since(start))
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

// TestGatherRunsInOrder covers the fan-out: the calls run at the same
// time and the results come back in the list's order.
func TestGatherRunsInOrder(t *testing.T) {
	nested := &fakeNested{block: 200 * time.Millisecond}
	start := time.Now()
	res, err := run(t, `
rs = gather([("read", {"path": "a"}), ("read", {"path": "b"})])
exit([r["outcome"] for r in rs])
`, nested, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if string(res.Value) != `["ok","ok"]` {
		t.Errorf("the exit value is %s, want both outcomes in order", res.Value)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Errorf("the gather took %s, want the calls to overlap", time.Since(start))
	}
	if len(nested.made()) != 2 {
		t.Errorf("the gather made %d calls, want 2", len(nested.made()))
	}
}

// TestGatherRefusesAWidthItCannotRun covers the fan-out's bound: too
// wide a gather is a script error, not a stampede.
func TestGatherRefusesAWidthItCannotRun(t *testing.T) {
	var calls []string
	for i := 0; i < 20; i++ {
		calls = append(calls, `("read", {"path": "`+strings.Repeat("a", 1)+`"})`)
	}
	_, err := run(t, "gather(["+strings.Join(calls, ", ")+"])", &fakeNested{}, codemode.Limits{})
	if err == nil {
		t.Fatal("the over-wide gather ran")
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
