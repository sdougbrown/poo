# Stage 10 eval gate notes (after fix passes 3–5 and the reconciliation)

Run with `--sandbox bwrap --executor bwrap` at `73f7e44`. Since the first Stage 10 gate (`2026-10-23-074758-d32d1d6`), this branch has gained:
- `call_cli` running through the session's executor (fix passes 3–5);
- an executor contract change: `Command.Stderr` and `Process.Stopped`, and a context that has already ended now starts nothing;
- bwrap sandboxes that die with poo;
- the Stage 3 review fixes merged in from `main`.

Success is 96.7% over 240 runs, the same as the first Stage 10 gate. Against that gate, the comparison finds no difference above the noise floor. Against the Stage 1 baseline it flags one regression and two improvements:

| model | metric | baseline | first Stage 10 gate | this run |
|---|---|---|---|---|
| gemma4-think | input tokens | 25.9k | 39.4k | 30.0k |
| gemma4-think | output tokens | 1508 | 912 | 653 (better) |
| qwen | input tokens | 10.0k | 20.6k | 17.3k (flagged) |
| qwen | output tokens | 687 | 423 | 409 (better) |

- **gemma4-think:** no run reached `max_turns`; the first Stage 10 gate had 2 and Stage 9 had 5. It averages 10.2 turns.
- **qwen:** its turns match the baseline (6.1 against 5.5). Its extra input comes from the larger per-request prompt. qwen has no prompt cache here, so every turn resends the `code` profile's tool schemas.

Failures:
- `find-off-by-one-loop` for both gemma4 models (0/3). They have failed it at every gate since the baseline.
- One gemma4 `fix-cache-never-expires` run that reached `max_turns`.
- One minimax `fix-retry-backoff` run.
