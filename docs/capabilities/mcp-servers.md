---
title: MCP Servers
description: Connect stdio Model Context Protocol servers, call their tools via call_mcp and find_tool, subscribe to resource feeds, and handle bounded auto-relaunch.
---

# MCP Servers

`poo` runs Model Context Protocol (MCP) servers as session-owned stdio child processes. Loading an `mcp.json` manifest **declares** which servers exist; a profile's `mcp_servers` list **selects** which ones a session actually starts.

The model calls server tools through `call_mcp`, searches them beside CLI registry commands with `find_tool`, and can receive asynchronous messages from server resources through `poo` resource feeds.

## Loading `mcp.json` manifests

`poo` reads standard `mcp.json` files (the same `mcpServers` format used by Claude Code and other harnesses) from:

1. **Default user directory**: `$XDG_CONFIG_HOME/poo/mcp/` (typically `~/.config/poo/mcp/`), loaded when `mcp.defaults` is absent or `true`. A missing directory is not an error.
2. **Configured paths**: `mcp.paths` in settings (files or directories), plus `POO_MCP_PATH` (OS path-list separated) and repeatable `--mcp-path` flags on `poo run`, `poo serve`, and `poo prompt`.

Directories contribute their `.json` files and skip non-`.json` files; an explicit file path that is not `.json` fails the load. Relative paths resolve against the working directory; `~/` resolves under the home directory.

```yaml
mcp:
  paths:
    - examples/avenor-mcp
  defaults: false   # ignore ~/.config/poo/mcp/
```

A profile can override `mcp.defaults` with `mcp_defaults`.

### Manifest rules

- Top-level `mcpServers` is required. Each key is the server name (letters, digits, `-`, and `_`).
- `poo` reads `command` (required), `args`, `env`, `type`, and `url`. Other top-level keys and entry fields are ignored so one `mcp.json` can be shared across harnesses.
- Only `stdio` transport is supported. Entries with `type` set to `http`, `sse`, or `streamable-http` (or a `url` with no `type`) are skipped with a warning naming the server. Any other `type` value fails the load.
- Duplicate server names across files and duplicate JSON object keys within a file both fail the load.

## Profile opt-in (`mcp_servers`)

Loading a manifest starts nothing. A session starts only the servers named in its profile's `mcp_servers` list:

```yaml
profiles:
  avenor:
    base: code
    mcp_servers: [avenor]
profile: avenor
```

`mcp_servers` adds to the base profile's selection, deduplicated in order of first appearance. Selecting a server name that no loaded manifest defines produces a warning and skips it. A binary that does not link `call_mcp` fails the build if a profile selects any server.

### Process lifecycle and trust

- **Session-owned**: Each selected server starts when the session is created or resumed (with the session's ID and working directory) and stops when the session closes.
- **Startup budget**: Startup is bounded at **30 seconds** per server, including the MCP handshake and initial tool listing. A server that fails to start fails session creation or resume.
- **Unsandboxed host process**: MCP servers start directly on the host — not through the session's executor or sandbox — and run as the user running `poo`. They inherit the host environment minus `tools.env.scrub`, plus the manifest's resolved `env`. Select an MCP server with the same trust boundary as installing a program.

## Launch template variables

`command`, each element of `args`, and each `env` value support template expansion, resolved **at server launch** (not when settings load):

| Syntax | Resolution |
|---|---|
| `${NAME}` | Host environment variable `NAME`. Missing variable fails launch naming the server and variable. Can re-introduce a variable removed by `tools.env.scrub`. |
| `${NAME:-default}` | Host environment variable `NAME`, or `default` if unset. |
| `${session:id}` | The session's ID (stable across session resume). Cannot take a default. |
| `$$` | Literal `$`. |

Syntax errors — unterminated `${`, empty variable name, a default on `session:id`, or any `session:` variable other than `id` — fail manifest load.

## Calling tools: `call_mcp` and `find_tool`

The model reaches MCP tools through a single dispatcher, `call_mcp`, using `<server>/<tool>` qualified names:

```json
{
  "name": "avenor/spawn",
  "args": {
    "prompt": "Run the test suite and report failures.",
    "dir": "/path/to/repo"
  }
}
```

- **Permissions**: `call_mcp` is `side_effecting` and its primary argument is `name`. A [permission rule](/operations/permissions-and-safety) can allow or deny an entire server by prefix (for example `command_prefix: "avenor/"`) without listing individual tools.
- **Hints vs. permissions**: MCP tool annotations (`readOnlyHint`, `destructiveHint`) appear in the prompt catalog and `find_tool` results as `(read-only)` or `(destructive)` hints for the model. They never change the call's permission class.
- **Background jobs**: `call_mcp` supports `background: true` and `/bg` promotion in the REPL.
- **Outputs**: Text responses are elided in the middle if they exceed the observation cap. `structuredContent` rides along when it is valid JSON under the same cap (otherwise omitted with a note). Returned images become attachments when the active model accepts `image` input and the image fits within 5 MB. Otherwise, a note explains what was omitted.
- **Errors**: Unknown `<server>/<tool>` names are refused before calling. A stopped server returns `server <name> is down` as a recoverable error. A transport failure mid-call is reported and **never retried**, because the server may already have executed side effects.

### `find_tool` integration

When a session has a [decision model](/capabilities/decision-model):

- If the MCP offering exceeds 30 tools, the prompt points the model at `find_tool` instead of listing every tool inline.
- `find_tool` searches MCP tools and CLI registry commands together over a live snapshot of the session's offering. Each hit includes the tool's argument schema and tells the model which dispatcher (`call_mcp` or `call_cli`) and argument (`name` or `command`) to use.
- Because the snapshot is live, a down or relaunching server contributes no stale hits. Once it relaunches, its newly listed tools are immediately searchable.

## Resource feeds (`poo` block)

A manifest can include a top-level `poo` block to turn an MCP resource into a session feed. Unlike `mcpServers`, the `poo` block is **strict**: unknown fields fail the load.

```json
{
  "mcpServers": {
    "avenor": {
      "command": "avenor",
      "args": ["mcp", "--bind-host", "poo", "--bind-session", "${session:id}"]
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
              "command": ["jq", "-c", ".text | fromjson | .messages[] | {from, text}"]
            }
          }
        }
      }
    }
  }
}
```

Fields under `poo.servers.<server>.feeds.<feed>`:

- `resource` (required): Resource URI to subscribe to.
- `description`: Shown in the `jobs` prompt section.
- `on_idle`: `wake` or `hold` (inherits `jobs.on_idle` when omitted).
- `filter`: Optional [feed filter](/capabilities/feeds-and-daemon) using either `command` or `question`. A `command` filter fans one resource read into zero, one, or many inputs. A `question` filter screens each record with the decision model.

How resource feeds run:

- The server must advertise `resources/subscribe` in its handshake, or session creation fails.
- After subscribing, `poo` reads the resource once immediately (draining any backlog queued before subscription) and reads it again on each resource-updated notification.
- Each text content block is emitted as a record with the server name as `from` and the text verbatim (empty text and binary contents are skipped). Reads are drained at-most-once.
- Declared `command` filters run through the session's executor and working directory. Standard feed caps apply: 1 MiB per record, and 4 MiB or 256 records per filter run.
- Feed names must be lowercase letters, digits, `-`, and `_`, and must be unique across all MCP servers and configured command feeds. Two feeds cannot subscribe to the same server resource.
- Running resource feeds appear in `jobs` as `feed:<name>` and can be stopped with `cancel_job`.

## Bounded auto-relaunch

If a running MCP server exits unexpectedly during a session, `poo` relaunches it automatically:

- **Backoff**: First retry waits **1 second**, doubling on each consecutive failure up to **30 seconds**.
- **Reset**: A generation that stays up for at least **60 seconds** resets the failure counter.
- **Exhaustion**: After **5 consecutive failures**, the server is marked down for the remainder of the session:
  - `call_mcp` calls to that server return `server <name> is down`.
  - Any resource feeds attached to that server suspend with fault `filter_exited` (`server <name> stopped after 5 relaunches`).
  - The session itself keeps running; resuming the session starts with a fresh relaunch budget.
- **Re-sync**: On a successful relaunch, `poo` re-lists the server's tools (updating `call_mcp` and `find_tool` live; the initial prompt catalog is not re-rendered) and re-subscribes its resource feeds.

## Example: Avenor over MCP

The repository includes a complete working example in [`examples/avenor-mcp/`](https://github.com/sdougbrown/poo/tree/main/examples/avenor-mcp):

- [`examples/avenor-mcp/mcp.json`](https://github.com/sdougbrown/poo/blob/main/examples/avenor-mcp/mcp.json) launches `avenor mcp --bind-host poo --bind-session ${session:id}` and subscribes to `avenor://inbox` with a `jq` filter that fans out batched messages and attaches reply instructions for asks.
- [`examples/avenor-mcp/settings.yaml`](https://github.com/sdougbrown/poo/blob/main/examples/avenor-mcp/settings.yaml) loads `examples/avenor-mcp`, selects `mcp_servers: [avenor]` on a profile, and disables ambient `mcp`, `feeds`, and `cli_registry` defaults so the MCP inbox does not contend with the older CLI+feed example in `examples/avenor/`.

```sh
poo run --settings examples/avenor-mcp/settings.yaml \
  "spawn a child to run the test suite, then answer its questions"
```

## Related

- [CLI Registry & Script Tools](/capabilities/cli-registry) — wrap local CLIs and scripts without an MCP server
- [Feeds & Daemon](/capabilities/feeds-and-daemon) — command feeds, filters, and idle wake behavior
- [Built-in Tools & Jobs](/capabilities/tools-and-jobs) — `jobs`, `cancel_job`, and background execution
- [Manifests Reference](/reference/manifests) — complete manifest reference
