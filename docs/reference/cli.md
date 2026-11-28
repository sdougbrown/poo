---
title: CLI Reference
description: Complete reference for the poo CLI front door, shared flags, and every subcommand.
---

# CLI Reference

Every command, flag, and help text in `poo` comes from a single declaration in `cmd/poo/app.go`. Run `poo <command> --help` or `poo help <command>` for the exact usage of any command, or `poo --version` for the short build version.

## Front Door

A bare `poo` launches the front-door interface linked into the binary — `repl` by default, or whichever linked interface the resolved settings file names under `ui`. If no settings file exists yet, a bare `poo` on a terminal runs the interactive welcome flow; non-interactively, it exits `1`.

Three top-level shortcuts dispatch directly without naming a subcommand:

| Invocation | Equivalent | Behavior |
|---|---|---|
| `poo` | `poo repl` (or `ui` setting) | Launches the configured front-door UI with the discovered settings file. |
| `poo -p <prompt> [flags]` | `poo run [flags] -- <prompt>` | Runs a one-shot session. `--print` is an alias for `-p`. Accepts `run` and shared flags before or after the prompt; `--` ends flag parsing. |
| `poo -c [flags]` | `poo repl --continue [flags]` | Reopens the most recent session in the working directory. `--continue` is the long form. |
| `poo --resume <id> [flags]` | `poo repl --resume <id> [flags]` | Reopens the session identified by `<id>`. Mutually exclusive with `-c`/`--continue`. |

```bash
# One-shot prompt with flags after the prompt
poo -p "Summarize the git log" --max-turns 4 --ephemeral

# Reopen the latest session in this directory
poo -c

# Reopen a specific session by ID
poo --resume 0195c8a2-7b1e-7000-8000-000000000001
```

## Shared Flags

Every subcommand accepts these common flags. Precedence is profile, then settings file, then CLI flags (last writer wins), except repeated manifest path flags append to the settings' paths. See [Settings Discovery](/configuration/settings-discovery) for how `--settings` is resolved when omitted.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--settings` | string | discovered | Path to the settings file (JSON, TOML, or YAML). |
| `--system-prompt` | string | — | File holding the system prompt's base section. |
| `--append-system-prompt` | string | — | Text appended to the system prompt, or `@file` to read from a file. |
| `--permission-unmatched` | string | profile/settings | Action for side-effecting tool calls no rule covers: `allow`, `ask`, or `deny`. |
| `--session-dir` | string | settings | Directory where session files are stored. |
| `--user-prompt-file` | string | platform config `AGENTS.md` | File holding the user's own instructions. |
| `--ephemeral` | bool | `false` | Run without writing a session to disk. |
| `--profile` | string | settings / `code` | Profile to run under (for example `base`, `code`, `search`, or a custom profile). |
| `--feeds-path` | string (repeatable) | — | Additional feed manifest path. |
| `--mcp-path` | string (repeatable) | — | Additional MCP manifest path. |
| `--cli-registry-path` | string (repeatable) | — | Additional CLI registry manifest path. |

## Endpoint Flags

`run`, `repl`, `serve`, and `daemon` accept these endpoint flags to define or override the `chat` model on the command line. When any endpoint flag is supplied, `--endpoint`, `--model-id`, and a positive `--context-limit` are all required.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--endpoint` | string | — | OpenAI-compatible endpoint URL, e.g. `http://localhost:4000/v1`. |
| `--model-id` | string | — | Model identifier sent to the endpoint. |
| `--context-limit` | int | `0` | Model's context window in tokens (required and `> 0` when using endpoint flags). |
| `--api-key-env` | string | — | Name of the environment variable holding the API key. |
| `--tools-mode` | string | `native` | How tool calls are requested: `native`, `text`, or `auto`. |

## Subcommands

### `poo run`

Run a single one-shot session. The final answer is written to stdout and progress/diagnostics to stderr. Flags stop at the first non-flag argument, so everything from the first prompt word onward is prompt text. Use `poo -p` to place flags after the prompt.

Unless the settings file assigns a `chat` role, `--endpoint`, `--model-id`, and `--context-limit` are required. See [Limits & Exit Codes](/operations/limits-and-exit-codes) for exit codes and signal handling.

**Positional arguments:** `prompt...` (required, variadic)

**Flags:** Accepts [Shared Flags](#shared-flags), [Endpoint Flags](#endpoint-flags), and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--permission-fallback` | string | settings | Decision when the permission asker chain is exhausted: `allow` or `deny`. |
| `--reasoning-effort` | string | settings | Reasoning level to send: `default` (no field), `off`, `minimal`, `low`, `medium`, `high`, or `xhigh`. |
| `--max-turns` | int | `0` (unlimited) | Turn limit for the run. |
| `--max-output` | int | model default | Override the model's output-token limit. |
| `--truncation-retries` | int | `1` | Consecutive output-limit truncations to retry more concisely before settling (`0` disables retries). |
| `--context-strategy` | string | `mine` | Compaction strategy: `summary`, `mine`, `pointer`, `native`, `handoff`, or `none`. |
| `--context-keep-recent` | int | strategy default | Verbatim tail retained by block compaction strategies (`summary`, `mine`, `pointer`), in tokens. |
| `--log` | string | — | Write session records to a specific JSONL file instead of the session store. |
| `--working-dir` | string | current directory | Working directory for tools (must exist). |
| `--executor` | string | `local` | Bash tool executor: `local` or `bwrap`. |
| `--project-prompt` | bool | `false` | Include the working directory's `AGENTS.md` (or `CLAUDE.md`) in the system prompt. |
| `--timeout` | duration | — | Wall-clock context deadline for the run, e.g. `5m`. |
| `--no-wait-jobs` | bool | `false` | Exit at the first settle instead of waiting for background jobs; running jobs stop on close. |

```bash
poo run --max-turns 12 --timeout 5m --project-prompt "Find and fix the failing unit test."

poo run --endpoint http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --log session.jsonl \
  "List the Go packages in this repository."
```

---

### `poo repl`

Launch an interactive terminal session with vi keybindings and markdown rendering. Starts `poo serve` as a child process and forwards supplied shared, endpoint, and tool flags to it. Type `/help` inside the REPL for interactive commands. See [Interactive REPL](/getting-started/repl).

**Flags:** Accepts [Shared Flags](#shared-flags), [Endpoint Flags](#endpoint-flags), and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--working-dir` | string | current directory | Working directory for tools. |
| `--executor` | string | `local` | Bash tool executor: `local` or `bwrap`. |
| `-c`, `--continue` | bool | `false` | Continue the most recent session in the working directory. |
| `--resume` | string | — | Resume the session named by this ID. |

```bash
poo repl --working-dir ./my-repo --executor bwrap
poo repl --resume 0195c8a2-7b1e-7000-8000-000000000001
```

---

### `poo serve`

Serve the JSON-RPC 2.0 wire protocol over stdin and stdout (one JSON object per line). See [Wire Protocol](/extending/wire-protocol).

**Flags:** Accepts [Shared Flags](#shared-flags), [Endpoint Flags](#endpoint-flags), and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--working-dir` | string | current directory | Working directory for tools. |
| `--executor` | string | `local` | Bash tool executor: `local` or `bwrap`. |

```bash
poo serve --profile code --working-dir /srv/project
```

---

### `poo daemon`

Host a long-lived, feed-driven session without reading stdin. Defaults the session mode to `durable` when neither `--ephemeral` nor `session.mode` is set, starts all non-optional feeds, and writes lifecycle events one fact per line to stderr. See [Feeds & Daemon](/capabilities/feeds-and-daemon) and [Daemon Exit Codes](/operations/limits-and-exit-codes#poo-daemon-exit-codes).

**Flags:** Accepts [Shared Flags](#shared-flags), [Endpoint Flags](#endpoint-flags), and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--session` | string | generated | Session ID to load, or create if it does not exist. When omitted, a new ID is generated and printed to stderr. |
| `--auto-resume` | bool | `true` | Resume a pending unsettled run automatically on load. |
| `--working-dir` | string | current directory | Working directory for tools. |

```bash
poo daemon --session ops-watcher --working-dir /srv/app
poo daemon --session ops-watcher --auto-resume=false
```

---

### `poo models`

List the effective model catalog — combining settings, `models_file`, curated built-ins, and live endpoint probes — or explain where each field of a single model came from. See [Models & Catalog](/configuration/models-and-catalog).

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json` | bool | `false` | Print the model list as JSON. |
| `--explain` | string | — | Print each field of the named model and the catalog layer that set it. |

```bash
poo models
poo models --json
poo models --explain codex/gpt-5.4
```

---

### `poo roles`

Resolve every configured role (`chat`, `decider`, `compaction`, and any additional assigned roles) against the catalog and print the model, provider, and credential source (`env:<VAR>`, `static`, or `none`). Prints no secrets, trades no credentials, and exits `1` if any assigned role fails to resolve.

**Flags:** Accepts [Shared Flags](#shared-flags) only.

```bash
poo roles
poo roles --settings ./codex.settings.yaml
```

---

### `poo prompt`

Print the exact rendered system prompt a session would send to the model. Does not require a configured chat model. See [Profiles & Prompts](/configuration/profiles-and-prompts).

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--project-prompt` | bool | `false` | Include the working directory's `AGENTS.md` (or `CLAUDE.md`) in the prompt. |
| `--pretty` | bool | `false` | Format each section under a `--- <name> ---` header and summarize the skill catalog. |

```bash
poo prompt --pretty --project-prompt
poo prompt --profile search
```

---

### `poo login`

Sign in to a provider and store the credential in `auth_file` (or poo's default `auth.json`). Keys are read from an interactive terminal prompt or stdin — never via a flag. Existing credentials are not overwritten unless confirmed interactively or forced with `--force`. See [Providers & Auth](/configuration/providers-and-auth).

**Positional arguments:** `[provider]` (optional). Supported providers:

- `codex` — OpenAI Codex (browser OAuth sign-in with loopback callback or URL paste fallback)
- `ollama-cloud` — Ollama Cloud (API key)
- `opencode` — OpenCode (API key)
- `typesafe` — TypeSafe (API key)

A bare `poo login` presents the provider list interactively.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--force` | bool | `false` | Replace an existing credential without asking. |
| `--no-browser` | bool | `false` | Print the sign-in URL instead of opening a browser. |
| `--wait` | int | `300` | Seconds to wait for a browser sign-in callback. |

```bash
poo login codex
poo login codex --no-browser --wait 600
printf '%s' "$OPENCODE_API_KEY" | poo login opencode --force
```

---

### `poo auth status`

Report the resolved credential file, file format, provider, OAuth client, account ID, expiry, and state (`live`, `refresh needed`, `no readable expiry`, or expired under a read-only file). Prints no secrets and does not refresh tokens.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json` | bool | `false` | Print the status report as JSON. |

```bash
poo auth status
poo auth status --json
```

---

### `poo auth refresh`

Refresh the OpenAI Codex credential in the resolved credential file when the access token is within the leeway window of expiring. Exits `0` silently on success (including when no refresh is due) and non-zero on failure.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--leeway` | duration | `5m` | Refresh when this much time or less remains before expiry (must be positive). |
| `--force` | bool | `false` | Refresh immediately even if the access token is still fresh. |

```bash
poo auth refresh
poo auth refresh --leeway 15m
poo auth refresh --force
```

---

### `poo auth import pi`

Copy the OpenAI Codex credential from pi's `auth.json` into poo's credential file. Because OAuth refresh rotates the refresh token, only one tool can keep the imported credential alive after the first refresh. Does not modify pi's file.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--from` | string | `pi_auth_file`, then `~/.pi/agent/auth.json` | Source `auth.json` file to copy from. |
| `--force` | bool | `false` | Replace an existing credential in poo's file without asking. |

```bash
poo auth import pi
poo auth import pi --from ~/.pi/agent/auth.json --force
```

---

### `poo sessions list`

List sessions in the session store with columns `ID`, `CREATED`, `PROFILE`, `RUNS`, `REASON`, `SIZE`, and `LOCKED`. See [Sessions & Durability](/operations/sessions-and-durability).

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json` | bool | `false` | Print the session summaries as JSON. |

```bash
poo sessions list
poo sessions list --json --session-dir /var/lib/poo/sessions
```

---

### `poo sessions prune`

Delete sessions matched by the retention policy (`session.retention` in settings, overridden by flags). Locked sessions are skipped and reported on stderr. Exits `2` if neither settings nor flags specify a retention policy.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--older-than` | int | settings (`max_age_days`) | Delete sessions whose last activity is older than this many days. |
| `--keep` | int | settings (`max_sessions`) | Keep this many most recently active sessions. |
| `--dry-run` | bool | `false` | Print sessions that would be deleted without deleting anything. |

```bash
poo sessions prune --older-than 30 --dry-run
poo sessions prune --keep 20
```

---

### `poo version`

Print the binary's version, VCS revision, commit timestamp, dirty/modified status, and Go toolchain version embedded by the Go build.

**Flags:** Accepts [Shared Flags](#shared-flags) and:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json` | bool | `false` | Print build information as JSON (`version`, `revision`, `time`, `modified`, `go_version`). |

```bash
poo version
poo version --json
poo --version
```
