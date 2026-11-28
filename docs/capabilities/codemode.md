---
title: Codemode
description: Execute model-written QuickJS or Starlark scripts through poo's nested-call seam with full permission, doom-loop, and session-log enforcement.
---

# Codemode

Codemode lets the model write a short script to orchestrate tool calls instead of issuing them one turn at a time. Intermediate tool outputs stay inside the script, and only what the script prints and returns enters the conversation context.

In `poo`, codemode is not a privileged escape hatch: it is wired through `poo`'s nested-call seam (`poo.NestedCaller`). Every tool call a script makes passes through the same permission engine, doom-loop detector, and session log as a direct call from the model.

## How it works

1. The model invokes the `codemode` tool with a `script` argument.
2. `poo` runs the script inside a sandboxed engine (`poo/codemode.Engine`) that exposes no host filesystem, network, or timers — only the session's offered tools.
3. Each tool call the script makes is dispatched through `poo.NestedCaller`:
   - **Permissions:** Every nested call is judged individually by the [permission policy and asker chain](/operations/permissions-and-safety). A refusal or error is returned to the script as structured data (`outcome`, `is_error`) rather than crashing the run.
   - **Doom-loop detection:** Repeated identical or near-identical calls inside a script count toward the session's doom-loop thresholds.
   - **Session logging:** Each nested call is recorded in the [session log](/reference/session-log-v0) with mode `"nested"` and the parent `codemode` call's handle.
   - **Single-level nesting:** A script cannot invoke `codemode` itself.
4. Nested tool calls keep up to 1,000,000 characters of output internally so the script can filter or summarize large results. Only the script's printed lines and final return value are sent back to the model, bounded by `output_cap_chars` and the session's observation cap.
5. If a script fails or times out partway through, `poo` still reports the call count and prior printed output. The model can then see what already ran.

## Engines

`poo` ships two engines behind the `poo/codemode.Engine` interface. A custom binary that links no engine offers no `codemode` tool; see [Build Your Own Binary](/extending/custom-binary) for wiring engines.

### QuickJS (default)

`poo/codemode/quickjs` runs QuickJS-NG compiled to WebAssembly under `wazero`. It requires no Node.js installation, mounts an inert null filesystem, and provides no network or timer APIs.

A script is the body of an async JavaScript function:

```js
const files = await Promise.all([
  tools.read({ path: "README.md" }),
  tools.read({ path: "go.mod" }),
]);
return files.map((r) => (r.is_error ? r.text : r.text.split("\n")[0]));
```

Key primitives:

- `await tools.<name>(args)` invokes a session tool and resolves to `{ handle, tool, outcome, text, is_error, structured }`. `structured` is a native JSON value (or `null` when absent; invalid JSON remains a string).
- `Promise.all([...])` batches calls together. A batch executes concurrently when every call in it is read-only; otherwise calls run in submission order.
- `await parallel(() => [...])` overlaps independent calls, including writes. Tool promises must be created synchronously inside the callback and returned once each:

  ```js
  const [a, b] = await parallel(() => [
    tools.edit({ path: "a.txt", old: "draft", new: "final" }),
    tools.edit({ path: "b.txt", old: "draft", new: "final" }),
  ]);
  ```

  If two `poo` writes overlap on the same resolved path in one process, one returns `is_error: true` with `structured: { code: "edit_conflict" }`. `poo` does not detect shell command conflicts or roll back a batch.
- `print(value)` appends a line to the script's output; `return value` returns a JSON value.
- Batches run in submission order, with at most 16 calls per batch.

### Starlark

`poo/codemode/starlark` runs scripts in Starlark, a deterministic Python-like dialect with no file, network, clock, module-loading, or recursion capabilities, and an explicit execution-step budget (`exec_steps`).

Enable it with `tools.codemode.engine: starlark`:

```yaml
tools:
  codemode:
    engine: starlark
    exec_steps: 250000
```

Starlark builtins:

- `call(name, args)` runs one tool call and returns a dict with `handle`, `tool`, `outcome`, `text`, `is_error`, and `structured`.
- `gather([(name, args), ...])` runs a batch of up to 16 calls in list order (concurrently when all calls are read-only, sequentially otherwise).
- `json.decode(text)` and `json.encode(value)` convert between JSON strings and Starlark values.
- `print(x)` records an output line.
- `exit(value)` ends the script and returns `value` as JSON.

## Background execution

`codemode` supports background execution like other [background-capable tools](/capabilities/tools-and-jobs). Pass `background: true` on the tool call to run the script as a background job:

- The script's nested tool calls continue running after the originating turn ends, up to the job's timeout (`jobs.timeout_s`).
- Its output and return value arrive as a job result when the script finishes.
- Nested calls inside a batch cannot themselves request background jobs.

## Configuration

Configure codemode under `tools.codemode` in your settings file (see [Settings Reference](/reference/settings)):

```yaml
tools:
  codemode:
    engine: quickjs          # or "starlark"
    timeout_s: 120           # seconds per run
    script_max_bytes: 65536
    output_cap_chars: 16384
```

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `tools.codemode.engine` | string | `"quickjs"` | Script runtime: `"quickjs"` or `"starlark"`. |
| `tools.codemode.timeout_s` | int | `120` | Wall-clock limit for one script run, in seconds. |
| `tools.codemode.script_max_bytes` | int | `65536` | Maximum accepted script length in bytes (64 KiB). |
| `tools.codemode.output_cap_chars` | int | `16384` | Maximum printed characters retained before truncation (16 KiB). |
| `tools.codemode.exec_steps` | int | `10000000` (Starlark) | Execution step budget. Supported by Starlark only; setting it with QuickJS (or an unset engine) is a configuration error. |

To disable codemode in a session or profile, add `codemode` to `tools.disabled` or `profiles.<name>.tools.disabled`.
