---
title: Limits & Exit Codes
description: Run budgets, truncation retries, settle-reason exit codes for poo run, and daemon exit codes.
---

# Limits & Exit Codes

`poo` enforces explicit per-run limits and maps every run outcome to a distinct process exit code. Scripts, CI jobs, and supervisors can distinguish natural completion from budgets, provider failures, and configuration errors.

## Run Limits

Configure run limits under `limits` in your settings file (see [Settings Reference](/reference/settings)). Every limit must be non-negative (`>= 0`). Setting a budget to `0` disables it, except `max_continuations`, where `0` selects the default (`3`).

| Setting | Type | Default | CLI Flag (`poo run`) | Settle Reason (Exit Code) | Description |
|---|---|---|---|---|---|
| `limits.max_tokens_per_run` | int | `0` (off) | — | `budget_exhausted` (`14`) | Total token budget for a single run (settle source `max_run_tokens`). |
| `limits.max_duration_s` | int (seconds) | `0` (off) | — (see `--timeout` below) | `budget_exhausted` (`14`) | Wall-clock duration budget for a run, evaluated between turns (settle source `max_duration`). |
| `limits.max_turns` | int | `0` (off) | `--max-turns` | `max_turns` (`11`) | Maximum number of model turns per run. |
| `limits.max_continuations` | int | `3` | — | `hook_continue_limit` (`12`) | Maximum `BeforeSettle` hook continuations allowed in one run. |
| `limits.truncation_retries` | int | `1` | `--truncation-retries` | `output_truncated` (`10`) | Consecutive output-limit truncations the run asks the model to retry more concisely before settling. Set to `0` to settle on the first truncation. |

```yaml
version: 1
limits:
  max_tokens_per_run: 500000
  max_duration_s: 300
  max_turns: 25
  max_continuations: 3
  truncation_retries: 1
```

### `limits.max_duration_s` vs. `poo run --timeout`

These two controls are distinct:

- **`limits.max_duration_s`** is a run budget. The session checks elapsed time at turn boundaries and settles cleanly with `budget_exhausted` (exit code `14`, detail source `max_duration`).
- **`poo run --timeout <duration>`** sets a Go context deadline on the invocation. When the deadline expires, the in-flight request or tool call is cancelled immediately and the run settles with `cancelled` (exit code `130`, detail source `cancel:deadline`).

## Settle-Reason Exit Codes (`poo run`)

Every settled run records a closed, typed `Reason` (defined in `reason.go`). `poo run` exits with the code for that reason:

| Exit Code | Settle Reason(s) | Meaning |
|---|---|---|
| `0` | `end_turn`, `finished`, `stop_requested` | Run completed normally: the model stopped on its own (`end_turn`), called the `finish` tool with a valid result (`finished`), or stopped at a hook/stop condition's request (`stop_requested`). |
| `1` | *(unclassified / operational)* | Operational failure with no settle-reason code of its own (for example, transport setup failure, session start failure, or an unknown settle reason). |
| `2` | *(usage / configuration)* | Command refused its arguments, settings, working directory, or a system prompt that failed to render. |
| `10` | `output_truncated` | Provider stopped at the output-token limit and all `truncation_retries` were spent. |
| `11` | `max_turns` | Run reached `limits.max_turns` / `--max-turns`. |
| `12` | `hook_continue_limit` | `BeforeSettle` hooks requested more continuations than `limits.max_continuations`. |
| `13` | `context_exhausted` | Context window filled and could not be compacted further. |
| `14` | `budget_exhausted` | Run exhausted `limits.max_tokens_per_run` or `limits.max_duration_s`. |
| `15` | `invalid_turns` | Model produced unusable turns (for example, unparseable or missing tool calls) for the maximum consecutive turns allowed. |
| `20` | `provider_error` | Provider request failed and could not be retried, or the retry budget was spent. |
| `21` | `tool_fatal` | A tool returned a fatal error (`ErrFatal`), indicating an unusable environment. |
| `22` | `hook_error` | An executable hook failed or missed its deadline. |
| `23` | `store_error` | Session store failed; the session is marked broken. |
| `24` | `provider_limit` | Provider refused the request for quota or plan reasons (the session itself is not broken). |
| `130` | `cancelled` | Run was cancelled by `SIGINT`/`SIGTERM` or `--timeout`. |

::: tip Signal handling in `poo run`
The first `SIGINT` or `SIGTERM` cancels the active run so it can settle cleanly and exit `130`. A second interrupt forces immediate session teardown (bounded to 250 ms) and exits `130`.
:::

### Precedence and Multi-Run Exit Codes

- **Arbitration order:** When multiple settle candidates occur on the same turn, higher-severity reasons win. `cancelled` beats all other reasons, followed by failures (`store_error`, `hook_error`, `tool_fatal`, `provider_limit`/`provider_error`), then limits (`context_exhausted`, `hook_continue_limit`, `max_turns`, `budget_exhausted`, `invalid_turns`), and finally natural stops (`finished`, `stop_requested`, `output_truncated`, `end_turn`).
- **Background job wakes:** When `poo run` waits for background jobs (unless `--no-wait-jobs` is set), job completions can wake follow-up runs within the same session. `poo run` combines exit codes across all settled runs so the **first non-zero exit code wins**; exit `0` requires every settled run to succeed.

## `poo daemon` Exit Codes

`poo daemon` hosts a long-lived durable session driven by feeds (see [Feeds & Daemon](/capabilities/feeds-and-daemon) and [CLI Reference](/reference/cli#poo-daemon)). Its exit codes report daemon lifecycle status to a process supervisor:

| Exit Code | Meaning |
|---|---|
| `0` | Clean shutdown after `SIGINT` or `SIGTERM` (feeds stopped, records flushed, lease released). |
| `1` | Operational failure or **lost lease**. Happens if another process takes the lease, the store fails to open or resume, or resuming a pending run breaks the session. |
| `2` | Usage, argument, settings, or invalid `--session` ID error. |
