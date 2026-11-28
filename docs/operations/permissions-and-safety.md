---
title: Permissions & Safety
description: Configure permission rules, asker chains, doom-loop detection, protected paths, working directory boundaries, and child environment filtering in poo.
---

# Permissions & Safety

Every tool call passes through the permission policy (`poo/perm`) before it executes. The policy runs as the first before-tool hook and resolves each call to either **allow** or **deny**. Calls configured to **ask** block on the asker chain until an asker answers or the chain falls back.

## Evaluation Order

The policy evaluates each tool call in a fixed order:

1. **Doom-loop detection** — checks exact and near repeat counts first, including for malformed calls that failed argument preparation.
2. **Policy rules** — checks `permission.rules` in order; the first matching rule wins.
3. **Outside-working-dir asks** — if the tool sets `ask_outside_working_dir: true` and the target path resolves outside the working directory, routes the call to `ask` (bypassing both read-only auto-allow and `permission.unmatched`).
4. **Read-only auto-allow** — any remaining unmatched `read_only` tool call is allowed.
5. **Unmatched action** — any remaining unmatched `side_effecting` tool call takes `permission.unmatched`.

## Permission Rules

Rules live under `permission.rules` and are evaluated top to bottom. Each rule must specify `action` (`allow`, `ask`, or `deny`) and match either a `tool` name or a `class` (`read_only` or `side_effecting`), never both.

```yaml
permission:
  unmatched: ask
  fallback: deny
  rules:
    - tool: bash
      command_contains: ["rm -rf", "sudo", "curl | sh"]
      action: ask
    - tool: bash
      command_prefix: "git status"
      action: allow
    - tool: fetch
      command_prefix: "docs.example.com"
      action: allow
    - tool: fetch
      command_prefix: "https://api.example.com/v1/"
      action: allow
    - class: read_only
      action: allow
```

### Rule Fields

| Field | Applies to | Description |
|---|---|---|
| `tool` | Exact tool name | Matches a single tool (e.g., `bash`, `edit`, `fetch`). Mutually exclusive with `class`. |
| `class` | Tool class | Matches `read_only` or `side_effecting` tools. Mutually exclusive with `tool`. |
| `command_prefix` | Named `tool` only | Matches the prefix of the tool's primary argument (`command` for `bash`, `url` for `fetch`). Mutually exclusive with `command_contains`. |
| `command_contains` | Named `tool` only | List of non-empty substrings; matches if any substring appears anywhere in the tool's primary argument. Mutually exclusive with `command_prefix`. |
| `action` | Required | `allow`, `ask`, or `deny`. |

### Prefix Matching and URL Hostname Rules

For non-URL arguments such as `bash` commands, `command_prefix` trims leading whitespace and performs a plain lexical prefix check. Because compound shell commands match their leading segment (`git status && rm -rf x` matches `git status`), `command_prefix` is a heuristic rather than a security boundary.

For tools whose primary argument is an absolute `http` or `https` URL (such as `fetch`), `command_prefix` uses canonical URL matching:

- **With a scheme** (`https://api.example.com/v1/`): matches only that scheme, host (or its subdomains), and path prefix.
- **Without a scheme** (`docs.example.com`): acts as a **hostname rule**. The call's own scheme decides, so a single rule covers both `http` and `https` URLs for that host and its subdomains.
- Query strings and fragments are ignored during comparison. Adjacent hosts (such as `evil-docs.example.com`) do not match.

### Substring Matching (`command_contains`)

`command_contains` matches when any listed substring appears anywhere in the tool's primary argument after trimming leading whitespace.

::: warning Tripwire, not a safety boundary
`command_contains` is plain text matching. A command spelled around a substring — using extra spaces, split flags, quoting, or shell variables — will not match. Use `command_contains` as a tripwire to prompt on risky patterns, and pair it with a [bubblewrap sandbox](/operations/sandbox) when hard isolation is required.
:::

## Unmatched Calls and Fallback

| Setting | CLI Flag | Default | Valid Values | Purpose |
|---|---|---|---|---|
| `permission.unmatched` | `--permission-unmatched` | `ask` | `allow`, `ask`, `deny` | Action for a `side_effecting` call that no rule matches. |
| `permission.fallback` | `--permission-fallback` | `deny` | `allow`, `deny` | Decision when the asker chain is exhausted, times out, or has no available asker. |

A policy can produce an `ask` via `unmatched: ask`, an `ask` rule, `ask_outside_working_dir`, or an explicit doom-loop `ask`. If no configured asker can ever answer in the current mode, poo emits a load-time warning naming the `fallback` decision.

## Asker Chain

When a call resolves to `ask`, the policy queries the askers listed in `permission.askers` in order. Each asker can return `allow`, `deny`, or `defer` (errors, panics, timeouts, and unavailable askers all defer to the next link). The entire chain is bounded by `permission.chain_timeout_s` (default `120` seconds).

Setting `permission.askers: []` explicitly configures an empty chain, sending every ask straight to `permission.fallback`. Omitting `askers` uses the binary's default chain.

```yaml
permission:
  askers: [decide, wire]
  chain_timeout_s: 120
  fallback: deny
  decide:
    allow: 0.85
    deny: 0.99
    timeout_ms: 1500
```

### Built-in Askers

- **`decide`** — queries the configured decision model (`roles.decide`) with recent conversation turns, the working directory, and the tool call arguments. If the model's `yes` probability meets `permission.decide.allow` (default `0.85`), the call is allowed; if `no` meets `permission.decide.deny` (default `0.99`), it is denied. Any lower confidence, error, or timeout (`permission.decide.timeout_ms`, default `1500` ms) defers to the next asker. Skipped automatically if no decision model is configured.
- **`wire`** — sends a `permission/ask` request to a connected `poo serve` client (such as `poo repl`) that declared the `permission_answers` capability. Default per-asker budget is `60` seconds (capped by the remaining chain timeout). Defers if no capable client is connected.

## Doom-Loop Detection

The core tracks consecutive repeats of tool calls and hands the counts to the permission policy before rules are checked:

```yaml
permission:
  doom_loop:
    threshold: 3
    near_threshold: 5
    action: ask
```

| Field | Default | Description |
|---|---|---|
| `threshold` | `3` | Exact repeat count (same tool and arguments) that triggers the doom-loop action. |
| `near_threshold` | `5` | Near repeat count (same call shape) that triggers the doom-loop action. |
| `action` | `ask` | Action taken when either threshold is reached: `allow`, `ask`, or `deny`. |

When `action` is `allow` (or an asker allows the call), the repeat counter resets. When denied, the model receives a refusal message stating how many times the call was repeated so it can change approach.

## Protected Paths

`tools.protected_paths` lists path globs that file tools refuse to read, search, edit, or write:

```yaml
tools:
  protected_paths:
    - "**/.env"
    - "**/.env.*"
    - "~/.ssh/**"
```

Protected paths apply regardless of `restrict_to_working_dir` or `ask_outside_working_dir`. Under the `local` bash executor, `tools.protected_paths` only guards the built-in file tools — `bash` commands can still read any path the host process can. Under the `bwrap` executor, protected paths inside bound directories are masked with empty mounts. See [Sandboxing (bwrap)](/operations/sandbox) for details.

## Working Directory Boundaries

By default, file tools (`read`, `grep`, `edit`, `write`) can access paths outside the session's working directory. You can confine them or require approval per tool:

### Restrict to Working Directory

```yaml
tools:
  read:  { restrict_to_working_dir: true }
  grep:  { restrict_to_working_dir: true }
  edit:  { restrict_to_working_dir: true }
  write: { restrict_to_working_dir: true }
```

`restrict_to_working_dir: true` rejects paths outside the working directory before the permission policy runs. Restricted `read` is still permitted to read the session's configured skill directories.

### Ask Outside Working Directory

```yaml
tools:
  read:  { ask_outside_working_dir: true }
  grep:  { ask_outside_working_dir: true }
  edit:  { ask_outside_working_dir: true }
  write: { ask_outside_working_dir: true }
```

`ask_outside_working_dir: true` keeps outside paths reachable, but routes calls whose resolved path lies outside the working directory through the asker chain. This runs after `permission.rules` are evaluated and overrides both `permission.unmatched` and the `read_only` auto-allow.

If both `restrict_to_working_dir` and `ask_outside_working_dir` are enabled on the same tool, the restriction runs first. Only paths the restriction still permits outside the working directory (such as skill directories on `read`) reach the ask.

::: tip File tool limits do not confine `bash`
Neither `restrict_to_working_dir` nor `ask_outside_working_dir` restricts `bash` commands. For process and filesystem isolation, run commands through the [bubblewrap sandbox](/operations/sandbox).
:::

## Child Environment Filtering

`tools.env` controls which environment variables child processes (such as `bash` commands) inherit from the session:

```yaml
tools:
  env:
    scrub:
      - OPENAI_API_KEY
      - ANTHROPIC_API_KEY
      - AWS_SECRET_ACCESS_KEY
```

- **`tools.env.scrub`** — removes the listed variable names from the child environment while passing through everything else.
- **`tools.env.passthrough`** — restricts the child environment to *only* the listed variable names, dropping all others.

Separately, a launcher can set `POO_CREDENTIAL_ENV` (colon-separated variable names on Unix) to keep credential variables available to manifests via `env:NAME` bindings while withholding them from `bash` commands. See [Sandboxing (bwrap)](/operations/sandbox) for launcher credential details.
