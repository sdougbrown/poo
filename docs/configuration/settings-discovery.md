---
title: Settings Discovery
description: How poo locates settings files, selects parsers, validates keys, and resolves relative paths.
---

# Settings Discovery

Every poo command loads a single settings file—there is no merging across project and user settings files. If discovery finds no file, poo runs on defaults that CLI flags can override.

For the full list of supported keys, see the [Settings Reference](/reference/settings).

## Discovery Order

Commands check these locations in order and stop at the first match:

1. **CLI flag (`--settings <path>`)**: Uses the exact path given. If the file does not exist, poo fails immediately.
2. **Environment variable (`POO_SETTINGS`)**: Uses the exact path in `POO_SETTINGS`. If the file does not exist, poo fails immediately.
3. **Project directory (`.poo/`)**: Starts in the working directory (or current directory when empty) and walks up to the filesystem root, checking `<dir>/.poo/` at each level.
4. **User config directories**:
   - **Linux**: `$XDG_CONFIG_HOME/poo` when set, otherwise `~/.config/poo`.
   - **macOS**: `~/Library/Application Support/poo` first, then `~/.config/poo`.
   - **Windows**: `%AppData%\poo`.

::: tip Explicit paths are promises
Missing files in `.poo/` or user config directories are skipped silently. A missing path passed via `--settings` or `POO_SETTINGS` always fails the command.
:::

## File Extension Priority & Formats

Inside each searched directory, poo looks for these filenames in order:

1. `settings.json`
2. `settings.yaml`
3. `settings.yml`
4. `settings.toml`

The first regular file found wins. A `settings.json` in the same directory shadows `settings.yaml`, `settings.yml`, and `settings.toml`.

The file extension selects the parser:

| Extension | Parser |
|---|---|
| `.yaml`, `.yml` | YAML |
| `.toml` | TOML |
| `.json` (or any other / no extension) | JSON |

All three formats normalize to the same canonical JSON representation before decoding, so keys, types, and validation rules behave identically across JSON, YAML, and TOML.

YAML scalar keys are read as their text. An unquoted `true:` or `false:` and an unquoted integer key such as `0:` become the strings `"true"`, `"false"`, and `"0"`. A key written either way names the same setting. Two keys in one mapping that spell the same text, such as `True:` and `"true":`, are refused. The normalization applies to every settings YAML file, hooks included.

## Strict Unknown-Key Rejection

Settings are decoded strictly (`DisallowUnknownFields`). Any unrecognized key—or a key with the wrong value type—fails the load and names the offending field.

Validation runs in two stages after decoding:

1. **Schema rules** (`config/schema/settings.umpire.json`): Enforces required fields (`version: 1`), value ranges and enums, mutual exclusions (such as `skills.default` vs `skills.user_path`, and `system_prompt.base_file` vs `system_prompt.replace`), and cross-field dependencies (such as `permission.decide.*` requiring `roles.decider`).
2. **Runtime checks**: Validates role names, permission rule structure, asker names, protected path globs, engine-specific limits, and the existence of referenced files (`system_prompt.base_file`, `finish.schema_file`, `session.user_prompt_file`, `pi_auth_file`).

### Legacy Key Aliases

Three pre-v1 keys are still accepted and rewritten automatically when the current key name is absent:

| Legacy key | Current key | Scope |
|---|---|---|
| `cliregistry` | `cli_registry` | Any depth |
| `pi_models_file` | `models_file` | Any depth |
| `base_url` | `endpoint` | Entries inside `endpoints` only |

## Relative Path Resolution Rules

Absolute paths are always kept as-is. Leading `~` or `~/` expands to the current user's home directory (`$HOME`) across all path keys (`~user` is not expanded and is treated as a relative path).

Relative paths resolve against different base directories depending on the key:

### Resolved against the settings file's directory

These keys point to companion configuration files that travel with the settings file:

- `models_file`
- `auth_file`
- `pi_auth_file`
- `profiles_files`
- `system_prompt.base_file`
- `finish.schema_file`
- `profiles.<name>.prompt_file` (when declared inline in the settings file)
- `profiles.<name>.prompt_append_file` (when declared inline in the settings file)

When `config.Parse` parses a settings document in memory with no file path, relative paths in these keys stay relative to the working directory.

### Resolved against the declaring profiles file's directory

When a profile loads from an external file in `profiles_files`, these keys resolve against **that profiles file's directory** rather than the main settings directory:

- `profiles.<name>.prompt_file`
- `profiles.<name>.prompt_append_file`

### Resolved against the working directory

These keys point to project-scoped manifests and guidance directories:

- `cli_registry.paths`
- `feeds.paths`
- `mcp.paths`
- `skills.paths`

Leading `~` or `~/` in any of these lists resolves under the user's home directory instead of the working directory.

### Home expansion only (relative to process working directory)

These keys expand a leading `~` or `~/` and otherwise pass through relative to the process working directory:

- `session.dir` (defaults to `~/.local/state/poo/sessions`)
- `session.user_prompt_file`
- `skills.user_path`

### Default Companion File Discovery

When optional companion keys are omitted, poo checks beside the settings file or in the platform user config directory:

| Resource | Default behavior when omitted |
|---|---|
| `models_file` | Loads `models.yaml` or `models.json` beside the settings file (having both in the same directory is an error). |
| `auth_file` / `pi_auth_file` | Uses `auth.yaml` or `auth.json` beside the loaded `models_file`, or `auth.yaml` (falling back to legacy `auth.json`) in the user config directory. On macOS, an existing `~/.config/poo/auth.yaml` (or `auth.json`) stands in when the native `~/Library/Application Support/poo/` file is absent. |
| `cli_registry.defaults`, `feeds.defaults`, `mcp.defaults` | Enabled (`true`) by default; loads `cli-registry/`, `feeds/`, and `mcp/` from the user config directory (with `~/.config/poo/` fallback on macOS). |
| `session.user_prompt` | Reads `AGENTS.md` from the native user config directory (`~/Library/Application Support/poo` on macOS). |
