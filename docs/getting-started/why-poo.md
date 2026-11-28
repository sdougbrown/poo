---
title: Why poo?
description: The design philosophy behind poo — engineered agents, the extensibility ladder, stdlib-only core, explicit FSM, non-silent history, and Harbor eval results.
---

# Why poo?

Many JS-written harnesses like pi promote a self-improving agent. `poo` promotes an **engineered agent**.

Instead of a runtime plugin ecosystem that mutates itself mid-session, `poo` ships two deliverables from one Go codebase. The first is a **headless CLI** that runs a session to completion without a TUI. The second is an **embeddable Go library** that a wrapping application links into its own binary. What you leave out of a build cannot be reached at runtime — a read-only proving binary simply does not link `bash`.

## The Extensibility Ladder

Not every customization requires writing Go. `poo` separates operational configuration from structural capability across three layers:

1. **Drop-in manifests (no rebuild):** Extend an existing `poo` binary with declarative JSON/YAML files on disk:
   - [`cli-registry`](/capabilities/cli-registry) manifests to expose CLI tools through `call_cli` (and `find_tool` when a catalog grows large).
   - [`mcp.json`](/capabilities/mcp-servers) manifests for MCP servers and resource feeds.
   - [`feeds`](/capabilities/feeds-and-daemon) manifests for long-lived command streams that deliver events into a session.
   - [`skills`](/capabilities/skills) directories (`SKILL.md`) for on-demand procedural guidance.
2. **YAML profiles & prompt sections (no rebuild):** Shape behavior per project or role in `settings.yaml`. Compose profiles (`base`, `code`, `search`, or custom profiles), append or swap named prompt sections, and tune compaction strategies. You can also attach permission rules, asker chains, and `bwrap` sandboxing.
3. **Compile-time Go binary (`server.New`):** Use this layer when you need hard capability boundaries, custom tools, custom executors, or a bespoke wire server. Import `poo` as a library, wire your packages into `server.New`, and ship a single static binary. See [Build Your Own Binary](/extending/custom-binary).

## Stdlib-Only Core

The core packages — `poo`, `poo/prompt`, `poo/decide`, and `poo/wire/protocol` — depend on the Go standard library only. `internal/depcheck` enforces this boundary in `go test`.

Opt-in packages (YAML/TOML config, MCP SDK, QuickJS/Starlark codemode, SQLite, readline) live outside the core. Consumers embedding `poo` only pull in the dependencies they actually link.

## Explicit FSM & Closed Settle Reasons

Agent loops built around `while True` and exception-driven control flow make wire protocols, session replay, and headless automation fragile. `poo` runs an explicit finite state machine:

```
idle → requesting → streaming → executing → compacting → settling → settled
```

Every run terminates with a closed, typed settle reason (such as completed, cancelled, turn/token/duration limit, or truncation). Runs never rely on a magic string printed to stdout or a control message mixed into the transcript. Tool calls are recorded as typed records with explicit outcomes, and the `finish` tool carries a schema-validated result.

## Non-Silent History

`poo` enforces a single governing rule for transcripts: **nothing rewrites the model's view of history silently.**

- The append-only session store preserves the full trajectory.
- Compaction never mutates or drops older turns behind the scenes. Strategies like `summary`, `handoff`, `mine`, `pointer`, and `native` record a visible, typed replacement in the session log while leaving the underlying store intact.
- The rendered system prompt is frozen for the length of a run so provider prompt caches remain valid.

## Proven on Evals

Architecture claims only matter if the harness can drive models through real multi-step engineering tasks. `poo` ships both a 20-task local evaluation suite (`poo-eval`) and a [Harbor](https://github.com/harbor-framework/harbor) adapter (`evals/harbor`) for Terminal-Bench and SWE-bench.

In head-to-head Terminal-Bench runs on locally hosted `glm-5.3-flash`, `poo` beat `pi` and tied `mini-swe-agent`. That is not an exhaustive benchmark across every frontier model, and results will vary by model and dataset. Still, it shows that `poo`'s explicit FSM, stateless shell contract, and compaction pipeline hold up under relentless autonomous runs.

## Further Reading

- [Prior Art](/extending/prior-art) — exactly what `poo` copies and rejects from `mini-swe-agent` and `pi-durable`.
- [Evals & Benchmarks](/reference/evals) — running `poo-eval` and the Harbor Terminal-Bench / SWE-bench adapter.
- [Build Your Own Binary](/extending/custom-binary) — wiring custom tools, profiles, and conformance tests at compile time.
