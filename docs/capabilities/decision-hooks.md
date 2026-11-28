---
title: Decision Hooks
description: Declare hook policies that match tool calls, gather probe evidence, ask the decision model typed questions, and return a typed decision or a policy fallback.
---

# Decision Hooks

A **decision hook** is a policy file, not code. It names one of `poo`'s [hook points](/extending/architecture#hook-points), the calls it matches, the evidence it gathers, and a small graph of typed questions. The [decision model](/capabilities/decision-model) answers those questions. Each answer then picks a terminal decision the core carries out.

Hooks never become a second lifecycle owner. A hook returns a typed decision or an error, and the existing owners do the rest. `execute.go` records the paired tool result, the finalizer settles the run, and the job owner closes a background job's completion signal.

## A complete `settings.yaml`

```yaml
# settings.yaml
version: 1

roles:
  chat: opencode-go/glm-5.3
  decider: typesafe/jev-latest

hooks:
  pre-push-gofmt:
    point: before_tool
    match:
      tool: bash
      command_contains: ["git push", "git commit"]
    max_fires_per_run: 3
    firing_timeout_ms: 8000
    on_uncertain: fail_open
    on_error: fail_open
    context:
      turns: 8
      include_tool_call: true
      include_working_dir: true
      probes:
        - name: unformatted_files
          command: ["gofmt", "-l", "."]
          timeout_ms: 3000
        - name: go_mod
          tool:
            name: read
            args: { path: "go.mod", limit: 30 }
    entry: is_pushing_go_code
    steps:
      is_pushing_go_code:
        timeout_ms: 1500
        question:
          kind: bool
          instructions: "Is the pending bash command committing or pushing Go changes while unformatted_files is non-empty?"
          true: "committing or pushing while unformatted Go files exist"
          false: "safe to proceed"
        branches:
          "yes":
            min_probability: 0.75
            next: classify_fix
          "no":
            min_probability: 0.5
            action: { kind: allow }
      classify_fix:
        timeout_ms: 1500
        question:
          kind: choice
          instructions: "Which instruction should be given to the agent?"
          options:
            - { name: run_gofmt, desc: "Tell the agent to run gofmt first" }
            - { name: allow_anyway, desc: "False positive; allow the command" }
        branches:
          run_gofmt:
            min_probability: 0.70
            action:
              kind: deny
              message: "Unformatted Go files were detected. Run `gofmt -w .` before committing or pushing."
          allow_anyway:
            min_probability: 0.5
            action: { kind: allow }

profiles:
  strict-go:
    base: code
    hooks: [pre-push-gofmt]
  relaxed-go:
    base: strict-go
    hooks_disabled: [pre-push-gofmt]
```

Hooks declare inline under `hooks`, or in separate files named by `hooks_files`. A [profile](/configuration/profiles-and-prompts) turns hooks on: `hooks` selects names, and `hooks_disabled` drops names the base chain runs. A profile that runs hooks requires `roles.decider`, and the build fails naming `roles.decider` when the role is unset. A command probe also requires a linked executor matching `tools.bash.executor`.

## Points and their decisions

| `point` | Allowed `match` fields | Terminal `action.kind` | `fail_open` (default) | `fail_closed` | `fail_run` |
|---|---|---|---|---|---|
| `before_run` | `trigger` (`prompt`, `job`, `feed`) | `pass`, `append` (message) | `StartDecision{}` | refused | run fails |
| `before_tool` | `tool`, `class`, `command_prefix`, `command_contains` | `allow`, `deny` (message), `short_circuit` (message) | `Allow()` | `Deny(fail_closed_message)` | run fails |
| `after_tool` | `tool`, `class`, `command_prefix`, `command_contains`, `is_error` | `keep`, `annotate` (message), `replace` (message), `redact` | `Keep()` | refused | run fails, or the job fails |
| `after_turn` | `min_turn`, `interrupted` | `pass`, `append` (message), `stop` (message) | `TurnDecision{}` | `StopTurn(fail_closed_message)` | run fails |
| `before_settle` | `reason` (`end_turn`, `output_truncated`), `min_turn` | `accept`, `continue` (message), `stop` (message) | `Accept()` | `StopSettle(fail_closed_message)` | run fails |

A point refuses a matcher field or a decision it does not own. `before_run` and `after_tool` have no blocking decision, so `fail_closed` there is a spec error, not a silent `fail_open`.

`deny` hands the model a message beside the call's error result. `short_circuit` answers the call without running it, and its message is the result text. `redact` replaces the result text with the session's redaction notice. `append` at `after_turn`, and `continue` at `before_settle`, write a user message and send the run back to the model.

## Matching a call

- A matcher that sets nothing fires on every activity at its point.
- `tool` and `class` name one thing or the other, never both. `class` is `read_only` or `side_effecting`.
- `command_prefix` and `command_contains` need a `tool`, and never appear together.
- `command_contains` is any-of: one substring holding is the matcher holding.
- The command is the tool's primary argument, compared as text with leading whitespace trimmed. A compound command matches its first part only, so `cd repo && git push` does not hold `command_prefix: git`.
- Matcher text is lexical, unlike [permission rules](/operations/permissions-and-safety): a `fetch` URL in a matcher is compared as written, never canonicalized.
- A command matcher on a tool with no primary argument fails the build: every call would be unmatched, and the hook would never fire.

An unmatched call costs nothing: no probe and no decision-model call. A matching call after the firing cap is reached costs no decision-model call either, and takes the point's neutral decision (see [Caps and budgets](#caps-and-budgets)).

## Evidence and probes

`context.turns` adds the last N conversation turns, and `include_working_dir` adds the directory the session works in. Probes add facts kept apart from the conversation, keyed by name in the evidence.

`include_tool_call` adds the call the hook judges: its tool, class, summary, and arguments. The summary and arguments are the ones a permission ask shows, the same view the permission decider gets. They are not the model's own description of the call. The summary is clipped to 512 characters. An argument object past 8192 bytes keeps both ends of each long string value, with the middle elided. At `after_tool` the result is in the evidence whatever `include_tool_call` says, and its text is clipped to `poo.EvidenceOutputCap` (512).

| Probe | Reports |
|---|---|
| `command: [...]` | The exit status, plus the head of the command's combined output. |
| `tool: {name, args}` | The read-only tool's text, capped at 8192 characters. |

A probe runs a command or a tool, never both, and a hook runs at most four probes sequentially. A probe that names no `timeout_ms` gets 3000 milliseconds. A tool probe names `read`, `grep` or `history`, and the built session must offer that tool: a disabled tool fails the build, and so does a tool that only runs in the background, because a probe is a call the hook waits on. A command probe runs on the executor `tools.bash.executor` names, in the session's working directory, with the session's scrubbed environment.

A probe should exit promptly on SIGTERM. The executor sends SIGTERM first and waits a kill grace before SIGKILL, 2 seconds for `local` and `bwrap`. A probe that ignores the signal can push its stop into the hook's deadline.

### Probe trust model

A probe runs in the run's path, so its reach is the session's. With the `local` executor a probe command is **not sandboxed**: it runs in `poo`'s own process space and reaches whatever `poo` reaches. Point `tools.bash.executor` at `bwrap` when a probe must be confined (see [the sandbox](/operations/sandbox)). A probe is operator-written policy, never model output, so a probe's argv is as trusted as the settings file that holds it.

The [permission engine](/operations/permissions-and-safety) does not see a probe. A probe raises no ask, so a `deny` rule and `ask_outside_working_dir` bound the model's calls and not a hook's: what a probe reaches is what the tool's own restrictions allow, such as `restrict_to_working_dir` and `tools.protected_paths`. A hook that must be confined gets that from the executor, not from a permission rule.

A **nonzero exit code is evidence, not an error**. `gofmt -l` reports findings with status 1, and a hook that read that status as a failure could never ask about the finding. A probe fails when it cannot start, times out, is stopped, or calls a tool that reports an error; that failure takes `on_error`.

Evidence is bounded. The probe output cap is 8192 bytes per probe, and the whole payload is held to 32768 bytes. The fit drops turns oldest first, then halves the longest probe output. Evidence that still does not fit is a hook error.

## Questions and confidence

A step asks one `bool`, `choice`, or `score` question, and its `branches` map answer keys to a next step or a terminal action. A `bool` answers `yes` and `no`, a `choice` answers its option names, and a `score` answers `"0"` through `"N-1"`. A step may name its own `timeout_ms`; otherwise its ask runs under the hook's firing timeout.

YAML reads an unquoted `true:` or `false:` as a boolean key, and an unquoted integer key such as `0:` as a number. The loader keeps each as the text it is spelled with. Bool question descriptions (`true:` and `false:`) and score branch keys (`0:` through `N-1:`) are accepted unquoted, in inline settings and in `hooks_files`. Two keys that spell the same text, such as `True:` and `"true":`, are refused. The normalization applies to every settings YAML file, not only hooks.

The runner scores each answer key as `P(key) * label_mass` and takes the best one. A tie, a best key no branch covers, or a score under the branch's `min_probability` is *uncertain*, and `on_uncertain` answers it. A label mass outside `[0, 1]`, a missing answer, or an invalid distribution is an *error*, and `on_error` answers it. `min_probability` sits above zero and at most one.

## Fail-open by default

A policy the spec leaves out is `fail_open`, so a hook that cannot answer changes nothing. Silence from the decision model, a probe failure, a malformed answer, and evidence over its cap all take the same default.

| Policy | What the run does |
|---|---|
| `fail_open` | Takes the point's neutral decision and carries on. |
| `fail_closed` | Takes the point's blocking decision, worded by `fail_closed_message`. |
| `fail_run` | Fails the run with `hook_error` (exit code 22) through the core's finalizer. |

`fail_closed_message` is required when either policy is `fail_closed`, and refused otherwise. A `fail_run` error on a background job's late `after_tool` completion fails the job, not a run, because no run owns that call. An interrupted activity drops the hook's decision and is not a hook error.

## Caps and budgets

`max_fires_per_run` caps how many decisions take effect in one run, and the count lives in memory in the session's runner. Every matching call is judged until the cap is reached. Once it is reached, a further matching call gets the point's neutral decision with no decision-model call. A deny hook therefore stops denying after N calls, and the model can retry the call. Only a decision that changes something is charged: a neutral answer costs nothing, and a `fail_closed` block counts. Concurrent calls are all judged, and a call whose decision loses the race for the last slot gets the neutral decision. A late background completion counts against its job, because the run that started the job may already have settled. A new session gets fresh runners, so a resumed session starts its counts again.

| Bound | Value |
|---|---|
| Steps per hook | 8 |
| Probes per hook | 4 |
| Probe output | 8192 bytes |
| Call arguments | 8192 bytes |
| Evidence | 32768 bytes |
| Probe `timeout_ms` | 3000 by default |
| `firing_timeout_ms` | 10000 by default |

The runner clamps a hook's budget under the session's hook timeout (`HookTimeout`, 30 seconds by default). It keeps the hook's own answer 2 seconds ahead of that deadline, and floors the budget at 500 milliseconds.

## A `before_settle` example

```yaml
# hooks/settle.yaml
hooks:
  keep-going:
    point: before_settle
    match:
      reason: end_turn
      min_turn: 2
    max_fires_per_run: 1
    on_uncertain: fail_open
    on_error: fail_open
    context:
      turns: 6
    entry: is_work_unfinished
    steps:
      is_work_unfinished:
        question:
          kind: bool
          instructions: "Did the last turn leave a task the session was asked to finish?"
          true: "the task is unfinished"
          false: "the task is done"
        branches:
          "yes":
            min_probability: 0.8
            action:
              kind: continue
              message: "The task is unfinished. Continue with the next step."
          "no":
            min_probability: 0.6
            action: { kind: accept }
```

A `continue` spends the run's continuation budget (`limits.max_continuations`), and a run that spends it settles `hook_continue_limit` (exit code 12). Keep `max_fires_per_run` small on this point.

## Where a hook is visible

Every question carries the ID `hook:<name>:<step>`, and a decision record's questions name the hook that asked. A message a hook writes carries the origin `hook:<name>` in the [session log](/reference/session-log-v0), so a reader can tell the hook's word from the model's.

A hook that gathers no command probe reports itself idempotent, so a durable session may run it again after a crash. A hook with a command probe is never re-executed on recovery, because the operator's command may not be safe to repeat.
