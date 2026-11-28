# poo

`poo` is a modular agent harness written in Go. One codebase produces a **headless CLI and interactive REPL** for OpenAI-compatible and Anthropic endpoints. It also ships as an **embeddable Go library** for custom binaries with only the tools, providers, and policies you link.

> Many JS-written harnesses promote a *self-improving* agent. `poo` promotes an *engineered* agent.

**Status: experimental.** Go APIs, settings, and wire protocol can change without a deprecation period until the TUI launches.

**Docs:** [poo.douggo.com](https://poo.douggo.com) · **Agent index:** [llms.txt](https://poo.douggo.com/llms.txt) · [llms-full.txt](https://poo.douggo.com/llms-full.txt)

## Why `poo`

- **Compile-time composition.** Core `package poo` depends only on the Go standard library (enforced by `internal/depcheck`). Link only what you want via `server.New`—unlinked tools and executors cannot be reached at runtime.
- **No-rebuild extensibility when you want it.** Drop a JSON manifest into `~/.config/poo/cli-registry/` to expose a CLI or script as a tool. Attach `mcp.json` servers per profile, or add `SKILL.md` directories.
- **Feeds & resident daemon (`poo daemon`).** Long-lived commands and MCP resource subscriptions push async inputs into a durable, lease-backed session. Each feed supports decision-model or script filtering and idle-wake controls.
- **Transparent compaction.** Choose `mine` (default), `pointer`, `summary`, `handoff`, `native` (Codex), or `none`, optionally with a dedicated `roles.compaction` model. Nothing rewrites stored history silently.
- **Decision model (`roles.decider`).** Wire a fast classifier model (such as TypeSafe Jev) into permission askers, feed record filters, and large tool-catalog search (`find_tool`).
- **Defense-in-depth safety.** Permission rules and asker chains, doom-loop detection, protected paths, per-tool working-directory controls, child env scrubbing, and a Bubblewrap (`bwrap`) sandbox executor.
- **Explicit FSM & wire conformance.** Closed settle-reason enum mapped 1:1 to exit codes, JSON-RPC 2.0 over stdio (`poo serve`), and an exported black-box conformance suite (`wire/conformance`) with `FakeEndpoint`.

## 60-Second Quickstart

Install a release binary into `~/.local/bin` (set `BINDIR` to override):

```sh
curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | sh
```

Or install from source (Go 1.26+):

```sh
go install github.com/sdougbrown/poo/cmd/poo@latest
```

Run a one-shot session against any OpenAI-compatible endpoint:

```sh
poo run --endpoint http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --max-turns 8 \
  "List the Go packages in this repository."
```

Or sign in to a built-in provider (`opencode`, `codex`, `ollama-cloud`, `typesafe`). Copy a starter file from `examples/` to `.poo/settings.yaml`, verify your roles, and open the REPL:

```sh
poo login opencode
cp examples/opencode.settings.yaml .poo/settings.yaml
poo roles
poo
```

## Documentation Map

All documentation lives in [`docs/`](docs/) and publishes to [poo.douggo.com](https://poo.douggo.com).

| If you want to… | Read |
|---|---|
| Understand the design philosophy & extensibility ladder | [Why `poo`?](docs/getting-started/why-poo.md) · [Prior Art](docs/extending/prior-art.md) |
| Install, configure starter settings, or learn the REPL | [Install](docs/getting-started/install.md) · [Quickstart](docs/getting-started/quickstart.md) · [Interactive REPL](docs/getting-started/repl.md) |
| Configure models, endpoints, or `models.yaml` | [Models & Catalog](docs/configuration/models-and-catalog.md) · [Models File Reference](docs/reference/models-file.md) |
| Set reasoning effort, budgets, sampling, or transport quirks | [Reasoning & Sampling](docs/configuration/reasoning-and-sampling.md) |
| Sign in to Codex, OpenCode, Ollama Cloud, or TypeSafe | [Providers & Auth](docs/configuration/providers-and-auth.md) |
| Customize profiles, system prompts, `AGENTS.md`, or skills | [Profiles & Prompts](docs/configuration/profiles-and-prompts.md) · [Skills](docs/capabilities/skills.md) |
| Run async feeds or a durable `poo daemon` | [Feeds & Daemon](docs/capabilities/feeds-and-daemon.md) · [Manifests Reference](docs/reference/manifests.md) |
| Tune context compaction (`mine`, `pointer`, `native`, and more) | [Context Compaction](docs/capabilities/compaction.md) |
| Add custom tools without rebuilding Go (CLI registry / MCP) | [CLI Registry & Script Tools](docs/capabilities/cli-registry.md) · [MCP Servers](docs/capabilities/mcp-servers.md) |
| Use Codemode (QuickJS WASM / Starlark) or a decision model | [Codemode](docs/capabilities/codemode.md) · [Decision Model](docs/capabilities/decision-model.md) |
| Lock down permissions, `bwrap` sandboxing, or durable sessions | [Permissions & Safety](docs/operations/permissions-and-safety.md) · [Sandboxing](docs/operations/sandbox.md) · [Sessions & Durability](docs/operations/sessions-and-durability.md) |
| Embed `poo` as a Go library or speak the JSON-RPC wire protocol | [Architecture](docs/extending/architecture.md) · [Build Your Own Binary](docs/extending/custom-binary.md) · [Wire Protocol](docs/extending/wire-protocol.md) |
| Look up every CLI flag, settings key, or exit code | [CLI Reference](docs/reference/cli.md) · [Settings Reference](docs/reference/settings.md) · [Limits & Exit Codes](docs/operations/limits-and-exit-codes.md) |
| Inspect the session log format or wire API mapping | [Session Log v0](docs/reference/session-log-v0.md) · [Wire API Mapping](docs/reference/wire-api-mapping.md) |

## Evals & Benchmarks

`poo` ships two evaluation tracks documented in [Evals & Benchmarks](docs/reference/evals.md):

- **`poo-eval`** (`evals/`): 20 tasks (coding fixes, find-and-explain, long-context) run inside `bwrap` isolation with fixture integrity hashing.
- **Harbor adapter** (`evals/harbor/`): runs a static Linux `poo` binary on Terminal-Bench and SWE-bench for paired harness comparisons. In head-to-head Terminal-Bench runs on locally hosted `glm-5.3-flash`, `poo` beat `pi` and tied `mini-swe-agent` (single-model scope, not an exhaustive benchmark).

## Repository Layout

The root directory holds `package poo` (the stdlib-only core agent loop, FSM, hooks, and message model). Everything else lives in opt-in subpackages:

| Area | Packages |
|---|---|
| **Core & contracts** | `poo` (root), `prompt/`, `decide/`, `wire/protocol/` (stdlib-only) |
| **CLI, REPL & server** | `cmd/poo/`, `repl/`, `server/`, `wire/`, `config/`, `profile/` |
| **Tools & scripting** | `tool/*` (`bash`, `read`, `edit`, `write`, `grep`, `fetch`, `codemode`, `cliregistry`, `callmcp`, `history`, `finish`), `codemode/*`, `cli/` |
| **Feeds, MCP & skills** | `feed/`, `mcp/`, `mcp/manifest/`, `skill/` |
| **Providers & compaction** | `provider/*` (`openai`, `anthropic`, `codex`, `auth`, `catalog`), `compact/*` (`mine`, `pointer`, `summary`, `handoff`, `native`) |
| **Safety & sessions** | `perm/`, `perm/decide/`, `protect/`, `exec/`, `exec/bwrap/`, `session/`, `session/sqlite/` |
| **Testing & evals** | `pootest/`, `wire/conformance/`, `eval/`, `evals/`, `cmd/poo-eval/` |

## Development

Requires [mise](https://mise.jdx.dev/) and Go 1.26 (see `mise.toml`):

```sh
mise exec -- go vet ./...
mise exec -- go test -race ./...
```

Preview the docs site locally:

```sh
npm --prefix docs install
npm --prefix docs run docs:dev
```

## License

Apache-2.0. See [LICENSE](LICENSE).
