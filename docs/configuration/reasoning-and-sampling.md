---
title: Reasoning & Sampling
description: Configure reasoning controls, effort levels, token budgets, per-model sampling, transport quirks, and tool-calling modes.
---

# Reasoning & Sampling

poo separates **how a model controls reasoning** from **what reasoning level a session requests**. You declare the control mechanism on the model definition, and set the requested level in `reasoning.chat`, CLI flags, or the REPL.

## Setting Reasoning Effort on a Custom Model

A common mistake with custom models is setting `reasoning.chat: medium` without telling poo how the model accepts reasoning on the wire. Without a `reasoning` control on the model definition, poo treats the model as `mode: none` and refuses the setting with a configuration error.

Setting reasoning effort on a custom model takes **two parts**:

1. **Declare the model's reasoning control** on the model entry in `settings.yaml` (or [models.yaml](/reference/models-file)).
2. **Select the active level** in `settings.yaml`, via `--reasoning-effort`, or interactively in the REPL.

```yaml
# settings.yaml
version: 1

endpoints:
  myserver:
    endpoint: https://api.example.com/v1
    api_key_env: EXAMPLE_API_KEY

models:
  custom-reasoner:
    endpoint: myserver
    model_id: my-reasoning-model
    context_limit: 131072
    # Part 1: declare how this model accepts reasoning
    reasoning:
      mode: effort
      param: reasoning_effort
    # Optional: restrict accepted levels (lowest first)
    reasoning_levels: [off, low, medium, high]

roles:
  chat: custom-reasoner

# Part 2: select the active reasoning level
reasoning:
  chat: medium
```

You can also set Part 2 per run or interactively:

- **CLI (`poo run`)**: `--reasoning-effort medium`. When you pass a named effort level (`minimal`, `low`, `medium`, `high`, `xhigh`) on `--reasoning-effort`, `poo run` forces `reasoning: { mode: effort, param: reasoning_effort }` even if the model did not declare one. Passing `default` or `off` preserves the model's own reasoning control.
- **REPL**: press `Shift+Tab` or run `/thinking` to cycle levels, or run `/thinking medium` (or `/thinking <tokens>` for budget models). See [Interactive REPL](/getting-started/repl).

Verify the merged control on any model with `poo models --explain <model>`.

## Reasoning Modes

`reasoning.mode` declares how a model's thinking is controlled on the wire. Four modes exist:

| Mode | Purpose | Default `param` if omitted |
| :--- | :--- | :--- |
| `none` | Model reasoning cannot be controlled (default when unset) | — |
| `toggle` | Boolean on/off switch (common on local servers) | `chat_template_kwargs.enable_thinking` |
| `effort` | Named effort level (`minimal`, `low`, `medium`, `high`, `xhigh`, or model-declared levels) | `reasoning_effort` |
| `budget` | Positive integer token budget | `thinking.budget_tokens` |

### How Settings Map to Request Fields

The table below shows what poo sends for each combination of `reasoning.chat` (or session level) and `reasoning.mode`:

| Setting | `none` | `toggle` | `effort` | `budget` |
| :--- | :--- | :--- | :--- | :--- |
| `default` (or unset) | No field | No field | No field | No field |
| `off` | No field | `<param>: false` | Off word or quietest level (see below) | No field |
| Named level (e.g. `high`) | Error | `<param>: true` | `<param>: "<level>"` | Error |
| Positive integer (e.g. `4096`) | Error | `<param>: true` | Error | `<param>: 4096` |

- `default` always omits the reasoning field so the endpoint's own default applies.
- `off` is accepted by every model: `toggle` sends `false`, `effort` sends the model's off word (or quietest level), and `none`/`budget` omit the field.

### Common `param` Spellings

`reasoning.param` supports dot-separated paths, which poo encodes as nested JSON objects in the request body:

| Backend / API | `mode` | `param` | Wire JSON sent |
| :--- | :--- | :--- | :--- |
| OpenAI-compatible Chat Completions | `effort` | `reasoning_effort` | `{"reasoning_effort": "medium"}` |
| OpenAI Responses / Codex (`api: responses`) | `effort` | `reasoning_effort` | `{"reasoning": {"effort": "medium", "summary": "auto"}}` |
| Local servers (llama.cpp, vLLM, SGLang) | `toggle` | `chat_template_kwargs.enable_thinking` | `{"chat_template_kwargs": {"enable_thinking": true}}` |
| Anthropic Messages (adaptive effort) | `effort` | `output_config.effort` | `{"output_config": {"effort": "high"}, "thinking": {"type": "adaptive"}}` |
| Anthropic Messages (token budget) | `budget` | `thinking.budget_tokens` | `{"thinking": {"type": "enabled", "budget_tokens": 4096}}` |

Notes on transport behavior:
- **Anthropic Messages (`api: anthropic_messages`)**: when `param` starts with `output_config.`, the transport automatically sets `thinking.type: "adaptive"`. When `param` starts with `thinking.`, it automatically sets `thinking.type: "enabled"`.
- **Codex / Responses (`api: responses`)**: only accepts `mode: effort` with `param: reasoning_effort`. Toggle or budget controls on a Responses model return a configuration error.

## `reasoning_levels` and Off-Word Handling

`reasoning_levels` lists the exact levels a model accepts, ordered **lowest first**:

```yaml
reasoning:
  mode: effort
  param: reasoning_effort
reasoning_levels: [none, low, medium, high, xhigh]
```

When `reasoning_levels` is set:

- **Validation**: poo rejects levels outside the list (except `default` and `off`) instead of sending a value the server would ignore or reject.
- **Verbatim spelling**: declared levels are sent as spelled (including custom names such as `max` or `none`).
- **REPL cycling**: `/thinking` and `Shift+Tab` cycle through the model's declared levels.
- **`off` resolution on `effort` models**:
  1. If `reasoning_levels` contains `"off"` or `"none"`, poo sends that word verbatim (so an endpoint that spells off as `none` receives `"none"`).
  2. If `reasoning_levels` is non-empty but contains neither `"off"` nor `"none"`, poo sends the first (quietest) declared level (e.g. `"low"`).
  3. If `reasoning_levels` is empty, poo sends `"none"`.
  4. On the Codex Responses transport, `off` is always translated to `reasoning.effort: "none"`.

When switching models mid-session in the REPL, if the current reasoning level is not accepted by the new model, poo resets it to `off`.

## Per-Model Sampling

poo supports four sampling parameters: `temperature`, `top_p`, `top_k`, and `min_p`. Unset parameters are omitted from requests so the endpoint uses its own defaults.

Sampling is layered in two places:

1. **Model defaults** in [models.yaml](/reference/models-file) under `endpoints.<ep>.models.<id>.sampling`.
2. **Per-model overrides** in `settings.yaml` under the top-level `sampling` map, keyed by catalog model name:

```yaml
# settings.yaml
sampling:
  local/qwen3-coder:
    temperature: 0.2
    top_p: 0.9
    top_k: 40
    min_p: 0.05
```

Overrides merge field by field over the model's defaults.

- If a model's catalog temperature mode is `fixed` or `unsupported`, poo drops `temperature` even when an override sets it.
- The Anthropic Messages transport sends `temperature`, `top_p`, and `top_k` (it does not send `min_p`).

## Transport Quirks

`quirks` is an ordered list of named request-shaping and response-repair adaptations applied by the OpenAI-compatible Chat Completions transport. Set `quirks` on a model in `settings.yaml` or `models.yaml`:

```yaml
models:
  local-audio:
    endpoint: sglang
    model_id: qwen-omni
    inputs: [text, audio]
    quirks: [think_tags, audio_url]
```

Shipped quirks:

| Quirk | Phase | Behavior |
| :--- | :--- | :--- |
| `think_tags` | Response | Extracts `<think>...</think>` blocks from streamed text into structured thinking blocks |
| `chat_template_thinking` | Request | Sets `chat_template_kwargs.enable_thinking: true` unless already claimed by the model's reasoning control |
| `simplify_tool_schemas` | Request | Strips tool parameter JSON schemas down to `type`, `properties`, and `required` for servers with weak schema parsers |
| `degenerate_stream` | Response | Detects and logs repetitive/degenerate output loops without interrupting the stream |
| `text_tool_calls` | Response | Enables the text tool-call parser even when `tools_mode` is not `text` or `auto` |
| `audio_url` | Request | Rewrites `input_audio` content parts to `audio_url` data URLs (needed by servers such as SGLang that mirror `image_url`) |

Rules:
- Quirks run in the order listed.
- A quirk never overrides a request parameter already set by the model's `reasoning` or `sampling` controls.
- Unknown quirk names are skipped with a warning rather than failing the request.

## Tool-Calling Modes (`tools_mode`)

`tools_mode` controls how poo advertises tools and parses tool calls for a model:

| Value | Behavior |
| :--- | :--- |
| `native` | Sends tool definitions on the wire and reads structured wire tool calls |
| `text` | Describes tools in the system prompt and parses tool calls from response text |
| `auto` | Sends tool definitions on the wire and falls back to parsing response text when the wire returns no tool calls |
| `none` | Sends no tools |

When `tools_mode` is `text` or `auto`, the OpenAI Chat Completions text parser supports `hermes`, `qwen_xml`, `mistral`, `json`, and `auto` formats (set by the curated catalog's `tools.text_format`, defaulting to `auto`). On the Anthropic Messages transport, `native`, `auto`, and unset all send native wire tools.
