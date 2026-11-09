package starlark_test

import (
	"context"
	"encoding/json"
	"math"
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
		results[i] = poo.NestedResult{Handle: "rX", Tool: call.Tool, Outcome: poo.OutcomeOK, Text: "[rX · " + call.Tool + "]\nthe result", Structured: json.RawMessage(`{"k":1}`)}
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

// TestAScriptLoopsAtTheTopLevel covers the shape a model writes: a loop
// and a branch at the top level that reassign a global between calls.
func TestAScriptLoopsAtTheTopLevel(t *testing.T) {
	nested := &fakeNested{}
	res, err := run(t, `
count = 0
for path in ["a.txt", "b.txt", "c.txt"]:
    r = call("read", {"path": path})
    if not r["is_error"]:
        count += 1
exit(count)
`, nested, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if string(res.Value) != "3" {
		t.Errorf("the script returned %s, want 3", res.Value)
	}
	if res.Calls != 3 {
		t.Errorf("the script made %d calls, want 3", res.Calls)
	}
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

// TestAScriptSeesTheStructuredPayload covers the payload's shape: the
// structured result is the JSON text as a string, so the script reads it
// with json.decode and the engine makes no second schema round trip.
func TestAScriptSeesTheStructuredPayload(t *testing.T) {
	res, err := run(t, `
r = call("read", {"path": "x"})
exit(r["structured"])
`, &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	var value string
	if err := json.Unmarshal(res.Value, &value); err != nil {
		t.Fatalf("the exit value is not a JSON string: %s: %v", res.Value, err)
	}
	if value != `{"k":1}` {
		t.Errorf("the script saw structured %q, want the payload's JSON text", value)
	}
}

// TestAScriptDecodesTheStructuredPayload covers the decoder: the script
// turns a result's payload into a value it can read.
func TestAScriptDecodesTheStructuredPayload(t *testing.T) {
	res, err := run(t, `
r = call("read", {"path": "x"})
exit(json.decode(r["structured"])["k"])
`, &fakeNested{}, codemode.Limits{})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if string(res.Value) != "1" {
		t.Errorf("the script returned %s, want the decoded k", res.Value)
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
	if res.Truncated {
		t.Error("the run reported a cut, want the whole output kept")
	}
}

// TestThePrintCapSpansPrints covers the cap across prints: the first
// line takes what it needs, the second gets only the room the first
// left, and a print after the cap is dropped.
func TestThePrintCapSpansPrints(t *testing.T) {
	res, err := run(t, `
print("0123456789")
print("abcdefghij")
print("dropped")
`, &fakeNested{}, codemode.Limits{OutputCapChars: 20})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Output) != 2 {
		t.Fatalf("the output is %q, want the first line and the cut second", res.Output)
	}
	if res.Output[0] != "0123456789" {
		t.Errorf("the first line is %q, want it kept whole", res.Output[0])
	}
	// The first line and its newline take 11 of the 20 characters, so
	// the second keeps 9 of its 10 and says it was cut.
	if res.Output[1] != "abcdefghi …" {
		t.Errorf("the second line is %q, want the 9 characters the cap leaves", res.Output[1])
	}
	if !res.Truncated {
		t.Error("the run did not report the cut")
	}
}

// TestAPrintCutKeepsTheTextValid covers the print cap: it counts
// characters, so the cut keeps the characters the cap leaves and lands
// on a boundary.
func TestAPrintCutKeepsTheTextValid(t *testing.T) {
	res, err := run(t, `print("日本語日本語日本語")`, &fakeNested{}, codemode.Limits{OutputCapChars: 8})
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
	kept := strings.TrimSuffix(res.Output[0], " …")
	if got := utf8.RuneCountInString(kept); got != 8 {
		t.Errorf("the capped line keeps %d characters, want the 8 the cap allows: %q", got, res.Output[0])
	}
	if !res.Truncated {
		t.Error("the run did not report the cut")
	}
}

// TestAPrintUnderTheCapIsKeptWhole covers the cap's unit: a line of
// multi-byte characters that fits in characters prints in full, though
// its bytes are over the cap.
func TestAPrintUnderTheCapIsKeptWhole(t *testing.T) {
	res, err := run(t, `print("日本語")`, &fakeNested{}, codemode.Limits{OutputCapChars: 8})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Output) != 1 || res.Output[0] != "日本語" {
		t.Errorf("the output is %q, want the line kept whole", res.Output)
	}
	if res.Truncated {
		t.Error("the run reported a cut, want the line kept whole")
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
// loops past its budget is stopped, and the run fails.
func TestTheStepBudgetStopsALoop(t *testing.T) {
	_, err := run(t, `
while True:
    x = 1
`, &fakeNested{}, codemode.Limits{ExecSteps: 1000})
	if err == nil {
		t.Fatal("the infinite loop ran to completion")
	}
	if !strings.Contains(err.Error(), "too many steps") {
		t.Fatalf("the loop stopped for another reason: %v", err)
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

// TestATimeoutThatOverflowsIsRefused covers a timeout the run cannot
// hold: the engine scales it to a duration, and one past the duration's
// range wraps negative and leaves the script with no wall-clock limit.
func TestATimeoutThatOverflowsIsRefused(t *testing.T) {
	tooLong := int(math.MaxInt64/int64(time.Second)) + 1
	_, err := run(t, `exit(1)`, &fakeNested{}, codemode.Limits{TimeoutS: tooLong})
	if err == nil {
		t.Fatal("the run took a timeout too large for its duration")
	}
	if !strings.Contains(err.Error(), "the codemode limit timeout_s is over") {
		t.Errorf("the failure is %q, want it to name the oversized timeout", err)
	}
}

// TestGatherRefusesAnEntryItCannotRead covers the entries a model gets
// wrong: each fails the run with a message naming the entry, and none
// reaches the seam.
func TestGatherRefusesAnEntryItCannotRead(t *testing.T) {
	for _, c := range []struct {
		name   string
		script string
		want   string
	}{
		{"a bare name", `gather(["read"])`, "gather entry 0 is not a (name, args) pair"},
		{"a longer tuple", `gather([("read", {}, 1)])`, "gather entry 0 is not a (name, args) pair"},
		{"a number for a name", `gather([(1, {})])`, "gather entry 0's name is not a string"},
	} {
		t.Run(c.name, func(t *testing.T) {
			nested := &fakeNested{}
			if _, err := run(t, c.script, nested, codemode.Limits{}); err == nil {
				t.Fatal("the gather ran, want the entry refused")
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the failure is %q, want it to name %q", err, c.want)
			}
			if len(nested.handed()) != 0 {
				t.Errorf("the gather handed the seam %v, want nothing", nested.handed())
			}
		})
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

// TestARecursiveFunctionIsRefused covers the sandbox's recursion rule:
// a script cannot call itself.
func TestARecursiveFunctionIsRefused(t *testing.T) {
	_, err := run(t, `
def f(n):
    return 0 if n == 0 else f(n - 1)

exit(f(3))
`, &fakeNested{}, codemode.Limits{})
	if err == nil {
		t.Fatal("the recursive function ran, want it refused")
	}
	if !strings.Contains(err.Error(), "called recursively") {
		t.Errorf("the failure is %q, want it to name the recursion", err)
	}
}
