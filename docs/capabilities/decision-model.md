---
title: Decision Model
description: Configure roles.decider to power fast yes/no and classifier judgments for permissions, feed filtering, large tool catalog search, and declarative hooks.
---

# Decision Model

`poo` supports a dedicated **decision model** (`roles.decider`) alongside the chat model (see [Roles and Required Operations](/configuration/models-and-catalog#roles-and-required-operations)). Instead of free-form prose, it takes a state payload and typed questions and returns a probability distribution over the allowed answers. Questions can be `Bool` (yes/no), `Choice` (up to 26 named options), or `Score` (2 to 10 levels).

Every call through a session's decider is wrapped by `decide.Recorded` (`poo/decide`), which writes a `decision` record to the [session log](/reference/session-log-v0). Each record includes the caller origin, `state_hash`, questions, answers, outcome (`ok`, `deferred`, or `invalid`), latency, and wire request count.

## Configuring `roles.decider`

A model assigned to `roles.decider` must declare `ops: [systemone]` (or `decisions`). Models that serve only chat turns are rejected at construction time.

### Built-in TypeSafe provider

`poo` ships a built-in `typesafe` decisions provider (`https://api.typesafe.ai/v1`) serving `typesafe/jev-latest` and `typesafe/jev-preview`. Jev answers decision questions only and serves no chat turns.

Authenticate with `poo login typesafe` or set `TYPESAFE_API_KEY` in the environment (see [Providers & Auth](/configuration/providers-and-auth)), then assign the role:

```yaml
roles:
  chat: opencode-go/glm-5.3
  decider: typesafe/jev-latest

decider:
  timeout_ms: 1500
```

The TypeSafe endpoint rejects a request-level `instructions` field and has no OpenAI-shaped `/models` list. Its built-in definition therefore sets `instructions_per_question: true` (folding call framing into each question) and `skip_probe: true`.

### Custom decision endpoints and models

To point `roles.decider` at a newer TypeSafe model or a self-hosted `systemone`-compatible endpoint, declare the endpoint and model with `ops: [systemone]`:

```yaml
endpoints:
  my-decider:
    endpoint: https://api.typesafe.ai/v1
    api_key_env: TYPESAFE_API_KEY
    skip_probe: true
    instructions_per_question: true

models:
  my-decider/jev-next:
    endpoint: my-decider
    model_id: jev-next
    ops: [systemone]
    inputs: [text]
    context_limit: 64000

roles:
  chat: opencode-go/glm-5.3
  decider: my-decider/jev-next
```

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `roles.decider` | string | unset | Model name (`provider/model`) for the decider role. Must serve `systemone` or `decisions`. |
| `decider.timeout_ms` | int | unset | Global per-call latency budget in milliseconds (minimum `1` when set). |
| `endpoints.<name>.instructions_per_question` | bool | `false` | Fold request-level framing into each question for endpoints that reject a top-level `instructions` field. |
| `models.<name>.ops` | string[] | `[]` | Operations served by the model; include `systemone` for decision models. |

## Where `poo` uses the decision model

Today, four built-in subsystems call `roles.decider` when it is configured.

::: info Growing surface
This subsystem set is not the end of the road. `poo` is under active development, and more decision-backed capabilities and integrations will be added over time. Custom Go binaries can also call `poo/decide.Decider` directly from their own [hooks](/extending/architecture).
:::

### 1. Permission asker (`perm/decide`)

The `decide` permission asker (`poo/perm/decide`) sits in the [permission asker chain](/operations/permissions-and-safety) to judge unmatched side-effecting tool calls automatically.

When asked, it sends one `Bool` question (`allow`) along with a bounded evidence payload framed as data, not instructions:

- Up to 20 recent conversation turns (`DefaultEvidenceTurns`), with message text capped at 512 chars, tool inputs at 256 chars, and tool outputs at 512 chars.
- The working directory and the pending call's tool name, summary, class, reason, and permitted arguments.

The asker compares the returned probabilities against two thresholds:

- `P(yes) >= permission.decide.allow` → **allow** (default `0.85`).
- `P(no) >= permission.decide.deny` → **deny** (default `0.99`).
- Anything in between — as well as timeouts, errors, panics, or invalid probability distributions — **defers** to the next asker in `permission.askers` (or `permission.fallback`). A deferral never grants permission.

```yaml
roles:
  chat: opencode-go/glm-5.3
  decider: typesafe/jev-latest

permission:
  askers: [decide, wire]
  fallback: deny
  decide:
    allow: 0.85
    deny: 0.99
    timeout_ms: 1500
```

Setting `decide` in `permission.askers` or configuring any `permission.decide.*` key requires `roles.decider` to be set.

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `permission.decide.allow` | float | `0.85` | Minimum `yes` probability (`0`–`1`) to allow a call. |
| `permission.decide.deny` | float | `0.99` | Minimum `no` probability (`0`–`1`) to deny a call. |
| `permission.decide.timeout_ms` | int | `1500` | Per-call latency budget for the permission asker, in milliseconds. |

### 2. Feed record filtering

A [feed manifest](/capabilities/feeds-and-daemon) can declare a question `filter` to screen incoming messages before they enter the session. The model can also set one dynamically on a running feed:

```yaml
filter:
  question: "Is this message actionable for the active repository?"
  min_no_probability: 0.8
```

For each emitted record, `poo` asks the decision model one `Bool` question (`yes` = deliver, `no` = drop) over `{ feed, from, text }` with a `1500ms` timeout and an 8 KiB combined sender+text limit. A message is dropped only when `P(no)` (multiplied by `label_mass` if reported) is at least `min_no_probability` (valid range `(0.5, 1]`; default `0.6` for model-set prompts).

Question filters **fail open** and deliver messages unfiltered when screening cannot run. That includes sessions with no decider (logged once per feed attempt), image-only messages, and payloads over 8 KiB. Messages also pass through when the decider times out or defers.

### 3. Large CLI and MCP catalog search (`find_tool`)

Sessions with [CLI Registry](/capabilities/cli-registry) manifests and [MCP servers](/capabilities/mcp-servers) can accumulate dozens or hundreds of entries. When the catalog exceeds 30 entries (`cli.FindThreshold`) and a decision model is configured, `poo` offers `find_tool` instead of dumping the full catalog into the system prompt.

When the model calls `find_tool` with a natural-language `intent`, `poo` asks the decision model one `Bool` question per offered entry. Requests batch up to 64 questions each, with up to 4 requests in flight. `find_tool` returns the top 8 matching entries ranked best-first with their schemas. If the search fails or the decider is unavailable, `find_tool` returns a short notice instead of dumping the catalog.

### 4. Declarative hooks (`hook/`)

A [decision hook](/capabilities/decision-hooks) is a policy file that declares its own questions and the decision each answer takes. A hook's steps are `Bool`, `Choice`, or `Score` questions with their own instructions, and the answer's confidence is checked against a `min_probability` per branch. No decider means no hook: a profile that activates hooks fails to build without `roles.decider`, rather than failing open at every call.

Each step's question carries the ID `hook:<name>:<step>`, and each ask is recorded like any other decision. Hooks share the decider with the three subsystems above, so `decider.timeout_ms` bounds one hook ask as well.
