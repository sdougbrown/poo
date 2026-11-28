---
title: Skills
description: Offer progressive-disclosure SKILL.md guidance directories without bloating the system prompt.
---

# Skills

Skills are reusable task instructions stored as `SKILL.md` files on disk. Instead of injecting every skill's full body into the system prompt, poo uses progressive disclosure. The prompt lists a compact catalog of skill names, one-line descriptions, and file paths. The model reads a skill's `SKILL.md` with `read` only when its description matches the task.

Offering thirty skills costs thirty catalog entries in the system prompt rather than thirty full documents.

## Directory layout and `SKILL.md` format

Each configured skills directory is scanned one level deep for subdirectories containing a `SKILL.md` file. Symlinks to directories are followed, so shared skill collections work without copying files.

```text
~/.agents/skills/
  release-checklist/
    SKILL.md
    scripts/check.sh
  sql-review/
    SKILL.md
```

A `SKILL.md` begins with YAML front matter delimited by `---`:

```markdown
---
description: Run the pre-release verification checklist before tagging a version.
---

# Release Checklist

1. Run `go test ./...` and confirm a clean working tree.
2. Check `./scripts/check.sh` against the target tag.
```

- **Skill name:** Derived from the subdirectory name (for example, `release-checklist`).
- **`description`:** Required for a skill to appear in the catalog. Multi-line descriptions are collapsed to a single line, trimmed to the first sentence, and capped at 200 runes.
- **Relative references:** Skills can reference companion files such as `scripts/check.sh`. The prompt instructs the model to resolve relative paths against the skill's directory (the parent of `SKILL.md`).
- **Filtering and errors:**
  - A subdirectory without `SKILL.md`, without YAML front matter, or with an empty `description` is silently skipped.
  - A `SKILL.md` whose YAML front matter fails to parse causes the build to fail.
- **Name collisions:** Directories are scanned in order (`user_path` or `default` first, then `skills.paths`, then `POO_SKILLS_PATH`). When two directories define the same skill name, the first directory wins. The rendered catalog is sorted alphabetically by skill name.

## Configuration

Configure skill discovery under `skills` in your settings file:

```yaml
skills:
  default: true
  paths:
    - .agents/skills
    - ~/team-skills
```

| Field | Type | Behavior |
| --- | --- | --- |
| `skills.default` | bool | When `true`, loads skills from `~/.agents/skills`. If that directory does not exist, it is silently ignored. Mutually exclusive with `skills.user_path`. |
| `skills.user_path` | string | Path to your personal skills directory (supports `~/`). If the path is missing or not a directory, poo emits a warning and skips it. Mutually exclusive with `skills.default`. |
| `skills.paths` | list of strings | Additional skill directories, resolved relative to the working directory (or `~/`). Missing or non-directory paths are dropped with a warning rather than failing the build. |

You can also append skill directories via the `POO_SKILLS_PATH` environment variable (OS path-list separated).

Inspect the resulting `skills` prompt section with [`poo prompt --pretty`](/configuration/profiles-and-prompts#inspecting-the-rendered-prompt).

## Hiding a skill from model invocation

Set `disable-model-invocation: true` in a skill's front matter to keep it out of the model's `<available_skills>` catalog:

```markdown
---
description: Force-push and rewrite release tags after a botched publish.
disable-model-invocation: true
---
```

The model is never told the skill exists and will not proactively invoke it. A user can still explicitly ask the model to read and follow that `SKILL.md` file when needed.

## Interaction with restricted `read`

When `tools.read.restrict_to_working_dir: true` confines the `read` tool to the session's working directory (see [Permissions & Safety](/operations/permissions-and-safety)), skill files typically live outside that directory (such as `~/.agents/skills`).

poo automatically registers every loaded skill directory as an additional read root for the session's `read` tool:

- The model can read any `SKILL.md` and companion files inside loaded skill directories even when `read` is restricted to the working directory.
- This applies to every loaded directory, even when none of its skills appear in the catalog. Even if every skill sets `disable-model-invocation: true`, the directory stays readable so a user can direct the model to follow it.
- `tools.protected_paths` is still enforced and continues to block protected files.
