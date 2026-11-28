---
title: CLI Registry & Script Tools
description: Inject custom CLI commands and non-CLI JSON scripts into poo without building a custom Go binary.
---

# CLI Registry & Script Tools

You do not need to build a custom Go binary to give `poo` new tools. Drop a JSON manifest into `~/.config/poo/cli-registry/`, or point `cli_registry.paths` at a manifest directory. `poo` compiles the argument schema and validates calls before starting a process. It then runs the command through the session's executor, sandbox, permissions, and background jobs.

The manifest format is powered by `github.com/sdougbrown/cli-registry` (`poo/cli` and `poo/tool/cliregistry`).

## Walkthrough: wrapping a CLI without recompiling

Suppose you have `writetighter` on `PATH` and want the model to lint prose with `writetighter lint`. Create a manifest at `~/.config/poo/cli-registry/writetighter.json`:

```json
{
  "version": 1,
  "tools": {
    "tighten-lint": {
      "description": "Run writetighter lint on a file or directory to catch prose issues.",
      "command": ["writetighter", "lint"],
      "inputSchema": {
        "type": "object",
        "properties": {
          "path": {
            "type": "string",
            "description": "File or directory to lint."
          },
          "kind": {
            "type": "string",
            "enum": ["pr", "plan", "doc", "commit"],
            "description": "Document kind for rule selection."
          }
        },
        "required": ["path"],
        "additionalProperties": false
      },
      "bindings": [
        {
          "property": "kind",
          "style": "space",
          "flag": "--kind"
        },
        {
          "property": "path",
          "style": "positional"
        }
      ],
      "output": {
        "mode": "raw"
      }
    }
  }
}
```

Start a session with a side-effecting profile (such as `code`):

- Because the manifest sits in `~/.config/poo/cli-registry/`, `poo` loads it automatically with no settings change.
- Because the catalog has 30 or fewer entries, `tighten-lint` is promoted to a direct tool: the model calls `tighten-lint({"path": "docs/README.md", "kind": "doc"})` directly.
- `poo` validates the arguments against `inputSchema` before spawning a process, builds `["writetighter", "lint", "--kind", "doc", "--", "docs/README.md"]`, and runs it through the session's executor.

::: tip Protecting flag-like values
Separate-value flag bindings (`style: "space"`) reject values that start with `-` at validation time so a model cannot inject extra flags. `equals` bindings (`--flag=value`) stay inside a single argv element, and `positional` bindings are separated with `--`.
:::

## Manifest discovery and `cli_registry.defaults`

`poo` loads manifests from two sources, in order:

1. **Default user directory**: `$XDG_CONFIG_HOME/poo/cli-registry/` (typically `~/.config/poo/cli-registry/`). A missing directory is ignored.
2. **Configured paths**: `cli_registry.paths` in settings, plus any paths added by `POO_CLI_REGISTRY_PATH` (OS path-list separated) or repeatable `--cli-registry-path` flags on `poo run`, `poo serve`, and `poo prompt`.

Relative paths in `cli_registry.paths` resolve against the working directory; paths starting with `~/` resolve under the home directory. Duplicate tool names across two manifests fail the load and name both files.

```yaml
cli_registry:
  paths:
    - ./tools/registry
    - ~/shared-manifests/git.json
  defaults: true   # default; set false to ignore ~/.config/poo/cli-registry/
```

To isolate a project, binary, or test from ambient user manifests, set `cli_registry.defaults: false`. A profile can override that setting with `cli_registry_defaults`:

```yaml
cli_registry:
  defaults: false

profiles:
  personal:
    base: code
    cli_registry_defaults: true
```

## Direct tools vs. `call_cli` (the 30-entry threshold)

How registered entries appear to the model depends on the catalog size after availability filtering:

| Catalog size | Tools offered | Prompt catalog section |
|---|---|---|
| `1` to `30` entries | One direct tool per entry (manifest name + `inputSchema`) | None (the tool list already carries every schema) |
| `> 30` entries with a [decision model](/capabilities/decision-model) | `call_cli` + `find_tool` | Search guidance pointing to `find_tool` |
| `> 30` entries without a decision model | `call_cli` | Full catalog listing every entry and argument hint |

Both shapes execute through the exact same runner, executor, sandbox, timeout, environment resolution, and observation cap.

When behind `call_cli`, the model invokes an entry by name:

```json
{
  "command": "tighten-lint",
  "args": {
    "path": "docs/README.md",
    "kind": "doc"
  }
}
```

With a decision model configured, `find_tool` takes `{"intent": "..."}` and asks the decider one yes/no question per entry. It returns the top 8 matches with their argument schemas and dispatcher (`call_cli` or `call_mcp`).

### Collision behavior

Direct-tool promotion is **all or nothing**. The entire catalog stays behind `call_cli` instead of promoting if any of the following is true:

- The catalog has more than 30 entries.
- Any entry's name collides with a built-in/linked tool, a core tool (`jobs`, `cancel_job`, `subscribe_feed`, `finish`, `call_cli`, `find_tool`), or another tool the profile already offers. A manifest never shadows an existing tool.
- The profile explicitly names `call_cli` or `find_tool` itself.

::: info Reserved `background` argument
Direct tools support `background: true` by default. If a manifest's `inputSchema` already defines a property named `background`, that tool keeps its own argument and runs foreground-only rather than failing the session.
:::

## Disabling the registry

To turn off the CLI registry for a session or profile without deleting manifests, disable `call_cli`:

```yaml
tools:
  disabled: [call_cli]
```

Disabling `call_cli` suppresses both direct-tool promotion and `call_cli` itself.

## Availability gating (`profile_side_effecting`)

Without availability rules, `poo` offers the registry only to side-effecting profiles (such as `code`, not `search`). This default protects read-only profiles because arbitrary CLI commands can modify state.

Manifests can declare an Umpire `availability` block to gate entries per profile. `poo` supplies one boolean condition:

- `profile_side_effecting` (`boolean`): `true` when the profile offers any side-effecting tool, `false` for read-only profiles.

Once manifests declare `availability` conditions:

- Read-only profiles (such as `search`) earn the registry and receive only the entries whose rules leave them available.
- Profiles with no tools at all still receive no registry entries.
- Declaring any condition other than `profile_side_effecting`, or declaring `profile_side_effecting` as a type other than `boolean`, fails the build.

## Execution, environment, and permissions

Every registered command runs where `bash` runs:

- **Executor & sandbox**: Runs through the session's configured executor (local or [bubblewrap](/operations/sandbox)), in the session's working directory unless the manifest fixes a directory.
- **Environment**: Manifest `env` bindings accept literals, `env:NAME`, and `session:id`. `env:NAME` reads from the session's scrubbed environment, so variables removed by `tools.env.scrub` or excluded by `tools.env.passthrough` are unavailable. `session:id` resolves at call time to the session's ID and stays stable across resume. See [`examples/avenor/tools.json`](https://github.com/sdougbrown/poo/blob/main/examples/avenor/tools.json) for a multi-tool manifest using `session:id` and `stdin`/`equals`/`positional` bindings.
- **Permissions**: Registered tools are statically `side_effecting`. Behind `call_cli`, the primary argument is `command`, so [permission rules](/operations/permissions-and-safety) can match by command prefix. When promoted to direct tools, rules match by the tool's own name.
- **Background jobs**: Registered tools accept `background: true` and support `/bg` promotion in the REPL.

## Non-CLI JSON script tools

A registered tool does not have to wrap an existing CLI. Using a `json` stdin binding and `output.mode: "json"`, any script in any language becomes a typed tool:

```json
{
  "version": 1,
  "tools": {
    "sum_pair": {
      "description": "Sum a pair of numbers.",
      "command": ["python3", "fn_notes.py"],
      "inputSchema": {
        "type": "object",
        "properties": {
          "data": {
            "type": "object",
            "properties": {
              "a": { "type": "number" },
              "b": { "type": "number" }
            },
            "required": ["a", "b"],
            "additionalProperties": false
          }
        },
        "required": ["data"],
        "additionalProperties": false
      },
      "bindings": [
        {
          "property": "data",
          "style": "json"
        }
      ],
      "output": {
        "mode": "json"
      }
    }
  }
}
```

The contract is three rules:

1. **Stdin**: `style: "json"` serializes an object- or array-valued property as JSON onto the script's stdin. The bound property must be a concrete `object` or `array` in `inputSchema`, and a tool may have at most one `stdin` or `json` binding. An optional `flag` is emitted only when the property is present.
2. **Stdout**: `output.mode: "json"` parses the script's stdout as JSON and returns it directly as the tool result (bypassing the `bash` exit-code envelope). Any stderr written on a successful exit is appended after a newline.
3. **Errors**: A nonzero exit (outside `output.success_exit_codes`) returns a recoverable error envelope containing stdout, stderr, and the exit code.

::: warning `null` and over-cap JSON results
Two edge cases fall back to the standard exit-code envelope. This happens when stdout decodes to JSON `null`, or when the serialized result exceeds the observation cap. If a script can return an empty result, return an object such as `{"result": null}` instead of bare `null`.
:::

Non-CLI script tools join the exact same catalog, 30-entry promotion threshold, schema validation, permission policy (`side_effecting`), sandbox, and background job machinery as CLI entries. The `cli-registry` repository also ships a small Python helper (`helpers/python/poo_fn.py`) and runnable example (`examples/fn.json` + `examples/fn_notes.py`), though any executable that reads JSON from stdin and writes JSON to stdout works.

## Related

- [MCP Servers](/capabilities/mcp-servers) — connect persistent stdio MCP servers and resource feeds
- [Built-in Tools & Jobs](/capabilities/tools-and-jobs) — built-in tools, observation caps, and background jobs
- [Manifests Reference](/reference/manifests) — full manifest schemas for CLI registry, feeds, and MCP
- [Build Your Own Binary](/extending/custom-binary) — when you actually want a custom Go binary with custom tools
