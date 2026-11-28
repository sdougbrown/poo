---
title: Interactive REPL
description: Using poo repl — vi input mode, Bash Ctrl chords, slash commands, permission prompts, statusline, and continuing or resuming sessions.
---

# Interactive REPL

`poo repl` is poo's terminal interface. It accepts the same flags as `poo serve`, launches `poo serve` as a subprocess, and connects to it over the JSON-RPC wire protocol. Replies render as Markdown with syntax-highlighted code blocks, and tool calls and results render as decorated blocks.

## Starting the REPL

```bash
# Launch the default UI (repl)
poo

# Explicit subcommand with flags
poo repl --profile code --working-dir ./my-repo
```

Running bare `poo` launches the interface configured by `ui` in settings (defaulting to `repl`). If no settings file exists yet, bare `poo` runs an interactive setup flow in the terminal to configure a sign-in and chat model.

To use a different default profile in interactive sessions than in headless `poo run` / `poo serve` invocations, set `repl_profile` in your settings file.

When stdin and stdout are both terminals, the REPL pins the input editor and statusline to the bottom. The transcript scrolls above them. When piped, it falls back to plain line-oriented I/O.

## Input Editing and Keybindings

At a terminal, the REPL uses a readline editor in **vi insert mode** (`vi-insert`) and persists input history in `<user-cache-dir>/poo/repl-history-v2`.

### Control Keys and Bash Chords

| Key | Behavior |
|---|---|
| `Enter` | Submit the entire draft (including multi-line drafts). |
| `Ctrl-J` | Insert a newline into the draft without submitting. |
| `Esc` | Stop the active turn/run if working; switch to vi command mode (`vi-command`) when idle. |
| `Ctrl-C` | Cancel the active run if working; clear the current draft if non-empty; exit the REPL (exit code `130`) when the prompt is empty. |
| `Shift+Tab` | Cycle the chat model's reasoning level (same as `/thinking`). |
| `Ctrl-A` | Jump to the beginning of the line. |
| `Ctrl-E` | Jump to the end of the line. |
| `Ctrl-B` | Move cursor back one character. |
| `Ctrl-F` | Move cursor forward one character. |
| `Ctrl-T` | Transpose the two characters before the cursor. |
| `Ctrl-K` | Kill from the cursor to the end of the line. |
| `Ctrl-U` | Clear from the cursor to the start of the line. |
| `Ctrl-W` | Cut the word behind the cursor. |

### Steering an Active Run

Submitting a regular message (not a slash command) during an active run sends a **steer** message (`run/message`). The session queues it and delivers it to the model at the end of the current turn. Slash commands other than `/bg` wait until the run settles.

## Slash Commands

| Command | Description |
|---|---|
| `/model` | List available models in the catalog (numbered), marking the active chat model with `←`. |
| `/model <text>` | Switch to an exact model name, or filter the catalog by fragment. A single match switches immediately; multiple matches print a ranked, numbered list. |
| `/model <number>` | Switch to a model by number from the most recent `/model` or `/models` list. |
| `/models` | Probe configured endpoints for what they currently serve, refresh the catalog, and list the models. Supports `/models <text>` and `/models <number>`. |
| `/thinking` | Cycle the reasoning level (`default`, `minimal`, `low`, `medium`, `high`, `xhigh`, `off`). Same as `Shift+Tab`. |
| `/thinking <level>` | Set the reasoning level explicitly, or pass a token budget for budget-based models. `default` omits the reasoning field; `off` asks the model not to think. |
| `/bg` | Move the currently running backgroundable tool call (such as `bash`) into a session background job without restarting it. |
| `/less` | Open the session transcript in `less -R`. |
| `/help` | Show the command list (aliases: `/h`, `?`). |
| `/cancel` | Cancel the active run. |
| `/quit` | Close the session and exit the REPL (aliases: `/q`, `/exit`). |

::: tip Backgrounding with `/bg`
`/bg` only moves a call when exactly one backgroundable call is running. The promoted job survives run cancellation, but it keeps the original `bash` command timeout. For long-running work that needs the full job timeout, launch it with `background: true` from the start.
:::

## Permission Prompts

The REPL declares the `permission_answers` wire capability, so asks routed to the `wire` asker appear inline in the terminal:

```text
⚙ permission · bash
rm -rf build/
  allow bash? [y/N]
```

Type `y` or `yes` to allow the call. Any other input — including pressing `Enter` on an empty line or reaching EOF — denies the call. See [Permissions & Safety](/operations/permissions-and-safety) for configuring rules and askers.

## Statusline

The statusline sits on the bottom row of the pinned footer (or inline before the prompt when no pinned footer is active) and displays:

```text
⌂ my-repo │ ⎇ main │ gpt-6 │ 🧠 medium │ ctx 12.4k/128k
```

- **Folder (`⌂`)** — basename of the session's working directory.
- **Git branch (`⎇`)** — current branch or short commit hash when in a detached HEAD (omitted outside a git repository).
- **Model** — active chat model name.
- **Reasoning (`🧠`)** — active reasoning level (omitted when set to `default`).
- **Context fill (`ctx`)** — input token count from the most recent request against the model's prompt limit.

## Continuing and Resuming Sessions

Logged sessions can be reopened directly from the command line:

```bash
# Reopen the most recently active unlocked session for the current directory
poo repl --continue
poo repl -c
poo -c

# Reopen a specific session by ID
poo repl --resume <session-id>
poo --resume <session-id>
```

Find session IDs with `poo sessions list`.

When a session is reopened:

- It retains the working directory, profile, and model recorded in its log.
- The REPL renders up to the **newest 500 history items** as scrollback before the first prompt. None of the replayed scrollback is re-sent to the model.
- If the previous process crashed, any recovery actions (such as a truncated tail or interrupted runs) are shown as a warning.
- `--continue` skips sessions currently locked by another process.

### Refusals (Exit Code 2)

The REPL refuses to start and exits with code `2` when:

- Both `--continue` and `--resume` are passed together.
- `--continue` finds no unlocked logged session matching the working directory.
- `--ephemeral` or `session.mode: ephemeral` is active.
- `--resume <id>` targets a session whose recorded working directory differs from the current one (resume from that directory or pass `--working-dir <dir>`).
