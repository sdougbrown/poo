# Stage 7 eval gate notes

Run with `--sandbox bwrap --executor bwrap`. Success is 97.5% over 240 runs, against 94.2% at the Stage 1 baseline. This is the first gate in which `poo run` installs the `code` profile's default policy.

- **Doom-loop denials.** A model that repeats an exact call meets the doom-loop guard from the third repeat. `poo run` has no Asker, so the fallback denies, and the model sees the denial. gemma4 on `fix-cache-never-expires` repeated `cat cachekit/__init__.py` until the turn ceiling, giving 41 denied asks per run. The guard worked as the plan describes. No ordinary call was denied.
- **The flagged regression, MiniMax `input_tokens` (+44%).** MiniMax took the same number of turns as at Stage 6 (8.25 against 8.2). The mean is pulled up by one long task, `long-change-timeout-everywhere`, whose runs read 186k–405k tokens of large files. That's variance on that task, not a change in behavior.
