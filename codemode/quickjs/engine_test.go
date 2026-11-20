package quickjs_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/codemode/quickjs"
)

type nested func(context.Context, []poo.NestedCall) ([]poo.NestedResult, error)

func (n nested) Call(ctx context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
	return n(ctx, calls)
}

func noCalls(_ context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
	panic("unexpected tool call: " + calls[0].Tool)
}

func TestQuickJSReportsOnlyWhatTheScriptKeeps(t *testing.T) {
	var batches [][]poo.NestedCall
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `
const r = call("read", {path: "input.txt"});
const batch = gather([["grep", {pattern: JSON.parse(r.structured).word}], ["blocked", {}]]);
Promise.resolve().then(() => call("late", {}));
print("日本語日本語");
return {matches: batch[0].text.split("\n").length, denied: batch[1].is_error};
`,
		Limits: codemode.Limits{OutputCapChars: 3},
		Nested: nested(func(_ context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
			batches = append(batches, calls)
			if len(batches) == 1 && len(calls) == 1 {
				return []poo.NestedResult{{Structured: json.RawMessage(`{"word":"needle"}`)}}, nil
			}
			return []poo.NestedResult{{Text: "one\ntwo"}, {Outcome: poo.OutcomeDenied, IsError: true}}, nil
		}),
	})
	if err != nil || res.Calls != 3 || string(res.Value) != `{"matches":2,"denied":true}` {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if len(res.Output) != 1 || res.Output[0] != "日本語 …" || !res.Truncated {
		t.Fatalf("printed = %q (truncated=%v)", res.Output, res.Truncated)
	}
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 2 ||
		batches[0][0].Tool != "read" || string(batches[0][0].Arguments) != `{"path":"input.txt"}` ||
		batches[1][0].Tool != "grep" || string(batches[1][0].Arguments) != `{"pattern":"needle"}` || batches[1][1].Tool != "blocked" {
		t.Fatalf("nested batches = %+v", batches)
	}
}

func TestQuickJSRunawayStopsAndNextScriptRuns(t *testing.T) {
	engine := quickjs.New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := engine.Run(ctx, codemode.RunOptions{
			Script: `call("started"); while (true) {}`,
			Nested: nested(func(context.Context, []poo.NestedCall) ([]poo.NestedResult, error) {
				close(started)
				return []poo.NestedResult{{}}, nil
			}),
		})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("script stopped before the loop: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("script did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not stop QuickJS")
	}
	res, err := engine.Run(t.Context(), codemode.RunOptions{Script: `return 42;`, Nested: nested(noCalls)})
	if err != nil || string(res.Value) != "42" {
		t.Fatalf("next run = %+v, %v", res, err)
	}
}

func TestQuickJSBoundsGuestExecution(t *testing.T) {
	for _, test := range []struct {
		name, script string
		limits       codemode.Limits
		want         string
	}{
		{"deadline", `while (true) {}`, codemode.Limits{TimeoutS: 1}, "deadline exceeded"},
		{"memory", `return new ArrayBuffer(128 * 1024 * 1024).byteLength;`, codemode.Limits{}, "out of memory"},
		{"step budget", `return 1;`, codemode.Limits{ExecSteps: 100}, "does not support the exec_steps limit"},
		{"batch width", `gather(Array.from({length: 17}, () => ["read", {}]));`, codemode.Limits{}, "gather takes at most"},
		{"async result", `return Promise.resolve(42);`, codemode.Limits{}, "scripts must be synchronous"},
		{"oversized return", `return "x".repeat(2097152);`, codemode.Limits{}, "print a summary instead"},
		{"call after return", `return {toJSON() { call("read", {}); return 1; }};`, codemode.Limits{}, "after returning"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := quickjs.New().Run(ctx, codemode.RunOptions{Script: test.script, Nested: nested(noCalls), Limits: test.limits})
			if err == nil || !strings.Contains(err.Error(), test.want) || ctx.Err() != nil {
				t.Fatalf("run error = %v; want %q before parent timeout", err, test.want)
			}
		})
	}
}

func TestQuickJSLeavesNoHostGlobalsInTheScript(t *testing.T) {
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		// Every global the wrapper must have removed, plus the ones a
		// QuickJS bump could reintroduce under a familiar name. The
		// snapshot fails loudly when a dependency upgrade re-exposes one.
		Script: `return Object.getOwnPropertyNames(globalThis);`,
		Nested: nested(noCalls),
	})
	if err != nil {
		t.Fatalf("run = %+v, %v", res, err)
	}
	var names []string
	if err := json.Unmarshal(res.Value, &names); err != nil {
		t.Fatalf("the globals snapshot is not a list: %v", err)
	}
	for _, danger := range []string{
		"std", "os", "bjson", "print", "console", "scriptArgs",
		"setTimeout", "setInterval", "clearTimeout", "clearInterval",
		"QJS_PROXY_VALUE", "performance", "Date", "navigator", "gc", "eval",
		"fetch", "process", "require", "module", "__pooTools", "__pooPrint",
	} {
		if slices.Contains(names, danger) {
			t.Errorf("the script can see the global %q", danger)
		}
	}
}

// TestAScriptThatReturnsNothingIsASuccess covers the print-only script:
// like a Starlark script that never exits, it is a clean run whose
// printed lines are the whole result, with no return value.
func TestAScriptThatReturnsNothingIsASuccess(t *testing.T) {
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `print("kept line"); print("dropped");`,
		Nested: nested(noCalls),
	})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Value != nil {
		t.Fatalf("the run returned a value: %s", res.Value)
	}
	if len(res.Output) != 2 || res.Output[0] != "kept line" || res.Output[1] != "dropped" {
		t.Fatalf("the run's output is %q", res.Output)
	}
}

func TestAPanickingToolCallReportsNoHostStack(t *testing.T) {
	_, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `call("panic", {})`,
		Nested: nested(func(context.Context, []poo.NestedCall) ([]poo.NestedResult, error) {
			panic("a host bug")
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "the tool bridge failed") {
		t.Fatalf("run error = %v", err)
	}
	for _, leak := range []string{"goroutine", ".go", "/home/"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error leaks host details: %q", err.Error())
		}
	}
}
