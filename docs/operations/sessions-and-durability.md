---
title: Sessions & Durability
description: Persistence modes, JSONL and SQLite stores, session locking and lease TTLs, crash recovery, auto-resume, and session retention in poo.
---

# Sessions & Durability

Poo persists session transcripts, tool outcomes, permission decisions, and run state as append-only records. For the exact on-disk record schemas, see [Session Log v0](/reference/session-log-v0).

## Persistence Modes

Configure persistence with `session.mode` (or `--ephemeral` on the CLI):

| Mode | Persistence | Crash Behavior on Reload |
|---|---|---|
| `logged` (default) | Writes one JSONL file per session under `session.dir` (`~/.local/state/poo/sessions` by default). | Truncates torn tails; settles unfinished runs, in-flight tool calls, and jobs as `interrupted`. |
| `ephemeral` | Keeps records in memory for the lifetime of the process. Writes nothing to disk. | Cannot be loaded or resumed. |
| `durable` | Persists records (JSONL or SQLite) under `session.dir` with lease-based ownership. | Truncates torn tails and builds a continuation plan to resume the unsettled run instead of settling it interrupted. |

```yaml
session:
  mode: durable
  dir: ~/.local/state/poo/sessions
  durable:
    store: sqlite
    lease_ttl_s: 30
    max_resumptions: 3
    auto_resume: false
```

## Storage Backends: JSONL vs. SQLite

Logged sessions always use the file-backed JSONL store. Durable sessions select a backend via `session.durable.store`:

| Backend | Setting Value | Files on Disk | Locking / Ownership |
|---|---|---|---|
| **File (JSONL)** | `file` (default) | `<dir>/<session_id>.jsonl`, `<dir>/<session_id>.lock` (and `<dir>/<session_id>.lease` when leased) | `flock` for `logged`; lease file + short critical-section `flock` for `durable`. |
| **SQLite** | `sqlite` | `<dir>/sessions.db` (WAL journaling) | Row-based leases in the `leases` table; every write and lease transition runs in an immediate transaction. |

SQLite stores session metadata in `sessions`, append-only records in `records`, and ownership leases in `leases` with indexed queries.

## Locking and Lease TTLs

Poo enforces single-writer exclusivity on every open session:

- **File lock (`flock`)** — Used by `logged` sessions. Opening a session acquires an exclusive non-blocking `flock` on `<session_id>.lock` and holds it until the store closes. If the process crashes, the OS kernel releases the lock immediately so another process can recover the session.
- **Lease authority (`lease_ttl_s`)** — Used by `durable` sessions (and all SQLite-backed sessions). Opening a session acquires a lease with a unique 16-byte fencing token and a TTL configured by `session.durable.lease_ttl_s` (default `30` seconds). A background heartbeat renews the lease every `TTL / 3` seconds.
  - While the lease is live, another process attempting to open the session is refused with `ErrSessionLocked`.
  - If the owner dies or stops heartbeating past `expires_at`, a new owner can take over the lease. The takeover logs a warning naming the previous owner and expiry time.
  - Once a lease is lost or taken over, the previous owner is fenced by its token. Subsequent writes fail with `ErrLeaseLost`, and late releases leave the new owner's lease untouched.

## Crash Recovery and Resumption

Loading a stored session (`session/load` over the wire, `session.Resume` in Go, or `poo repl --continue` / `--resume`) scans the session log and recovers any incomplete state.

### Logged Recovery

1. Any torn trailing JSONL line from a mid-write crash is truncated.
2. Unsettled runs are marked `interrupted`.
3. In-flight tool calls and background jobs without results receive an `interrupted` outcome (`outcome unknown; the process stopped during this call` / `...this job`) so the model knows the effect is uncertain.
4. Undelivered job results are re-queued for the session.

### Durable Recovery (`auto_resume` and `max_resumptions`)

Instead of settling the latest unfinished run as interrupted, `durable` mode runs the `DurablePlanner` to build a continuation plan:

- **Tool calls** — `read_only` calls, `idempotent` tools, and side-effecting calls whose start record never landed are scheduled for replay. Side-effecting calls that already started are settled as `interrupted` so the model can decide how to proceed.
- **Permission asks** — Asks whose wall-clock deadline has not passed remain live and are re-armed; expired asks are resolved using their recorded `fallback` decision.
- **Partial output & compaction** — Partial assistant messages and uncommitted compactions are discarded cleanly; the run resumes at the appropriate phase (`requesting`, `streaming`, `executing`, or `waiting`).
- **Session jobs & feeds** — Running jobs and in-flight feed attempts died with the previous process and are ended as `interrupted`; feed subscriptions are restored.

Two settings control resumption behavior:

- **`session.durable.max_resumptions`** (default `3`) — Maximum number of times a single run may be resumed across crashes. Once reached, the planner stops continuing the run and settles it as `interrupted` with `resumption_limit` to prevent infinite crash loops.
- **`session.durable.auto_resume`** (default `false`) — When `false`, a loaded durable session holds its continuation plan idle until explicitly resumed. When `true`, loading the session automatically resumes the unsettled run.

## Explicit Log Path (`--log`)

On `poo run`, `--log <path>` writes the session log to a specific file path instead of `session.dir`.

```bash
poo run --log ./traces/build-fix.jsonl "Fix the failing unit tests"
```

`--log` always creates a new file and refuses to extend or overwrite an existing file.

## Managing Sessions

### Listing Sessions

`poo sessions list` inspects the configured session directory (override with `--session-dir`):

```bash
poo sessions list
poo sessions list --json
```

Table columns: `ID`, `CREATED`, `PROFILE`, `RUNS`, `REASON`, `SIZE`, and `LOCKED`.

### Pruning and Retention

Retention is opt-in — without a configured policy or CLI flags, `poo sessions prune` deletes nothing and exits with a usage error.

Configure defaults in settings under `session.retention`:

```yaml
session:
  retention:
    max_age_days: 30
    max_sessions: 200
```

Run `poo sessions prune` to apply the configured policy or override it with flags:

```bash
# Preview what would be deleted
poo sessions prune --dry-run

# Delete sessions with no activity in the last 14 days, keeping at most 50
poo sessions prune --older-than 14 --keep 50
```

| Setting | CLI Flag | Description |
|---|---|---|
| `session.retention.max_age_days` | `--older-than <days>` | Delete sessions whose last activity is older than this many days. |
| `session.retention.max_sessions` | `--keep <count>` | Keep at most this many most recently active sessions. |
| — | `--dry-run` | Print sessions that would be deleted without removing them. |

Sessions currently held by another process (`LOCKED: true`) are always skipped during pruning.
