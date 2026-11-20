// Package quickjs runs JavaScript codemode scripts in QuickJS-NG under wazero.
package quickjs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fastschema/qjs"
	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/codemode/internal/script"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

// ScriptLanguage names the script language the engine runs, so the tool
// renders the guidance the engine's scripts need.
func (*Engine) ScriptLanguage() string { return "quickjs" }

func (*Engine) Run(ctx context.Context, opts codemode.RunOptions) (result codemode.RunResult, err error) {
	limits, err := script.CheckRun(opts)
	if err != nil {
		return result, err
	}
	if limits.ExecSteps != 0 {
		return result, fmt.Errorf("quickjs does not support the exec_steps limit; use timeout_s")
	}
	null, err := os.Stat(os.DevNull)
	if err != nil || null.Mode()&os.ModeCharDevice == 0 {
		return result, fmt.Errorf("QuickJS requires a null device to disable its filesystem mount")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutS)*time.Second)
	defer cancel()
	var output script.Output
	// qjs panics when a WASM call traps, including after wazero closes a
	// cancelled guest. The trap itself stays out of the reported error: a
	// qjs panic embeds the host's Go stack, which the model must not see.
	// A real error keeps precedence over the synthesized trap one.
	defer func() {
		trap := recover()
		cause := runCtx.Err()
		result.Output, result.Truncated = output.Lines, output.Truncated
		switch {
		case err != nil:
		case trap == nil:
			// A clean completion keeps its result even when the context
			// finished during cleanup: no trap means nothing went wrong.
		case cause != nil:
			result.Value = nil
			err = fmt.Errorf("the script stopped: %w", cause)
		default:
			result.Value = nil
			err = fmt.Errorf("the QuickJS runtime stopped unexpectedly")
		}
	}()

	vm, err := newVM(runCtx)
	if err != nil {
		return result, fmt.Errorf("could not start the QuickJS runtime: %w", err)
	}
	defer vm.Close()
	js := vm.Context()
	js.SetFunc("__pooTools", func(this *qjs.This) (*qjs.Value, error) {
		if err := runCtx.Err(); err != nil {
			return nil, err
		}
		arg, err := stringArg(this)
		if err != nil {
			return nil, err
		}
		calls, err := decodeBatch(arg)
		if err != nil {
			return nil, err
		}
		if len(calls) == 0 {
			return js.NewString("[]"), nil
		}
		results, err := nestedCall(runCtx, opts.Nested, calls)
		if err != nil {
			return nil, err
		}
		if len(results) != len(calls) {
			return nil, fmt.Errorf("asked for %d calls and got %d results", len(calls), len(results))
		}
		result.Calls += len(results)
		data, err := encodeResults(results)
		if err != nil {
			return nil, err
		}
		return js.NewString(string(data)), nil
	})
	js.SetFunc("__pooPrint", func(this *qjs.This) (*qjs.Value, error) {
		line, err := stringArg(this)
		if err != nil {
			return nil, err
		}
		output.Print(line, limits.OutputCapChars)
		return js.NewUndefined(), nil
	})
	// The single-call bridge resolves its promise before returning, on
	// the same goroutine. An awaiting script then continues as a job of
	// the eval's own loop, so no host call crosses goroutines.
	js.SetAsyncFunc("__pooCall", func(this *qjs.This) {
		promise := this.Promise()
		if err := runCtx.Err(); err != nil {
			promise.Reject(js.NewString(err.Error()))
			return
		}
		args := this.Args()
		if len(args) != 2 || !args[0].IsString() || !args[1].IsString() {
			promise.Reject(js.NewString("a tool call needs a name and its arguments"))
			return
		}
		name, raw := args[0].String(), args[1].String()
		if !json.Valid([]byte(raw)) {
			promise.Reject(js.NewString(fmt.Sprintf("the %s call's arguments are not JSON", name)))
			return
		}
		results, err := nestedCall(runCtx, opts.Nested, []poo.NestedCall{{Tool: name, Arguments: json.RawMessage(raw)}})
		if err != nil {
			promise.Reject(js.NewString(err.Error()))
			return
		}
		if len(results) != 1 {
			promise.Reject(js.NewString(fmt.Sprintf("the %s call came back with %d results", name, len(results))))
			return
		}
		result.Calls++
		data, err := encodeResults(results)
		if err != nil {
			promise.Reject(js.NewString(err.Error()))
			return
		}
		promise.Resolve(js.NewString(string(data)))
	})

	// The script runs through one Eval with the async flag. QuickJS's
	// own loop pump drives the awaited calls, and the deadline closes
	// the guest when a script never settles.
	names, err := json.Marshal(toolNames(opts.Tools))
	if err != nil {
		return result, fmt.Errorf("the session's tool names: %w", err)
	}
	source, err := json.Marshal(opts.Script)
	if err != nil {
		return result, fmt.Errorf("the script: %w", err)
	}
	value, err := js.Eval("codemode.js", qjs.Code(wrapper+"("+string(names)+")("+string(source)+")"), qjs.FlagAsync())
	if err != nil {
		return result, fmt.Errorf("the script failed: %w", err)
	}
	// The async eval hands back the script's pending promise. Awaiting
	// it runs QuickJS's own loop on this goroutine, so every host
	// crossing stays single-threaded. A promise that never settles is
	// stopped by the deadline closing the guest. The promise itself is
	// left for the runtime's close: freeing it after the await traps
	// the guest.
	value, err = value.Await()
	if err != nil {
		return result, fmt.Errorf("the script failed: %w", err)
	}
	defer value.Free()
	if !value.IsUndefined() {
		raw := value.String()
		// The wrapper only ever returns JSON.stringify output, so this
		// guards the wrapper's own contract rather than the script.
		if !json.Valid([]byte(raw)) {
			return result, fmt.Errorf("the script did not return a JSON value")
		}
		result.Value = json.RawMessage(raw)
	}
	return result, nil
}

// toolNames lists the session's tools in offer order, for the wrapper's
// `tools` object.
func toolNames(infos []poo.ToolInfo) []string {
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name
	}
	return names
}

// newVM builds a guest whose filesystem mount is inert.
func newVM(runCtx context.Context) (*qjs.Runtime, error) {
	return qjs.New(qjs.Option{
		Context:            runCtx,
		CloseOnContextDone: true,
		// qjs always mounts CWD at the guest's root. It offers no way to
		// mount nothing, so the path points below the null device, where
		// no host directory exists. Any file operation traps the runtime
		// instead of opening a host file. The wrapper's globals deletion
		// is the boundary the model sees; this mount backs it.
		CWD:          filepath.Join(os.DevNull, "poo-codemode"),
		MemoryLimit:  64 << 20,
		MaxStackSize: 256 << 10,
		Stdout:       io.Discard,
		Stderr:       io.Discard,
	})
}

// nestedCall runs one batch of tool calls. A panicking tool must not
// surface as a script error carrying the host's Go stack. qjs stringifies
// a panic it catches, and that message reaches the model.
func nestedCall(ctx context.Context, nested poo.NestedCaller, calls []poo.NestedCall) (results []poo.NestedResult, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("the tool bridge failed")
		}
	}()
	return nested.Call(ctx, calls)
}

// The wrapper deletes the host's surface, then runs the script as the
// body of an async function whose only powers are the tool bridges. The
// session's tool names are injected as the NAMES argument, so the model
// calls `await tools.<name>(args)` instead of guessing names into a
// string dispatch.
const wrapper = `((NAMES) => async (source) => {
  const tools = globalThis.__pooTools, callOne = globalThis.__pooCall, log = globalThis.__pooPrint;
  for (const name of ["__pooTools", "__pooCall", "__pooPrint", "std", "os", "bjson",
    "print", "console", "scriptArgs", "setTimeout", "setInterval", "clearTimeout",
    "clearInterval", "QJS_PROXY_VALUE", "performance", "Date", "navigator",
    "gc", "eval"]) {
    if (!delete globalThis[name]) throw new Error("could not remove " + name);
  }
  const encode = JSON.stringify, decode = JSON.parse, text = String;
  let active = false;
  const gather = (batch) => {
    if (!active) throw new Error("codemode scripts cannot call tools after returning");
    return decode(tools(encode(batch)));
  };
  const named = {};
  for (const name of NAMES) {
    named[name] = (args) => {
      if (!active) throw new Error("codemode scripts cannot call tools after returning");
      return callOne(name, encode(args === undefined ? null : args)).then(decode).then((r) => r[0]);
    };
  }
  const call = (name, args = null) => gather([[name, args]])[0];
  const print = (value) => { if (active) log(text(value)); };
  const compile = Object.getPrototypeOf(async function () {}).constructor;
  const program = compile("call", "gather", "print", "tools", '"use strict";\n' + source);
  active = true;
  try {
    const value = await program(call, gather, print, named);
    active = false;
    // A script that returns nothing is a clean no-value result, like a
    // Starlark script that never exits: undefined passes to the host.
    const encoded = value === undefined ? undefined : encode(value);
    // The guest measures its own return value, so an oversized one never
    // crosses into host memory: the host would copy it several times.
    if (typeof encoded === "string" && encoded.length > 1048576) throw new Error("codemode: the return value is over 1048576 characters; print a summary instead");
    return encoded;
  } finally {
    active = false;
  }
})`

func stringArg(this *qjs.This) (string, error) {
	args := this.Args()
	if len(args) != 1 || !args[0].IsString() {
		return "", fmt.Errorf("expected a string")
	}
	return args[0].String(), nil
}

func decodeBatch(raw string) ([]poo.NestedCall, error) {
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("gather expects a JSON array of [name, args] pairs")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, fmt.Errorf("gather expects an array of [name, args] pairs")
	}
	var calls []poo.NestedCall
	for dec.More() {
		if len(calls) == script.MaxGatherWidth {
			return nil, fmt.Errorf("gather takes at most %d calls", script.MaxGatherWidth)
		}
		var pair []json.RawMessage
		if err := dec.Decode(&pair); err != nil || len(pair) != 2 {
			return nil, fmt.Errorf("gather entry %d is not a [name, args] pair", len(calls))
		}
		var name string
		if err := json.Unmarshal(pair[0], &name); err != nil || name == "" {
			return nil, fmt.Errorf("gather entry %d needs a tool name", len(calls))
		}
		calls = append(calls, poo.NestedCall{Tool: name, Arguments: pair[1]})
	}
	return calls, nil
}

func encodeResults(results []poo.NestedResult) ([]byte, error) {
	type toolResult struct {
		Handle     string  `json:"handle"`
		Tool       string  `json:"tool"`
		Outcome    string  `json:"outcome"`
		Text       string  `json:"text"`
		IsError    bool    `json:"is_error"`
		Structured *string `json:"structured"`
	}
	values := make([]toolResult, len(results))
	for i, r := range results {
		values[i] = toolResult{Handle: r.Handle, Tool: r.Tool, Outcome: string(r.Outcome), Text: r.Text, IsError: r.IsError}
		if r.Structured != nil {
			s := string(r.Structured)
			values[i].Structured = &s
		}
	}
	return json.Marshal(values)
}
