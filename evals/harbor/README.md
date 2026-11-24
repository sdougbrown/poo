# Running `poo` on Harbor (Terminal-Bench & SWE-bench)

This directory contains a [Harbor](https://github.com/harbor-framework/harbor)
adapter (`poo_agent:PooAgent`) that uploads a static Linux `poo` binary into
each task container, runs `poo run` headlessly, and reports token usage and
session metrics back to Harbor.

## 1. Prerequisites

1. **Install Harbor**:
   ```bash
   uv tool install harbor
   ```

2. **Container runtime (Docker or Podman)**:
   - On Docker: make sure the daemon is running (`docker ps`).
   - On rootless Podman (Linux): enable the Docker-compatible socket and export
     `DOCKER_HOST` before running `harbor`:
     ```bash
     systemctl --user enable --now podman.socket
     export DOCKER_HOST="unix://$XDG_RUNTIME_DIR/podman/podman.sock"
     ```
   - Or use a remote sandbox backend (`--env modal` / `--env daytona`) if you
     don't want local containers.

3. **Build the static Linux `poo` binary**:
   ```bash
   evals/harbor/build-bin.sh
   ```
   This builds `evals/harbor/bin/poo-linux-<arch>` with `CGO_ENABLED=0`, so it
   runs inside any Linux task image (Debian, Ubuntu, Alpine, etc.) with no libc
   dependencies. Re-run this script whenever you change `poo`.

## 2. Quick smoke test (3 tasks on Terminal-Bench 4.1)

Start small to verify container networking, auth, and tool execution before
spending tokens on the full 66-task suite:

```bash
export OPENAI_API_KEY="sk-..."

PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent-import-path poo_agent:PooAgent \
  --model openai/gpt-4o \
  --ak context_limit=128000 \
  --ak max_turns=80 \
  --n-tasks 3 \
  --n-concurrent 2
```

### Using a local / homelab OpenAI-compatible endpoint

Pass `--ak endpoint=...` and `--ak model_id=...` (note: inside a container,
`localhost` is the container itself, so use your router's LAN hostname like
`http://grace:4000/v1` or `http://host.containers.internal:4000/v1` /
`http://host.docker.internal:4000/v1`):

```bash
PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent-import-path poo_agent:PooAgent \
  --ak endpoint=http://grace:4000/v1 \
  --ak model_id=qwen \
  --ak context_limit=131072 \
  --ak tools_mode=native \
  --n-tasks 3 \
  --n-concurrent 2
```

### Using a `poo` settings file

If you want to pin compaction strategies, sampling, or built-in providers
(e.g. OpenCode Console or Codex), pass `--ak settings=...` (and optionally
`--ak auth_file=...`):

```bash
PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent-import-path poo_agent:PooAgent \
  --ak settings=/path/to/settings.yaml \
  --ak auth_file=/path/to/auth.json \
  --n-tasks 3
```

## 3. Paired harness comparison (`poo` vs OpenCode / Claude Code / Codex)

Harbor ships built-in adapters for other CLI harnesses, so you can hold the
model constant and compare pass rate, token usage, and wall-clock time on the
same tasks:

```bash
# Run poo
PYTHONPATH=evals/harbor harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent-import-path poo_agent:PooAgent \
  --model openai/gpt-4o \
  --ak context_limit=128000 \
  --n-tasks 15 \
  --n-concurrent 4

# Run OpenCode (or claude-code / codex) on the same dataset and model
harbor run \
  --dataset terminal-bench/terminal-bench@4.1.0 \
  --agent opencode \
  --model openai/gpt-4o \
  --n-tasks 15 \
  --n-concurrent 4
```

Results and raw `session.jsonl` transcripts land under Harbor's `jobs/`
directory.

## 4. Running SWE-bench Verified with the same adapter

The same adapter works on any Harbor dataset, including SWE-bench:

```bash
PYTHONPATH=evals/harbor harbor run \
  --dataset swebench-verified \
  --agent-import-path poo_agent:PooAgent \
  --model openai/gpt-4o \
  --ak context_limit=128000 \
  --n-tasks 10 \
  --n-concurrent 4
```

## Supported `--ak` options

| Key | Default | Description |
|---|---|---|
| `poo_bin` | `evals/harbor/bin/poo-linux-<arch>` | Path to static Linux `poo` binary (or `POO_HARBOR_BIN` env var) |
| `settings` | none | Local `poo` settings file uploaded into `/logs/agent/` |
| `auth_file` | none | Local `auth.json` uploaded into `/logs/agent/auth.json` |
| `endpoint` | inferred from `--model` | OpenAI-compatible base URL (or `POO_ENDPOINT` env var) |
| `model_id` | inferred from `--model` | Model ID passed to `--model-id` |
| `api_key_env` | inferred from `--model` | Host env var forwarded into the container (or `POO_API_KEY_ENV`) |
| `context_limit` | `131072` | Context window in tokens (`--context-limit`) |
| `tools_mode` | none | `native`, `text`, or `auto` (`--tools-mode`) |
| `reasoning_effort` | none | Passed to `--reasoning-effort` |
| `max_turns` | `100` | Turn cap (`--max-turns`) |
| `max_output` | none | Output token cap (`--max-output`) |
| `profile` | `code` | `poo` profile (`--profile`) |
| `context_strategy` | none | Compaction strategy (`--context-strategy`) |
| `context_keep_recent` | none | Compaction verbatim tail in tokens (`--context-keep-recent`) |
| `timeout` | none | Wall-clock limit, e.g. `15m` (`--timeout`) |
| `append_system_prompt` | none | Extra system prompt text (`--append-system-prompt`) |

## Adapter unit tests

```bash
python3 -m unittest discover -s evals/harbor -v
```
