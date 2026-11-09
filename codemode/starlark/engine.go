// Package starlark runs codemode scripts in Starlark.
//
// Starlark is a deterministic Python dialect: no file, network or clock
// builtins, no recursion, and a step budget the interpreter enforces.
// A script's only capability is calling tools: the `call` builtin (and
// its `gather` sibling for a batch of them) go through the nested-call
// seam, so every call the script makes is judged and recorded by the
// session.
package starlark

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jsonmod "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/internal/caps"
)

// The engine's own defaults, for the limits a session leaves at zero.
const (
	DefaultTimeoutS       = 120
	DefaultScriptMaxBytes = 64 * 1024
	DefaultOutputCapChars = 16 * 1024
	DefaultExecSteps      = 10 * 1000 * 1000
	// MaxGatherWidth is the most calls one gather may fan out to.
	MaxGatherWidth = 16
)

// exitReason is the thread-cancel reason the exit builtin leaves. The
// run checks its own flag, so an exit is a result and not a failure.
const exitReason = "codemode: exit"

// Engine runs codemode scripts in Starlark.
type Engine struct{}

// New returns the Starlark engine.
func New() *Engine { return &Engine{} }

// Name is the engine's name.
func (e *Engine) Name() string { return "starlark" }

// state is what the builtins and the run share.
//
// A run's builtins close over the run's state, so two runs share
// nothing. Every builtin runs on the interpreter's own goroutine: the
// seam does the fan-out, so the state needs no lock.
type state struct {
	nested  poo.NestedCaller
	ctx     context.Context
	output  []string
	printed int
	calls   int
	exited  bool
	value   json.RawMessage
}

// Run executes one script.
func (e *Engine) Run(ctx context.Context, opts codemode.RunOptions) (codemode.RunResult, error) {
	limits, err := resolveLimits(opts.Limits)
	if err != nil {
		return codemode.RunResult{}, err
	}
	if len(opts.Script) > limits.ScriptMaxBytes {
		return codemode.RunResult{}, fmt.Errorf("the script is %d bytes, over the %d byte limit", len(opts.Script), limits.ScriptMaxBytes)
	}
	if opts.Nested == nil {
		return codemode.RunResult{}, fmt.Errorf("the script has no way to call tools")
	}

	st := &state{nested: opts.Nested}
	thread := &starlark.Thread{
		Name: "codemode",
		Print: func(_ *starlark.Thread, msg string) {
			if st.printed >= limits.OutputCapChars {
				return
			}
			// A single print that crosses the remaining budget is cut
			// to it, with a note: no one print can flood the result.
			room := limits.OutputCapChars - st.printed
			if len(msg) > room {
				msg = caps.Prefix(msg, room) + " …"
			}
			st.output = append(st.output, msg)
			st.printed += len(msg) + 1
		},
		Load: func(_ *starlark.Thread, _ string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("the script cannot load modules")
		},
	}
	thread.SetMaxExecutionSteps(uint64(limits.ExecSteps))

	// The run's context bounds the script: a timeout, an interrupt and
	// the session's own cancellation all land as a thread cancel. The
	// watcher lives only as long as the run context does, and the run
	// cancels that before it returns.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutS)*time.Second)
	defer cancel()
	st.ctx = runCtx
	go func() {
		<-runCtx.Done()
		thread.Cancel("codemode: the run's time is up or the run was interrupted")
	}()

	predeclared := starlark.StringDict{
		"call":   starlark.NewBuiltin("call", st.callBuiltin),
		"gather": starlark.NewBuiltin("gather", st.gatherBuiltin),
		"exit":   starlark.NewBuiltin("exit", st.exitBuiltin),
	}
	_, err = starlark.ExecFileOptions(syntax.LegacyFileOptions(), thread, "script.star", opts.Script, predeclared)

	result := codemode.RunResult{
		Output: st.output,
		Value:  st.value,
		Calls:  st.calls,
	}
	if st.exited {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("the script failed: %w", err)
	}
	return result, nil
}

// resolveLimits fills the limits the session left at zero and refuses
// one it got wrong: a negative limit has no meaning, and a negative
// ExecSteps would wrap to a huge uint64 and switch the step budget off.
func resolveLimits(limits codemode.Limits) (codemode.Limits, error) {
	for _, limit := range []struct {
		name  string
		value int
	}{
		{"timeout_s", limits.TimeoutS},
		{"script_max_bytes", limits.ScriptMaxBytes},
		{"output_cap_chars", limits.OutputCapChars},
		{"exec_steps", limits.ExecSteps},
	} {
		if limit.value < 0 {
			return codemode.Limits{}, fmt.Errorf("the codemode limit %s is negative", limit.name)
		}
	}
	if limits.TimeoutS == 0 {
		limits.TimeoutS = DefaultTimeoutS
	}
	if limits.ScriptMaxBytes == 0 {
		limits.ScriptMaxBytes = DefaultScriptMaxBytes
	}
	if limits.OutputCapChars == 0 {
		limits.OutputCapChars = DefaultOutputCapChars
	}
	if limits.ExecSteps == 0 {
		limits.ExecSteps = DefaultExecSteps
	}
	return limits, nil
}

// callBuiltin runs one tool call: `call("read", {"path": "..."})`.
//
// A refusal, a failure and a cancellation come back as data in the
// result; only a call that could not be made at all is a Starlark
// error.
func (st *state) callBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var value starlark.Value = starlark.None
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name, "args?", &value); err != nil {
		return nil, err
	}
	return st.call(thread, name, value)
}

// call makes one nested call and shapes its result.
func (st *state) call(thread *starlark.Thread, name string, value starlark.Value) (starlark.Value, error) {
	raw, err := marshal(thread, value)
	if err != nil {
		return nil, err
	}
	results, err := st.nested.Call(st.ctx, []poo.NestedCall{{Tool: name, Arguments: raw}})
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("the %s call came back with %d results", name, len(results))
	}
	st.calls++
	return nestedToStarlark(results[0]), nil
}

// gatherBuiltin fans out: `gather([("read", {...}), ("grep", {...})])`.
//
// The whole list is one batch, and the results come back in the order
// the list names them. The seam decides how the batch runs: the calls
// that only read run at the same time, and the rest run in order. The
// arguments are marshalled before the batch starts, on the
// interpreter's own goroutine: a Starlark thread is not safe for
// concurrent use, so nothing after this point touches it.
func (st *state) gatherBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "calls", &list); err != nil {
		return nil, err
	}
	if list.Len() == 0 {
		return starlark.NewList([]starlark.Value{}), nil
	}
	if list.Len() > MaxGatherWidth {
		return nil, fmt.Errorf("gather takes at most %d calls, got %d", MaxGatherWidth, list.Len())
	}
	calls := make([]poo.NestedCall, list.Len())
	for i := 0; i < list.Len(); i++ {
		entry := list.Index(i)
		tuple, ok := entry.(starlark.Tuple)
		if !ok || tuple.Len() != 2 {
			return nil, fmt.Errorf("gather entry %d is not a (name, args) pair", i)
		}
		name, ok := starlark.AsString(tuple.Index(0))
		if !ok {
			return nil, fmt.Errorf("gather entry %d's name is not a string", i)
		}
		raw, err := marshal(thread, tuple.Index(1))
		if err != nil {
			return nil, fmt.Errorf("gather entry %d: %w", i, err)
		}
		calls[i] = poo.NestedCall{Tool: name, Arguments: raw}
	}
	results, err := st.nested.Call(st.ctx, calls)
	if err != nil {
		return nil, fmt.Errorf("gather: %w", err)
	}
	if len(results) != len(calls) {
		return nil, fmt.Errorf("gather asked for %d calls and came back with %d results", len(calls), len(results))
	}
	st.calls += len(results)
	values := make([]starlark.Value, len(results))
	for i, res := range results {
		values[i] = nestedToStarlark(res)
	}
	return starlark.NewList(values), nil
}

// exitBuiltin ends the script: `exit({"answer": 42})`.
//
// The value is the run's return value, and an exit without one leaves
// the run without a value. The thread is cancelled, which unwinds the
// script; the run sees the exit flag and treats the stop as the result.
func (st *state) exitBuiltin(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value = starlark.None
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "value?", &value); err != nil {
		return nil, err
	}
	if value != starlark.None {
		raw, err := marshal(thread, value)
		if err != nil {
			return nil, err
		}
		st.value = raw
	}
	st.exited = true
	thread.Cancel(exitReason)
	return starlark.None, nil
}

// marshal turns a Starlark value into JSON.
func marshal(thread *starlark.Thread, value starlark.Value) (json.RawMessage, error) {
	out, err := starlark.Call(thread, jsonmod.Module.Members["encode"], starlark.Tuple{value}, nil)
	if err != nil {
		return nil, fmt.Errorf("the call's arguments: %w", err)
	}
	return json.RawMessage(string(out.(starlark.String))), nil
}

// nestedToStarlark shapes a nested result for the script.
//
// The structured payload stays a JSON string: the script parses what it
// needs, and the engine does not make a second schema round trip.
func nestedToStarlark(res poo.NestedResult) starlark.Value {
	dict := starlark.NewDict(6)
	_ = dict.SetKey(starlark.String("handle"), starlark.String(res.Handle))
	_ = dict.SetKey(starlark.String("tool"), starlark.String(res.Tool))
	_ = dict.SetKey(starlark.String("outcome"), starlark.String(string(res.Outcome)))
	_ = dict.SetKey(starlark.String("text"), starlark.String(res.Text))
	_ = dict.SetKey(starlark.String("is_error"), starlark.Bool(res.IsError))
	if res.Structured == nil {
		_ = dict.SetKey(starlark.String("structured"), starlark.None)
	} else {
		_ = dict.SetKey(starlark.String("structured"), starlark.String(res.Structured))
	}
	return dict
}
