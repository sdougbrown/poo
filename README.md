# poo

Poo is a modular agent harness written in Go, with two deliverables from one
codebase: a **headless CLI** that runs a session to completion without a TUI,
and a **Go library** that a wrapping application links into its own binary,
injecting its own tools, providers and policies.

The customization story is "import it and make your own", not "use our binary
and our plugin surface". Tools, providers and policy modules are ordinary Go
packages wired at compile time, so a consumer compiles a harness containing
exactly what it wants — and what it leaves out cannot be reached at runtime.

**Status: experimental.** The Go APIs, settings and wire protocol may change
without a deprecation period until the TUI launches.

## What works today

Stage 1 is the walking skeleton: enough of a harness to put a real model on
a real task and measure what happens.

```
poo run --base-url http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --max-turns 8 --log session.jsonl \
  "List the Go packages in this repository."
```

The command talks to an OpenAI-compatible endpoint, runs the `bash` and
`read` tools, and settles with a reason. The exit code says why: 0 for
`end_turn`, 11 for `max_turns`, 13 for `context_exhausted`, 20 for
`provider_error` and 130 for `cancelled`.

Every run writes an append-only JSONL log. The record names and their
fields are in [docs/session-log-v0.md](docs/session-log-v0.md). A log holds
one session: `poo run` creates the file exclusively and refuses a path
that already exists, rather than appending a second session to it.

## Layout

| Package | Role |
|---|---|
| `poo` | Core: agent loop FSM, tool registry, hooks, event stream, session lifecycle |
| `poo/decide` | Decision-model types: questions, answers, the decider, the recording wrapper |
| `poo/prompt` | The system prompt's vocabulary: named sections, the alternatives they offer, and the environment they read |
| `poo/pootest` | Exported test helpers: a scripted transport, an in-memory sink, a stub decider, a scripted compactor |
| `poo/tool/*` | Opt-in tools: `read`, `edit`, `grep`, `bash`, `cliregistry` |
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
