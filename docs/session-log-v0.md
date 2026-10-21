# Session log v0

A poo session log is an append-only JSONL file: one JSON object per line,
in the order the records were produced. The log is written with no lock and
no recovery. This document is the contract a consumer parses.

A session log holds one session. The file is created exclusively, so a run
pointed at a log that already exists fails before it sends a request rather
than appending a second session to the log.

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
| `usage` | Once per provider request. |
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
| `reasoning_effort` | string | The `reasoning_effort` the run sends. Absent when it sends none. |
| `build` | object | The binary that produced the session. |

`model` holds:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | The provider's model identifier. |
| `base_url` | string | The endpoint, without credentials. Absent when empty. |
| `max_output` | integer | The model's output-token ceiling. Absent when unknown. |

The `base_url` never carries credentials: a URL that names a user and a
password is written without its userinfo, so a consumer can read the log
without picking up a secret.

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
| `reasoning_effort` | string | The `reasoning_effort` the run sends. Absent when it sends none. |

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
continued. A bash call that hit its timeout is an `error` outcome, because
the model has to see that the command failed rather than read its partial
output as a finished command.

## `usage`

The token accounting of one provider request. Every request whose stream
started gets a record, so a consumer can see the time to the first token
even when the endpoint reports no token counts.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the request belongs to. |
| `turn` | integer | The turn the request belongs to. |
| `request` | integer | The request's number within the run, starting at 1. |
| `input_tokens` | integer | The prompt tokens the endpoint reported. `0` when it reported none. |
| `output_tokens` | integer | The completion tokens the endpoint reported. `0` when it reported none. |
| `cache_read_tokens` | integer | The prompt tokens served from the provider's cache. **Absent** when the endpoint does not report it. |
| `ttft_ms` | number | The time to the first streamed token, in milliseconds. Measured by the transport, not reported by the endpoint. |

The time to the first token is the time to the first *token*: a non-empty
text delta, a thinking delta or a tool call. A chunk that only announces
the assistant's role carries no token, so it does not start the clock.

Every provider request whose stream started gets a `usage` record,
including a request whose stream failed part way through: the request
still cost tokens, and the record is what a consumer reads to account for
it. A request that failed before the endpoint's stream started has no
usage record: no token was spent on it.

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
| `cancelled` | 130 | The run was cancelled, or its deadline passed before the model finished. |

A cancellation and a deadline are the same outcome: the run was stopped
before the model finished, and both settle `cancelled`.

`detail.source` names the condition. The values the skeleton writes:

| Source | Meaning |
|---|---|
| `end_turn` | The model stopped on its own. |
| `max_turns` | The run hit its turn ceiling. |
| `no_output_room` | The computed `max_tokens` was under 256, so the request was never sent. |
| `provider:context_length` | The endpoint reported that the request was too long. It is recognised whatever status the endpoint sent it with, including a 5xx. |
| `provider:<kind>` | The provider request failed with that kind. |
| `finish:length` | The provider stopped on the output-token limit. |
| `cancel:context` | The run's context was cancelled. |
| `cancel:deadline` | The run's context deadline passed. |

## Reading a log

A consumer reads the file line by line and decodes each line as a JSON
object. It keys off `record`, and it must tolerate fields it does not
recognise: a later stage adds records and fields, and a consumer that
fails on an unknown field breaks on a newer log.

The order of the records is the order of the events. A `tool.call` is
always followed by its `tool.result`, and a `run.started` is always
followed by a `run.settled`. The core writes the settle record itself, so
a run that was cancelled, or whose provider failed, still ends with one.
Every record is written under a context of its own, bounded at 5 s, so a
cancelled run still records what it did. A `usage` record the store
refuses is dropped rather than ending the run. Only a store that refuses
the run's other records stops it before its `run.settled`.

## The tools' observations

A `tool.result` carries the tool's own text in `text`, and the tool's
side-channel payload in `structured`. The two tools the skeleton ships
have these shapes.

The bash tool's payload is the observation the model reads, as JSON:

| Field | Type | Meaning |
|---|---|---|
| `returncode` | integer or null | The command's exit status. Null when the process was killed, which is the case for a timeout. |
| `timed_out` | boolean | Whether the command hit its timeout. |
| `output` | string | The output, when it fits under the observation cap. |
| `output_head` | string | The head of the output, when it was elided. |
| `output_tail` | string | The tail of the output, when it was elided. |
| `elided_chars` | integer | The characters dropped from the middle. Absent when the output was not elided. |
| `hint` | string | How to see the elided middle. Absent when the output was not elided. |

The observation cap and `elided_chars` are counted in characters, not
bytes, and a cut never splits a rune. A byte that is not part of a valid
character counts as one character. The output is captured up to 1 MiB,
and a capture cut that lands in the middle of a character drops the
partial one. Bytes the command printed are otherwise kept as they are.

The read tool's payload is the range the model saw. Its text carries a
header naming the range, such as `[path · lines 1–2000 of 5312]`, and the
body below it. The header says `· capped` when the output cap ended the
read early, and names an empty range when the offset is past the end of
the file. A line is truncated at 2000 characters, counted in characters,
and a cut never splits a rune. A carriage return at the end of a line is
stripped, so a file with CRLF endings reads the same as one with LF
endings.
