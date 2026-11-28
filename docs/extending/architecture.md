---
title: Architecture
description: Package layout, stdlib-only core dependency boundary, session FSM states, hook points, and provider-neutral message model.
---

# Architecture

Poo is a modular Go agent harness designed as a library first. The core package owns the agent loop, tool registry, hook points, event stream, and session lifecycle. Everything else—tools, executors, providers, persistence stores, and the wire server—is an opt-in package wired at compile time. What a binary does not link cannot be reached at runtime.

## Package layout

Packages are grouped by subsystem:

### Core and contracts

| Package | Role |
|---|---|
| `poo` | Core agent loop FSM, tool registry, hooks, event stream, and session lifecycle |
| `poo/prompt` | System prompt vocabulary: named sections, variants, and template environment |
| `poo/decide` | Decision-model contract: questions, answers, `Decider`, and recording wrapper |
| `poo/exec` | Executor contract (`Executor`, `Command`, `Process`), registry, and env scrubbing |
| `poo/protect` | Protected-path matcher shared by tools and permission policy |
| `poo/pootest` | Exported test helpers: scripted transport, in-memory sink, stub decider, scripted compactor |

### Tools and execution

| Package | Role |
|---|---|
| `poo/tool/*` | Opt-in tools: `read`, `edit`, `write`, `grep`, `bash`, `cliregistry`, `callmcp`, `codemode`, `fetch`, `finish` |
| `poo/exec/bwrap` | Linux bubblewrap sandboxing executor |
| `poo/codemode/*` | Opt-in codemode engines (`quickjs`, `starlark`) |
| `poo/profile/*` | Named tool profiles (`code`, `search`, etc.) with default prompt and permission posture |

### Providers, permissions, and persistence

| Package | Role |
|---|---|
| `poo/provider/*` | Provider seam, model catalog (`provider/catalog`), and wire transports (OpenAI, Anthropic, etc.) |
| `poo/perm` | Permission engine: tool call policy, doom-loop detection, and asker chain |
| `poo/perm/decide` | Permission asker backed by a decision model |
| `poo/session` | Session persistence seam: store, versioned codec, history projection, and locking |
| `poo/session/sqlite` | Opt-in SQLite session store |

### Capabilities and integrations

| Package | Role |
|---|---|
| `poo/cli` | CLI registry catalog and manifest loader |
| `poo/feed` | Feed manifest loader, environment builder, and stdout record runner |
| `poo/mcp` | MCP client: server lifecycle, tool dispatch, resource feeds, and relaunch loop |
| `poo/mcp/manifest` | `mcp.json` manifest loader, launch templates, and resource feed declarations |
| `poo/skill` | Skill directory discovery and `SKILL.md` frontmatter loader |

### Server, wire, and CLI

| Package | Role |
|---|---|
| `poo/wire/protocol` | Public JSON-RPC 2.0 wire contract: method names, error codes, and request/result DTOs |
| `poo/wire` | JSON-RPC 2.0 server over stdio |
| `poo/wire/conformance` | Black-box protocol conformance suite and exported fake provider endpoint |
| `poo/server` | `wire.Factory` implementation bridging `config` and `wire` |
| `poo/config` | Opt-in settings loading, schema validation, and runtime assembly |
| `poo/cmd/poo` | Reference headless CLI and `poo serve` binary |

## Stdlib-only core rule

The core package (`poo`) depends only on the Go standard library plus `poo/prompt`, `poo/decide`, and `poo/scopeschema`—each of which imports only the standard library. Several contract packages are also strictly standard-library-only so clients and extensions can depend on them without pulling in the rest of the module:

- `poo/wire/protocol`
- `poo/prompt` and `poo/prompt/agentsmd`
- `poo/decide`
- `poo/exec`
- `poo/protect`
- `poo/caps`
- `poo/scopeschema`
- `poo/mcp/wire`

`internal/depcheck` enforces these boundaries during `go test`. It inspects each checked package's non-test dependency closure via `go list -e -deps -json`. The test suite fails if any package imports an unapproved internal or third-party dependency. For example, only `poo/mcp` may import the MCP Go SDK. A binary that omits an MCP launcher never links that SDK.

See [Build Your Own Binary](/extending/custom-binary#the-closure-gate) for how custom binaries enforce their own dependency exclusions.

## Session FSM

A session moves through a closed set of explicit states (`poo.State`). Consumers add behavior through hooks rather than introducing new states.

| State | Meaning |
|---|---|
| `idle` | The session has no active run. |
| `requesting` | The run is requesting a response from the provider. |
| `streaming` | The provider's response stream is arriving. |
| `executing` | The run is executing tool calls requested by the model. |
| `compacting` | The run is compacting the conversation view before the next request. |
| `waiting` | The run is open while waiting on background jobs it started. |
| `settling` | The run is finalizing and recording its settle outcome. |
| `settled` | The run has completed; a new prompt transitions back to `requesting`. |

All state changes pass through a single transition function checked against the legal transition table (`poo.IsLegal`):

| From | Allowed next states |
|---|---|
| `idle` | `requesting` |
| `requesting` | `streaming`, `executing`, `compacting`, `requesting`, `waiting`, `settling` |
| `streaming` | `executing`, `requesting`, `compacting`, `waiting`, `settling` |
| `executing` | `executing`, `requesting`, `compacting`, `waiting`, `settling` |
| `compacting` | `requesting`, `waiting`, `settling` |
| `waiting` | `requesting`, `compacting`, `settling` |
| `settling` | `settled` |
| `settled` | `requesting` |

Once a run enters `settling` or `settled`, background work from the ending run cannot move it backwards; only a subsequent run moves `settled` back to `requesting`.

## Hook points

Hooks extend the session lifecycle without altering the FSM. Every hook receives a `poo.HookContext` carrying the session ID, run ID, turn number, read-only `HistoryView`, `ContextBudget`, and optional `Decider`. Hooks may append to history through their return decisions, never mutate prior messages in place.

| Option / Interface | When it runs | Decisions / Effect |
|---|---|---|
| `WithSessionStart` (`SessionStartHook`) | Once when the session is built, before the first run | Append messages, edit system prompt sections (`StartDecision`) |
| `WithBeforeRun` (`BeforeRunHook`) | At the start of every run, before the first provider request | Append messages, edit system prompt sections (`StartDecision`) |
| `WithPolicy` (`PolicyHook`) | Before all other `BeforeTool` hooks; reserved for the permission engine | `Allow()` or `Deny(detail)` only; exempt from hook deadline |
| `WithBeforeTool` (`BeforeToolHook`) | After argument preparation and policy check, before tool execution | `Allow()`, `Deny(detail)`, or `ShortCircuit(ToolResult)` |
| `WithAfterTool` (`AfterToolHook`) | After a tool call finishes, before the model sees the result | `Keep()`, `Annotate(text)`, `Replace(ToolResult)`, or `Redact(ToolResult)` |
| `WithAfterTurn` (`AfterTurnHook`) | At the end of every turn, including interrupted turns | `AppendMessages(...)` or `StopTurn(detail)` |
| `WithBeforeSettle` (`BeforeSettleHook`) | When the model stops on its own | `Accept()`, `Continue(Message)`, or `StopSettle(detail)` |
| `WithStopCondition` (`StopCondition`) | At every turn end after the tool batch finishes | Ends the run when matched |
| `WithObserver` (`Observer`) | On every session event, in order | Read-only observation; never settles a run |
| `WithSessionAttach` (`AttachFunc`) | Once during live session construction (including resume), before feeds or runs | Attach session-owned subsystems (such as MCP servers) and return a close function |

Hooks that are safe to re-run after a crash mid-hook on durable sessions may implement `poo.IdempotentHook`. Hooks that do not implement it are never re-executed on recovery.

## Provider-neutral message model

The core maintains a single provider-neutral message representation for all supported wire APIs. It carries text, tool calls, tool results, reasoning or thinking blocks, opaque provider blocks, and image attachments. Transports translate this model to and from Chat Completions, OpenAI Responses, and Anthropic Messages without leaking provider-specific types into the core.

For the exact round-trip mapping across all three APIs, see [Wire API Mapping](/reference/wire-api-mapping).
