---
title: Settings Reference
description: Complete key-by-key reference for poo settings files across JSON, YAML, and TOML.
---

# Settings Reference

Every key in a poo settings file is optional except `version`. Unknown keys are rejected at load time. See [Settings Discovery](/configuration/settings-discovery) for file lookup order, supported formats, and relative path resolution rules.

## Top-Level Metadata & Interface

### `version`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `version` | `int` | Required (`1`) | Settings format version. Must be `1`. |

### `ui`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `ui` | `string` | Binary's default front door (`repl`) | Interface launched by a bare `poo` invocation. Must be linked into the binary. |

## Profiles

See [Profiles & Prompts](/configuration/profiles-and-prompts) for profile inheritance and layering details.

### `profile` & `repl_profile`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `profile` | `string` | Binary's default profile | Profile name used when building a session (`run`, `serve`, etc.). |
| `repl_profile` | `string` | Falls back to `profile` / binary default | Profile used by the interactive REPL for new conversations when no flag overrides it. Reopened sessions keep the profile recorded in their log. |

### `profiles_files`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `profiles_files` | `string[]` | Empty | External YAML or JSON files defining reusable profiles under a top-level `profiles` key. Loaded as layers below inline `profiles` (inline wins, then earlier files). Relative paths resolve against the settings file's directory; `prompt_file` and `prompt_append_file` inside a profiles file resolve against that profiles file's directory. |

### `profiles`

Map of `<name>` to profile definition object. Each entry defines a named variant of a linked profile or another defined profile.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `profiles.<name>.base` | `string` | Required | Name of the linked or defined profile this profile inherits from. |
| `profiles.<name>.mcp_defaults` | `bool` | Inherits `mcp.defaults` | Overrides whether poo's default `mcp` directory loads for sessions under this profile. |
| `profiles.<name>.mcp_servers` | `string[]` | Inherits base | MCP server names to start. Appends to the base profile's list (deduplicated, preserving first appearance). Missing servers warn and are skipped. |
| `profiles.<name>.feeds` | `string[]` | Inherits base | Feed manifest names this profile requires. Appends to the base profile's list (deduplicated). Unloaded feeds warn and are skipped. |
| `profiles.<name>.feeds_defaults` | `bool` | Inherits `feeds.defaults` | Overrides whether poo's default `feeds` directory loads for sessions under this profile. |
| `profiles.<name>.cli_registry_defaults` | `bool` | Inherits `cli_registry.defaults` | Overrides whether poo's default `cli-registry` directory loads for sessions under this profile. |
| `profiles.<name>.prompt_file` | `string` | Empty | File whose text replaces the profile's prompt section. Relative paths resolve against the declaring file's directory. |
| `profiles.<name>.prompt_append_file` | `string` | Empty | File whose text appends to the profile's prompt section. Mutually exclusive with `prompt_append`. Relative paths resolve against the declaring file's directory. |
| `profiles.<name>.prompt_append` | `string` | Empty | Inline text appended to the profile's prompt section. Mutually exclusive with `prompt_append_file`. |
| `profiles.<name>.hooks` | `string[]` | Inherits base | [Decision hook](/capabilities/decision-hooks) names this profile runs. Adds to the base's chain (deduplicated, first-appearance order); a name the settings do not declare fails the build. |
| `profiles.<name>.hooks_disabled` | `string[]` | Empty | Hook names dropped from this profile's chain, including ones the base runs. A name the profile both runs and turns off fails the build. |
| `profiles.<name>.model_family` | `bool` | `false` | When `true`, includes model-family guidance keyed by the active model's `prompt_variant`. |
| `profiles.<name>.tools.disabled` | `string[]` | Empty | Tool names disabled for sessions running under this profile. |

```yaml
profile: reviewer
repl_profile: code
profiles_files:
  - ./team-profiles.yaml

profiles:
  reviewer:
    base: code
    prompt_append: "Focus on correctness and edge cases. Do not modify files."
    model_family: true
    tools:
      disabled: [edit, write, bash]
```

## Endpoints, Models, Roles & Auth

See [Models & Catalog](/configuration/models-and-catalog), [Models File Reference](/reference/models-file), and [Providers & Auth](/configuration/providers-and-auth).

### `endpoints`

Map of `<name>` to endpoint configuration object. Credentials resolve from `api_key_env` first, falling back to literal `api_key`.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `endpoints.<name>.endpoint` | `string` | Required | Base URL for the endpoint (legacy alias: `base_url`). |
| `endpoints.<name>.api_key_env` | `string` | Empty | Environment variable name holding the API key. Automatically scrubbed from child tool environments. |
| `endpoints.<name>.api_key` | `string` | Empty | Literal API key stored in settings (fallback when `api_key_env` is unset or empty). |
| `endpoints.<name>.session_header` | `string` | Empty | HTTP header name that carries the session ID for gateway routing. |
| `endpoints.<name>.skip_probe` | `bool` | `false` | Skip probing the endpoint for its served model list. |
| `endpoints.<name>.instructions_per_question` | `bool` | `false` | Fold decision framing into each question instead of sending a request-level `instructions` field. |

### `models`

Map of `<name>` to model configuration object. Omitted fields inherit from the curated catalog, `models_file`, or endpoint discovery.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `models.<name>.endpoint` | `string` | Required | Name of the endpoint serving this model. |
| `models.<name>.model_id` | `string` | Required | Model identifier sent to the endpoint. |
| `models.<name>.api` | `string` | Chat completions | Wire API dialect used to talk to the model (e.g., `responses`). See [Wire API Mapping](/reference/wire-api-mapping). |
| `models.<name>.tools_mode` | `string` | Model default | How tool calls are requested: `native`, `text`, `auto`, or `none`. |
| `models.<name>.ops` | `string[]` | Model default | Operations the model serves (e.g., `compact` for native compaction). |
| `models.<name>.inputs` | `string[]` | Model default | Input modalities the model accepts (e.g., `text`, `image`). |
| `models.<name>.context_limit` | `int` | Model default | Total context window size in tokens (prompt plus output). |
| `models.<name>.input_limit` | `int` | Model default | Maximum prompt tokens accepted when smaller than `context_limit`. |
| `models.<name>.max_output` | `int` | Model default | Maximum tokens the model writes in one response. |
| `models.<name>.reasoning` | `object` | Model default | How reasoning is controlled on the wire. Contains `mode` (`none`, `toggle`, `effort`, `budget`) and `param` (wire parameter path, e.g., `reasoning_effort` or `chat_template_kwargs.enable_thinking`). |
| `models.<name>.reasoning_levels` | `string[]` | Model default | Accepted reasoning levels from lowest to highest (e.g., `[off, minimal, low, medium, high, xhigh]`). Sent verbatim as spelled. |
| `models.<name>.quirks` | `string[]` | Empty | Named transport adaptations applied in order (e.g., `audio_url`). Unknown names warn and are skipped. |
| `models.<name>.prompt_variant` | `string` | Empty | Variant key used to select model-family prompt sections. |

### `models_file`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `models_file` | `string` | Auto-discovers sibling `models.yaml` or `models.json` | Path to a models file loaded as a catalog layer below inline `endpoints` and `models` (legacy alias: `pi_models_file`). Relative paths resolve against the settings file's directory. |

### `roles`

Map of role name to model name (either a key in `models` or `<endpoint>/<id>` from `models_file` / built-in providers).

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `roles.chat` | `string` | None | Primary chat model for the session. |
| `roles.decider` | `string` | None | Decision model used by the `decide` permission asker and CLI registry filtering. See [Decision Model](/capabilities/decision-model). |
| `roles.compaction` | `string` | Falls back to `roles.chat` | Model used for compaction requests (`summary`, `mine`, `pointer`, `handoff`). |

### `auth_file`, `pi_auth_file` & `auth_readonly`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `auth_file` | `string` | Sibling `auth.yaml`/`auth.json` beside `models_file`, else platform config dir | Path to poo's credential file (or a shared read-only file such as `~/.codex/auth.json`). Mutually exclusive with `pi_auth_file`. Relative paths resolve against the settings file's directory. |
| `pi_auth_file` | `string` | None | Path to pi's `auth.json`, read and written in pi's format. Must exist. Mutually exclusive with `auth_file`. Relative paths resolve against the settings file's directory. |
| `auth_readonly` | `bool` | `false` | When `true`, poo never refreshes, signs in, imports, or writes the credential file. Use when another tool owns token refresh. |

```yaml
version: 1
models_file: ./models.yaml
auth_file: ~/.codex/auth.json
auth_readonly: true

endpoints:
  local:
    endpoint: http://localhost:4000/v1
    api_key_env: LOCAL_API_KEY

models:
  qwen3:
    endpoint: local
    model_id: qwen3-coder
    context_limit: 131072
    max_output: 8192
    tools_mode: auto

roles:
  chat: qwen3
  decider: local/gemma-decider
```

## Reasoning & Sampling

See [Reasoning & Sampling](/configuration/reasoning-and-sampling).

### `reasoning`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `reasoning.chat` | `string` | `default` | Reasoning level for the chat model: `default`, `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, or a positive integer token budget (e.g., `"8192"`). `default` omits the reasoning field; `off` asks the model not to think. |

### `sampling`

Map of `<model>` name to per-model sampling overrides. Omitted fields are not sent so the endpoint's default applies.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `sampling.<model>.temperature` | `float` | Unset | Sampling temperature. |
| `sampling.<model>.top_p` | `float` | Unset | Nucleus sampling threshold. |
| `sampling.<model>.top_k` | `float` | Unset | Number of candidate tokens considered by the sampler. |
| `sampling.<model>.min_p` | `float` | Unset | Minimum token probability threshold. |

```yaml
reasoning:
  chat: medium

sampling:
  qwen3:
    temperature: 0.6
    top_p: 0.95
```

## Decider

See [Decision Model](/capabilities/decision-model).

### `decider`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `decider.timeout_ms` | `int` | Decider default | Per-call latency budget for the decider model, in milliseconds. Minimum `1`. |

## Decision Hooks

See [Decision Hooks](/capabilities/decision-hooks). A profile that runs a hook requires `roles.decider`, and a command probe requires a linked executor matching `tools.bash.executor`.

### `hooks_files`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `hooks_files` | `string[]` | Empty | External YAML or JSON files declaring hooks under a top-level `hooks` key. Loaded as layers below inline `hooks` (inline wins, then earlier files). Relative paths resolve against the settings file's directory. |

### `hooks`

Map of `<name>` to hook spec. Every spec is checked at load time; a hook that could not run is a load failure. YAML keys are read as their text (see [Settings Discovery](/configuration/settings-discovery)): an unquoted `true:` description or `0:` branch key is the word it spells. Two keys spelling the same text are refused.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `hooks.<name>.point` | `string` | Required | Where the hook runs: `before_run`, `before_tool`, `after_tool`, `after_turn`, or `before_settle`. |
| `hooks.<name>.match` | `object` | Empty (fires on everything at the point) | Narrows the activity the hook fires on; each point accepts its own fields. Matching is lexical, so a URL is never canonicalized. A `command_prefix` or `command_contains` match on a tool with no primary argument fails the build. |
| `hooks.<name>.max_fires_per_run` | `int` | `0` (no cap) | Decisions per run that take effect, counted in memory in the session's runner. Every matching call is judged until the cap is reached. After the cap, a matching call gets the point's neutral decision with no decider call. A deny hook stops denying after N calls. A neutral answer, or a decision that loses the race for the last slot, costs nothing. A `fail_closed` block counts. A late background completion counts against its job. |
| `hooks.<name>.firing_timeout_ms` | `int` | `10000` | Time the hook has to answer, clamped under the session's hook timeout minus 2 seconds (floor 500 ms). |
| `hooks.<name>.on_error` | `string` | `fail_open` | What a failed firing does: `fail_open`, `fail_closed`, or `fail_run`. `fail_closed` is refused at `before_run` and `after_tool`. |
| `hooks.<name>.on_uncertain` | `string` | `fail_open` | What an answer no branch accepts does: the same policies. |
| `hooks.<name>.fail_closed_message` | `string` | Empty | Text a `fail_closed` policy hands the model. Required when either policy is `fail_closed`, refused otherwise. |
| `hooks.<name>.context.turns` | `int` | `0` | Recent conversation turns the evidence holds (`≥ 0`). |
| `hooks.<name>.context.include_tool_call` | `bool` | `false` | Adds the call the hook judges to the evidence: the tool's ask summary and evidence arguments, not the model's description. The `after_tool` result is always included, clipped to `poo.EvidenceOutputCap` (512). |
| `hooks.<name>.context.include_working_dir` | `bool` | `false` | Adds the session's working directory to the evidence. |
| `hooks.<name>.context.probes[].name` | `string` | Required | Name the probe's evidence is keyed by. Unique within the hook. |
| `hooks.<name>.context.probes[].command` | `string[]` | Empty | argv a command probe runs, on the session's executor. Excludes `tool`. |
| `hooks.<name>.context.probes[].tool` | `object` | Empty | Read-only tool probe: `name` (`read`, `grep`, or `history`) and `args`. Excludes `command`. |
| `hooks.<name>.context.probes[].timeout_ms` | `int` | `3000` | Time the probe has (`≥ 0`). A probe never outlives its hook. The executor sends SIGTERM first and waits a kill grace, so a probe should exit promptly on SIGTERM. |
| `hooks.<name>.entry` | `string` | Required | Step the graph starts at. |
| `hooks.<name>.steps` | `object` | Required (1–8) | Steps by name; every step must be reachable from `entry` and no path may loop. |
| `hooks.<name>.steps.<step>.timeout_ms` | `int` | `0` (the hook's firing timeout) | Time this step's ask may take. |
| `hooks.<name>.steps.<step>.question.kind` | `string` | Required | `bool`, `choice`, or `score`. |
| `hooks.<name>.steps.<step>.question.instructions` | `string` | Required | What the model decides, in prose. |
| `hooks.<name>.steps.<step>.question.true` / `.false` | `string` | Empty | What `yes` and `no` mean. `bool` only; unquoted YAML keys are accepted. |
| `hooks.<name>.steps.<step>.question.options` | `object[]` | Empty | `name` and `desc` pairs. `choice` only. |
| `hooks.<name>.steps.<step>.question.levels` | `string[]` | Empty | Scale levels. `score` only. |
| `hooks.<name>.steps.<step>.branches.<answer>` | `object` | Required | One branch per answer key the question offers. An unquoted score key (`0:`, `1:`) is read as its text. |
| `hooks.<name>.steps.<step>.branches.<answer>.min_probability` | `float` | Required | Confidence the answer needs, above `0` and at most `1`. Below it the firing is uncertain. |
| `hooks.<name>.steps.<step>.branches.<answer>.next` | `string` | Empty | Step the answer moves to. Excludes `action`. |
| `hooks.<name>.steps.<step>.branches.<answer>.action.kind` | `string` | Empty | Terminal decision the point allows. Excludes `next`. |
| `hooks.<name>.steps.<step>.branches.<answer>.action.message` | `string` | Empty | Text the decision carries; required for `append`, `deny`, `short_circuit`, `annotate`, `replace`, `stop`, and `continue`. |

```yaml
hooks_files: [hooks/go-checks.yaml]

hooks:
  no-force-push:
    point: before_tool
    match:
      tool: bash
      command_contains: ["git push --force", "git push -f"]
    max_fires_per_run: 1
    entry: classify
    steps:
      classify:
        question:
          kind: bool
          instructions: "Is this the force push to refuse?"
          true: "the call force-pushes"
          false: "the call is something else"
        branches:
          "yes":
            min_probability: 0.9
            action: { kind: deny, message: "Force push refused." }
          "no":
            min_probability: 0.5
            action: { kind: allow }
```

## Context & Compaction

See [Context Compaction](/capabilities/compaction).

### `context`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `context.strategy` | `string` | `mine` | Compaction strategy: `summary`, `mine`, `pointer`, `handoff`, `native`, or `none`. `native` requires a model declaring the `compact` op and a native-compacting transport. |
| `context.notice_ratio` | `float` | Removed (ignored) | Removed setting. Accepted for backward compatibility and emits a warning. |
| `context.hard_ratio` | `float` | Removed (ignored) | Removed setting. Accepted for backward compatibility and emits a warning. |
| `context.handoff.ratio` | `float` | Strategy default | Fraction of the prompt limit (`0`–`1`) that triggers handoff compaction. |
| `context.handoff.max_tokens` | `int` | Strategy default | Token limit for the handoff request (`≥ 0`). |
| `context.summary.ratio` | `float` | Strategy default | Fraction of the prompt limit (`0`–`1`) that triggers summary compaction. |
| `context.summary.keep_recent_tokens` | `int` | Strategy default | Recent history tokens kept verbatim (`≥ 0`). |
| `context.summary.max_tokens` | `int` | Strategy default | Token limit for the summary request (`≥ 0`). |
| `context.mine.ratio` | `float` | Strategy default | Fraction of the prompt limit (`0`–`1`) that triggers mine compaction. |
| `context.mine.keep_recent_tokens` | `int` | Strategy default | Recent history tokens kept verbatim (`≥ 0`). |
| `context.mine.max_tokens` | `int` | Strategy default | Token limit for the mining request (`≥ 0`). |
| `context.mine.pointer_max_tokens` | `int` | Strategy default | Token limit for the pointer pass (`≥ 0`). |
| `context.pointer.ratio` | `float` | Strategy default | Fraction of the prompt limit (`0`–`1`) that triggers pointer compaction. |
| `context.pointer.keep_recent_tokens` | `int` | Strategy default | Recent history tokens kept verbatim (`≥ 0`). |
| `context.pointer.max_tokens` | `int` | Strategy default | Token limit for the compose request (`≥ 0`). |
| `context.pointer.pointer_max_tokens` | `int` | Strategy default | Token limit for the pointer pass (`≥ 0`). |
| `context.pointer.snippet_budget` | `int` | Strategy default | Maximum bytes of snippet text collected by queries (`≥ 0`). |
| `context.pointer.escalate_after` | `int` | Strategy default | Number of pointer compactions before escalating to full-span fallback (`≥ 0`). |

```yaml
context:
  strategy: mine
  mine:
    ratio: 0.75
    keep_recent_tokens: 8192
```

## Tools & Sandboxing

See [Built-in Tools & Jobs](/capabilities/tools-and-jobs), [Codemode](/capabilities/codemode), and [Sandboxing (bwrap)](/operations/sandbox).

### `tools`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `tools.read.restrict_to_working_dir` | `bool` | `false` | Confine `read` to the session's working directory. |
| `tools.read.ask_outside_working_dir` | `bool` | `false` | Route `read` targets outside the working directory through the permission asker. |
| `tools.grep.restrict_to_working_dir` | `bool` | `false` | Confine `grep` to the session's working directory. |
| `tools.grep.ask_outside_working_dir` | `bool` | `false` | Route `grep` targets outside the working directory through the permission asker. |
| `tools.edit.restrict_to_working_dir` | `bool` | `false` | Confine `edit` to the session's working directory. |
| `tools.edit.ask_outside_working_dir` | `bool` | `false` | Route `edit` targets outside the working directory through the permission asker. |
| `tools.write.restrict_to_working_dir` | `bool` | `false` | Confine `write` to the session's working directory. |
| `tools.write.ask_outside_working_dir` | `bool` | `false` | Route `write` targets outside the working directory through the permission asker. |
| `tools.bash.timeout_s` | `int` | `120` | Per-command wall-clock timeout, in seconds. |
| `tools.bash.executor` | `string` | `local` | Command executor linked into the binary (e.g., `local`, `bwrap`). |
| `tools.codemode.engine` | `string` | `quickjs` | Script runtime for `codemode`: `quickjs` or `starlark`. |
| `tools.codemode.timeout_s` | `int` | Engine default | Wall-clock timeout for one script run, in seconds. |
| `tools.codemode.script_max_bytes` | `int` | Engine default | Maximum accepted script size, in bytes. |
| `tools.codemode.output_cap_chars` | `int` | Engine default | Maximum printed output characters retained per run. |
| `tools.codemode.exec_steps` | `int` | Engine default | Starlark execution step budget. Rejected if non-zero when `engine` is `quickjs`. |
| `tools.fetch.timeout_s` | `int` | `30` | Timeout per HTTP fetch, in seconds. |
| `tools.fetch.max_bytes` | `int` | Tool default | Maximum response body size read, in bytes. |
| `tools.fetch.user_agent` | `string` | Tool default | Custom `User-Agent` header sent by `fetch`. |
| `tools.fetch.allow_private_hosts` | `bool` | `false` | Allow loopback and private IP ranges in `fetch` (link-local and reserved ranges remain blocked). |
| `tools.bwrap.network` | `bool` | `true` | Share host network inside the `bwrap` sandbox. Set `false` to isolate network access. |
| `tools.bwrap.ro_binds` | `string[]` | `/usr`, `/bin`, `/lib`, `/lib64`, `/etc` | Host paths mounted read-only inside the `bwrap` sandbox. |
| `tools.bwrap.rw_binds` | `string[]` | Working dir only | Additional host paths mounted read-write inside the `bwrap` sandbox. |
| `tools.env.scrub` | `string[]` | Endpoint credential env vars | Environment variable names removed from child processes and session environment. |
| `tools.env.passthrough` | `string[]` | Unset (all non-scrubbed vars) | Allowlist of environment variable names kept for child processes; all others are dropped. |
| `tools.protected_paths` | `string[]` | Empty | Glob patterns of paths tools must not read or modify (masked inside `bwrap`). |
| `tools.disabled` | `string[]` | Empty | Tool names disabled regardless of profile. Naming an unlinked or core-required tool is an error. |

```yaml
tools:
  edit:
    restrict_to_working_dir: true
  write:
    restrict_to_working_dir: true
  bash:
    executor: bwrap
    timeout_s: 60
  bwrap:
    network: false
  protected_paths:
    - "**/.env"
    - "~/.ssh/**"
  disabled: [fetch]
```

## Permissions

See [Permissions & Safety](/operations/permissions-and-safety).

### `permission`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `permission.unmatched` | `string` | Profile default | Action for a side-effecting call that matches no rule: `allow`, `ask`, or `deny`. |
| `permission.fallback` | `string` | `deny` | Decision when the asker chain is exhausted or times out: `allow` or `deny`. |
| `permission.askers` | `string[]` | Binary's default chain | Ordered list of askers (`decide`, `wire`). An explicit empty list `[]` skips all askers and goes straight to `fallback`. `decide` requires `roles.decider`. |
| `permission.chain_timeout_s` | `int` | `120` | Overall timeout for the asker chain, in seconds. Minimum `1`. |
| `permission.doom_loop.threshold` | `int` | Policy default | Exact repeat count that triggers the doom-loop action. Minimum `1`. |
| `permission.doom_loop.near_threshold` | `int` | Policy default | Near-repeat count that triggers the doom-loop action. Minimum `1`. |
| `permission.doom_loop.action` | `string` | Policy default | Action taken on a doom loop: `allow`, `ask`, or `deny`. |
| `permission.decide.allow` | `float` | `0.85` | Probability threshold (`0`–`1`) at which the decider allows a call. Requires `roles.decider`. |
| `permission.decide.deny` | `float` | `0.99` | Probability threshold (`0`–`1`) at which the decider denies a call. Requires `roles.decider`. |
| `permission.decide.timeout_ms` | `int` | `1500` | Per-call timeout for the `decide` asker, in milliseconds. Minimum `1`. Requires `roles.decider`. |
| `permission.rules` | `object[]` | Empty | Ordered permission rules; first match wins. See table below. |

### `permission.rules[]`

Each rule must specify **either** `tool` **or** `class` (never both). `command_prefix` and `command_contains` require `tool` and are mutually exclusive.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `tool` | `string` | Empty | Tool name to match (must be linked into the binary). |
| `class` | `string` | Empty | Tool class to match: `read_only` or `side_effecting`. |
| `command_prefix` | `string` | Empty | Prefix matched against the tool's primary argument (e.g., bash command or fetch URL). A schemeless URL prefix matches both `http` and `https`. |
| `command_contains` | `string[]` | Empty | Non-empty substrings matched anywhere in the tool's primary argument (any single match triggers). |
| `action` | `string` | Required | Action on match: `allow`, `ask`, or `deny`. |

```yaml
permission:
  unmatched: ask
  fallback: deny
  askers: [decide, wire]
  chain_timeout_s: 60
  rules:
    - class: read_only
      action: allow
    - tool: bash
      command_prefix: "git status"
      action: allow
    - tool: bash
      command_contains: ["rm -rf", "sudo "]
      action: deny
```

## System Prompt

See [Profiles & Prompts](/configuration/profiles-and-prompts).

### `system_prompt`

`base_file` and `replace` are mutually exclusive.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `system_prompt.base_file` | `string` | Empty | File holding the prompt's base section. Relative paths resolve against the settings file's directory; `~/` resolves under `$HOME`. |
| `system_prompt.replace` | `string` | Empty | Inline text replacing the entire prompt (except the CLI catalog section when active). |
| `system_prompt.append` | `string` | Empty | Text appended to the base prompt section. |
| `system_prompt.sections` | `map[string]string` | Empty | Named prompt sections to set or override. |
| `system_prompt.remove` | `string[]` | Empty | Named prompt sections to remove. |

## Limits & Background Jobs

See [Limits & Exit Codes](/operations/limits-and-exit-codes) and [Built-in Tools & Jobs](/capabilities/tools-and-jobs).

### `limits`

A value of `0` disables the limit (except `truncation_retries`, where `0` disables retries and omission uses the default of `1`).

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `limits.max_tokens_per_run` | `int` | `0` (off) | Maximum tokens consumed in a single run (`≥ 0`). |
| `limits.max_duration_s` | `int` | `0` (off) | Wall-clock time limit for a single run, in seconds (`≥ 0`). |
| `limits.max_turns` | `int` | `0` (off) | Maximum model turns in a single run (`≥ 0`). |
| `limits.max_continuations` | `int` | `3` | Maximum `before_settle` continuations allowed in a run (`≥ 0`); `0` keeps the runtime default of `3`. |
| `limits.truncation_retries` | `int` | `1` | Consecutive truncated turns retried before settling `output_truncated`. Set `0` to disable (`≥ 0`). |

### `jobs`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `jobs.max_running` | `int` | Runtime default | Maximum concurrent background jobs (`≥ 0`). |
| `jobs.timeout_s` | `int` | Runtime default | Per-job wall-clock timeout, in seconds (`≥ 0`). |
| `jobs.on_idle` | `string` | Runtime default | Handling for a job result arriving while the session is idle: `wake` or `hold`. |
| `jobs.max_unattended_wakes` | `int` | `0` (no limit) | Maximum consecutive woken runs without client input (`≥ 0`). |

## Structured Output

### `finish`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `finish.schema_file` | `string` | Empty | Path to a valid JSON Schema file used to validate typed final results. Relative paths resolve against the settings file's directory; `~/` resolves under `$HOME`. |

## Manifests & Skills

See [CLI Registry & Script Tools](/capabilities/cli-registry), [Feeds & Daemon](/capabilities/feeds-and-daemon), [MCP Servers](/capabilities/mcp-servers), [Skills](/capabilities/skills), and [Manifests Reference](/reference/manifests).

### `cli_registry`

Legacy alias: `cliregistry`.

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `cli_registry.paths` | `string[]` | Empty | Registry directories. Relative paths resolve against the working directory; `~/` resolves under `$HOME`. |
| `cli_registry.defaults` | `bool` | `true` | Load manifests from poo's default `cli-registry` directory under the user config directory. Set `false` to isolate. |

### `feeds`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `feeds.paths` | `string[]` | Empty | Feed manifest directories. Relative paths resolve against the working directory; `~/` resolves under `$HOME`. |
| `feeds.defaults` | `bool` | `true` | Load manifests from poo's default `feeds` directory under the user config directory. Set `false` to isolate. |

### `mcp`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `mcp.paths` | `string[]` | Empty | `mcp.json` manifest files or directories. Relative paths resolve against the working directory; `~/` resolves under `$HOME`. |
| `mcp.defaults` | `bool` | `true` | Load manifests from poo's default `mcp` directory under the user config directory. Set `false` to isolate. |

### `skills`

`default` and `user_path` are mutually exclusive. Missing skill directories are dropped with a warning (silently for `default`).

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `skills.default` | `bool` | `false` | When `true`, offer skills from `~/.agents/skills`. Mutually exclusive with `user_path`. |
| `skills.user_path` | `string` | Empty | Custom user skills directory (`~/` expanded). Mutually exclusive with `default`. |
| `skills.paths` | `string[]` | Empty | Additional skill directories. Relative paths resolve against the working directory; `~/` resolves under `$HOME`. |

## Session Persistence & Prompts

See [Sessions & Durability](/operations/sessions-and-durability).

### `session`

| Key | Type | Default / Behavior | Description |
|---|---|---|---|
| `session.mode` | `string` | `logged` | Session persistence mode: `logged`, `ephemeral`, or `durable`. |
| `session.dir` | `string` | `~/.local/state/poo/sessions` | Directory where session logs and state live (`~/` expanded). |
| `session.project_prompt` | `bool` | `false` | Include the working directory's `AGENTS.md` (or `CLAUDE.md` if absent) in the prompt. Reads only the working directory; never walks up. |
| `session.user_prompt` | `bool` | `false` | Load user instructions from the binary's default config path. |
| `session.user_prompt_file` | `string` | Empty | Path to a custom user instructions file (`~/` expanded). Setting this enables user instructions without setting `user_prompt: true`. |
| `session.retention.max_age_days` | `int` | Unset (opt-in) | Delete sessions whose last activity is older than this many days on prune (`≥ 0`). |
| `session.retention.max_sessions` | `int` | Unset (opt-in) | Keep at most this many sessions on prune (`≥ 0`). |
| `session.durable.max_resumptions` | `int` | `3` | Maximum crash resumptions before the durable planner gives up. Minimum `1`. |
| `session.durable.lease_ttl_s` | `int` | `30` | Session lease duration before expiry, in seconds. Minimum `1`. |
| `session.durable.auto_resume` | `bool` | `false` | Automatically resume an unsettled run when a durable session loads. |
| `session.durable.store` | `string` | `file` | Durable storage backend: `file` or `sqlite`. |

```yaml
session:
  mode: durable
  dir: ~/.local/state/poo/sessions
  project_prompt: true
  retention:
    max_age_days: 30
    max_sessions: 200
  durable:
    store: sqlite
    max_resumptions: 3
    lease_ttl_s: 30
    auto_resume: false
```
