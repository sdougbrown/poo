---
title: Prior Art
description: What poo adopts and rejects from mini-swe-agent and pi-durable, and how those choices shaped the harness.
---

# Prior Art

`poo` builds on concrete ideas from earlier agent harnesses and deliberately rejects others. This page records what was studied, what `poo` adopted, and where it diverges.

## `mini-swe-agent`

- **Source:** [SWE-agent/mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent)
- **Version studied:** `v2.4.6` (`04d809ceab9df28f9adaed044884180159172930`)
- **Areas studied:** default and interactive agent loops, local shell execution, exception control flow, YAML config templates, and tool-call parsing.

### Adopted

- **Bash as the primary verb.** `mini-swe-agent` treats shell execution as the main way to act on a repo. `poo` keeps `bash` central in the `code` [profile](/configuration/profiles-and-prompts) and adds `read`, `grep`, `edit`, and `write` so read-only binaries can omit `bash` entirely.
- **Stateless shell per call.** Every command runs in a fresh subshell; `cd` and exported variables do not persist across calls. `poo`'s system prompt states this contract explicitly.
- **Linear, append-only history.** Nothing rewrites the model's view of history silently. Compaction always writes a visible replacement and leaves the session store intact.
- **Observation envelope with head/tail elision.** Above a context-relative cap, `poo` renders the head and tail of command output with an elision count. The envelope hints at `grep`, `sed`, `head`, or `tail` to inspect smaller slices.
- **Section-based prompt structure.** `poo` splits the operating contract, tool rules, and environment context into named [prompt sections](/configuration/profiles-and-prompts). It keeps `mini-swe-agent`'s shell guidance while using native tool calls where supported.
- **Process-group kill on timeout.** Timed-out commands kill their entire process group (with a grace period before `SIGKILL`) so no orphaned child processes survive.

### Rejected

- **Bash as the only tool.** Read-only builds cannot link `bash` at all, so file inspection needs dedicated read-only tools.
- **Magic-string completion.** Instead of ending a run when stdout prints a sentinel string, `poo` provides the typed, schema-validated [`finish` tool](/capabilities/tools-and-jobs).
- **Exception-driven control flow.** `poo` uses an explicit [session FSM](/extending/architecture) with a closed [settle-reason enum](/operations/limits-and-exit-codes). The [wire protocol](/extending/wire-protocol), `run/inspect`, and session replay all depend on a known state set.
- **Format-error retry counters.** `poo` returns malformed tool calls to the model as recoverable validation errors and bounds retries with `max_continuations`.
- **Control messages inside the transcript.** `poo` keeps the message list to model and tool turns and records control outcomes as typed [session log records](/reference/session-log-v0).
- **Heavy core dependencies.** `poo`'s core packages (`poo`, `poo/prompt`, `poo/decide`, `poo/wire/protocol`) depend only on the Go standard library, enforced by `internal/depcheck`. Opt-in packages (such as the [REPL](/getting-started/repl), SQLite store, and QuickJS engine) carry their own dependencies only when linked.

## `pi-durable`

- **Source:** ["Pi Durable"](https://earendil.com/posts/pi-durable/) (Earendil Engineering, October 2026)
- **Areas studied:** storage backends, single-owner locking, durable tasks, prompt sections, extensions, forks, and compaction.

### Adopted

- **Single-owner storage authority.** `poo` supports JSONL and SQLite [session stores](/operations/sessions-and-durability) and enforces single-process ownership via file locks and lease TTLs.
- **Session-to-run lifecycle.** A session holds a sequence of runs (one active run at a time) behind a clean store, projection, and authority seam.
- **Write-ahead durability.** `poo` writes every record as it happens. Loading a `logged` session marks any interrupted run; loading a `durable` session plans and resumes the run from the interrupted phase.
- **Section-based prompts.** Like `pi-durable`, `poo` assembles prompts from named sections. Unlike `pi-durable`, `poo` freezes the rendered prompt for the length of a run so provider prompt caches stay valid.
- **Structured tool-call records.** Every tool call is recorded with its intent and outcome as it runs.
- **Transport-level prefix forks.** [Compaction strategies](/capabilities/compaction) (`mine`, `pointer`, `handoff`) fork the last main request byte-for-byte to reuse the provider's prompt cache without creating a separate conversation branch.
- **Handoff notes.** `poo`'s `handoff` compaction strategy mirrors `pi-durable`'s `reset(note)` pattern as an opt-in strategy alongside the default `mine` strategy.

### Rejected

- **Task graphs and subagent trees.** `poo` runs one agent loop per session with no built-in subagent tree. Multi-agent orchestration belongs to an external supervisor (such as Avenor via [feeds](/capabilities/feeds-and-daemon) or [MCP](/capabilities/mcp-servers)).
- **First-class forked conversations.** Sessions remain linear; forks exist only at the transport layer for compaction passes.
- **Runtime-mutable extension registries.** `poo` composes Go extensions at compile time via [`server.New`](/extending/custom-binary). For no-rebuild extensibility, operators drop in declarative [CLI registry](/capabilities/cli-registry) or [MCP](/capabilities/mcp-servers) manifests instead of mutating a running registry.
- **Automatic tool replay policies.** `poo` does not tag tools `replay: "safe"` to re-run them automatically after a crash. Recovery records an interrupted outcome so the caller or durable planner can continue safely.
- **Typed application-state documents.** `poo` has no built-in document store for plans or todos.
- **Silent background compaction.** Compaction never rewrites history behind the model's back. Every strategy writes a visible replacement and leaves stored history intact.

## Related Pages

- [Why `poo`?](/getting-started/why-poo) — core philosophy and extensibility ladder
- [Architecture](/extending/architecture) — package boundaries, FSM states, and hooks
- [Context Compaction](/capabilities/compaction) — compaction strategies and transparent history
- [Sessions & Durability](/operations/sessions-and-durability) — JSONL/SQLite stores, leases, and crash recovery
