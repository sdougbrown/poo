---
title: Quickstart
description: Run your first poo session in 60 seconds against a local OpenAI-compatible endpoint or a built-in provider, verify roles, and launch the REPL.
---

# Quickstart

You can run a headless session in under a minute with either a local OpenAI-compatible server or a built-in cloud provider.

## Option A: Local OpenAI-compatible endpoint

No settings file is required. Pass `--endpoint` and `--model-id` directly to `poo run`:

```bash
poo run \
  --endpoint http://localhost:4000/v1 \
  --model-id gemma4 \
  --context-limit 262144 \
  --max-turns 8 \
  --log session.jsonl \
  "List the Go packages in this repository."
```

`poo` runs the session to completion, writes the structured transcript to `session.jsonl`, and exits with a code reflecting the settle reason.

## Option B: Built-in provider

`poo` ships built-in provider records for OpenCode Console, Codex (`openai-codex`), and Ollama Cloud. Authenticate once with `poo login`:

```bash
# Pick one:
poo login opencode
poo login codex
poo login ollama-cloud
```

Credentials are stored in `poo`'s default credential file (`~/.config/poo/auth.yaml` on Linux, `~/Library/Application Support/poo/auth.yaml` on macOS). Once authenticated, assign a model to the [`roles.chat` role](/configuration/models-and-catalog#roles-and-required-operations) in your settings file:

```yaml
# .poo/settings.yaml
version: 1
profile: code
roles:
  chat: opencode-go/glm-5.3        # or openai-codex/gpt-6.1-sol, or ollama-cloud/glm-5.3
```

## Starter settings files

The repository includes annotated starter configs under `examples/`:

| File | Use case |
|---|---|
| `examples/code.settings.yaml` | Local OpenAI-compatible endpoint (`gemma4`), `code` profile, `decide` asker, `bwrap` executor, `summary` compaction. |
| `examples/sandboxed.settings.yaml` | `code` profile with guardrails: `unmatched: ask`, `bwrap` with `network: false`, `protected_paths`, `restrict_to_working_dir`, and `fetch` disabled. |
| `examples/opencode.settings.yaml` | OpenCode Console gateway (`opencode-go/glm-5.3`), no `endpoints` or `models` block needed. |
| `examples/codex.settings.yaml` | Built-in `openai-codex` provider with `native` compaction and model-family prompt guidance. |

Copy one to `.poo/settings.yaml` in your project root (or `~/.config/poo/settings.yaml` as your user default), or pass it explicitly with `--settings`:

```bash
poo run --settings examples/code.settings.yaml "Run go test ./... and summarize any failures."
```

## Preflight with `poo roles`

Before starting a session, verify that every [role](/configuration/models-and-catalog#roles-and-required-operations) in your settings resolves to a valid model and credential:

```bash
poo roles
```

`poo roles` prints each assigned role, the provider serving it, and where that provider's credential comes from — without printing secrets or making network calls. If a role cannot resolve, it exits non-zero and lists the models that could serve it.

## Launch the interactive REPL

With a settings file in place, run a bare `poo` to open the interactive REPL:

```bash
poo
```

`poo` discovers settings automatically in order: `--settings`, `POO_SETTINGS`, `.poo/settings.{json,yaml,yml,toml}` walking up from the current directory, then your user config directory. See [Interactive REPL](/getting-started/repl) for slash commands and keybindings.

## Next Steps

- **Tune reasoning effort and sampling** for custom or local models: [Reasoning & Sampling](/configuration/reasoning-and-sampling)
- **Run async event feeds in a resident daemon**: [Feeds & Daemon](/capabilities/feeds-and-daemon)
- **Add a CLI tool without rebuilding `poo`**: [CLI Registry & Script Tools](/capabilities/cli-registry)
- **Lock down bash and filesystem access**: [Sandboxing (`bwrap`)](/operations/sandbox)
