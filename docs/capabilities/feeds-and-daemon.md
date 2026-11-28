---
title: Feeds & Daemon
description: Push asynchronous external events into a poo session with long-lived feeds, filter them before delivery, and host always-on agents with poo daemon.
---

# Feeds & Daemon

Most agent harnesses only move when a user types a prompt. `poo` can also run **feeds** — long-lived external commands or MCP resource subscriptions that push asynchronous inputs into a session as `feed:<name>`. Paired with `poo daemon`, a session becomes a durable, lease-backed resident process that sleeps while idle, wakes when something happens, and resumes across restarts.

## What feeds are

A feed is a source that runs beside a session and emits messages into it. Feeds come in two forms:

- **Command feeds** (declared in `feeds` manifests): long-lived processes started through the session's executor whose stdout carries one JSON message per line.
- **MCP resource feeds** (declared in `mcp.json`'s `poo` block): subscriptions to an MCP server's `resources/subscribe` URI that drain updates into the same delivery and filter pipeline. See [MCP Servers](/capabilities/mcp-servers) for server setup and relaunch behavior.

Every feed input is delivered to the model as untrusted outside input wrapped in a `<feed>` XML element:

```xml
<feed name="avenor-inbox" from="worker-1">Tests failed in pkg/auth; should I revert the fixture change?</feed>
```

- `name` is the feed's label: `<name>` for always-on or unscoped feeds, or `<name>[<field>=<value>]` for scoped optional subscriptions.
- `from` is omitted when the message has no sender.
- `&` and `<` are escaped in the text body (and `"`, `\n`, `\r` in attributes) so payload content cannot close the element.
- Lifecycle transitions are recorded as `feed.started` and `feed.ended` in the [session log](/reference/session-log-v0).

### Command feed stdout protocol

A command feed writes one JSON object per line to stdout:

```json
{"text": "PR #42 has a new review comment.", "from": "alice"}
```

A message may attach an image beside its text, or send an image alone with `text` omitted:

```json
{"text": "Build dashboard snapshot", "image": {"path": "shot.png", "media": "image/png"}}
```

Protocol rules:

- `text` is required unless `image` is present. `from` is optional; `id` is reserved and ignored; unknown fields are ignored; blank lines are skipped.
- Maximum line length is **1 MiB**.
- `image.path` is required (relative paths resolve against the feed command's working directory). `image.media` is optional; when present, the file's magic bytes must match it. Images are capped at **5 MB** and refused if unreadable, mismatched, oversized, or if the session's model does not accept image input.
- Protocol violations fail the attempt immediately, including malformed JSON, lines over 1 MiB, missing text or image, and refused inputs. `poo` stops the command, discards remaining output, and reaps the process.
- Stderr is written to the session's structured log (up to **64 KiB** per attempt) and never shown to the model.

## Feed manifests

Feed manifests are JSON, YAML (`.yaml`/`.yml`), or TOML (`.toml`) files loaded from `feeds.paths`, `$XDG_CONFIG_HOME/poo/feeds/` (unless disabled), `POO_FEEDS_PATH`, and `--feeds-path`.

```json
{
  "version": 1,
  "feeds": {
    "inbox": {
      "description": "Messages from the team queue.",
      "command": ["my-cli", "inbox"],
      "env": {
        "QUEUE_URL": "env:QUEUE_URL",
        "SESSION_ID": "session:id",
        "QUEUE_MODE": "batch"
      },
      "restart": true,
      "on_idle": "wake",
      "max_pending": 32
    }
  }
}
```

Key fields:

- `version`: must be `1`.
- Feed name: matches `^[a-z0-9][a-z0-9_-]*$` and must be unique across all loaded feed manifests and MCP resource feeds.
- `command`: required argv array.
- `description`: rendered in the jobs prompt so the model knows what each feed provides.
- `env`: added on top of the CLI registry's benign environment variables (never inheriting variables scrubbed by `tools.env.scrub`). Values may be literals, `env:NAME` (from the scrubbed host environment), `session:id` (the session's native ID, preserved across resumes), or `scope:NAME` (for optional feeds). Unresolved references fail the attempt before the command starts.
- `max_pending`: caps undelivered queued inputs for the feed (default `16`). Past the cap, the oldest undelivered input is dropped unread.

See the [Manifests Reference](/reference/manifests#feed-manifests) for the complete schema and validation rules.

## Restart policy

Set `"restart": true` to restart a command feed after its process exits or hits a protocol failure:

- Backoff starts at **1s** and doubles on each consecutive exit or failure, capped at **60s**.
- An attempt that stays up for **60s or longer** resets the backoff timer.
- `cancel_job` and session close always cancel any pending restart.
- **Exception:** if a [command filter](#command-filter-external-script) fails, the feed is **suspended** and does not restart regardless of `restart`.

## Idle wake behavior

`on_idle` controls whether a feed input can start a new run when the session is idle:

| `on_idle` | Behavior when session is idle |
|---|---|
| `wake` | Starts a new run to handle the incoming feed message. |
| `hold` | Queues the message until a run is already active. |
| *(omitted)* | Inherits `jobs.on_idle` for always-on feeds; defaults to `hold` for `optional: true` feeds. |

Whenever a run starts (from a `wake` feed, a background job, or client input), it drains all queued inputs — including held ones.

To guard against runaway autonomous loops, set `jobs.max_unattended_wakes` in settings to cap consecutive woken runs with no client input in between (`0` disables the cap).

## Optional feeds (`subscribe_feed` and `cancel_job`)

A feed marked `"optional": true` does not start when the session opens. Instead, the model starts instances on demand with `subscribe_feed` and stops them with `cancel_job`.

Optional feeds can declare a `scope` — string fields (with optional regex `pattern`s) that the model supplies when subscribing and that bind into `env` via `scope:<field>`:

```json
{
  "version": 1,
  "feeds": {
    "git-feedback": {
      "description": "Feedback on one repository's pull requests.",
      "command": ["git-feedback", "--watch"],
      "optional": true,
      "scope": {
        "url": {"type": "string", "pattern": "^https://"}
      },
      "env": {
        "GIT_FEEDBACK_URL": "scope:url"
      },
      "on_idle": "wake",
      "restart": true
    }
  }
}
```

How subscriptions work:

- **Starting an instance:** the model calls `subscribe_feed` with `{"feed": "git-feedback", "scope": {"url": "https://github.com/org/repo"}}`. Scope values are validated against the manifest schema; missing fields, unknown fields, or pattern mismatches are returned as tool refusals.
- **Origin and job ID:** scoped instances are identified as `feed:<name>[<field>=<value>]` with fields sorted by name and values query-escaped (e.g., `feed:git-feedback[url=https%3A%2F%2Fgithub.com%2Forg%2Frepo]`). Unscoped optional feeds use `feed:<name>`.
- **Idempotency:** subscribing to the same feed and scope reaches the existing instance instead of starting a duplicate. If the instance was stopped or suspended, `subscribe_feed` starts it again.
- **Instance cap:** a session runs at most **8** feed instances at once (counting running, restarting, and suspended instances). A ninth distinct subscription is refused until one is stopped with `cancel_job`.
- **Inspecting and stopping:** `jobs` lists all running and suspended feeds with tool type `feed`. `cancel_job` accepts the feed's job ID (`feed:<name>` or `feed:<name>[...]`) and stops it. See [Built-in Tools & Jobs](/capabilities/tools-and-jobs).
- **Reload behavior:** when a session reloads, always-on feeds start automatically. **Durable** sessions also restore active optional subscriptions from the session log; **logged** sessions do not (the model must call `subscribe_feed` again).

## Filtering before delivery

A feed's `filter` block screens records before they enter the session queue. A manifest may configure either a **question filter** or a **command filter**, never both.

### Question filter (decision model)

A question filter asks the session's [decision model](/capabilities/decision-model) a yes/no question about each message's text:

```json
"filter": {
  "question": "Does this event report a failed required check or an unclear status?",
  "min_no_probability": 0.95
}
```

- Phrase `question` so **yes** means deliver. `min_no_probability` must be `> 0.5` and `<= 1`.
- The decider sees only the feed name, sender, and message text — never the conversation history.
- A message is dropped only when `P(no)` (multiplied by label mass, if the endpoint reports one) meets or exceeds `min_no_probability`. Dropped messages never enter the queue and cannot wake an idle session.
- **Fails open:** messages pass through unfiltered when screening cannot run. This includes sessions with no decision model (warns once), failed calls, or calls exceeding the **1.5s** budget. Image-only messages and messages whose sender plus text exceeds **8 KiB** also pass through.

**Runtime question override:** `subscribe_feed` accepts an optional `prompt` to set or replace a feed instance's question without restarting it. It also accepts an optional `min_no_probability`, defaulting to the manifest's threshold or `0.6`. Passing an empty `prompt` clears the override and restores the manifest's question. Setting a `prompt` is refused if the feed uses a command filter or if the session has no decision model.

### Command filter (external script)

A command filter runs an external command once per source record, transforming or dropping it before delivery:

```json
"filter": {
  "command": ["/path/to/excerpt", "--max", "2000"]
}
```

- The source JSON record is written to the filter's stdin.
- The filter writes a stream of JSON record objects to stdout (compact or pretty-printed). Empty stdout drops the record; multiple objects fan one source record out into multiple inputs.
- Output records accept `text`, `from`, `image`, and an optional structured `data` object (only command-filtered feeds carry `data`).
- Runs under the same executor, working directory, and environment as the feed command.
- Caps: **1 MiB** per record on input and output, and at most **4 MiB** and **256 records** per filter invocation.
- **Fails closed:** a nonzero exit, invalid JSON output, or exceeding any cap fails the attempt and **suspends** the feed (ignoring `restart`). `poo` writes filter diagnostics to a log file beside the session and wakes the session with a fault report (`<feed fault="..." log="...">`). Optional feeds can be restarted with `subscribe_feed`; always-on and MCP resource feeds remain suspended until session close or resume.

## Profile feed declarations and `feeds.defaults`

- **`feeds.defaults`** (default `true`): controls whether `$XDG_CONFIG_HOME/poo/feeds/` loads automatically alongside `feeds.paths`. A profile's `feeds_defaults` overrides `feeds.defaults` and is inherited from its `base` profile. See [Manifests Reference](/reference/manifests#defaults-and-profile-interaction).
- **`profiles.<name>.feeds`**: declares feed names that a profile expects to be present (inherited and merged from `base`). This is a **presence assertion**, not a switch — `feeds.paths` and defaults still decide what loads, and an MCP resource feed satisfies the name as well. If a declared feed is not loaded, `poo` emits a warning rather than failing the build. See [Profiles & Prompts](/configuration/profiles-and-prompts).

## `poo daemon`

`poo daemon` hosts a long-lived, feed-driven session without reading stdin. It is designed to run under a supervisor (`systemd`, `launchd`, a container entrypoint, or a background terminal) and keep a session alive across restarts.

```sh
poo daemon --session ops-inbox --working-dir /srv/app
```

Key behaviors:

- **Durable by default:** unless `--session-mode` or `session.mode` is set, `poo daemon` selects `durable` mode so runs, pending inputs, and optional feed subscriptions survive crashes. See [Sessions & Durability](/operations/sessions-and-durability).
- **Lease-backed ownership:** the daemon acquires the session's lease (or takes it over if the previous owner died). If another owner takes over the lease while the daemon is running, `poo daemon` exits immediately with code `1` rather than writing into another owner's session.
- **No wire client:** permission asks cannot prompt an interactive user; the asker chain is `[decide]` (active only when a decision model is configured).
- **Clean shutdown:** `SIGINT` or `SIGTERM` stops feeds, flushes session records, releases the lease, and exits `0`.

### Flags

In addition to shared and endpoint flags (`--settings`, `--profile`, `--feeds-path`, `--mcp-path`, `--cli-registry-path`, etc.):

| Flag | Default | Description |
|---|---|---|
| `--session <id>` | generated ID | Session ID to load (taking over a dead owner's lease) or create if it does not exist. |
| `--auto-resume` | `true` | Resume an unsettled pending run on load. Overrides `session.durable.auto_resume`. |
| `--working-dir <dir>` | current dir | Working directory for the session's tools and feeds. |

### Lifecycle stderr

`poo daemon` logs one lifecycle fact per line to stderr:

```text
poo daemon: started
poo daemon: session ops-inbox
poo daemon: 1 feed(s)
poo daemon: idle
```

When taking over a session with an unsettled run:

```text
poo daemon: started
poo daemon: session ops-inbox
poo daemon: 1 feed(s)
poo daemon: the session was taken over: a run was left unsettled
poo daemon: resumed run_01J...
```

On shutdown or lease loss:

- `poo daemon: shutdown` (on `SIGINT` / `SIGTERM`)
- `poo daemon: the lease was lost: a takeover owner now holds the session`

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Clean shutdown after `SIGINT` or `SIGTERM`. |
| `1` | Operational failure: store open/create/resume error, failed pending-run resume, broken session, or lost lease. |
| `2` | Usage or configuration error: invalid flags, bad working directory, invalid settings, or invalid session ID. |

See [Limits & Exit Codes](/operations/limits-and-exit-codes) for exit codes across all `poo` subcommands.

## End-to-end example

This example wires an Avenor inbox feed and CLI delegation tools into a durable daemon session that wakes whenever a child agent sends a question.

**1. Feed manifest (`feeds.json`):**

```json
{
  "version": 1,
  "feeds": {
    "avenor-inbox": {
      "description": "Receive Avenor messages",
      "command": ["avenor", "feed", "broker"],
      "env": {
        "AVENOR_BIND_HOST": "poo",
        "AVENOR_BIND_SESSION": "session:id"
      },
      "on_idle": "wake",
      "restart": true
    }
  }
}
```

**2. Settings (`settings.yaml`):**

```yaml
feeds:
  defaults: false
  paths: [./feeds.json]

cli_registry:
  defaults: false
  paths: [./examples/avenor/tools.json]

jobs:
  max_unattended_wakes: 10

session:
  mode: durable
  durable:
    lease_ttl_s: 30
```

**3. Start the daemon:**

```sh
poo daemon --settings settings.yaml --session avenor-coordinator
```

`poo daemon` prints its startup lines to stderr, starts `avenor feed broker` with `AVENOR_BIND_SESSION=avenor-coordinator`, and waits idle:

```text
poo daemon: started
poo daemon: session avenor-coordinator
poo daemon: 1 feed(s)
poo daemon: idle
```

When `avenor feed broker` prints a JSON line to stdout, `poo` wakes the idle session and delivers `<feed name="avenor-inbox" ...>` to the model. The model can then reply via its registered tools. If the daemon is stopped with `SIGTERM` and started again with `--session avenor-coordinator`, it reattaches to the same session ID, restarts `avenor-inbox`, and resumes any unsettled run.
