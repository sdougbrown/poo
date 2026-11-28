---
title: Wire Protocol
description: JSON-RPC 2.0 control protocol over stdio served by poo serve, including session methods, run methods, server notifications, permission asks, and poo/wire/protocol.
---

# Wire Protocol

`poo serve` exposes a JSON-RPC 2.0 control protocol over stdin and stdout, framed as one JSON object per line (NDJSON). The public contract lives in `poo/wire/protocol`.

## Handshake (`poo/initialize`)

`poo/initialize` must be the first request on the stream. Calling any other method before the handshake succeeds returns JSON-RPC error `-32002` (`data.code: "not_initialized"`).

The client sends its supported `protocol_versions` (currently `[1]`), name, version, and `capabilities`:

```json
{"jsonrpc":"2.0","id":1,"method":"poo/initialize","params":{"protocol_versions":[1],"client":{"name":"my-client","version":"0.1.0","capabilities":{"permission_answers":true}}}}
```

If no protocol version is shared, the server returns `-32001` (`protocol_version_unsupported`). On success, the server returns the negotiated version, `build` metadata, `capabilities` (`decider`, `compaction`, `persistence`, `jobs`, `feeds`, `history_origin`), `profiles`, `default_profile`, and `roles`.

::: info Client capability: `permission_answers`
Only a client that declares `capabilities.permission_answers: true` receives `permission/ask` requests from the server. Clients that omit it are never asked—every ask falls back immediately, and `session/create` / `session/load` report a warning.
:::

## Strict parameter decoding

Every method decodes its `params` object strictly via `protocol.DecodeParams`:

- Omitting an optional field selects its default.
- Passing an explicit `null` for `params` or any field returns `-32602` with `data.code: "invalid_params"`.
- Passing an unrecognized field at any nesting depth returns `-32602` with `data.code: "unknown_field"`.

## Session methods

| Method | Purpose | Key behavior |
|---|---|---|
| `session/create` | Create a new session | Requires `working_dir`. Optional `mode` must be in the handshake's `capabilities.persistence`. Response is queued before the event observer attaches, so no event arrives before the client learns `session_id`. |
| `session/load` | Open a recorded session | Requires `session_id`. Recovers torn tails, reports `recovered_tail`, `interrupted_runs`, `warnings`, and any `pending_run` on durable sessions. |
| `session/resume_run` | Continue a durable session's pending run | Starts the pending run asynchronously and returns `{"run_id": "..."}`. Returns `no_pending_run` or `busy` when not applicable. |
| `session/get` | Inspect current session state | Returns FSM `state`, `run_id`, active `jobs`, rendered `prompt` state, `reasoning`, `broken`, `pending_run`, and `awaiting` status. |
| `session/configure` | Update session settings between runs | Refused with `busy` while a run is active. Re-renders the system prompt if model or variant changes; rejects and preserves prior state on template error. |
| `session/history` | Page recorded session history | Accepts `session_id`, optional `before_seq`, and `limit` (capped at `poo.MaxHistoryLimit`). Pages backwards; returns `items` oldest-first and `has_more`. |
| `session/close` | Close a session and release locks | Cancels active work, stops feeds/jobs/MCP servers, closes the store, and returns `{}`. |

## Run, job, and catalog methods

| Method | Purpose | Key behavior |
|---|---|---|
| `run/prompt` | Start a new run | Requires `session_id` and `text` (optional `delivered` metadata). Returns `{"run_id": "..."}` immediately; watch progress via `event` notifications or `run/inspect`. |
| `run/message` | Send input to an active run | Requires `session_id` and `text`. `mode` is `"steer"` (default), `"interrupt"`, or `"follow_up"`. Returns `{"input_id": "..."}`. |
| `run/inspect` | Poll events and optionally wait for settle | Accepts `after_seq` and optional `wait: {"until": "settled", "run_id": "...", "timeout_ms": ...}` (default 30s). Returns `events`, `next_seq`, `has_more`, `state`, and `settled` or `timed_out: true`. |
| `run/cancel` | Cancel an active run | Requires `session_id` and `run_id`. Idempotent for already-settled runs; returns `run_not_found` for unknown IDs. |
| `run/background` | Move a live tool call to a background job | Requires `session_id`, `run_id`, and `handle`. Returns `{"job": "..."}` or `call_not_backgroundable`. |
| `job/cancel` | Stop a background job or feed | Requires `session_id` and `job` (e.g. `j1` or `feed:inbox`). Idempotent for unknown or finished jobs. |
| `models/list` | Return the effective model catalog | Accepts optional `refresh: true`. Returns `{"models": [...]}`. |
| `settings/get` | Return effective merged settings | Returns the post-override settings object. |

## Server notifications and requests

### Event notifications (`event`)

The server pushes every session event as a JSON-RPC notification with `method: "event"`. `protocol.Event` marshals as a flat JSON object containing `session_id`, session-monotonic `seq`, `type`, `at`, and the event's own fields:

```json
{"jsonrpc":"2.0","method":"event","params":{"session_id":"01J...","seq":4,"type":"message.delta","at":"2026-01-01T00:00:00Z","run_id":"r1","text":"hello"}}
```

Event types cover the full lifecycle:

- **Runs & turns**: `run.started`, `run.settling`, `run.settled`, `turn.started`, `turn.ended`, `turn.interrupted`, `message.delta`, `provider.retry`
- **Inputs**: `input.queued`, `input.delivered`, `input.dropped`, `input.held`
- **Tools & permissions**: tool call/result events and permission ask/decision events
- **Jobs & feeds**: `job.started`, `job.ended`, `feed.started`, `feed.ended`

Clients should ignore unknown event types for forward compatibility within protocol version 1.

### Permission asks (`permission/ask`)

Unlike `event` notifications, a permission ask is a **server-initiated JSON-RPC request** with an `id` equal to `params.ask_id`:

```json
{"jsonrpc":"2.0","id":"ask-1","method":"permission/ask","params":{"session_id":"01J...","run_id":"r1","ask_id":"ask-1","tool":"bash","summary":"rm -rf build","class":"side_effecting","reason":"policy"}}
```

For doom-loop asks, `reason` is `"doom_loop"` and `repeat_count` includes `exact` and `near` counts. The client responds with the matching `id` and a `protocol.AskResult`:

```json
{"jsonrpc":"2.0","id":"ask-1","result":{"decision":"allow"}}
```

`decision` must be `"allow"` or `"deny"`. An optional `message` string on a denial is shown to the model in the tool result and recorded in the session log.

## `poo/wire/protocol` package

`poo/wire/protocol` imports only the Go standard library so external Go clients can depend on the wire contract without importing `poo` or any of its runtime packages. It exports:

- Envelope types: `Request`, `Response`, `Notification`, `Error`, `ErrorData`
- Method constants (`MethodInitialize`, `MethodSessionCreate`, `MethodRunPrompt`, `MethodPermissionAsk`, `MethodEvent`, etc.)
- Parameter and result DTOs for every method, plus `Event` with symmetric flat `MarshalJSON`/`UnmarshalJSON`
- JSON-RPC error codes (`-32700` to `-32603`, `-32000` `CodeServerError`, `-32001` `CodeVersionUnsupported`, `-32002` `CodeNotInitialized`)
- Machine-readable `ErrorData.Code` constants (`session_not_found`, `session_locked`, `busy`, `invalid_settings`, `unknown_model`, `unknown_profile`, `unknown_field`, etc.)—clients should branch on `error.data.code` rather than parsing `error.message`

When embedding `wire.Serve`, transport backpressure and teardown limits are configurable via `wire.Bounds` (defaults: 10,000 queued messages, 16 MiB queue bytes, 10s write stall, 5s drain).
