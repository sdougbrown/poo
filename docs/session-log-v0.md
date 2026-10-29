# Session log v0

A poo session log is an append-only JSONL file: one JSON object per line,
in the order the records were produced. This document is the contract a
consumer parses.

A session log holds one session. The file is created exclusively, so a run
pointed at a log that already exists fails before it sends a request rather
than appending a second session to the log.

## Where a session's log lives

A logged session's log is `<dir>/<session_id>.jsonl`, and its lock is
`<dir>/<session_id>.lock`. The lock is an exclusive `flock`, taken when the
session is created or loaded and held until the session is closed: a second
process is refused while the session is live, and the kernel releases the
lock when the holding process dies. The lock file is never deleted, and a
lock file with no log beside it is a harmless leftover.

The log is built in a temporary file and renamed into place, so a session
that was cut short during its construction is never a session: the
temporary file is left behind, and a prune removes it once it is old.

A log that was cut short in the middle of a record is truncated when the
session is loaded: the bytes after the last newline are not a record. The
load then recovers the session, appending the records that resolve what
the session left open: an `interrupted` result, and the message that
carries it, for every tool call without one, a denied
`permission.answered` for every ask without an answer, a `run.settled`
with the reason `interrupted` for every run that never settled, a
`job.ended` with the outcome `interrupted` for every job that never
ended, and an `input.dropped` with the reason `interrupted` for every
input that never reached the model. The recovery appends only what is
missing, so loading a session twice writes nothing twice.

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

A record that someone produced also carries an `origin`:

| Field | Type | Meaning |
|---|---|---|
| `origin` | string | Who produced the record: `"user"`, `"model"`, `"tool"`, `"system"`, `"job"`, `"steer"`, `"interrupt"`, `"follow_up"`, `"compaction"` or `"hook:<name>"`. **Absent** on the records that no one produced: the session's own header, a run's boundaries, the usage accounting. |

## Record names

| Name | Written when |
|---|---|
| `session.created` | Once, first, when the session starts. |
| `run.started` | Once per run, when the run begins. |
| `system_prompt` | Once per run whose prompt differs from the previous one, with the whole prompt. |
| `message` | Once per message that enters the model's history. |
| `tool.call` | Once per tool call the model makes, before it runs. |
| `tool.result` | Once per tool call, after it runs. |
| `usage` | Once per provider request. |
| `provider.attempt` | Once per attempt of a request that failed. |
| `input.queued` | Once per input the session accepts. |
| `input.delivered` | Once per input that reaches the model. |
| `input.dropped` | Once per input that never reaches the model. |
| `input.held` | Once per round of job results that were held instead of delivered. |
| `job.started` | Once per background job, when it is accepted. |
| `job.ended` | Once per background job, when it finishes. |
| `observer.dropped` | Once a second at most, per observer that fell behind. |
| `turn.interrupted` | Once per turn an interrupt cut short. |
| `compaction` | Once per compaction a compactor produced. |
| `session.models_changed` | Once per model switch, between the runs. |
| `decision` | Once per decision call, failures included. |
| `run.settled` | Once per run, when the run ends. |

The fields of the records grow over time. A consumer must tolerate a field
it does not recognise: the fields below are the ones the current core
writes, and a later stage adds more.

## `session.created`

The first record of a log.

| Field | Type | Meaning |
|---|---|---|
| `format` | string | The record vocabulary. Always `"poo-session-v0"`. |
| `session_id` | string | The session's identifier. |
| `mode` | string | The persistence mode. `"logged"` for a `poo run`. |
| `created_at` | string | RFC 3339, UTC. |
| `working_dir` | string | The absolute directory the run works in. |
| `profile` | string | The profile the session was built from. Absent when the log was written before the field existed. |
| `roles` | object | The models the session talked to. See below. |
| `model` | object | The model the session talks to. See below. |
| `tools` | array of objects | The tools linked into the session, in call order. |
| `settings_hash` | string | The hash of the settings the session was built with. Absent when the log was written before the field existed. |
| `context_limit` | integer | The model's context window, in tokens. |
| `tools_mode` | string | How the run asks for tool calls: `"native"`, `"text"` or `"auto"`. |
| `reasoning_effort` | string | The `reasoning_effort` the run sends. Absent when it sends none. |
| `build` | object | The binary that produced the session. |

`roles` holds the session's effective roles:

| Field | Type | Meaning |
|---|---|---|
| `chat` | object | The chat model. Absent when the session had none. |
| `decider` | object | The decision model. Absent when the session had none. |

A role holds model info: its `name`, its `model_id`, its `endpoint`,
its `api`, its `ops` and its `context_limit`. The roles are the ones the
session ran with, not the ones the current settings would pick: a session
loaded by another binary keeps the roles it was made with.

`model` holds:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | The provider's model identifier. |
| `endpoint` | string | The endpoint, without credentials. Absent when empty. Renamed from `base_url` before v1; older logs carry `base_url`. |
| `max_output` | integer | The model's output-token limit. Absent when unknown. |

The `endpoint` never carries credentials: a URL that names a user and a
password is written without its userinfo, so a consumer can read the log
without picking up a secret.

Each entry of `tools` holds:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The tool's identifier. |
| `class` | string | The tool's side-effect class: `"read_only"` or `"side_effecting"`. |
| `background` | string | The tool's background mode: `"allowed"` or `"always"`. Absent when the tool never runs in the background. |

`build` holds:

| Field | Type | Meaning |
|---|---|---|
| `name` | string | The binary's name. |
| `version` | string | The binary's version. |

## `run.started`

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run's identifier: a ULID (26 characters of Crockford base32, the time first). |
| `prompt_hash` | string | The hex SHA-256 of the prompt text. |
| `started_at` | string | RFC 3339, UTC. |
| `model_id` | string | The model the run talks to. |
| `max_turns` | integer | The turn limit. `0` means the run is unlimited. |
| `tools_mode` | string | How the run asks for tool calls. |
| `reasoning_effort` | string | The `reasoning_effort` the run sends. Absent when it sends none. |
| `trigger` | string | What started the run: `"prompt"` or `"job"`. Absent on a log written before the field existed. |
| `jobs` | array of objects | The job results that started a job run. Absent on a prompt run. |

A `jobs` entry holds:

| Field | Type | Meaning |
|---|---|---|
| `job` | string | The job's identifier, e.g. `"j3"`. |
| `handle` | string | The handle of the tool call the job came from, e.g. `"r1"`. The call returned at once with a `started` outcome, and the job carries that call's handle. |
| `tool` | string | The tool the job ran. |
| `text` | string | The job's result, as the model reads it. |
| `outcome` | string | The job's outcome. |

## `message`

One message in the model's history. The messages in the log are the same
list the model sees: nothing is trimmed, rewritten or summarised.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the message belongs to. |
| `turn` | integer | The turn the message belongs to. `0` for the run's input. |
| `origin` | string | Who produced the message: `"user"`, `"model"`, `"tool"`, `"system"`, `"job"`, `"steer"`, `"interrupt"`, `"follow_up"`, `"compaction"` or `"hook:<name>"`. |
| `role` | string | The message's role: `"system"`, `"user"`, `"assistant"` or `"tool"`. |
| `content` | array of objects | The message's content blocks. |
| `message_id` | string | The message's session-monotonic id, e.g. `"m7"`. Absent when the message has none. |
| `partial` | boolean | `true` when the message was cut short. Absent when it was not. |
| `cause` | string | Why the message was cut short: `"interrupt"` or `"provider_error"`. Absent unless the message is partial. |
| `delivery_note` | object | How much of the previous reply the user had seen. Absent when the message carries none. |

A partial message is the text a cut-short response carried. The
assistant's own tool calls in a partial response did not run, and the
calls that did not run are recorded as `skipped`.

`delivery_note` holds:

| Field | Type | Meaning |
|---|---|---|
| `message_id` | string | The assistant message the user had seen part of. |
| `upto_chars` | integer | How many characters of that message the user had seen, in code points over its text blocks. |
| `text` | string | The rendered note. It is prefixed to the text of the message, followed by a blank line, so the message's `content` holds it too. Empty when the whole reply was presented. |

The note is part of the record, so a replay shows exactly what the user
saw. The assistant message itself is never trimmed: the note is the
record.

Each content block holds:

| Field | Type | Meaning |
|---|---|---|
| `kind` | string | `"text"`, `"thinking"`, `"tool_call"`, `"tool_result"` or `"provider"`. |
| `text` | string | The block's text. Absent when empty. |
| `tool_call_id` | string | The provider's id for the call. Set on `tool_call` and `tool_result` blocks. |
| `name` | string | The tool's name. Set on `tool_call` and `tool_result` blocks. |
| `arguments` | object or string | A `tool_call` block's arguments. A JSON object when the model sent valid JSON, and a JSON string holding the raw text when it did not. |
| `is_error` | boolean | `true` when a `tool_result` reports a failure. Absent when false. |
| `provider` | object | The block's opaque provider companion. Absent when the block has none. |

A `tool_result` block's `text` is the model-facing text of the result.

A `provider` block holds:

| Field | Type | Meaning |
|---|---|---|
| `api` | string | The wire API the payload belongs to: `"chat_completions"`, `"responses"` or `"anthropic_messages"`. |
| `type` | string | The provider's own name for the payload, e.g. `"thinking_signature"`, `"reasoning"`, `"redacted_thinking"` or `"compaction"`. |
| `data` | any | The payload, verbatim. The core never interprets it. |
| `required` | boolean | `true` when a request cannot be made without the payload. Absent when false. |

A `provider` payload may also ride as the `provider` field of a `thinking`
block: an Anthropic thinking block is one block on the wire, with a text
part and a signature, and poo keeps the two together.

## `system_prompt`

The prompt a run rendered, whole. A prompt that differs from the previous
one is written; an unchanged one is not. The record is what a restored
session takes: the prompt is never rendered from the sections again, so a
session that was recorded under another prompt keeps that prompt.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the prompt belongs to. |
| `variant` | string | The variant the prompt rendered in. Absent when it rendered in the default one. |
| `text` | string | The whole rendered prompt. |
| `hash` | string | The hex SHA-256 of the text. |

## `tool.call`

A tool call the model made. The record is written before the tool runs, so
a log shows what the model asked for even when the tool never finished.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the call belongs to. |
| `turn` | integer | The turn the call belongs to. |
| `handle` | string | The call's handle within the turn, e.g. `"r1"`. |
| `tool` | string | The tool the model called. |
| `arguments` | object or string | The full arguments, in the same shape as a `tool_call` content block. The reserved `background` argument is kept here, even though the core removes it before the tool sees it. |
| `mode` | string | How the call ran: `"sync"` in the turn, or `"job"` in the background. Absent when the log was written before the field existed. |

## `tool.result`

The outcome of a tool call.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the result belongs to. |
| `turn` | integer | The turn the result belongs to. |
| `handle` | string | The call's handle within the turn. |
| `tool` | string | The tool that ran. |
| `outcome` | string | `"ok"`, `"error"`, `"denied"`, `"skipped"`, `"cancelled"`, `"interrupted"` or `"started"`. |
| `text` | string | The model-facing text of the result. |
| `structured` | any | The tool's side-channel payload. Absent when the tool has none. |
| `original` | object | The result the tool produced, when a hook replaced it. Absent when no hook replaced it, and absent for a redacted result: the original of a redacted result never reaches the log. |
| `redacted` | boolean | `true` when a hook redacted the result. Absent when it did not. |
| `redacted_by` | string | The name of the hook that redacted the result. Absent unless the result was redacted. |
| `hooks` | array of objects | The after-tool hooks' decisions, in the order the hooks ran. Absent when no hook acted. A redacted chain's decisions carry no `text`. |

`original` holds:

| Field | Type | Meaning |
|---|---|---|
| `text` | string | The model-facing text of the result the tool produced. |
| `structured` | any | The tool's side-channel payload. Absent when the tool had none. |
| `is_error` | boolean | `true` when the result reported a failure. Absent when it did not. |

A `hooks` entry holds:

| Field | Type | Meaning |
|---|---|---|
| `hook` | string | The name of the hook that decided. |
| `action` | string | What the hook did: `"keep"`, `"annotate"`, `"replace"` or `"redact"`. |
| `text` | string | The text the hook added or substituted. Absent when the chain was redacted, and absent when the hook kept the result. |

An `error` outcome is recoverable: the model saw the text and the run
continued. A bash call that hit its timeout is an `error` outcome, because
the model has to see that the command failed rather than read its partial
output as a finished command.

The `started` outcome belongs to a background job: the call returned at
once and its result arrives later as a message. The `denied`, `skipped`,
`cancelled` and `interrupted` outcomes arrive with the later stages that
produce them.

An `interrupted` outcome is written by a load, never by a live run: the
call was cut short, its outcome is unknown, and the model is told so. The
result is the model-facing text of the call, and the message that carries
it answers the call the model made, so a session that is loaded again asks
the model nothing it cannot answer.

## `input.queued`

An input the session accepted. The record is written before the call
that accepted the input returns, so an accepted input is never lost: its
fate is in the records.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `run_id` | string | The run the input belongs to. Absent when the session had no run. |
| `input_id` | string | The input's identifier: a ULID. |
| `mode` | string | How the input is delivered: `"steer"`, `"interrupt"` or `"follow_up"`. |
| `text` | string | The message text. |
| `delivery_note` | object | How much of the previous reply the user had seen. Absent when the input carries none. Its `text` is empty when the reply was still streaming at acceptance; the delivered message carries the note rendered at delivery. |
| `queued_at` | string | RFC 3339, UTC. |

## `input.delivered`

An input that reached the model.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `run_id` | string | The run the input was delivered to. |
| `input_id` | string | The input's identifier: a ULID. |
| `mode` | string | How the input was delivered. |
| `delivered_at` | string | RFC 3339, UTC. |

## `input.dropped`

An input that never reached the model. The reason is the run's final
settle reason.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `run_id` | string | The run the input belonged to. Absent when the session had no run. |
| `input_id` | string | The input's identifier: a ULID. |
| `mode` | string | How the input was accepted. |
| `reason` | string | Why the input was dropped. |
| `dropped_at` | string | RFC 3339, UTC. |

## `input.held`

Job results that were held instead of delivered. A held result waits
for the next run, or for the next load: it is never lost.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `run_id` | string | The run the results were held for. Absent when the session had no run. |
| `jobs` | array of strings | The jobs whose results were held. |
| `reason` | string | Why they were held: `"on_idle"`, `"wake_limit"` or `"session_closed"`. |
| `held_at` | string | RFC 3339, UTC. |

## `job.started`

A background job was accepted and started. The record is the
acceptance: the job, the call it came from, the run that started it and
the tool it runs.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `job` | string | The job's identifier, e.g. `"j3"`. |
| `handle` | string | The handle of the tool call the job came from, e.g. `"r1"`. The call returned at once with a `"started"` outcome. |
| `tool` | string | The name of the tool the job runs. |
| `run_id` | string | The run that started the job. |
| `started_at` | string | RFC 3339, UTC. |

## `job.ended`

A background job finished. The record carries the result the model
reads, so a held result survives a crash and a load can deliver it.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `job` | string | The job's identifier, e.g. `"j3"`. |
| `handle` | string | The handle of the tool call the job came from. |
| `tool` | string | The name of the tool the job ran. |
| `run_id` | string | The run that started the job. |
| `outcome` | string | How the job ended: `"ok"`, `"error"`, `"cancelled"` or `"interrupted"`. |
| `cause` | string | Why the job ended, when something else ended it. Absent unless the cause is `"session_closed"`. |
| `timed_out` | boolean | `true` when the job hit the jobs timeout. Absent when it did not. |
| `fatal` | boolean | `true` when the job failed with a fatal error. A run that receives the result settles `tool_fatal`. Absent when the job did not. |
| `input_id` | string | The id the queued result is delivered under. |
| `text` | string | The result the model reads. |
| `ended_at` | string | RFC 3339, UTC. |

## `observer.dropped`

The events an observer missed. The record is written at most once a
second per observer, so an observer that fell behind does not flood the
log, and it is never delivered to an observer: the diagnostics must not
cause more overflow.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `observer` | string | The observer that fell behind. |
| `count` | integer | How many events the observer missed. |
| `dropped_at` | string | RFC 3339, UTC. |

## `turn.interrupted`

A turn an interrupt cut short. The record follows the turn's end: a
client reads it as a note on the turn that just closed.

| Field | Type | Meaning |
|---|---|---|
| `session_id` | string | The session's identifier. |
| `run_id` | string | The run the turn belongs to. |
| `turn` | integer | The turn that was interrupted. |
| `input_ids` | array of strings | The inputs whose messages aborted the turn. |
| `interrupted_at` | string | RFC 3339, UTC. |

## `compaction`

What a compactor did. The store keeps every original record, and the
compaction record says what the model's view was replaced with.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the compaction belongs to. |
| `strategy` | string | The strategy that produced the compaction. |
| `compaction` | object | The compaction itself. Its shape is the strategy's form: `replace`, `rewrite`, `window` or `end`. |

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

## `provider.attempt`

One attempt of a provider request that failed. Every attempt is written,
the last one included, so a reader of the log sees what the request tried
and what each attempt failed with.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run the request belongs to. |
| `turn` | integer | The turn the request belongs to. |
| `request` | integer | The request's number within the run, starting at 1. |
| `attempt` | integer | The attempt's number, counting from 1. |
| `error` | string | The failure's message. |
| `kind` | string | The provider error kind, when the failure is one. **Absent** when it is not. |
| `delay_ms` | integer | How long the transport waited before the next attempt. **Absent** when no retry followed. |

## `session.models_changed`

The session switched models. The switch happens between the runs, so a
reader of the log knows which model answered which turn.

| Field | Type | Meaning |
|---|---|---|
| `from` | string | The name of the model the session talked to. |
| `to` | string | The name of the model the session talks to now. |
| `warnings` | array of strings | What the caller should know about the switch. **Absent** when there was nothing to say. |
| `omitted` | array of objects | The provider blocks that are left out of the requests from now on, because they belong to another API. Each object holds `api`, `type` and `count`. **Absent** when nothing was left out. |

The history is never truncated by a switch. A view that does not fit the
new window is a warning, and the next run settles `context_exhausted`.

## `decision`

One decision call. The record is written whatever the outcome was, so a
reader of the log sees the calls that could not be answered too.

| Field | Type | Meaning |
|---|---|---|
| `origin` | string | Who asked the questions. |
| `questions` | array of objects | The questions that were asked. Each holds `id`, `instructions` and its kind's own fields. |
| `state_hash` | string | The hex SHA-256 of the state payload the questions were asked about. |
| `answers` | array of objects | The answers, when the call succeeded. **Absent** when it did not. |
| `outcome` | string | `"ok"`, `"deferred"` or `"invalid"`. |
| `error` | string | The failure, when the call did not succeed. **Absent** when it did. |
| `latency_ms` | number | How long the call took, in milliseconds. |
| `requests` | integer | The number of wire requests the call made. **Absent** when the decider did not report one. |
| `at` | string | RFC 3339, UTC. |

## `run.settled`

The end of a run. It is the last record of a run.

| Field | Type | Meaning |
|---|---|---|
| `run_id` | string | The run's identifier: a ULID (26 characters of Crockford base32, the time first). |
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
| `handoff` | string | The note a compactor wrote when it refused to compact. Absent when the run did not settle that way. |
| `undelivered_input` | array of objects | The accepted inputs that never reached the model. Absent when there were none. |
| `jobs_running` | array of strings | The jobs that were still running when the run settled. Absent when there were none. |
| `result` | any | The payload of a valid `finish` call, when the run settled as finished. Absent otherwise. |

A `undelivered_input` entry holds:

| Field | Type | Meaning |
|---|---|---|
| `input_id` | string | The input's identifier: a ULID. |
| `mode` | string | How the input was accepted: `"steer"`, `"interrupt"` or `"follow_up"`. |

`provider` holds:

| Field | Type | Meaning |
|---|---|---|
| `kind` | string | The failure class, e.g. `"context_length"` or `"server"`. |
| `status` | integer | The HTTP status. Absent when the failure was not a response. |
| `code` | string | The provider's own error code. Absent when it sent none. It also holds the kind a failure had before it was reclassified: an `interrupted_stream` keeps the kind it broke in here. |
| `retry_after_s` | integer | How long the endpoint asked the client to wait, in seconds. Absent when it said nothing. |

### Settle reasons

The reasons are a closed set, and the exit code is the process's exit
code. The precedence is the order of the table: a run collects a
candidate from every source, and the highest-precedence one wins at the
commit point.

| Precedence | Reason | Exit | Meaning |
|---|---|---|---|
| 1 | `cancelled` | 130 | The run was cancelled. |
| 2 | `store_error` | 23 | The session's store failed. The session becomes broken. |
| 3 | `hook_error` | 22 | An executable hook failed or missed its deadline. |
| 4 | `tool_fatal` | 21 | A tool failed with an error that means the environment is unusable. |
| 5 | `provider_limit` | 24 | The provider refused for quota or plan reasons. The session is not broken. |
| 5b | `provider_error` | 20 | A provider request failed and cannot be retried, or the retry budget was spent. |
| 6 | `context_exhausted` | 13 | The context window is full. |
| 7 | `hook_continue_limit` | 12 | The `BeforeSettle` hooks asked for more continuations than the run allows. |
| 8 | `max_turns` | 11 | The run hit its turn limit. |
| 8b | `budget_exhausted` | 14 | The run hit a token or time budget. |
| 8c | `invalid_turns` | 15 | The model kept producing turns the run could not use: it asked for tool calls and sent none, or its calls did not parse, for as many consecutive turns as the run allows. |
| 9 | `finished` | 0 | The model called the `finish` tool with a valid result. |
| 9b | `stop_requested` | 0 | A hook or a stop condition asked the run to stop. |
| 10 | `output_truncated` | 10 | The provider stopped on the output-token limit. |
| 11 | `end_turn` | 0 | The model stopped on its own. |
| — | `interrupted` | — | The run's outcome is unknown: the process stopped while the run was going. It is never produced live; a load writes it for a run that never settled. |

`provider_limit` and `provider_error` share a precedence level, because
a single failed request produces only one of them.

A cancellation and a deadline are the same outcome: the run was stopped
before the model finished, and both settle `cancelled`.

`detail.source` names the condition. The values the core writes:

| Source | Meaning |
|---|---|
| `end_turn` | The model stopped on its own. |
| `output_truncated` | The provider stopped on the output-token limit. |
| `max_turns` | The run hit its turn limit. |
| `max_run_tokens` | The run hit its token budget. |
| `max_duration` | The run hit its wall-clock budget. |
| `empty_turns` | The model reached the bound on consecutive turns that asked for tool calls and sent none. The detail's text says how many turns. A run that mixed the two kinds of unusable turn is named for its last turn. |
| `unparsed_calls` | The model reached the bound on consecutive turns whose tool calls did not parse. The detail's text says how many turns. A run that mixed the two kinds of unusable turn is named for its last turn. |
| `no_output_room` | The computed `max_tokens` was under 256, so the request was never sent. |
| `hard_limit` | The estimate crossed the hard ratio of the context window. |
| `compaction_failed` | The compactor failed at or above the hard ratio. |
| `provider:context_length` | The endpoint reported that the request was too long. It is recognised whatever status the endpoint sent it with, including a 5xx. |
| `provider:<kind>` | The provider request failed with that kind. |
| `finish` | The model called the `finish` tool with a valid result. |
| `before_settle` | A `BeforeSettle` hook stopped the run, or the run asked for more continuations than it allows. |
| `after_turn:<name>` | An `AfterTurn` hook stopped the run. |
| `stop_condition` | A stop condition matched. |
| `cancel:context` | The run's context was cancelled. |
| `cancel:deadline` | The run's context deadline passed. |
| `cancel:client` | The client cancelled the run. |
| `cancel:close` | The session closed while the run was active. |
| `store` | The session's store refused a record. |

## Reading a log

A consumer reads the file line by line and decodes each line as a JSON
object. It keys off `record`, and it must tolerate fields it does not
recognise: a later stage adds records and fields, and a consumer that
fails on an unknown field breaks on a newer log.

A log written by an earlier core is still readable. The fields added
after the first version are all optional: `message_id`,
`delivery_note` and `trigger` are absent from an older log, and a
consumer that needs them must treat an absent field as "the log does not
say".

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
