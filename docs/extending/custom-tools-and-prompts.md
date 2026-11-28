---
title: Custom Tools & Prompts
description: Inject Go tools via poo.Tool, override embedded prompt sections with PromptFS, and implement custom command executors with poo/exec.
---

# Custom Tools & Prompts

When building a custom binary, you can inject Go tools directly into the runtime. You can also replace or extend embedded system prompt sections and supply custom command executors.

::: tip No-rebuild alternatives
To add tools without compiling Go, use [CLI Registry & Script Tools](/capabilities/cli-registry) or [MCP Servers](/capabilities/mcp-servers). The CLI registry also supports JSON stdin/stdout scripts in any language. See the [Manifests Reference](/reference/manifests) for schemas.
:::

## Injecting Go tools

### The `poo.Tool` interface

Every Go tool implements `poo.Tool`:

```go
type Tool interface {
	Spec() ToolSpec
	Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error)
	Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error)
}
```

- `Spec()` returns `poo.ToolSpec`: the tool's `Name`, `Description`, JSON `Schema`, side-effect `Class` (`ClassReadOnly` or `ClassSideEffecting`), optional `PrimaryArg` (used by doom-loop detection), `Background` mode (`BackgroundNever`, `BackgroundAllowed`, or `BackgroundAlways`), and `Idempotent` flag (whether a durable session may safely replay an interrupted call).
- `Prepare(raw, env)` decodes and validates raw JSON arguments with no side effects. Return `*poo.ErrInvalidArgs` (via `poo.InvalidArgsf`) for recoverable validation errors so the model can correct its call. The returned `poo.Prepared` provides `Summary() string` for transcripts and events.
- `Call(ctx, in, env)` executes the prepared call using `poo.ToolEnv` (working directory, scrubbed `Env`, `SessionID`, `RunID`, `Handle`, `JobID`, `Decider`, `History`, `Nested` caller, and `Settings`).

Optional interfaces a tool or its `Prepared` value may implement:

| Interface | Implemented by | Purpose |
|---|---|---|
| `poo.PromptContributor` | `Tool` | Contributes a `tools.<name>` system prompt section when the tool is linked and offered |
| `poo.PromptVariantContributor` | `Tool` | Provides variant-specific prompt text keyed by variant name |
| `poo.AskSummaryer` | `Prepared` | Supplies permission-ask text independent of model-authored summaries |
| `poo.CanonicalKeyer` | `Prepared` | Returns a canonical JSON key for doom-loop comparison |
| `poo.EvidenceArgumenter` | `Prepared` | Selects which arguments a decision model may inspect |
| `poo.BackgroundShorthander` | `Tool` | Translates shorthand syntax (such as trailing `&`) into a managed background job |
| `poo.BackgroundableCaller` | `Tool` | Reports that a running synchronous call can be moved into a background job |

### Registering with `config.ToolFactory`

Pass tool constructors in `server.Options.Tools` (or `config.BuildOptions.Tools`):

```go
type ToolFactory func(ToolOptions) poo.Tool

type ToolOptions struct {
	Executor       exec.Executor
	Scrub          exec.Scrub
	ProtectedPaths *protect.Set
	Registry       *cli.Catalog
	Search         bool
	Inputs         []poo.Modality
	CodemodeEngine string
	ToolNames      []string
	Offered        cli.Offerings
	MCP            []MCPRunning
}
```

- The map key is the name used by profiles, permission rules, and the wire protocol, and must match `Spec().Name`.
- `ToolNames` lists the tools the session offers so refusal messages only reference tools the model can actually call.
- `Offered` exposes the live snapshot of registered CLI commands and running MCP server tools.
- `MCP` holds the session's started MCP servers in selection order (empty at build time; populated when a session starts its servers).

For tools with pluggable engines such as `codemode`, wire the engine you link inside the factory:

```go
"codemode": func(config.ToolOptions) poo.Tool {
	return codemode.New(starlark.New())
},
```

Unlinking a tool removes its schema and its prompt section from the binary entirely. To omit a linked tool per project without rebuilding, use `tools.disabled` in settings. Note that `poo/config` does not import any tool package, so settings files still decode and validate even when a tool is not linked.

## Custom prompt directories (`PromptFS`)

Each built-in prompt section lives in an embedded markdown file beside the package that owns it. A custom binary can override any section by embedding a directory and passing it via `server.Options.PromptFS` (or `config.BuildOptions.PromptFS` when calling `config.Build` directly):

```go
//go:embed prompts
var promptDir embed.FS

prompts, err := fs.Sub(promptDir, "prompts")
if err != nil {
	return err
}
srv, err := server.New(server.Options{
	// ...
	PromptFS: prompts,
})
```

### Directory layout and section names

Files are named after the sections in `prompt.Order()` (`base`, `profile`, `catalog`, `environment`, `project`) and `tools.<tool>` for tool-owned sections. Subdirectories represent dot prefixes, and variants are sibling files with a variant suffix:

```text
prompts/
  base.md
  profile.md
  environment.md
  tools/bash.md
  tools/bash.small-local.md
```

- `profile.md` replaces a profile's own `profile.Profile.Prompt` prose (rendered immediately after `base`).
- A default section file (e.g. `tools/bash.md`) replaces the default text while preserving any embedded variants not explicitly overridden.
- Unknown section names or non-markdown files fail on load, naming the offending file.
- A linked tool's section loads for all profiles, but only profiles offering that tool render it.

### Templates and validation

Each markdown file is parsed as a Go `text/template` and receives the same data struct as the section it replaces:

- `environment.md`: `{{.Cwd}}`, `{{.OS}}`, `{{.Date}}`, `{{.Model}}`
- `catalog.md`: `{{.Search}}` (bool), `{{.Count}}` (int), `{{.Call}}`, `{{.Find}}`, and `{{.Entries}}` (`{{.Name}}`, `{{.Description}}`)
- `tools/jobs.md`: `{{.Feeds}}` (`{{.Name}}`, `{{.Description}}`)

A single trailing newline at EOF is stripped during rendering; an extra blank line preserves a trailing newline.

Templates are syntax-checked at load time and rendered when a session is constructed (or reconfigured with a new model or variant). A render failure prevents session creation (`config.Error` with code `invalid_value` on `system_prompt` in CLI mode, or `invalid_settings` on `session/create` over the wire). On `session/configure`, a render error rejects the change and leaves the session unchanged.

### Prompt layer precedence

Layers apply in order, with later layers winning:

1. Embedded defaults
2. Binary's `PromptFS` directory
3. Settings (`system_prompt.*`, CLI `--system-prompt` / `--append-system-prompt`)
4. User instructions (`BuildOptions.UserPromptFile` / `session.user_prompt_file`, process-wide)
5. Project instructions (`AGENTS.md`, read per session from the session's working directory)

`system_prompt.replace` clears the `PromptFS` sections; if the session offers registry tools, the embedded catalog section is restored after replacement. The [Skills](/capabilities/skills) catalog is a separate section rendered between `catalog` and `environment`. See [Profiles & Prompts](/configuration/profiles-and-prompts) for operator-level prompt settings.

## Custom executors (`poo/exec`)

Tools that run subprocesses (`bash`, `call_cli`, feeds) never spawn OS processes directly—they delegate to an `exec.Executor`:

```go
type Executor interface {
	Name() string
	Start(ctx context.Context, c Command, out io.Writer) (Process, error)
}

type Process interface {
	Wait() (exitCode int, err error)
	Kill()
	Stopped() bool
}
```

`exec.Command` carries `Argv`, `Dir`, scrubbed `Env`, optional `*string` `Stdin`, and optional `Stderr` (when `nil`, stdout and stderr are merged into `out`). When `ctx` is cancelled, the executor must stop the command and its process tree, and `Stopped()` must report `true` after `Wait()` returns. If `Stdin` cannot be written completely before the child exits, `Wait()` reports `exec.ErrStdinIncomplete` alongside the exit error (unless the command was stopped).

Register custom executors in an `exec.Registry` and pass it to `server.Options.Executors`:

```go
registry := exec.NewRegistry()
registry.Register("my-sandbox", func(opts exec.Options) (exec.Executor, error) {
	return newMySandbox(opts.Masker, opts.Sandbox)
})
```

`exec.Options` supplies `Masker` (for expanding protected paths to mask inside mounts) and `Sandbox` (`Network`, `ROBinds`, `RWBinds`). See [Sandboxing (bwrap)](/operations/sandbox) for the built-in Linux bubblewrap implementation.
