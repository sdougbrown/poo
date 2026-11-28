---
title: Evals & Benchmarks
description: Built-in poo-eval suite, bwrap isolation, baseline management, and the Harbor adapter for Terminal-Bench and SWE-bench.
---

# Evals & Benchmarks

`poo` uses two evaluation tracks:

1. **Built-in `poo-eval` suite** — 20 local tasks for regression testing, baseline comparisons, and failure-mode analysis.
2. **Harbor adapter (`evals/harbor/`)** — external benchmark runs on Terminal-Bench and SWE-bench, including head-to-head comparisons against other CLI harnesses.

## Built-in `poo-eval` suite

The suite contains 20 tasks across three categories:

- **12 coding tasks**: Python packages with a bug and a failing `unittest` suite. Before grading, the harness restores the fixture's original test files so a run cannot pass by rewriting tests; test edits are reported as unexpected changes.
- **5 find-and-explain tasks**: Read-only repositories with one question and no files to change. `evals/tasks/check_answer.py` derives the required facts directly from the untouched workdir, asserts zero file edits, and verifies that the final message names every requested fact.
- **3 long-context tasks**: Working sets sized past a 32,768-token window and well under 262,144 tokens (`long-find-definition-in-tree`, `long-find-error-in-log`, `long-change-timeout-everywhere`). They test search discipline, narrowing large files, and multi-file edits without blind search-and-replace. (`long-change-timeout-everywhere` is the only task whose prompt expects test updates.)

Each task lives in `evals/tasks/<id>/` with a `task.json` and `check.sh` (exit `0` = solved). The model catalog is `evals/models.yaml`, and task repositories live under `evals/fixtures/`.

### Running and managing baselines

Run the suite against one or more models from `evals/models.yaml`:

```bash
poo-eval run --models gemma4,qwen --seeds 3
```

Each run writes its report and raw transcripts (`runs/`) to an ignored directory at `evals/runs/<date>-<git sha>/`. Nothing is committed until accepted:

```bash
# Promote a run's report.json, report.md, and summary.json to evals/baseline/
poo-eval accept evals/runs/<date>-<git sha>

# Compare a candidate run against the accepted baseline
poo-eval compare evals/runs/<date>-<git sha>

# Or compare two explicit run directories
poo-eval compare <baseline-dir> <candidate-dir>
```

`compare` writes its diff into the candidate directory. Commit updated `evals/baseline/` files in the same PR that produced the run; older baselines remain in git history.

### Isolation (`--isolation bwrap`)

`poo-eval run` wraps each `poo` process in `bubblewrap` by default (`--isolation bwrap`). Inside the sandbox, the process sees only:

- Read-only `/usr` and `/etc`
- Its own workdir at `/work`
- Its log directory at `/log`
- A private `/tmp` that also serves as `$HOME` (keeping user-level installs like `pip install --user` out of the run's diff)
- Its own PID namespace (closing `/proc` access to host processes)
- Shared network access so model endpoints remain reachable

The real home directory, repository, and task set are hidden. `check.sh` runs outside the sandbox so it can access the task set, which means pass/fail status is determined outside the model's reach. Workdirs are created under `<user cache dir>/poo-eval/work` (`--work-root` overrides this and rejects any path containing or inside the task set).

Passing `--isolation none` disables `bwrap` and runs model commands with your full privileges. Without sandboxing, a model that searches the filesystem can discover `check.sh` or `check_answer.py`, invalidating the result.

### Fixture integrity hashing

Before the first run, each invocation snapshots the fixtures its tasks use into `<work root>/<invocation>/fixtures/` and builds every workdir from that snapshot. The runner hashes each source fixture, its snapshot, and each task directory at startup and again after every run.

If any hash changes, the run is marked invalid (`source_changed`, `success: false`), a warning naming the run is written to `stderr`, and `poo-eval run` exits non-zero without restoring the modified source.

### Failure-mode study

For the recorded 240-run baseline (20 tasks × 4 models × 3 seeds) and cataloged harness and model pathologies, see [Observed Failure Modes](/reference/observed).

---

## Harbor adapter (`evals/harbor/`)

The Harbor adapter (`evals/harbor/`, added in PR #344) integrates `poo` with [Harbor](https://github.com/harbor-framework/harbor) via `poo_agent:PooAgent`. It uploads a static Linux `poo` binary into each task container, runs `poo run` headlessly, and reports token usage and session metrics back to Harbor.

### Prerequisites and building the static binary

1. Install Harbor (`uv tool install harbor`) and ensure a container runtime is available (Docker, rootless Podman with `podman.socket` and `DOCKER_HOST`, or a remote backend such as `--env modal` or `--env daytona`).
2. Build the static Linux binary (`CGO_ENABLED=0`) so it runs in any Linux task image with no libc dependencies:

```bash
evals/harbor/build-bin.sh
```

This writes `evals/harbor/bin/poo-linux-<arch>`. Re-run it after any change to `poo`.

### Running Terminal-Bench and SWE-bench

Run a quick smoke test on Terminal-Bench before running the full suite:

```bash
export OPENAI_API_KEY="sk-..."

PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent poo_agent:PooAgent \
  --model openai/gpt-4o \
  --ak context_limit=128000 \
  --ak max_turns=80 \
  --n-tasks 3 \
  --n-concurrent 2
```

The same adapter runs against SWE-bench Verified (`--dataset swebench-verified`) or any other Harbor dataset. For local or homelab OpenAI-compatible endpoints, pass `--ak endpoint=...` and `--ak model_id=...` using a LAN hostname or container host alias (`host.containers.internal` / `host.docker.internal`) rather than `localhost`:

```bash
PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent poo_agent:PooAgent \
  --ak endpoint=http://grace:4000/v1 \
  --ak model_id=qwen \
  --ak context_limit=131072 \
  --ak tools_mode=native \
  --n-tasks 3 \
  --n-concurrent 2
```

To pin compaction, sampling, or built-in providers, pass `--ak settings=/path/to/settings.yaml`. A settings run uses the model named in the file; overriding the model or window requires setting both `endpoint` and `model_id`.

### Paired harness comparisons

Harbor includes built-in adapters for other CLI harnesses, allowing head-to-head comparisons on the same dataset and model:

```bash
# Run poo
PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent poo_agent:PooAgent \
  --model openai/gpt-4o \
  --ak context_limit=128000 \
  --n-tasks 15 \
  --n-concurrent 4

# Run another harness (e.g., opencode, claude-code, codex) on the same slice
harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent opencode \
  --model openai/gpt-4o \
  --n-tasks 15 \
  --n-concurrent 4
```

Results and raw `session.jsonl` transcripts are written under Harbor's `jobs/` directory.

### Key `--ak` options

| Key | Default | Description |
| --- | --- | --- |
| `poo_bin` | `evals/harbor/bin/poo-linux-<arch>` | Path to static Linux `poo` binary (or `POO_HARBOR_BIN`) |
| `settings` | none | Local `poo` settings file uploaded into `/logs/agent/` |
| `endpoint` | inferred from `--model` | OpenAI-compatible base URL (or `POO_ENDPOINT`) |
| `model_id` | inferred from `--model` | Model ID (`--model-id`) |
| `api_key_env` | inferred from `--model` | Host env var forwarded into container (or `POO_API_KEY_ENV`) |
| `context_limit` | `131072` | Context window in tokens (`--context-limit`) |
| `tools_mode` | none | `native`, `text`, or `auto` (`--tools-mode`) |
| `reasoning_effort` | none | Passed to `--reasoning-effort` |
| `max_turns` | none | Turn cap (`--max-turns`); unset leaves the run uncapped |
| `max_output` | none | Output token cap (`--max-output`) |
| `truncation_retries` | none | Retries after an output-limit cut (`--truncation-retries`) |
| `profile` | `code` | `poo` profile (`--profile`) |
| `context_strategy` | none | Compaction strategy (`--context-strategy`) |
| `context_keep_recent` | none | Compaction verbatim tail in tokens (`--context-keep-recent`) |
| `timeout` | none | Wall-clock limit, e.g. `15m` (`--timeout`) |
| `append_system_prompt` | none | Extra system prompt text (`--append-system-prompt`) |

Run the adapter's unit tests with:

```bash
python3 -m unittest discover -s evals/harbor -v
```

### Benchmark note

In head-to-head Terminal-Bench runs on locally hosted `glm-5.3-flash`, `poo` beat `pi` and tied `mini-swe-agent`. This is not an exhaustive multi-model benchmark study, but it provides concrete evidence that `poo` drives relentless agent loops effectively on real terminal tasks.
