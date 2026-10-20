# Session log v0

A poo session log is an append-only JSONL file: one JSON object per line,
in the order the records were produced. The log is written with no lock and
no recovery. This document is the contract a consumer parses.

The first record is always `session.created`, and it carries
`"format": "poo-session-v0"`. A consumer that does not recognise the format
should stop rather than guess.

## Common fields

Every record carries these three fields, from the same envelope:

| Field | Type | Meaning |
|---|---|---|
| `record` | string | The record's name, e.g. `"tool.call"`. |
| `seq` | integer | The session-monotonic sequence number. The first record is 1. |
| `ts` | string | The record's timestamp, RFC 3339 with nanosecond precision, in UTC. |

The record's own fields follow the envelope.

## Record names

| Name | Written when |
|---|---|
| `session.created` | Once, first, when the session starts. |
| `run.started` | Once per run, when the run begins. |
| `message` | Once per message that enters the model's history. |
| `tool.call` | Once per tool call the model makes, before it runs. |
| `tool.result` | Once per tool call, after it runs. |
| `usage` | Once per provider request that reports token accounting. |
| `run.settled` | Once per run, when the run ends. |

## `session.created`

The first record of a log.

| Field | Type | Meaning |
|---|---|---|
| `format` | string | The record vocabulary. Always `"poo-session-v0"`. |
| `session_id` | string | The session's identifier. |
| `mode` | string | The persistence mode. `"logged"` for a `poo run`. |
| `created_at` | string | RFC 3339, UTC. |
| `working_dir` | string | The absolute directory the run works in. |
| `model` | object | The model the session talks to. See below. |
| `tools` | array of objects | The tools linked into the session, in call order. |
| `context_limit` | integer | The model's context window, in tokens. |
| `tools_mode` | string | How the run asks for tool calls: `"native"`, `"text"` or `"auto"`. |
| `build` | object | The binary that produced the session. |

`model` holds:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | The provider's model identifier. |
| `base_url` | string | The endpoint, without credentials. Absent when empty. |
| `max_output` | integer | The model's output-token ceiling. Absent when unknown. |

Each entry of `tools` holds:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The tool's identifier. |
| `class` | string | The tool's side-effect class: `"read_only"` or `"side_effecting"`. |

`build` holds:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The binary's name. |
| `version` | string | The binary's version. |

## `run.started`

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run's identifier. |
| `prompt_hash` | string | The hex SHA-256 of the prompt text. |
| `started_at` | string | RFC 3339, UTC. |
| `model_id` | string | The model the run talks to. |
| `max_turns` | integer | The turn ceiling. `0` means the run is unlimited. |
| `tools_mode` | string | How the run asks for tool calls. |

## `message`

One message in the model's history. The messages in the log are the same
list the model sees: nothing is trimmed, rewritten or summarised.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the message belongs to. |
| `turn` | integer | The turn the message belongs to. `0` for the run's input. |
| `origin` | string | Who produced the message: `"user"`, `"model"`, `"tool"` or `"system"`. |
| `role` | string | The message's role: `"system"`, `"user"`, `"assistant"` or `"tool"`. |
| `content` | array of objects | The message's content blocks. |

Each content block holds:

| Field | Type | Meaning |
|---|---|---|
| `kind` | string | `"text"`, `"thinking"`, `"tool_call"` or `"tool_result"`. |
| `text` | string | The block's text. Absent when empty. |
| `tool_call_id` | string | The provider's id for the call. Set on `tool_call` and `tool_result` blocks. |
| `name` | string | The tool's name. Set on `tool_call` and `tool_result` blocks. |
| `arguments` | object or string | A `tool_call` block's arguments. A JSON object when the model sent valid JSON, and a JSON string holding the raw text when it did not. |
| `is_error` | boolean | `true` when a `tool_result` reports a failure. Absent when false. |

A `tool_result` block's `text` is the model-facing text of the result.

## `tool.call`

A tool call the model made. The record is written before the tool runs, so
a log shows what the model asked for even when the tool never finished.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the call belongs to. |
| `turn` | integer | The turn the call belongs to. |
| `handle` | string | The call's handle within the turn, e.g. `"r1"`. |
| `tool` | string | The tool the model called. |
| `arguments` | object or string | The full arguments, in the same shape as a `tool_call` content block. |

## `tool.result`

The outcome of a tool call.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the result belongs to. |
| `turn` | integer | The turn the result belongs to. |
| `handle` | string | The call's handle within the turn. |
| `tool` | string | The tool that ran. |
| `outcome` | string | `"ok"` or `"error"`. |
| `text` | string | The model-facing text of the result. |
| `structured` | any | The tool's side-channel payload. Absent when the tool has none. |

An `error` outcome is recoverable: the model saw the text and the run
continued.

## `usage`

The token accounting of one provider request. A record is written only when
the endpoint reported usage.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the request belongs to. |
| `turn` | integer | The turn the request belongs to. |
| `request` | integer | The request's number within the run, starting at 1. |
| `input_tokens` | integer | The prompt tokens the endpoint reported. |
| `output_tokens` | integer | The completion tokens the endpoint reported. |
| `cache_read_tokens` | integer | The prompt tokens served from the provider's cache. **Absent** when the endpoint does not report it. |
| `ttft_ms` | number | The time to the first streamed token, in milliseconds. |

## `run.settled`

The end of a run. It is the last record of a run.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run's identifier. |
| `reason` | string | The settle reason. See below. |
| `detail` | object | Why the run settled. |
| `turns` | integer | The number of provider requests the run made. |
| `settled_at` | string | RFC 3339, UTC. |

`detail` holds:

| Field | Type | Meaning |
|---|---|---|
| `source` | string | The condition that settled the run. |
| `text` | string | The human-readable explanation. Absent when there is none. |
| `losing` | array of strings | Other candidates that lost the precedence order. Absent when there were none. |
| `provider` | object | Set when a provider request failed. |

`provider` holds:

| Field | Type | Meaning |
|---|---|---|
| `kind` | string | The failure class, e.g. `"context_length"` or `"server"`. |
| `status` | integer | The HTTP status. Absent when the failure was not a response. |
| `code` | string | The provider's own error code. Absent when it sent none. |

### Settle reasons

The skeleton's `poo run` produces these reasons. The exit code is the
process's exit code.

| Reason | Exit | Meaning |
|---|---|---|
| `end_turn` | 0 | The model stopped on its own. |
| `max_turns` | 11 | The run hit its turn ceiling. |
| `provider_error` | 20 | A provider request failed, or the provider stopped on the output-token limit. |
| `context_exhausted` | 13 | A provider context-length error, or the computed `max_tokens` fell under 256. |
| `cancelled` | 130 | The run was cancelled. |

`detail.source` names the condition. The values the skeleton writes:

| Source | Meaning |
|---|---|
| `end_turn` | The model stopped on its own. |
| `max_turns` | The run hit its turn ceiling. |
| `no_output_room` | The computed `max_tokens` was under 256, so the request was never sent. |
| `provider:context_length` | The endpoint reported that the request was too long. |
| `provider:<kind>` | The provider request failed with that kind. |
| `finish:length` | The provider stopped on the output-token limit. |
| `cancel:context` | The run's context was cancelled. |
| `cancel:signal` | The process was interrupted. |

## Reading a log

A consumer reads the file line by line and decodes each line as a JSON
object. It keys off `record`, and it must tolerate fields it does not
recognise: a later stage adds records and fields, and a consumer that
fails on an unknown field breaks on a newer log.

The order of the records is the order of the events. A `tool.call` is
always followed by its `tool.result`, and a `run.started` is always
followed by a `run.settled`.
