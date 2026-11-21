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
const r = await tools.read({path: "input.txt"});
const [matches, denied] = await Promise.all([tools.grep({pattern: JSON.parse(r.structured).word}), tools.blocked({})]);
print("日本語日本語");
return {matches: matches.text.split("\n").length, denied: denied.is_error};
`,
		Limits: codemode.Limits{OutputCapChars: 3},
		Tools:  []poo.ToolInfo{{Name: "read"}, {Name: "grep"}, {Name: "blocked"}},
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

// TestTheAwaitedCallsNameTheTools covers the surface the spike exists
// for: the session's tools arrive as named properties, a chain of
// awaited calls reads one call's result to decide the next, and each
// awaited call reaches the seam as its own batch.
func TestTheAwaitedCallsNameTheTools(t *testing.T) {
	var batches [][]poo.NestedCall
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `
const first = await tools.read({});
const second = await tools[first.is_error ? "blocked" : "grep"]({word: JSON.parse(first.structured).k});
return [first.tool, second.tool, second.text];
`,
		Tools: []poo.ToolInfo{{Name: "read"}, {Name: "grep"}, {Name: "blocked"}},
		Nested: nested(func(_ context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
			batches = append(batches, calls)
			if calls[0].Tool == "read" {
				return []poo.NestedResult{{Tool: "read", Structured: json.RawMessage(`{"k":1}`)}}, nil
			}
			return []poo.NestedResult{{Tool: calls[0].Tool, Text: "the grep text"}}, nil
		}),
	})
	if err != nil || string(res.Value) != `["read","grep","the grep text"]` {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if len(batches) != 2 || batches[0][0].Tool != "read" || batches[1][0].Tool != "grep" ||
		string(batches[1][0].Arguments) != `{"word":1}` {
		t.Fatalf("nested batches = %+v", batches)
	}
}

type parallelNested struct {
	nested
	parallel nested
}

func (n parallelNested) CallParallel(ctx context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
	return n.parallel(ctx, calls)
}

func TestToolBatchesRunInSubmissionOrder(t *testing.T) {
	var batches [][]string
	record := func(mode string) nested {
		return func(_ context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
			paths := []string{mode}
			results := make([]poo.NestedResult, len(calls))
			for i, call := range calls {
				var args struct{ Path string }
				if err := json.Unmarshal(call.Arguments, &args); err != nil {
					return nil, err
				}
				paths = append(paths, args.Path)
				results[i].Text = args.Path
			}
			batches = append(batches, paths)
			return results, nil
		}
	}
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `
const args = {path: "first"};
const first = tools.read(args);
args.path = "second";
const second = tools.read(args);
args.path = "write";
const batch = parallel(() => {
  const write = tools.write(args);
  const other = tools.write({path: "other"});
  return [other, write];
});
if (!(batch instanceof Promise)) throw new Error("parallel did not return a promise");
args.path = "last";
const last = tools.read(args);
const legacy = call("read", {path: "legacy"});
const [a, b, writes, c] = await Promise.all([first, second, batch, last]);
return [a.text, b.text, writes.map(r => r.text), c.text, legacy.text];
`,
		Tools:  []poo.ToolInfo{{Name: "read"}, {Name: "write"}},
		Nested: parallelNested{nested: record("default"), parallel: record("parallel")},
	})
	if err != nil || res.Calls != 6 || string(res.Value) != `["first","second",["other","write"],"last","legacy"]` {
		t.Fatalf("run = %+v, %v", res, err)
	}
	want := [][]string{{"default", "first", "second"}, {"parallel", "other", "write"}, {"default", "last"}, {"default", "legacy"}}
	if !slices.EqualFunc(batches, want, slices.Equal[[]string]) {
		t.Fatalf("batches = %v, want %v", batches, want)
	}
}

func TestOversizedNamedBatchRejectsEveryCall(t *testing.T) {
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `
const calls = Array.from({length: 17}, () => tools.read({}));
const results = await Promise.allSettled(calls);
return results.every(r => r.status === "rejected" && String(r.reason).includes("takes at most"));
`,
		Tools:  []poo.ToolInfo{{Name: "read"}},
		Nested: nested(noCalls),
	})
	if err != nil || res.Calls != 0 || string(res.Value) != "true" {
		t.Fatalf("run = %+v, %v", res, err)
	}
}

func TestExplicitParallelDoesNotFallBackToOrdinaryScheduling(t *testing.T) {
	_, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `await parallel(() => [tools.write({})]);`,
		Tools:  []poo.ToolInfo{{Name: "write"}},
		Nested: nested(noCalls),
	})
	if err == nil || !strings.Contains(err.Error(), "does not support explicit parallel batches") {
		t.Fatalf("run error = %v", err)
	}
}

// TestAnAwaitThatNeverSettlesStopsAtTheDeadline covers the async bound:
// the run blocks inside the guest's own loop, and the deadline closing
// the guest is what stops it.
func TestAnAwaitThatNeverSettlesStopsAtTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := quickjs.New().Run(ctx, codemode.RunOptions{
		Script: `await new Promise(() => {});`,
		Limits: codemode.Limits{TimeoutS: 1},
		Nested: nested(noCalls),
	})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") || ctx.Err() != nil {
		t.Fatalf("run error = %v; want the deadline before parent timeout", err)
	}
}

// TestTheDynamicImportDoorIsOpenButContained pins what the eval's own
// loop pump reopens: an async script can import the compiled-in
// modules. Their file operations still meet the inert mount, so the
// boundary is the mount, not the deletion. Whether to close this door
// is an open decision on the PR.
func TestTheDynamicImportDoorIsOpenButContained(t *testing.T) {
	res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `const std = await import("qjs:std"); return typeof std.open;`,
		Nested: nested(noCalls),
	})
	if err != nil || string(res.Value) != `"function"` {
		t.Fatalf("the import probe: run = %+v, %v", res, err)
	}
	_, err = quickjs.New().Run(t.Context(), codemode.RunOptions{
		Script: `const std = await import("qjs:std"); std.open("input.txt", "r"); return "read";`,
		Nested: nested(noCalls),
	})
	if err == nil || !strings.Contains(err.Error(), "the QuickJS runtime stopped unexpectedly") {
		t.Fatalf("the file probe: run error = %v; want the inert mount to stop it", err)
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

func TestInvalidParallelCallbacksDoNotDispatch(t *testing.T) {
	for _, test := range []struct{ name, callback, want string }{
		{"async", `async () => { await 0; return [tools.write({})]; }`, "synchronous callback"},
		{"throws", `() => { tools.write({}); throw new Error("discard batch"); }`, "discard batch"},
		{"omits call", `() => { tools.write({}); return []; }`, "exactly once"},
		{"duplicates call", `() => { const a = tools.write({}); tools.write({}); return [a, a]; }`, "exactly once"},
		{"foreign promise", `() => { tools.write({}); return [Promise.resolve()]; }`, "exactly once"},
		{"returns promise", `() => Promise.all([tools.write({})])`, "return an array"},
		{"synchronous call", `() => [call("write", {})]`, "use named tools"},
		{"nested batch", `() => [parallel(() => [tools.write({})])]`, "cannot be nested"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var handed []string
			record := nested(func(_ context.Context, calls []poo.NestedCall) ([]poo.NestedResult, error) {
				for _, call := range calls {
					handed = append(handed, call.Tool)
				}
				return make([]poo.NestedResult, len(calls)), nil
			})
			res, err := quickjs.New().Run(t.Context(), codemode.RunOptions{
				Script: `try { await parallel(` + test.callback + `); } catch (error) {
  await tools.read({});
  return String(error);
}
return "batch unexpectedly succeeded";`,
				Tools:  []poo.ToolInfo{{Name: "read"}, {Name: "write"}},
				Nested: parallelNested{nested: record, parallel: record},
			})
			if err != nil || !strings.Contains(string(res.Value), test.want) {
				t.Fatalf("run = %+v, %v; want %q", res, err, test.want)
			}
			if !slices.Equal(handed, []string{"read"}) {
				t.Fatalf("tools dispatched = %v, want only the read after recovery", handed)
			}
		})
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
		{"batch width", `await parallel(() => Array.from({length: 17}, () => tools.read({})));`, codemode.Limits{}, "tool batch takes at most"},
		{"oversized return", `return "x".repeat(2097152);`, codemode.Limits{}, "print a summary instead"},
		{"call after return", `return {toJSON() { call("read", {}); return 1; }};`, codemode.Limits{}, "after returning"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := quickjs.New().Run(ctx, codemode.RunOptions{Script: test.script, Tools: []poo.ToolInfo{{Name: "read"}}, Nested: nested(noCalls), Limits: test.limits})
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
