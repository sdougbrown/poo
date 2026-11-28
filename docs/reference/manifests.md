---
title: Manifests Reference
description: Side-by-side schema reference for feeds, cli_registry, and mcp.json manifests, XDG default directories, and profile interaction.
---

# Manifests Reference

`poo` loads three external manifest types: **feeds**, **CLI registry** (`cli_registry`), and **MCP servers** (`mcp.json`). All three share a discovery model and XDG default directories, while differing in format support, strictness, and how entries activate.

## Comparison

| Aspect | `feeds` | `cli_registry` | `mcp.json` (+ `poo` block) |
|---|---|---|---|
| **Purpose** | Long-lived event sources (`feed:<name>`) | Typed CLI and script tools (`call_cli`) | Stdio MCP servers (`call_mcp`) and MCP resource feeds |
| **Formats** | `.json`, `.yaml`, `.yml`, `.toml` | `.json`, `.yaml`, `.yml`, `.toml` | `.json` only (non-`.json` files skipped in dirs; rejected if named directly) |
| **Top-level envelope** | `version: 1`, `feeds: {...}` | `version: 1`, `tools: {...}`, optional `availability` | `mcpServers: {...}`, optional `poo: {...}` |
| **Unknown fields** | Rejected everywhere | Rejected everywhere | Ignored at top level and inside `mcpServers.<name>`; **rejected** inside `poo` |
| **Duplicate keys** | Rejected | Rejected | Rejected everywhere in file |
| **Duplicate names across files** | Load error (names both files) | Load error (names both files) | Load error for duplicate server names or resource feed names |
| **Default XDG directory** | `$XDG_CONFIG_HOME/poo/feeds/` | `$XDG_CONFIG_HOME/poo/cli-registry/` | `$XDG_CONFIG_HOME/poo/mcp/` |
| **Settings keys** | `feeds.paths`, `feeds.defaults` | `cli_registry.paths`, `cli_registry.defaults` | `mcp.paths`, `mcp.defaults` |
| **Profile override** | `profiles.<name>.feeds_defaults` | `profiles.<name>.cli_registry_defaults` | `profiles.<name>.mcp_defaults` |
| **Env var** | `POO_FEEDS_PATH` | `POO_CLI_REGISTRY_PATH` | `POO_MCP_PATH` |
| **CLI flag (repeatable)** | `--feeds-path` | `--cli-registry-path` | `--mcp-path` |
| **Activation** | Always-on feeds start automatically; `optional: true` starts via `subscribe_feed` | Offered on profiles with a side-effecting tool (filtered by `availability`) | Declared only; started only when named in `profiles.<name>.mcp_servers` |

## Discovery and default XDG directories

For each manifest kind, `poo` discovers files from:

1. The default XDG directory (when defaults are enabled):
   - `$XDG_CONFIG_HOME/poo/feeds/`
   - `$XDG_CONFIG_HOME/poo/cli-registry/`
   - `$XDG_CONFIG_HOME/poo/mcp/`
2. Paths listed in `<kind>.paths` in settings.
3. Paths from `POO_FEEDS_PATH`, `POO_CLI_REGISTRY_PATH`, or `POO_MCP_PATH` (OS path-list separated; scrubbed from child processes).
4. Repeatable CLI flags: `--feeds-path`, `--cli-registry-path`, `--mcp-path`.

Discovery rules:

- A missing default XDG directory is ignored, not an error.
- Paths starting with `~/` resolve under the user's home directory. Relative paths resolve against the binary's working directory (where the process starts), not a per-session working directory.
- Every path may be a file or a directory. Discovered files are deduplicated and sorted deterministically.
- Loading is all-or-nothing: a schema error, duplicate name, or missing explicit path fails the build and names the offending file.

## `*.defaults` and profile interaction

Whether a manifest kind's default XDG directory loads is decided by a two-level precedence chain:

1. **`profiles.<name>.<kind>_defaults`** (wins when set; inherited from the profile's `base` if omitted)
2. **`<kind>.defaults`** in top-level settings
3. **Default: `true`** (when neither is set)

```yaml
feeds:
  defaults: false        # off globally

profiles:
  worker:
    base: code
    feeds_defaults: true # re-enabled for this profile and its children
    cli_registry_defaults: false
    mcp_defaults: false
    mcp_servers: [avenor]
```

Two profile lists interact with manifests differently:

- **`profiles.<name>.mcp_servers`** is a **selection**: loading `mcp.json` only declares servers; a session starts only the servers named in its profile's `mcp_servers` (merged with `base`). An unknown server name emits a warning.
- **`profiles.<name>.feeds`** is a **presence assertion**, not a filter. It declares feeds the profile expects to exist, satisfied by either a feed manifest or an active MCP server's resource feed. Missing names emit a warning; loaded feeds still run regardless of this list.

See [Profiles & Prompts](/configuration/profiles-and-prompts) for full profile inheritance rules.

---

## Feed manifests

Top-level envelope:

```json
{
  "version": 1,
  "feeds": {
    "<name>": {
      "description": "Short summary shown in the jobs prompt.",
      "command": ["my-watcher", "--stream"],
      "env": {
        "API_TOKEN": "env:API_TOKEN",
        "SESSION_ID": "session:id",
        "TARGET_URL": "scope:url",
        "MODE": "watch"
      },
      "restart": true,
      "on_idle": "wake",
      "max_pending": 16,
      "optional": true,
      "scope": {
        "url": {"type": "string", "pattern": "^https://"}
      },
      "filter": {
        "question": "Does this event require action?",
        "min_no_probability": 0.9
      }
    }
  }
}
```

### Feed entry fields

| Field | Type | Required | Description |
|---|---|---|---|
| `command` | `string[]` | Yes | Non-empty argv executed by the session's executor. |
| `description` | `string` | No | Description rendered in the jobs prompt. |
| `env` | `map<string, string>` | No | Added on top of benign env vars. Values: literal, `env:NAME` (scrubbed host env), `session:id`, or `scope:NAME` (must match a declared `scope` field). |
| `restart` | `bool` | No | Default `false`. Restart with exponential backoff (`1s` to `60s`, reset after `60s` uptime). Ignored when suspended by a command filter fault. |
| `on_idle` | `"wake" \| "hold"` | No | Whether inputs wake an idle session. Defaults to `jobs.on_idle` (always-on) or `"hold"` (`optional: true`). |
| `max_pending` | `int` (`>= 0`) | No | Maximum queued undelivered inputs before dropping oldest. `0` or omitted uses default `16`. |
| `optional` | `bool` | No | Default `false`. When `true`, starts only via `subscribe_feed`. |
| `scope` | `map<string, ScopeField>` | No | Allowed only when `optional: true`. Keys match `^[a-z][a-z0-9_]*$`. Each value has `type: "string"` (required) and optional Go regex `pattern`. |
| `filter` | `Filter` | No | Either `{"question": "...", "min_no_probability": 0.5 < p <= 1}` or `{"command": [...]}`. Never both. |

Validation rules:

- `version` must be `1`.
- Feed names must match `^[a-z0-9][a-z0-9_-]*$` and cannot collide with another feed manifest or an MCP resource feed.
- A `scope` block on a non-optional feed fails load.
- A `scope:NAME` reference in `env` must name a declared field in `scope`.

For stdout line format, filter runtime behavior, and `subscribe_feed`, see [Feeds & Daemon](/capabilities/feeds-and-daemon).

---

## CLI Registry manifests

CLI registry manifests are loaded by `github.com/sdougbrown/cli-registry` and exposed through `call_cli` (and `find_tool` when a decision model is active and the catalog exceeds 30 entries).

Top-level envelope:

```json
{
  "version": 1,
  "tools": {
    "<name>": {
      "description": "Wait for a child run to finish.",
      "command": ["avenor", "await", "--until=done", "--format=json"],
      "env": {
        "AVENOR_BIND_HOST": "poo",
        "AVENOR_BIND_SESSION": "session:id"
      },
      "timeout": "36m",
      "inputSchema": {
        "type": "object",
        "properties": {
          "target": {"type": "string", "description": "Child run ID or label."}
        },
        "required": ["target"],
        "additionalProperties": false
      },
      "bindings": [
        {"property": "target", "style": "positional"}
      ],
      "output": {
        "mode": "json",
        "success_exit_codes": [0, 10, 11, 12]
      }
    }
  }
}
```

### Tool entry fields

| Field | Type | Required | Description |
|---|---|---|---|
| `description` | `string` | Yes | Shown in the catalog prompt and `find_tool` results. |
| `command` | `string[]` | Yes | Base argv executed through the session's executor. |
| `env` | `map<string, string>` | No | Added on top of benign env vars. Supports literals, `env:NAME` (scrubbed host env), and `session:id`. |
| `timeout` | `string` | No | Go duration string (e.g. `"36m"`, `"30s"`) overriding default command timeout. |
| `inputSchema` | `object` | No | JSON Schema validating model arguments before spawning a process. |
| `bindings` | `Binding[]` | No | Maps `inputSchema` properties onto argv or stdin. Each entry names `property`, `style` (`positional`, `equals`, `stdin`, `json`, etc.), and optional `flag`. |
| `output` | `object` | No | `mode`: `"raw"`, `"json"`, or `"json-pointer"`. Optional `success_exit_codes` (`int[]`) treats listed exit codes as non-errors. |

An optional top-level `availability` block may declare Umpire v1 rules over load-time conditions to filter which entries a session offers.

### Non-CLI script (`fn`) tools

A `cli_registry` entry can wrap a script with a pure JSON stdin/stdout contract:

```json
{
  "description": "Sum a pair",
  "command": ["python3", "fn_notes.py"],
  "bindings": [{"property": "data", "style": "json"}],
  "output": {"mode": "json"}
}
```

- `style: "json"` serializes a concrete `object` or `array` schema property to the script's stdin (a tool may have at most one `stdin` or `json` binding; `flag` is optional).
- `output.mode: "json"` returns stdout JSON directly as the tool result (nonzero exit is an error with stderr diagnostics).
- Edge case: a JSON `null` result falls back to the exit-code envelope; wrap nullable results in an object such as `{"result": null}`.

See [CLI Registry & Script Tools](/capabilities/cli-registry) for usage details.

---

## MCP manifests (`mcp.json` + `poo` block)

`mcp.json` uses the standard `mcpServers` format shared with Claude Code and other harnesses, plus an optional top-level `poo` block for resource feeds.

```json
{
  "mcpServers": {
    "avenor": {
      "command": "avenor",
      "args": ["mcp", "--bind-host", "poo", "--bind-session", "${session:id}"],
      "env": {
        "AVENOR_LOG": "${AVENOR_LOG:-info}"
      }
    }
  },
  "poo": {
    "servers": {
      "avenor": {
        "feeds": {
          "avenor-inbox": {
            "resource": "avenor://inbox",
            "on_idle": "wake",
            "description": "Messages from Avenor children.",
            "filter": {
              "command": ["jq", "-c", ".text | fromjson | .messages[]"]
            }
          }
        }
      }
    }
  }
}
```

### `mcpServers.<name>` fields

Server names must match `^[A-Za-z0-9_-]+$` and be unique across all loaded MCP manifests.

| Field | Type | Required | Description |
|---|---|---|---|
| `command` | `string` | Yes (for `stdio`) | Executable path or name (template string). |
| `args` | `string[]` | No | Command arguments (template strings). |
| `env` | `map<string, string>` | No | Environment variables (template strings), merged on top of the scrubbed host environment. An explicit `${NAME}` can re-inject a scrubbed variable. |
| `type` | `string` | No | Must be absent or `"stdio"` to launch. `"http"`, `"sse"`, and `"streamable-http"` are skipped with a warning; any other value fails load. |
| `url` | `string` | No | If set with no `type`, treated as `"http"` and skipped with a warning. |

Unknown top-level keys and unknown fields inside an `mcpServers` entry are ignored so one `mcp.json` works across harnesses.

### Launch-time templates

`command`, each element of `args`, and each value in `env` are parsed at load time and resolved when the server launches for a session:

| Syntax | Resolution |
|---|---|
| `${NAME}` | Host environment variable `NAME`. Launch fails if unset. |
| `${NAME:-default}` | Host environment variable `NAME`, or `default` if unset. |
| `${session:id}` | The session's native ID (preserved across resumes). |
| `$$` | Literal `$`. |

Load-time errors: unterminated `${`, empty variable name, a `:-default` on `${session:...}`, or any `session:` key other than `id`.

### The `poo` block

The top-level `poo` key is owned strictly by `poo`: any unknown field inside `poo` fails load.

Path: `poo.servers.<server>.feeds.<feed>`

| Field | Type | Required | Description |
|---|---|---|---|
| `resource` | `string` | Yes | MCP resource URI to subscribe to. Unique per server. |
| `description` | `string` | No | Description rendered in the jobs prompt. |
| `on_idle` | `"wake" \| "hold"` | No | Idle delivery policy; inherits `jobs.on_idle` when omitted. |
| `filter` | `Filter` | No | Same `command` or `question` filter schema as [Feed manifests](#feed-manifests). |

Validation rules:

- `<server>` must be defined in the same file's `mcpServers` and use the `stdio` transport.
- `<feed>` must match `^[a-z0-9][a-z0-9_-]*$` and be unique across all MCP resource feeds and all loaded `feeds` manifests.
- Two feeds on the same server cannot subscribe to the same `resource` URI.

See [MCP Servers](/capabilities/mcp-servers) for startup timeouts, relaunch budgets, and `call_mcp`.
