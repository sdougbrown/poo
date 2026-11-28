---
title: Models File Reference
description: Complete schema for models.yaml, pi models.json import rules, builtin-models.yaml, and POO_BUILTIN_MODELS.
---

# Models File Reference

A models file declares endpoints and the models each endpoint serves. It populates the catalog's `models_file` layer, below `settings.yaml` and above `curated` and `discovery` (see [Models & Catalog](/configuration/models-and-catalog)).

## Discovery and Format

poo locates a models file in this order:

1. The path set by `models_file` in `settings.yaml`. Relative paths resolve against the **settings file's directory**; `~/` resolves under the home directory.
2. Otherwise, if `settings.yaml` was loaded from disk, poo looks for `models.yaml` or `models.json` in the same directory.
   - If **both** `models.yaml` and `models.json` exist beside `settings.yaml`, loading fails with an error so neither file is silently ignored.
   - If a models file exists (or is explicitly named) and fails to read or validate, loading fails.

Both YAML and JSON are accepted through the same parser (leading UTF-8 BOMs are stripped). A file with a top-level `providers` key is imported as a [pi `models.json`](#pi-models-json-import); otherwise it is parsed as poo's native `models.yaml` format.

## Native `models.yaml` Schema

poo decodes `models.yaml` **strictly**: any unknown key is an error. The file must declare at least one endpoint, and each endpoint must declare at least one model.

Every model declared under endpoint `<ep>` and key `<id>` is registered in the catalog under the name **`<ep>/<id>`**.

```yaml
endpoints:
  home:
    endpoint: http://localhost:4000/v1
    api: chat_completions
    api_key_env: HOME_API_KEY
    session_header: x-session-id
    skip_probe: false
    instructions_per_question: false
    models:
      qwen3:
        model_id: qwen3-32b
        ops: [chat, systemone]
        inputs: [text, image]
        context_limit: 131072
        input_limit: 65536
        max_output: 16384
        tools_mode: auto
        reasoning:
          mode: toggle
          param: chat_template_kwargs.enable_thinking
        reasoning_levels: [off, high]
        prompt_variant: qwen
        quirks: [think_tags]
        sampling:
          temperature: 0.7
          top_p: 0.9
          top_k: 40
          min_p: 0.05
```

### Top-Level Keys

| Key | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `endpoints` | `map<string, Endpoint>` | Yes | Map of endpoint names to endpoint definitions. Must be non-empty. |

### Endpoint Keys (`endpoints.<name>`)

| Key | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `endpoint` | `string` | Yes | Base URL of the endpoint (e.g. `http://localhost:4000/v1`). |
| `api` | `string` | No | Wire API spoken by the endpoint: `chat_completions` (default when omitted), `responses`, or `anthropic_messages`. Inherited by models under this endpoint. |
| `api_key_env` | `string` | No | Environment variable holding the API key. Also matched against stored key providers in the auth file (see [Providers & Auth](/configuration/providers-and-auth)). |
| `api_key` | `string` | **Refused** | **Always rejected.** See [Literal `api_key` Refusal](#literal-api-key-refusal). |
| `session_header` | `string` | No | HTTP header name that carries the session ID on requests (for gateways that route a conversation's requests together). |
| `skip_probe` | `bool` | No | When `true`, skips discovery probing for this endpoint. Use for endpoints without a discoverable model list. |
| `instructions_per_question` | `bool` | No | When `true`, folds decider instructions into each question instead of sending a request-level `instructions` field. |
| `models` | `map<string, Model>` | Yes | Map of model keys to model definitions. Must be non-empty. |

### Model Keys (`endpoints.<name>.models.<id>`)

| Key | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `model_id` | `string` | No | Wire model identifier sent to the endpoint. Defaults to the map key `<id>`. |
| `ops` | `string[]` | No | Supported operations: `chat`, `systemone`, `decisions`, `compact`. Defaults to `[chat]` if no layer sets it. |
| `inputs` | `string[]` | No | Supported input modalities: `text`, `image`, `audio`. Unknown modality names fail load. |
| `context_limit` | `int` | No | Total context window in tokens. |
| `input_limit` | `int` | No | Maximum prompt tokens accepted by the endpoint when smaller than `context_limit`. |
| `max_output` | `int` | No | Maximum output tokens per response. |
| `reasoning` | `object` | No | Reasoning control object with `mode` (`none`, `toggle`, `effort`, `budget`) and `param` (dot-path string). See [Reasoning & Sampling](/configuration/reasoning-and-sampling). |
| `reasoning_levels` | `string[]` | No | Accepted reasoning levels, lowest first (e.g. `[off, low, medium, high, xhigh]`). |
| `tools_mode` | `string` | No | Tool-calling mode: `native`, `text`, `auto`, or `none`. |
| `prompt_variant` | `string` | No | Prompt section variant key (used by model-family prompt guidance). |
| `quirks` | `string[]` | No | Ordered list of transport quirks (e.g. `think_tags`, `audio_url`). See [Transport Quirks](/configuration/reasoning-and-sampling#transport-quirks). |
| `sampling` | `object` | No | Model sampling defaults: `temperature`, `top_p`, `top_k`, `min_p` (floats). |

### Literal `api_key` Refusal

A models file is designed to be shared or checked into version control. Writing a literal `api_key` on any endpoint in `models.yaml` fails load with an error. Instead:

- Set `api_key_env: MY_API_KEY` on the endpoint, or
- Store the credential in poo's auth file (`poo login`), or
- If you must write a literal key, put it on the endpoint in `settings.yaml` (which overrides the models file's endpoint).

## pi `models.json` Import

When a models file has a top-level `providers` key (or is a `models.json` written by `pi`), poo translates it into the `models_file` catalog layer under `<provider>/<model.id>`.

- **Forward compatibility**: pi fields that poo does not map are ignored rather than rejected.
- **Required fields**: at least one provider; each provider must have `baseUrl` and a non-empty `models` array; each model must have `id`.

### Field Mapping

| pi field | poo mapping / rule |
| :--- | :--- |
| `providers.<name>.baseUrl` | `endpoint` |
| `providers.<name>.api` | `""` or `openai-completions` → default (`chat_completions`); `anthropic-messages` → `anthropic_messages`; `openai-codex-responses` → `responses` (allowed **only** for provider `openai-codex`). Any other API name is refused. |
| `providers.<name>.apiKey` | `$VAR` → `api_key_env: VAR`. Values starting with `!` (shell commands) are ignored. Literal keys are copied into `settings.Endpoints[<name>]` (never stored in the catalog layer) unless `settings.yaml` already defines that endpoint. If empty or ignored, poo checks the resolved auth file for an `api_key` entry under `<name>`. |
| `models[].id` | `model_id` (and `<provider>/<id>` catalog name) |
| `models[].input` | `inputs` (validated against `text`, `image`, `audio`) |
| `models[].contextWindow` | `context_limit` |
| `models[].maxTokens` | `max_output` |
| `models[].reasoning` + `models[].compat.supportsReasoningEffort` | When both are `true`, sets `reasoning: { mode: effort, param: reasoning_effort }`. |
| `models[].thinkingLevelMap` | When `reasoning` and `supportsReasoningEffort` are both `true`: non-null keys in canonical order (`off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`) become `reasoning_levels`. If `thinkingLevelMap` is omitted, defaults to `[off, low, medium, high]`. |

## `builtin-models.yaml` and `POO_BUILTIN_MODELS`

poo embeds `config/builtin-models.yaml` in the binary using the exact same `models.yaml` schema. It provides definitions for built-in providers whose endpoints do not expose a full model list or require provider metadata (`session_header`, `skip_probe`, `api`, `reasoning_levels`):

- `openai-codex` (Responses API, `skip_probe: true`)
- `ollama-cloud` (`OLLAMA_API_KEY`)
- `opencode`, `opencode-go`, `opencode-anthropic`, `opencode-go-anthropic` (`OPENCODE_API_KEY`, `session_header: x-opencode-session`)
- `typesafe` (`TYPESAFE_API_KEY`, `skip_probe: true`, `instructions_per_question: true`, `ops: [systemone]`)

### Gating and Precedence

- Built-in providers are added only when a credential file exists at the resolved auth path.
- Each built-in endpoint is gated on its own credential. `openai-codex` requires an `openai-codex` entry in the credential file. Endpoints with `api_key_env` require that key via environment variable, auth file, or a literal in `settings.yaml`.
- Your `models.yaml` or `models.json` folds **over** the built-ins by name, so you can override any built-in endpoint or model without losing the others.

### Overriding with `POO_BUILTIN_MODELS`

Set the `POO_BUILTIN_MODELS` environment variable to a YAML or JSON file path (in the same `models.yaml` schema) to replace the embedded `builtin-models.yaml`:

```bash
POO_BUILTIN_MODELS=/path/to/custom-builtins.yaml poo models
```

When `POO_BUILTIN_MODELS` is set, poo always reads and validates that file even without a credential file. A missing or invalid file fails the load immediately.
