# AGENTS.md

`poo` is a modular Go agent harness: a headless CLI + interactive REPL and an embeddable Go library.

**Core philosophy:** Many JS-written harnesses promote a *self-improving* agent. `poo` promotes an *engineered* agent—compile-time composition, explicit FSM states, closed settle reasons, transparent history, and verifiable contracts.

## Start Here (Do Not Blindly Grep Root)

The repository root holds `package poo` (~60 `.go` files). Before grepping source files, read:

- `docs/public/llms.txt` — one-line map of every documentation page.
- `docs/reference/settings.md` — all 147 `settings.yaml` keys, types, and defaults.
- `docs/reference/cli.md` — every CLI command, subcommand, and flag.
- `docs/configuration/reasoning-and-sampling.md` — custom model reasoning (`mode`, `param`, `reasoning_levels`), sampling, and transport quirks.

## Feature & Package Map

| Subsystem | Source of Truth | Docs Page |
|---|---|---|
| Core loop, FSM, hooks, message model | root `package poo` (`state.go`, `execute.go`, `modelinfo.go`) | `docs/extending/architecture.md` |
| Settings schema, discovery & validation | `config/settings.go`, `config/schema/settings.umpire.json`, `config/checks.go` | `docs/reference/settings.md`, `docs/configuration/settings-discovery.md` |
| Models catalog, probing & `models.yaml` | `provider/catalog/`, `config/modelsfile.go` | `docs/configuration/models-and-catalog.md`, `docs/reference/models-file.md` |
| Providers & auth (`codex`, `opencode`, `ollama-cloud`, `typesafe`) | `provider/*`, `cmd/poo/login.go`, `cmd/poo/auth*.go` | `docs/configuration/providers-and-auth.md`, `docs/reference/wire-api-mapping.md` |
| CLI commands, flags, front door & daemon | `cmd/poo/app.go`, `cmd/poo/flags.go`, `cmd/poo/daemon.go` | `docs/reference/cli.md`, `docs/operations/limits-and-exit-codes.md` |
| Interactive REPL | `repl/` | `docs/getting-started/repl.md` |
| Profiles, system prompts & skills | `profile/*`, `prompt/`, `skill/`, `systemprompt.go` | `docs/configuration/profiles-and-prompts.md`, `docs/capabilities/skills.md` |
| Feeds & `poo daemon` | `feed/`, `feed.go`, `cmd/poo/daemon.go`, `subscribe_feed.md` | `docs/capabilities/feeds-and-daemon.md`, `docs/reference/manifests.md` |
| Context compaction (`mine`, `pointer`, `summary`, `handoff`, `native`) | `compact/*`, `compaction*.go` | `docs/capabilities/compaction.md` |
| Decision model (`roles.decider`) | `decide/`, `perm/decide/`, `decider.go` | `docs/capabilities/decision-model.md` |
| CLI registry & non-CLI script tools | `cli/`, `tool/cliregistry/` | `docs/capabilities/cli-registry.md`, `docs/reference/manifests.md` |
| MCP servers & resource feeds | `mcp/`, `mcp/manifest/`, `tool/callmcp/` | `docs/capabilities/mcp-servers.md`, `docs/reference/manifests.md` |
| Codemode (QuickJS WASM & Starlark) | `codemode/*`, `tool/codemode/` | `docs/capabilities/codemode.md` |
| Built-in tools & background jobs | `tool/*`, `jobs.md` | `docs/capabilities/tools-and-jobs.md` |
| Permissions, doom loop & bwrap sandbox | `perm/`, `protect/`, `exec/bwrap/` | `docs/operations/permissions-and-safety.md`, `docs/operations/sandbox.md` |
| Sessions, durability, SQLite & log v0 | `session/`, `session/sqlite/` | `docs/operations/sessions-and-durability.md`, `docs/reference/session-log-v0.md` |
| Wire protocol, server & conformance | `server/`, `wire/`, `wire/protocol/`, `wire/conformance/`, `pootest/` | `docs/extending/custom-binary.md`, `docs/extending/wire-protocol.md`, `docs/extending/conformance-and-testing.md` |
| Evals & Harbor Terminal-Bench adapter | `eval/`, `evals/`, `evals/harbor/`, `cmd/poo-eval/` | `docs/reference/evals.md`, `docs/reference/observed.md` |

## Architecture Invariants

- **Stdlib-only core:** `poo`, `poo/prompt`, `poo/decide`, and `poo/wire/protocol` import only the Go standard library. `internal/depcheck` enforces this in `go test`.
- **Compile-time composition:** Opt-in packages wire in at compile time via `server.New`. Do not introduce runtime plugin loading or global registries that leak unlinked tools.
- **Transparent history:** Compaction writes a visible replacement and keeps the session store intact. Never rewrite stored history silently.
- **Verification commands:**
  ```sh
  mise exec -- go vet ./...
  mise exec -- go test -race ./...
  ```

## Updating Documentation When Features Land

Keep `docs/` in sync in the same PR that changes user-facing behavior:

1. **Settings changes (`config/settings.go`):** Update `docs/reference/settings.md` and the relevant guide in `docs/configuration/`, `docs/capabilities/`, or `docs/operations/`.
2. **CLI flags or subcommands (`cmd/poo/app.go`, `flags.go`):** Update `docs/reference/cli.md` and `docs/operations/limits-and-exit-codes.md` (if exit codes or limits changed).
3. **Manifests (`feed/`, `cli/`, `mcp/manifest/`):** Update `docs/reference/manifests.md` and the matching capability guide.
4. **New pages:** Add YAML frontmatter (`title`, `description`), register the page in both `docs/.vitepress/config.mts` (sidebar) and `docs/scripts/generate-llms.mjs` (section list), and link it from `README.md` if it is a top-level entry point.
5. **Keep `README.md` concise:** `README.md` is a ~120-line front door and map, not a feature dump. Put detailed docs in `docs/`.
6. **Tighten prose with `writetighter`:** Before pushing doc edits, run `writetighter lint` (and `writetighter revise` when configured) on changed markdown files:
   - `--kind reference` for `docs/reference/*`
   - `--kind description` for guides and `README.md`
   - `--kind agent-instruction` for `AGENTS.md`
   Resolve all enforced findings (such as the 25-word sentence limit).
7. **Rebuild & regenerate `llms.txt`:** Run `npm --prefix docs run docs:build`. This regenerates `docs/public/llms.txt` and `docs/public/llms-full.txt` and fails on dead internal links. Commit the updated `llms.txt` and `llms-full.txt` alongside your doc changes.
