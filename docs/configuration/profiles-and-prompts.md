---
title: Profiles & Prompts
description: Configure built-in and named profiles, profile layering, system prompt sections, user and project instructions, and prompt inspection.
---

# Profiles & Prompts

A profile defines the tool set, default permission stance, prompt guidance, and manifest defaults a session starts with. The system prompt is assembled from named markdown sections that can be overridden per profile, in settings, or via CLI flags.

## Built-in profiles

Select a profile with `--profile`, the top-level `profile` setting, or `session/create.profile` over the wire protocol. The default is `code`.

| Profile | Tools | Unmatched side-effecting calls | Notes |
| --- | --- | --- | --- |
| `base` | none | policy default | Minimal contract; useful for pure chat or as a base for custom profiles. |
| `code` | `bash`, `read`, `grep`, `history`, `edit`, `write`, `fetch`, `codemode` | `allow` | Default profile. Ships coding-agent guidance in its `profile` section. |
| `search` | `read`, `grep` | `allow` | Read-only exploration profile. |

Additional tools are attached automatically when applicable:

- `jobs` and `cancel_job` are added by the core whenever a profile links a tool that supports background execution (such as `bash`).
- `finish` is added when `finish.schema_file` is configured.

## Disabling tools globally

Use `tools.disabled` to strip tools from every profile a settings file runs under:

```yaml
tools:
  disabled: [codemode, fetch]
```

- Naming a tool the active profile does not offer is ignored.
- Naming a tool the binary does not link, or a core-managed tool (`jobs`, `cancel_job`, `finish`), is an error.
- Disabling every tool a selected profile offers is an error, and such a profile is omitted from the binary's profile list.

## Named profiles

Define custom profiles under `profiles` in your settings file, then select them by name anywhere a profile is accepted:

```yaml
profiles:
  reviewer:
    base: code
    prompt_file: prompts/reviewer.md
    prompt_append: Focus on correctness and regressions.
    model_family: true
    feeds: [apron-room]
    feeds_defaults: false
    cli_registry_defaults: false
    mcp_defaults: false
    mcp_servers: [github]
    hooks: [pre-push-gofmt]
    tools:
      disabled: [edit, write, bash, codemode]
```

### Profile fields

| Field | Type | Behavior |
| --- | --- | --- |
| `base` | string | Required. Names a built-in linked profile or another defined profile. |
| `prompt_file` | string | Absolute path, `~/...`, or path relative to the settings file (or profiles file). Read when opening a new session, not when listing profiles. Replaces the `profile` prompt section without replacing `base` or tool sections. |
| `prompt_append` | string | Inline text appended to the `profile` section after the base's prose (or after `prompt_file` if both are set). Mutually exclusive with `prompt_append_file`. |
| `prompt_append_file` | string | File form of `prompt_append`, resolved against the same directory as `prompt_file`. Mutually exclusive with `prompt_append`. |
| `model_family` | bool | Off unless set to `true`. Enables the `family` prompt section keyed by the chat model's `prompt_variant` (ships `gpt-5.6`, `gpt-6`, and `gpt-6.2`). An unknown or unset variant leaves the prompt unchanged. |
| `feeds` | list of strings | Feed manifest names this profile declares it needs; adds to the base's feeds (deduplicated). This is a presence assertion, not a filter: `feeds.paths` still controls which feeds load, and every loaded feed runs. A declared feed that is not loaded is skipped with a warning rather than failing the session. |
| `feeds_defaults` | bool | Overrides `feeds.defaults` for sessions under this profile. Omitting it keeps the top-level setting. |
| `cli_registry_defaults` | bool | Overrides `cli_registry.defaults` for sessions under this profile. Omitting it keeps the top-level setting. |
| `mcp_defaults` | bool | Overrides `mcp.defaults` for sessions under this profile. Omitting it keeps the top-level setting. |
| `mcp_servers` | list of strings | [MCP servers](/capabilities/mcp-servers) this profile's sessions start, by server name; adds to the base's selection (deduplicated, first-appearance order). `mcp.paths` still controls which manifests load; a selected server that is not loaded is skipped with a warning, and no server starts without a selection. |
| `hooks` | list of strings | [Decision hooks](/capabilities/decision-hooks) this profile runs, by name; adds to the base's chain (deduplicated, first-appearance order). A name the settings do not declare fails the build. |
| `hooks_disabled` | list of strings | Hook names dropped from this profile's chain, including ones the base runs. A name the profile both runs and turns off fails the build. |
| `tools.disabled` | list of strings | Tool exclusions added on top of global `tools.disabled`. Exclusions do not alter [permission rules](/operations/permissions-and-safety) or sandbox the remaining tools. |

The hooks chain grows down the inheritance line: a profile runs its base's hooks, then its own, and `hooks_disabled` removes names at any depth. A profile that ends up with hooks needs `roles.decider` set, because every hook asks the decision model.

An explicit `system_prompt.replace`, or a `system_prompt` rule that sets or removes the `profile` section, takes precedence over `prompt_file`, `prompt_append`, and `prompt_append_file`. A resumed session keeps the prompt recorded in its log once it has run.

## Layering with `profiles_files`

To keep large profile definitions out of your main settings file, move them into separate files listed under `profiles_files`:

```yaml
profiles_files:
  - profiles.yaml
  - team/profiles.yaml
```

Each file uses the same vocabulary under a top-level `profiles` key:

```yaml
profiles:
  slim:
    base: code
    feeds_defaults: false
    cli_registry_defaults: false
    mcp_defaults: false
```

- **Format:** Determined by file extension — YAML (`.yaml` / `.yml`) or JSON (`.json`).
- **Strict validation:** Unknown keys, unreadable files, or a file declaring zero profiles fail the load.
- **Precedence:** The settings file's own `profiles` map wins over `profiles_files`. Within `profiles_files`, the first file that defines a profile name wins over later files.
- **Path resolution:** Relative entries in `profiles_files` resolve against the settings file's directory. Relative `prompt_file` and `prompt_append_file` paths inside a profiles file resolve against that profiles file's own directory, so a profiles file travels with its prompts.

## REPL default profile (`repl_profile`)

`repl_profile` sets the profile used by the [interactive REPL](/getting-started/repl) when invoked without `--profile`:

```yaml
profile: code
repl_profile: reviewer
```

- Scoped to the REPL only: `poo run` and `poo serve` continue to use `--profile` or the top-level `profile` setting.
- Applies only when the REPL starts a new conversation; a reopened session runs under the profile recorded in its session log.
- Must name a profile known to the binary.

## System prompt sections (`system_prompt`)

The system prompt is composed from named sections rendered in this order:

1. `persona` — single-line identity header
2. `base` — core operating contract
3. `profile` — active profile's instructions
4. `family` — model-family guidance (when `model_family: true`)
5. `tools` — per-tool sections (`tools.<name>`) in tool offer order
6. `catalog` — [CLI registry](/capabilities/cli-registry) catalog (when applicable)
7. `skills` — [skills](/capabilities/skills) catalog
8. `environment` — working directory, OS, and session date (`YYYY-MM-DD`)
9. `user` — operator instructions (`session.user_prompt` / `session.user_prompt_file`)
10. `project` — project instructions (`session.project_prompt`)
11. Custom sections added via `system_prompt.sections` (in insertion order)

Configure overrides under `system_prompt`:

```yaml
system_prompt:
  base_file: prompts/base.md
  append: Always cite file paths with line numbers.
  sections:
    style: Keep answers under five sentences.
  remove:
    - environment
```

| Field | Behavior |
| --- | --- |
| `base_file` | Replaces the `base` section with the file's contents. Relative paths resolve against the settings file; `~/` resolves under the home directory. Mutually exclusive with `replace`. CLI override: `--system-prompt`. |
| `replace` | Replaces the entire prompt with literal text, dropping the default sections. Mutually exclusive with `base_file`. |
| `append` | Appends text to the `base` section. CLI override: `--append-system-prompt` (prefix with `@` to read from a file). |
| `sections` | Map of `name: text` entries that replace an existing section or append a new custom section. |
| `remove` | List of section names to drop. Removing a section that is not present is a no-op. |

For embedding-level prompt customization with Go `fs.FS`, see [Custom Tools & Prompts](/extending/custom-tools-and-prompts).

## Project and user instructions

### `session.project_prompt`

Enable per-project instructions with `session.project_prompt: true` (or `--project-prompt`):

```yaml
session:
  project_prompt: true
```

- Reads `AGENTS.md`, or `CLAUDE.md` if `AGENTS.md` is absent, from the session's working directory only — it never walks up to parent directories.
- Populates the `project` section and is read per session from that session's working directory.
- Capped at 64 KiB (`65536` bytes); longer files are truncated at a UTF-8 rune boundary with a truncation notice.
- A directory with neither file adds no section; an unreadable or non-regular file is an error.

### `session.user_prompt` and `session.user_prompt_file`

Add operator-level instructions that apply across projects to the `user` section:

```yaml
session:
  user_prompt: true
  # Or point to an explicit file (enables user instructions on its own):
  # user_prompt_file: ~/.config/poo/my-instructions.md
```

- Read once when building the runtime, not per session.
- `session.user_prompt: true` reads `AGENTS.md` from poo's platform config directory. If that default file does not exist, it is silently skipped.
- `session.user_prompt_file` (or `--user-prompt-file`) names an explicit file (supports `~/`) and enables user instructions without setting `session.user_prompt: true`. Because an explicit path is a promise, a missing file fails the build.
- Precedence: `--user-prompt-file` > `session.user_prompt_file` > `session.user_prompt`.

## Inspecting the rendered prompt

Run `poo prompt` to print the exact rendered system prompt a session would send to the model:

```bash
poo prompt --profile code --project-prompt
```

Pass `--pretty` for human-readable output with `--- <section> ---` headers and a compact skill summary instead of the raw `<available_skills>` XML:

```bash
poo prompt --pretty --profile reviewer
```

`poo prompt` honors `--settings`, `--profile`, `--project-prompt`, `--user-prompt-file`, `--system-prompt`, and `--append-system-prompt`, and works without a configured chat model.
