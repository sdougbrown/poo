---
title: Providers & Auth
description: Configure credential files, built-in providers (Codex, Ollama Cloud, OpenCode, TypeSafe), custom OpenAI-compatible and Anthropic Messages endpoints, and auth verification.
---

# Providers & Auth

`poo` separates **credentials** (OAuth tokens and API keys) from **endpoints** (URLs, wire APIs, and headers). Built-in providers activate automatically when a credential is available, and custom endpoints can speak OpenAI Chat Completions or Anthropic Messages.

## Credential Files & Resolution

### File Formats

`poo` reads three credential file shapes and preserves the format it reads when writing back:

1. **`poo` versioned format (`auth.yaml`, default)** — YAML with a top-level `version: 1` and a `credentials` map. OAuth entries use the same field names as `pi`:
   ```yaml
   version: 1
   credentials:
     openai-codex:
       type: oauth
       access: ...
       refresh: ...
       expires: 1767225600000
       accountId: ...
   ```
   A JSON file in this same versioned shape is read as the same document and written as YAML on the first write.
2. **Legacy `auth.json` and `pi`'s `auth.json`** — Unversioned JSON keyed directly by provider ID. An existing `auth.json` in `poo`'s config directory is read in place so prior sign-ins are not lost; renaming it to `auth.yaml` moves `poo` with it. When `pi_auth_file` points at `pi`'s `auth.json`, `poo` preserves `pi`'s JSON shape so `pi` can keep reading it.
3. **Codex CLI `auth.json` (read-only)** — Owned and refreshed by the Codex CLI. `poo` reads the access token's `exp` claim and never writes to the file (requires `auth_readonly: true`).

### Path Resolution & Platform Defaults

Configure the credential path in settings with either `auth_file` or `pi_auth_file` (setting both is rejected):

```yaml
# Keep credentials beside your settings file
auth_file: auth.yaml
```

Relative paths resolve against the settings file's directory. When using `auth_file`, pass the same `--settings` path to `poo login`, `poo auth status`, and `poo auth refresh`.

When neither `auth_file` nor `pi_auth_file` is set, `poo` resolves the credential file in this order:

1. **Beside a loaded models file** — If a `models_file` (or adjacent `models.yaml` / `models.json`) is loaded, `poo` looks beside it for `auth.yaml`, `auth.json`, or `pi`'s `auth.json` (beside `models.json`).
2. **Platform config directory**:
   - **macOS**: `~/Library/Application Support/poo/auth.yaml`. If absent, `poo` falls back to an existing `~/.config/poo/auth.yaml` without merging the two. If neither exists, `poo login` creates the native `Application Support` file.
   - **Linux & Windows**: `auth.yaml` in `poo`'s platform config directory (e.g. `~/.config/poo/auth.yaml` on Linux).

### Credential Precedence

For API-key endpoints, `poo` resolves keys in strict order:

1. **Environment variable** named by the endpoint's `api_key_env` (highest precedence).
2. **Credential file** key entry matching that provider's environment variable (for example, an endpoint declaring `api_key_env: OLLAMA_API_KEY` reads the `ollama-cloud` key stored via `poo login ollama-cloud`).
3. **Literal `api_key`** written in settings or a models file (lowest precedence).

### Read-Only Mode (`auth_readonly`)

Set `auth_readonly: true` when sharing a credential file managed by another tool:

```yaml
auth_file: ~/.codex/auth.json
auth_readonly: true
```

In read-only mode, `poo` never writes to the file: `poo login`, `poo auth import`, `poo auth refresh`, and token refreshes during sessions are all refused. If the access token expires, `poo` returns an error directing you to refresh with the owning tool.

## Built-in Providers

Built-in provider definitions ship in `builtin-models.yaml` (override the embedded file with `POO_BUILTIN_MODELS=/path/to/file.yaml`). A built-in provider's models appear in the catalog once a credential for it is in reach. Models declared in `models_file` or settings `models` always win on name collisions. See [Models & Catalog](/configuration/models-and-catalog) for layering details.

| Provider | Sign-in command | Auth method / Env var | Wire API | Probed |
| :--- | :--- | :--- | :--- | :--- |
| `openai-codex` | `poo login codex` | OAuth (`chatgpt.com/backend-api`) | `responses` | No |
| `ollama-cloud` | `poo login ollama-cloud` | Key / `OLLAMA_API_KEY` | `chat_completions` | Yes |
| `opencode` / `opencode-go` | `poo login opencode` | Key / `OPENCODE_API_KEY` | `chat_completions` | Yes |
| `opencode-anthropic` / `opencode-go-anthropic` | `poo login opencode` | Key / `OPENCODE_API_KEY` | `anthropic_messages` | Yes |
| `typesafe` | `poo login typesafe` | Key / `TYPESAFE_API_KEY` | Decisions (`ops: [systemone]`) | No |

::: tip Key input security
Key-based `poo login <provider>` flows prompt interactively (with hidden input) or read a single line from `stdin`. There is no `--key` flag, keeping secrets out of shell history and process lists.
:::

### OpenAI Codex (`openai-codex`)

`openai-codex` uses OAuth against `https://chatgpt.com/backend-api` with `api: responses` and `skip_probe: true`. `poo` pins the OAuth credential strictly to that endpoint and refuses to send it anywhere else. Codex models serve `chat` turns and cannot be assigned to `decider`.

```yaml
roles:
  chat: openai-codex/gpt-6.1-sol
```

**Sign in:**

```sh
poo login codex
```

`poo login codex` (or `poo login openai-codex`) opens your browser and listens on a loopback callback port (default timeout: 5 minutes, adjustable with `--wait <seconds>`). Pass `--force` to replace an existing credential without prompting.

For headless or remote machines, use `--no-browser`:

```sh
poo login codex --no-browser
```

Open the printed URL on a machine with a browser, then paste the full final redirect URL back into the terminal (including its `state` query parameter; a bare authorization code is rejected).

**Background token refresh (`poo auth refresh`):**

`poo auth refresh` checks the same credential file that sessions use and refreshes `openai-codex` when the access token has 5 minutes or less remaining. It prints nothing on success, exits non-zero on failure, and makes no model request.

```sh
poo auth refresh --settings /abs/path/to/settings.yaml --leeway 30m
```

- Use `--leeway <duration>` (e.g. `30m`) to refresh earlier.
- Use `--force` for manual one-off refreshes regardless of expiry.
- For idle or daemonized agents, run `poo auth refresh` from a `systemd` user timer or `launchd` job with an absolute `--settings` path. Schedule the timer more frequently than `--leeway`, and **do not** pass `--force` on a timer. If OpenAI revokes the refresh grant, you must run `poo login codex` again.

**Importing from `pi` (`poo auth import pi`):**

```sh
poo auth import pi [--from /path/to/auth.json] [--force]
```

Copies the `openai-codex` OAuth credential from `--from`, then `pi_auth_file`, then `~/.pi/agent/auth.json` into `poo`'s credential file without modifying `pi`'s file.

::: warning One-way door
OAuth refreshes rotate the refresh token, so only one tool can own a grant's refresh lifecycle. After `poo`'s first refresh, `pi`'s copy becomes stale; if `pi` refreshes first, `poo`'s copy stops working. Run one tool per grant, not both.
:::

**Sharing Codex CLI's credential:**

To reuse a sign-in owned by the Codex CLI without conflicting refreshes, point `auth_file` at `~/.codex/auth.json` and enable `auth_readonly: true`. Run the Codex CLI to refresh when expired.

### Ollama Cloud (`ollama-cloud`)

```sh
poo login ollama-cloud
```

Stores an API key for `https://ollama.com/v1` (also reachable via `OLLAMA_API_KEY`). `poo` probes the endpoint immediately after login so the live model list is ready for your first session, and refreshes it on `poo models` or `/models`. Reference models as `ollama-cloud/<id>`:

```yaml
roles:
  chat: ollama-cloud/glm-5.3
```

### OpenCode (`opencode`, `opencode-go`, and Anthropic Messages siblings)

```sh
poo login opencode
```

Stores a single OpenCode Console key (`OPENCODE_API_KEY`) shared across four built-in endpoints (your workspace plan determines which endpoints bill it):

- **Chat Completions**: `opencode` (`https://opencode.ai/inference/openai/v1`) and `opencode-go` (`https://opencode.ai/inference/go/openai/v1`)
- **Anthropic Messages**: `opencode-anthropic` (`https://opencode.ai/inference/anthropic/v1`) and `opencode-go-anthropic` (`https://opencode.ai/inference/go/anthropic/v1`), configured with `api: anthropic_messages`

```yaml
roles:
  chat: opencode-go/glm-5.3
  # Or use the Anthropic Messages sibling:
  # chat: opencode-go-anthropic/claude-haiku-5-5
```

All four endpoints probe the gateway's live model list and declare `session_header: x-opencode-session`, which causes `poo`'s transports to attach the session ID on every turn, fork, and compaction request. The Anthropic Messages transport handles content blocks, streamed tool call arguments, thinking blocks with preserved signatures, adaptive effort (`output_config.effort`), and token thinking budgets.

### TypeSafe (`typesafe`)

```sh
poo login typesafe
```

Stores a TypeSafe Console key (`TYPESAFE_API_KEY`) for `https://api.typesafe.ai/v1`. TypeSafe serves Jev decision models (`jev-latest`, `jev-preview`) that declare `ops: [systemone]` — they answer `decider` queries and cannot serve `chat` turns. See [Decision Model](/capabilities/decision-model).

```yaml
roles:
  chat: opencode-go/glm-5.3
  decider: typesafe/jev-latest
```

The built-in endpoint sets `instructions_per_question: true` (folding framing into each question because the API rejects request-level `instructions`) and `skip_probe: true` (its model list is not OpenAI-shaped). To add a newer Jev model before a binary release, declare it under `models` on the `typesafe` endpoint with `ops: [systemone]`.

## Custom Endpoints

Declare custom OpenAI-compatible or Anthropic Messages servers under `endpoints` and `models` in settings (or in a [Models File](/reference/models-file)):

```yaml
endpoints:
  local-vllm:
    endpoint: http://localhost:8000/v1
    api_key_env: LOCAL_VLLM_KEY
  anthropic-gateway:
    endpoint: https://gateway.example.com/anthropic/v1
    api_key_env: GATEWAY_API_KEY
    session_header: x-session-id
    skip_probe: true

models:
  local-qwen:
    endpoint: local-vllm
    model_id: Qwen/Qwen3-32B
    api: chat_completions
  gateway-claude:
    endpoint: anthropic-gateway
    model_id: claude-sonnet-4-5
    api: anthropic_messages

roles:
  chat: gateway-claude
```

### Endpoint Fields

| Field | Type | Description |
| :--- | :--- | :--- |
| `endpoint` | `string` | Base URL for the provider API (required). |
| `api_key_env` | `string` | Environment variable holding the API key (checked before credential files and `api_key`). |
| `api_key` | `string` | Literal API key fallback when `api_key_env` is unset. |
| `session_header` | `string` | HTTP header name carrying the session ID on turns, forks, and compactions. |
| `skip_probe` | `bool` | Skip `GET /models` discovery when the endpoint does not expose an OpenAI-compatible model list. |
| `instructions_per_question` | `bool` | Fold decider framing into each question instead of sending a request-level `instructions` field. |

### Wire APIs (`api`)

Each model (or endpoint in a models file) selects its wire transport with `api`:

- `chat_completions` (default when omitted) — OpenAI Chat Completions API.
- `anthropic_messages` — Anthropic Messages API (content blocks, streaming tools, thinking signatures, effort/budget controls).
- `responses` — OpenAI Responses API (used by the built-in `openai-codex` transport).

See [Wire API Mapping](/reference/wire-api-mapping) for exact request/response field mappings across transports.

## Verification Workflow

Run these two read-only checks before starting a session. Neither prints secrets nor trades tokens.

### Check Stored Credentials (`poo auth status`)

```sh
poo auth status [--settings /path/to/settings.yaml] [--json]
```

Reports the resolved file path, format (`poo`, `pi`, or Codex CLI), provider, OAuth client ID (if recorded), account ID, expiry timestamp, and state:

- `live` — Ready to use as-is (always `live` for API keys).
- `refresh needed` — OAuth access token is expired or within leeway; `poo` will refresh it on use or on `poo auth refresh`.
- `no readable expiry` — OAuth token has no readable `exp` claim.
- `expired (read-only: refresh with the tool that owns the file)` — Expired token in an `auth_readonly: true` file.

Pass `--json` for machine-readable output (`file`, `format`, `provider`, `client_id`, `account_id`, `expires`, `state`).

### Verify Role Resolution (`poo roles`)

```sh
poo roles [--settings /path/to/settings.yaml]
```

Builds the catalog the same way a session does and prints each role (`chat`, `compaction`, `decider`) with its resolved `MODEL`, `PROVIDER`, and `CREDENTIAL` source (`env:<VAR>`, `static`, or `none`). Exits non-zero if any assigned role fails to resolve, listing candidate models that would satisfy it.

## Related Pages

- [Settings Discovery](/configuration/settings-discovery)
- [Models & Catalog](/configuration/models-and-catalog)
- [Wire API Mapping](/reference/wire-api-mapping)
- [Decision Model](/capabilities/decision-model)
- [CLI Reference](/reference/cli)
