# poo

Poo is a modular agent harness written in Go, with two deliverables from one
codebase: a **headless CLI** that runs a session to completion without a TUI,
and a **Go library** that a wrapping application links into its own binary,
injecting its own tools, providers and policies.

The customization story is "import it and make your own", not "use our binary
and our plugin surface". Tools, providers and policy modules are ordinary Go
packages wired at compile time, so a consumer compiles a harness containing
exactly what it wants — and what it leaves out cannot be reached at runtime.
[docs/build-your-own-binary.md](docs/build-your-own-binary.md) is the recipe:
the factory, the injected tools, the conformance suite and the closure gate.

**Status: experimental.** The Go APIs, settings and wire protocol may change
without a deprecation period until the TUI launches.

## What works today

A session runs against an OpenAI-compatible endpoint and settles with a
reason:

```
poo run --endpoint http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --max-turns 8 --log session.jsonl \
  "List the Go packages in this repository."
```

**Profiles.** The profile decides which tools a session offers. Pick one
with `--profile`; the default is `code`.

| Profile | Tools |
|---|---|
| `base` | none |
| `code` | `bash`, `read`, `grep`, `edit` |
| `search` | `read`, `grep` |

A settings file that names `finish.schema_file` also gets the typed
`finish` tool.

**Exit codes.** The exit code says why a run ended:

| Code | Reason |
|---|---|
| 0 | `end_turn`, `finished`, `stop_requested` |
| 10 | `output_truncated` |
| 11 | `max_turns` |
| 12 | `hook_continue_limit` |
| 13 | `context_exhausted` |
| 14 | `budget_exhausted` |
| 20 | `provider_error` |
| 21 | `tool_fatal` |
| 22 | `hook_error` |
| 23 | `store_error` |
| 24 | `provider_limit` |
| 130 | `cancelled` |

`poo run` exits 2 when it refuses its arguments, its settings or a prompt
that does not render. It exits 1 for a failure with no code of its own.

**Permissions.** A policy checks every tool call before it runs.
- A rule matches a tool name, a tool class or a bash command prefix.
- `permission.unmatched` (or `--permission-unmatched`) decides what happens
  to a side-effecting call no rule covers: `allow`, `ask` or `deny`.
- An `ask` goes through the asker chain. The `decide` asker asks a decision
  model. The `wire` asker asks the client of `poo serve`.
- `--permission-fallback` is the answer when no asker answers.
- A call repeated too often is a doom loop, and the file tools refuse the
  globs in `tools.protected_paths`.

The policy is in `poo/perm`.

**Sessions.** A logged session writes one JSONL file under `session.dir`,
one record per line. [docs/session-log-v0.md](docs/session-log-v0.md) lists
the records.
- `--log <path>` writes the log to a new file of your choice. It never
  extends an existing file.
- `--ephemeral` writes nothing.
- `poo sessions list` and `poo sessions prune` read and trim the store.
- `session/load` over the wire (or `session.Resume` in Go) resumes a
  stored session. A torn tail is recovered, an unfinished run is marked
  `interrupted`, and undelivered job results are queued.

**The wire.** `poo serve` speaks JSON-RPC 2.0 on stdin and stdout, one
object per line.
- Sessions: `session/create`, `session/load`, `session/configure`,
  `session/history`, `session/close`.
- Runs: `run/prompt`, `run/message`, `run/inspect`, `run/cancel`.
- The server also sends events, permission asks and job updates.

The contract is `poo/wire/protocol`. The conformance suite in
`poo/wire/conformance` drives any binary through it.

**Prompts.** The system prompt is a set of named markdown sections, each
embedded beside the package that owns it.
- A binary can replace sections with its own directory
  (`server.Options.PromptFS`).
- The `system_prompt.*` settings, `--system-prompt` and
  `--append-system-prompt` (`@file` reads a file) apply on top.
- `poo prompt` prints the sections a session would send.

**CLI registry.** When the settings list manifests in `cli_registry.paths`,
a profile with a side-effecting tool also offers `call_cli`. It runs one
registered command through the session's executor. Above 30 entries, with
a decision model configured, the profile also offers `find_tool` to search
the catalog. The code is in `poo/cli` and `poo/tool/cliregistry`.

**Your own binary.** A consumer links only the packages it wants, hands
its tools, profiles and executors to `server.New`, and serves the result
with `wire.Serve`. What it leaves out is not in the binary.
[docs/build-your-own-binary.md](docs/build-your-own-binary.md) is the
recipe, including the prompt directory, the conformance suite and the
closure gate.

## Layout

| Package | Role |
|---|---|
| `poo` | Core: agent loop FSM, tool registry, hooks, event stream, session lifecycle |
| `poo/decide` | Decision-model types: questions, answers, the decider, the recording wrapper |
| `poo/prompt` | The system prompt's vocabulary: named sections, the alternatives they offer, and the environment they read |
| `poo/pootest` | Exported test helpers: a scripted transport, an in-memory sink, a stub decider, a scripted compactor |
| `poo/tool/*` | Opt-in tools: `read`, `edit`, `grep`, `bash`, `cliregistry` |
| `poo/cli` | The cli registry's catalog: the manifests a session may call |
| `poo/exec` | The executor contract a binary fills: a command, its environment, and where it runs |
| `poo/protect` | The protected-path matcher: the paths a tool must not touch |
| `poo/server` | The `wire.Factory` a `serve` command is built from |
| `poo/provider/*` | Provider seam and transports |
| `poo/perm` | Permission engine: the policy over tool calls, the doom loop's action, and the asker chain that answers a call the policy asks about |
| `poo/perm/decide` | An asker that answers a permission call with a decision model |
| `poo/session` | Session seam: store, versioned codec, projection, authority |
| `poo/wire` | JSON-RPC 2.0 control protocol over stdio |
| `poo/wire/protocol` | The public contract: the method names, the error codes, and the request and result types a client depends on |
| `poo/config` | Opt-in settings loading and validation |
| `poo/cmd/poo` | The headless CLI |

The core depends on the standard library only (plus `poo/prompt` and
`poo/decide`). `internal/depcheck` enforces this in `go test`.

The message model is provider-neutral and carries what the three wire
APIs need. The round trip through Chat Completions, OpenAI Responses and
Anthropic Messages is written down in
[docs/wire-api-mapping.md](docs/wire-api-mapping.md).

## Development

Requires [mise](https://mise.jdx.dev/) and Go 1.25 (see `mise.toml`).

```
mise exec -- go vet ./...
mise exec -- go test -race ./...
```

## License

Apache-2.0. See [LICENSE](LICENSE).
