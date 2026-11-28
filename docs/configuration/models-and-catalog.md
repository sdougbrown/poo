---
title: Models & Catalog
description: Configure endpoints, models, roles, context limits, and understand how poo layers and inspects its model catalog.
---

# Models & Catalog

poo resolves every model through a single catalog that merges built-in family rules, live endpoint discovery, a models file, and `settings.yaml`. Sessions and transports read the merged `ModelInfo` record from this catalog.

## Endpoints, Models, and Roles

Configuration splits model setup into three blocks in `settings.yaml` (see [Settings Reference](/reference/settings)):

- **`endpoints`** — where a server lives and how to reach it (`endpoint` base URL, `api_key_env` or `api_key`, optional `session_header`, `skip_probe`, `instructions_per_question`).
- **`models`** — named model declarations that pair an endpoint name with a `model_id` and any model-specific overrides (`api`, `context_limit`, `input_limit`, `max_output`, `inputs`, `ops`, `tools_mode`, `reasoning`, `reasoning_levels`, `quirks`, `prompt_variant`).
- **`roles`** — assigns a catalog model name to each job a session performs.

```yaml
version: 1

endpoints:
  local:
    endpoint: http://localhost:4000/v1
    api_key_env: LOCAL_API_KEY

models:
  coder:
    endpoint: local
    model_id: qwen3-coder-30b
    context_limit: 131072
    max_output: 16384
    tools_mode: native

roles:
  chat: coder
  compaction: local/qwen3-8b
```

A role can reference a model declared under `models` (`coder`) or a model provided by a models file or live discovery using its `<endpoint>/<id>` name (`local/qwen3-8b`).

### Roles and Required Operations

| Role | Purpose | Required `ops` | Required? |
| :--- | :--- | :--- | :--- |
| `chat` | Primary conversation and tool-calling turns | `chat` | Yes (session fails to build if unresolved) |
| `decider` | Fast yes/no permission and state decisions (see [Decision Model](/capabilities/decision-model)) | `systemone` or `decisions` | Optional (warning if unresolved) |
| `compaction` | Summarizing or reconstructing history during compaction (see [Context Compaction](/capabilities/compaction)) | `chat` | Optional (warning if unresolved) |

If no layer sets `ops` on a model, the catalog defaults it to `[chat]`. Assigning a model that does not declare the operation a role needs (for example, assigning a `systemone`-only decider model to `chat`) is a configuration error.

## Catalog Layering

Each model in the catalog is assembled from four layers merged in order. Later layers win **field by field** — setting one field in a higher layer leaves every other field from lower layers intact:

1. **`curated`** — family rules embedded in the binary (`gemma`, `qwen`, `minimax`), matched by `model_id` prefix. They supply family defaults such as `context_limit`, `ops`, `tools.mode`, `reasoning`, `temperature`, and `prompt_cache`.
2. **`discovery`** — what the endpoint reports when probed (OpenAI `/models`, llama.cpp server props, or Ollama model show). Results are cached at `<StateDir>/poo/discovery.json` with a 10-minute freshness TTL (`DefaultDiscoveryTTL`). Session builds read the local cache without blocking on network probes; `poo models` and `poo models --explain` reprobe live. Set `skip_probe: true` on endpoints that do not expose a model list.
3. **`models_file`** — endpoints and models declared in a models file, assembled in two steps:
   - First, the embedded `builtin-models.yaml` (or the file named by `POO_BUILTIN_MODELS`) when a credential file exists, gated to endpoints whose credentials are present.
   - Second, your `models.yaml` or imported pi `models.json` (set via `models_file` or auto-discovered beside `settings.yaml`), folded over the built-ins by name.
4. **`settings`** — `endpoints`, `models`, and top-level `sampling` in `settings.yaml`.

Any field still unset after all four layers takes a catalog default (`default` layer): `api` defaults to the endpoint's `api` or `chat_completions`, and `ops` defaults to `[chat]`.

For the full `models.yaml` schema, pi `models.json` import rules, and `POO_BUILTIN_MODELS` behavior, see [Models File Reference](/reference/models-file). For credential resolution and built-in providers, see [Providers & Auth](/configuration/providers-and-auth).

## Context, Input, and Output Limits

poo distinguishes three token limits on a model:

| Field | Meaning | Effect |
| :--- | :--- | :--- |
| `context_limit` | Total context window shared by prompt and response | Sizes the output room (`max_tokens`) and bounds prompt size when `input_limit` is unset |
| `input_limit` | Largest prompt the endpoint accepts (when smaller than `context_limit`) | Caps the prompt budget used by compaction and model-switch checks (`min(context_limit, input_limit)`), while leaving `max_tokens` sized from `context_limit` |
| `max_output` | Maximum tokens the model writes in one response | Sets the request's `max_tokens` ceiling |

poo always computes and sends `max_tokens` rather than relying on an endpoint's hidden default. When `max_output` is unset or `0`, poo derives a default ceiling from `context_limit`:

| `context_limit` | Default `max_output` |
| :--- | :--- |
| Unknown (`<= 0`) | `16384` |
| `< 65536` | `8192` |
| `65536` – `131071` | `16384` |
| `>= 131072` | `32768` |

Before sending a request, poo clamps `max_tokens` to the room remaining in `context_limit` after the estimated prompt and output reserve (floored at `1`).

## Inspecting the Catalog

### `poo models` and `poo models --explain`

Run `poo models` to reprobe configured endpoints and print every available model:

```bash
poo models
poo models --json
```

To see every resolved field for one model and **which layer set it**, pass `--explain`:

```bash
poo models --explain coder
poo models --explain openai-codex/gpt-6.1-sol
```

Output shows `FIELD`, `VALUE`, and `LAYER` (`curated`, `discovery`, `models_file`, `settings`, or `default`), including nested fields such as `reasoning.mode`, `reasoning.param`, `reasoning.levels`, `tools.mode`, and `sampling.temperature`. Use this whenever a model behaves unexpectedly to see whether a value came from family defaults, a probe, a models file, or `settings.yaml`.

### `poo roles`

Run `poo roles` to verify role resolution before starting a session:

```bash
poo roles
```

It prints a table of `ROLE`, `MODEL`, `PROVIDER`, and `CREDENTIAL` (`env:<VAR>`, `static`, or `none`) for `chat`, `decider`, and `compaction`:

- Prints no secrets and triggers no token refresh.
- Uses the same catalog resolution as a session build.
- Exits non-zero if any assigned role cannot be resolved or does not serve the required operation. In that case, it lists the catalog models that could serve that role.
