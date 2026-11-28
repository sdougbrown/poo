---
title: Context Compaction
description: How poo manages long sessions transparently with mine, pointer, summary, handoff, native, and none compaction strategies, the fit rule, and the dedicated compaction role.
---

# Context Compaction

Long sessions eventually exceed a model's context window or an endpoint's prompt limit. `poo` handles this through pluggable **compaction strategies** configured under `context.strategy` (default: `mine`).

## Transparent history

Compaction in `poo` changes only the **view** sent to the model — never what the session recorded.

- **Nothing is rewritten silently.** Every replacement starts with a visible marker (`[Context compacted: turns 1-N reconstructed below]` or `[Context compacted: turns 1-N summarized below]`), provider windows append `[context compacted by provider]`, and tool-result rewrites preserve their handle with a compacted marker.
- **The store stays whole.** Every original turn and tool result remains in the session log intact and recoverable.
- **Retention pointers and file lists.** Replacement strategies append a list of files touched in the covered turns (`read`, `edit`, `write`). This keeps the model from re-reading those files. `mine` and `pointer` also append a retention pointer with the session ID and replaced turn range. When enabled, it names the `history` tool to search or read those turns back on demand.
- **One form per compaction.** Core validates every compaction before applying it. A compaction takes exactly one of four forms: a turn-range replacement (`Replace`), a tool-result rewrite (`Rewrite`), a provider-native window (`Window`), or a run-ending handoff (`End`). If a compaction fails or is rejected, the run continues uncompacted when the next request still fits, or settles with `context_exhausted` when it does not.

See [Sessions & Durability](/operations/sessions-and-durability) for how session logs are persisted and replayed.

## When compaction fires (the fit rule)

Every strategy compacts when the next request no longer fits its budget. `poo` tracks two limits from the active model's metadata (see [Models & Catalog](/configuration/models-and-catalog)):

- **`context_limit`** — the model's total context window (prompt + output).
- **`input_limit`** — the largest prompt the endpoint accepts, if lower than `context_limit`.

The effective prompt ceiling (`PromptLimit`) is the smaller of the two (ignoring whichever is unset). After at least one turn has been sent, the fit rule triggers when either condition holds:

1. **Window room is short:** `estimated_input + output_need + 256 > context_limit`
2. **Input cap is exceeded:** `estimated_input + 256 > input_limit`

Where `256` is the fixed token reserve (`OutputReserve`), and `output_need` is the larger of the run's `OutputLimit` and the strategy's fork output limit (for strategies that fork: `mine`, `pointer`, and `handoff`). Because a fork re-sends the whole current window, its output limit counts toward the fit rule.

Additional guards:

- **Over-window start:** Block strategies (`mine`, `pointer`, `summary`) also fire immediately, even before a turn completes. They trigger if the view leaves under `256` tokens of output room (`MinOutputTokens`) or exceeds `input_limit`.
- **At least two live turns:** Block strategies never compact a single live turn; there must be older turns behind the active work.
- **Must restore room:** Block strategies check whether the replacement (plus its marker, file list, and retention pointer) would actually end the shortage. On a window too small for the verbatim tail plus replacement to recover room, compaction is skipped rather than looping every turn. Similarly, `native` declines to repeat if the provider's last compacted input already left the window short, letting the request gate settle `no_output_room`.
- **Small-window tail cap:** The verbatim tail (`keep_recent_tokens`) is automatically capped at 50% of `PromptLimit` so small-window models can still compact.

### Optional early trigger (`ratio`)

Setting `ratio` (a float between `0` and `1`) under `context.mine`, `context.pointer`, `context.summary`, or `context.handoff` adds an earlier trigger when `estimated_input >= ratio * PromptLimit` (after at least one turn). When unset, the fit rule alone decides.

::: warning Removed keys: `notice_ratio` and `hard_ratio`
`context.notice_ratio` and `context.hard_ratio` have been removed and have no effect. A settings file that still sets them loads with a warning. Use the fit rule (default) or a strategy-specific `ratio` instead.
:::

## Strategy comparison

| Strategy | How it works | When to use | Requirements | Trade-offs |
|---|---|---|---|---|
| `mine` *(default)* | Forks the chat model for advisory pointers, then reads the covered transcript in a fresh request to reconstruct task-shaped working context. | General-purpose long sessions where task state matters more than chronological order. | Model must handle two side requests (fork + ask). | Higher compaction token cost (reads covered transcript); best fidelity for complex work. |
| `pointer` | Forks the chat model for JSON search queries and notes. Runs literal searches over retained history and composes context from matches and seed without reading the full transcript. | Very long sessions or when using a smaller-window `roles.compaction` model. | Readable session history for query extraction; falls back to `mine` on failure. | Bounded compose input; can miss unqueried details (recoverable via `history` tool). |
| `summary` | Single fresh request reads the covered turns and writes a structured chronological checkpoint. | Conventional summarization with a smaller output budget and no fork pass. | Model must handle one side request (ask). | No fork pass and no retention pointer; chronological summary can lose non-linear details. |
| `handoff` | Forks the chat model to write a continuation note, then ends the run (`context_exhausted`) with the handoff text. | Orchestrators that prefer starting fresh sessions from an explicit handoff rather than mutating history. | Caller must handle `context_exhausted` and seed a new session. | Terminates the run; does not continue in-place. |
| `native` | Asks the provider's own compaction endpoint to produce an opaque compacted window and installs it as the view. | When using a provider that supports server-side state compaction. | Model declares `ops: [chat, compact]` and uses a transport implementing `NativeCompactor` (currently `openai-codex`). | Provider-locked; opaque state cannot be transferred across models or providers. |
| `none` | Disables compaction completely. | Short runs, benchmarks, or callers managing context externally. | None. | Run settles `context_exhausted` (`no_output_room` or `prompt_limit`) once the window fills. |

## Strategies in detail

### `mine` (default)

`context.strategy: mine` reconstructs the covered turns into a task-shaped working context using two requests:

1. **Pointer pass (fork):** Re-sends the last main request on `roles.chat` and asks what the replacement must preserve (turns, file paths, error strings, open threads). The pass is best-effort: if the fork fails, is truncated, or returns a tool call, `mine` proceeds without pointers because the transcript still holds everything.
2. **Mining pass (ask):** Sends the covered transcript, any prior replacement, and the fork's pointers to `roles.compaction` (or `roles.chat` if unset). Tool results are capped and reasoning blocks are stripped. A truncated reply or tool call is rejected rather than installed.

The replacement includes the marker line, the reconstruction, a list of read/modified files, and a retention pointer naming the session and replaced turns.

```yaml
context:
  strategy: mine
  mine:
    # ratio: 0.85            # optional early trigger; fit rule decides when unset
    keep_recent_tokens: 12000 # default: 12000
    max_tokens: 32768         # default: 32768
    # pointer_max_tokens: 16384 # optional; defaults to window-scaled output limit
```

| Key | Default | Description |
|---|---|---|
| `ratio` | unset (`0`) | Optional share of `PromptLimit` for an early trigger. |
| `keep_recent_tokens` | `12000` | Tokens of newest history kept verbatim (capped at 50% of `PromptLimit`). |
| `max_tokens` | `32768` | Output token limit for the mining request. |
| `pointer_max_tokens` | window-scaled | Output token limit for the fork pass. Defaults to `8192` (`< 64k` window), `16384` (`64k–128k` or unknown), or `32768` (`>= 128k`). |

### `pointer`

`context.strategy: pointer` keeps `mine`'s fork but makes the pointers load-bearing so the covered transcript is never read whole:

1. **Pointer pass (fork):** Asks the chat model for a JSON object of literal search `queries` (capped at 8) and `notes`. Requests JSON response format when supported; if the endpoint refuses the parameter, retries once without it. Replies are parsed leniently. If malformed prose returns, a small reformat ask on `roles.compaction` tries to recover JSON before treating the text as `notes`.
2. **Extraction:** Runs each query as a literal search (`regexp.QuoteMeta`) over the store's retained history, deduplicating matches up to `snippet_budget` bytes (cut cleanly on UTF-8 rune boundaries).
3. **Compose pass (ask):** Sends only the orientation seed (the last three non-empty user messages), extracted snippets, notes, and any prior replacement to `roles.compaction`. Because the full transcript is never sent, the compaction model's window bounds only the compose request, not the session's total size.

**Fallback to `mine`:** If the compose request fails, hits `max_tokens`, returns a tool call, or returns empty text, `pointer` falls back to `mine` with the same `ratio`, `keep_recent_tokens`, `max_tokens`, and `pointer_max_tokens`. If `escalate_after` is set to a positive integer `N`, once the session has run `N` compactions, all subsequent compactions delegate directly to the full-span `mine` fallback.

```yaml
context:
  strategy: pointer
  pointer:
    # ratio: 0.85            # optional early trigger; fit rule decides when unset
    keep_recent_tokens: 12000 # default: 12000
    max_tokens: 32768         # default: 32768
    # pointer_max_tokens: 16384 # optional; defaults to window-scaled output limit
    snippet_budget: 32768     # default: 32768 (bytes)
    # escalate_after: 3       # optional; default 0 (never escalate)
```

| Key | Default | Description |
|---|---|---|
| `ratio` | unset (`0`) | Optional share of `PromptLimit` for an early trigger. |
| `keep_recent_tokens` | `12000` | Tokens of newest history kept verbatim. |
| `max_tokens` | `32768` | Output token limit for the compose request. |
| `pointer_max_tokens` | window-scaled | Output token limit for the fork (and reform) pass. Same window scaling as `mine`. |
| `snippet_budget` | `32768` | Maximum total bytes of retained-history snippets collected across queries. |
| `escalate_after` | `0` (off) | Number of compactions a session runs before delegating all later compactions to `mine`. |

### `summary`

`context.strategy: summary` is the conventional single-request checkpoint strategy. It skips the fork pass and sends covered turns (plus any prior summary) to `roles.compaction` (or `roles.chat`). It replaces those turns with the summary, marker line, and file list, without a retention pointer.

```yaml
context:
  strategy: summary
  summary:
    # ratio: 0.85            # optional early trigger; fit rule decides when unset
    keep_recent_tokens: 20000 # default: 20000
    max_tokens: 4096          # default: 4096
```

| Key | Default | Description |
|---|---|---|
| `ratio` | unset (`0`) | Optional share of `PromptLimit` for an early trigger. |
| `keep_recent_tokens` | `20000` | Tokens of newest history kept verbatim. |
| `max_tokens` | `4096` | Output token limit for the summarization request. |

### `handoff`

`context.strategy: handoff` refuses to compact in-place. When triggered, it forks the last main request on `roles.chat` and asks the model for a handoff note. It then ends the run with `context_exhausted`, carrying that note in the settle detail. A truncated or empty reply is rejected. The caller can then seed a fresh session with the handoff note.

```yaml
context:
  strategy: handoff
  handoff:
    # ratio: 0.85   # optional early trigger; fit rule decides when unset
    max_tokens: 2048 # default: 2048
```

| Key | Default | Description |
|---|---|---|
| `ratio` | unset (`0`) | Optional share of `PromptLimit` for an early trigger. |
| `max_tokens` | `2048` | Output token limit for the handoff fork request. |

### `native`

`context.strategy: native` asks the provider's transport to compact its own conversation state and installs the returned provider window (followed by `[context compacted by provider]`) as the model's view. System instructions remain intact.

`native` is opt-in and has strict capability checks:

- The active model must declare `compact` in its `ops` list (`ops: [chat, compact]`).
- Its transport must implement `NativeCompactor`. In this repository, only the `openai-codex` Responses transport implements native compaction.
- Settings validation refuses `context.strategy: native` on unsupported transports such as Chat Completions. Mid-session model switches are re-checked when compaction runs: switching to a model without `ops: [..., compact]` refuses native compaction rather than calling an incapable provider.

```yaml
models:
  openai-codex/gpt-6.1-sol:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    ops: [chat, compact]
roles:
  chat: openai-codex/gpt-6.1-sol
context:
  strategy: native
```

`native` exposes no `context.native` sub-keys in settings; it triggers strictly on the fit rule.

### `none`

`context.strategy: none` disables compaction. The session runs until the request gate blocks a request for `no_output_room` or `prompt_limit`, settling the run with `context_exhausted`.

```yaml
context:
  strategy: none
```

## The `compaction` role

`roles.compaction` assigns a dedicated model to a strategy's own standalone requests (`Ask`), defaulting to `roles.chat` when unset (see [Roles and Required Operations](/configuration/models-and-catalog#roles-and-required-operations)):

```yaml
roles:
  chat: openai-codex/gpt-6.1-sol
  compaction: opencode-go/glm-5.3
```

- **What moves to `roles.compaction`:** The summarization request (`summary`), the mining request (`mine`), and the compose and reformat requests (`pointer`). The model must serve the `chat` operation and is built through the same catalog and transport machinery as other roles.
- **Why the fork stays on `roles.chat`:** The pointer pass (`mine`, `pointer`) and handoff pass (`handoff`) fork the last main request byte-for-byte with appended instructions. Only the chat model's transport can reuse that cached prefix and see the exact window the chat model just worked in. Forks therefore always run on `roles.chat`.

## Related pages

- [Settings Reference](/reference/settings) — complete schema for `context.*` and `roles.compaction`
- [Models & Catalog](/configuration/models-and-catalog) — configuring `context_limit`, `input_limit`, `max_output`, and `ops`
- [Sessions & Durability](/operations/sessions-and-durability) — session log retention and replay
- [Limits & Exit Codes](/operations/limits-and-exit-codes) — `context_exhausted`, `no_output_room`, and `prompt_limit`
- [Built-in Tools & Jobs](/capabilities/tools-and-jobs) — the `history` tool for reading retained turns
