---
title: Built-in Tools & Jobs
description: Reference for poo's built-in tools (bash, read, edit, write, grep, fetch, history, finish, jobs, cancel_job) and background job mechanics.
---

# Built-in Tools & Jobs

`poo` ships a focused set of built-in tools. Profiles choose which tools a session offers; settings can narrow file-tool reach, tune limits, or remove individual tools with `tools.disabled`.

## Built-in tools overview

| Tool | Class | Primary arg | Backgroundable | Purpose |
|---|---|---|---|---|
| `bash` | `side_effecting` | `command` | Yes (`background: true`, `/bg`) | Run one command via `/bin/bash -c` |
| `read` | `read_only` | `path` | No | Read text line ranges, images, or audio |
| `edit` | `side_effecting` | `path` | No | Exact-string replacement (or file creation) |
| `write` | `side_effecting` | `path` | No | Create a new file (refuses existing files) |
| `grep` | `read_only` | `pattern` | No | RE2 regex search across a directory tree |
| `fetch` | `read_only` | `url` | No | HTTP(S) GET with HTML-to-Markdown conversion |
| `history` | `read_only` | `pattern` | No | Read or search retained session turns |
| `finish` | `read_only` | — | No | Opt-in typed JSON result validated by schema |
| `jobs` | `read_only` | — | No | List running jobs, held results, and feeds |
| `cancel_job` | `read_only` | — | No | Stop a running job or feed |

## `bash`

Runs a shell command via `/bin/bash -c` in the session's working directory, inside its own process group. When a call finishes, times out, or is cancelled, `poo` signals the entire process group (`SIGTERM`, then `SIGKILL` after a 2-second grace period).

**Arguments:**

- `command` (string, required): Shell command to execute.
- `timeout` (number, optional): Foreground timeout in milliseconds. Clamped to 10 minutes (`600000` ms), and can shorten — never exceed — `tools.bash.timeout_s`. Defaults to 120 seconds when unconfigured. Background calls use `jobs.timeout_s` instead.
- `description` (string, optional): Human-readable summary for the event stream and transcript. Permission asks always show the raw `command`, never `description`.

**Output & caps:**

- Captures up to **1 MiB** of output; excess output is drained and flagged with `truncated: true`.
- Observations are capped at `min(30000, floor(0.06 × context_limit × 3.5))` characters. Longer output is elided in the middle (`output_head`, `output_tail`, `elided_chars`, `hint`).
- Returns a JSON envelope with `returncode` (`null` if killed/timed out) and `timed_out`. Timeouts and signal kills are marked as recoverable errors so the model does not mistake partial output for success.

**Settings:**

```yaml
tools:
  bash:
    timeout_s: 120
    executor: local   # or bwrap (see Sandboxing)
  env:
    scrub: [OPENAI_API_KEY, ANTHROPIC_API_KEY]
    # passthrough: [PATH, HOME, TERM]  # exclusive allowlist
```

## `read`

Reads text files by line range, or returns image and audio files as native model attachments.

**Arguments:**

- `path` (string, required): File path (relative to working directory or absolute).
- `offset` (integer, optional): 1-indexed starting line (default `1`).
- `limit` (integer, optional): Number of lines to return (default `2000`).

**Text behavior:**

- Lines longer than `2000` characters are truncated so minified files cannot flood context.
- Output is capped at `min(70000, floor(0.10 × context_limit × 3.5))` characters.
- Prefixes output with `[<path> · lines <first>–<last> of <total>]` (and `· capped` when truncated).
- Non-media binary files (containing a NUL byte in the first 8 KiB) are refused with a hint to use `grep` or a command.

**Image and audio attachments:**

`read` sniffs file headers before the binary check:

- **Images**: Returned as `image` attachments up to **5 MB** when the active model declares `image` in `inputs`.
- **Audio**: `audio/wav` (`RIFF…WAVE`) and `audio/mpeg` (`ID3` or MP3 frame header) returned as `audio` attachments up to **25 MB** when the active model declares `audio` in `inputs`.
- If the active model does not support that modality or the file exceeds the size limit, `read` returns a one-line text description. This avoids sending an attachment the provider would reject.

**Reach & protection:**

- `tools.read.restrict_to_working_dir: true` confines reads to the working directory (configured [skills](/capabilities/skills) roots remain readable).
- `tools.read.ask_outside_working_dir: true` routes reads outside the working directory through the permission asker.
- `tools.protected_paths` blocks matching paths before opening the file.

## `edit`

Replaces an exact string in a file, or creates a missing file when `old` is empty.

**Arguments:**

- `path` (string, required): Target file path.
- `old` (string, required): Exact text to replace. Empty string creates a missing file; rejected if the file already exists.
- `new` (string, required): Replacement text.
- `replace_all` (boolean, optional): Replace every occurrence instead of requiring a unique match.

**Safety guarantees:**

- Fails loudly if `old` is not found or matches more than once without `replace_all: true`.
- Writes atomically and preserves the existing file's permission mode.
- Guards against concurrent edits to the same file (`{"code": "edit_conflict"}`).
- Honors `tools.edit.restrict_to_working_dir`, `tools.edit.ask_outside_working_dir`, and `tools.protected_paths`.

## `write`

Creates a new file with the given content, creating missing parent directories as needed.

**Arguments:**

- `path` (string, required): Path of the file to create.
- `content` (string, required): File contents.

`write` refuses to overwrite an existing file and directs the model to `edit`. It uses the same concurrent-write reservation (`edit_conflict`) and respects `tools.write.restrict_to_working_dir`, `tools.write.ask_outside_working_dir`, and `tools.protected_paths`.

## `grep`

Searches files in a directory tree using Go's RE2 regular expression engine — no external `ripgrep` binary required.

**Arguments:**

- `pattern` (string, required): RE2 regular expression. May be empty only when `mode` is `files_with_matches` (lists files).
- `path` (string, optional): Directory to search (defaults to working directory).
- `glob` (string, optional): Path filter matched against paths relative to the search root (e.g., `*.go`).
- `mode` (string, optional): `content` (default, matching lines), `files_with_matches`, or `count`.

**File selection & limits:**

- Inside a git work tree, `grep` asks `git ls-files --cached --others --exclude-standard` so `.gitignore` rules apply. The `git` invocation isolates config and disables hooks/fsmonitor (`core.fsmonitor=false`, `core.hooksPath=/dev/null`, scrubbed environment).
- Outside a git work tree, `grep` walks the directory and skips dot-directories.
- Symlinks, binary files (NUL byte in first 8 KiB), and `tools.protected_paths` are skipped (the footer reports how many protected files were skipped).
- Defaults to `250` results, `2000` characters per line, and the session's observation cap.
- Honors `tools.grep.restrict_to_working_dir` and `tools.grep.ask_outside_working_dir`.

## `fetch`

Performs a read-only HTTP(S) `GET` request and returns the response body as text.

**Arguments:**

- `url` (string, required): `http://` or `https://` URL.
- `format` (string, optional): `markdown` or `raw`. Defaults to `markdown` for `text/html` and `application/xhtml+xml` responses, and `raw` for all other text types (`text/*`, JSON, XML, JavaScript, or valid UTF-8).

**Behavior:**

- Follows up to **5 redirects**.
- Non-text / binary bodies return headers and metadata only.
- Output is capped at `tools.fetch.max_bytes` during download and elided to the observation cap when rendered.

**Private-host guard:**

`fetch` validates resolved IP addresses at dial time and dials the validated IP directly to prevent DNS rebinding. It also checks hosts on every redirect and when an environment proxy (`HTTP_PROXY` / `HTTPS_PROXY`) is active:

- **Blocked by default (liftable)**: Loopback (`127.0.0.0/8`, `::1`) and private ranges (RFC 1918, `fc00::/7`). Set `tools.fetch.allow_private_hosts: true` to allow them.
- **Always blocked (never liftable)**: Link-local addresses (including cloud metadata `169.254.169.254`), unspecified addresses, `0.0.0.0/8`, `100.64.0.0/10` (CGNAT), `198.18.0.0/15`, and `fec0::/10`.

**Settings:**

```yaml
tools:
  fetch:
    timeout_s: 30              # default: 30
    max_bytes: 4194304         # default: 4 MiB
    user_agent: "poo-fetch"    # default: poo-fetch
    allow_private_hosts: false # default: false
```

## `history`

Reads or searches turns retained in the session's store, including turns that [context compaction](/capabilities/compaction) replaced in the model's active view.

**Arguments (mutually exclusive):**

- `turn` (integer): 1-indexed turn number to read whole (truncated to the observation cap).
- `pattern` (string): RE2 regular expression to search across retained turns (up to `250` matches, `2000` characters per line, capped at the observation cap).

## `finish`

An opt-in read-only tool for headless runs that require structured JSON output instead of freeform prose. Enable it by setting `finish.schema_file` in settings (relative to the settings file, or `~/` under home):

```yaml
finish:
  schema_file: ./result-schema.json
```

`poo` wraps your schema in `{"result": <your-schema>}` and exposes `finish` to the model. On call:

- Arguments are validated against the schema (`type`, `properties`, `required`, `enum`, `items`, `additionalProperties`).
- Validation errors are returned as recoverable tool errors so the model can fix its payload and call `finish` again.
- A valid call records the structured result and settles the run as finished.

## `jobs` and `cancel_job`

Automatically registered by the core whenever a session has backgroundable tools or feeds (cannot be removed via `tools.disabled`):

- `jobs` (no arguments): Lists all running background jobs, held job results, and session feeds (`feed:<name>` or `feed:<name>[scope]`) with status and elapsed time.
- `cancel_job` (`{"job": "j3"}` or `{"job": "feed:inbox"}`): Stops a running job (delivering its partial output as its result) or stops a running/suspended feed.

---

## Background jobs

Long-running commands and RPCs can run asynchronously as session-owned background jobs.

### Starting a background job (`background: true`)

Any tool that allows background execution — `bash`, `call_cli` (and promoted CLI registry tools), `call_mcp`, and `codemode` — accepts an extra `"background": true` argument:

```json
{
  "command": "go test ./...",
  "background": true
}
```

- The call returns immediately with a job handle (for example `j1`).
- There is no `wait_job` tool: the model ends its turn and receives the job's result later as an incoming message.
- Starting a duplicate background job with the same tool and canonical arguments as an already-running job is refused.
- A job belongs to the **session**, not the run: it keeps running after the turn ends and survives run cancellation or interrupts. Only `cancel_job`, the job timeout, or closing the session stops it.

### Promoting a running call (`/bg`)

In `poo repl`, typing `/bg` while a foreground `bash`, `call_cli`, or `call_mcp` call is running detaches it into a background job without restarting the process:

- The promoted job survives run cancellation.
- **Timeout caveat**: A promoted `bash` call keeps its original foreground timeout rather than switching to `jobs.timeout_s`. Pass `background: true` at launch when a command needs the full job timeout.

### Job settings (`jobs.*`)

```yaml
jobs:
  max_running: 64           # default: 64 concurrent jobs per session
  timeout_s: 1800           # default: 1800s (30 minutes) per job
  on_idle: wake             # wake (default) or hold
  max_unattended_wakes: 0   # default: 0 (unlimited)
```

| Setting | Default | Behavior |
|---|---|---|
| `jobs.max_running` | `64` | Maximum concurrent background jobs per session. Further background calls are refused until a job finishes or is cancelled. |
| `jobs.timeout_s` | `1800` | Per-job wall-clock timeout in seconds (30 minutes). |
| `jobs.on_idle` | `wake` | What happens when a job result arrives while the session is idle. `wake` starts a new run with the result; `hold` queues it until the next run starts. |
| `jobs.max_unattended_wakes` | `0` | Maximum consecutive woken runs with no client input in between (`0` = no limit). Once reached, subsequent results are held (`wake_limit`) until the user sends input. |

## Related

- [CLI Registry & Script Tools](/capabilities/cli-registry) — add custom tools via JSON manifests
- [MCP Servers](/capabilities/mcp-servers) — connect stdio MCP servers and resource feeds
- [Feeds & Daemon](/capabilities/feeds-and-daemon) — long-lived event feeds and idle wake policies
- [Permissions & Safety](/operations/permissions-and-safety) — rules, askers, protected paths, and doom-loop detection
- [Sandboxing (bwrap)](/operations/sandbox) — isolate `bash` and CLI registry commands with bubblewrap
