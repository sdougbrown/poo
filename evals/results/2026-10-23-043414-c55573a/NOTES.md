# Stage 9 eval gate notes

Run with `--sandbox bwrap --executor bwrap` at `c55573a`. Success is 96.7% over 240 runs, against 94.2% at the Stage 1 baseline and 97.9% at Stage 8. The comparison flags two token regressions and one improvement:

| model | metric | baseline | Stage 9 |
|---|---|---|---|
| gemma4-think | input tokens | 25.9k | 54.9k |
| qwen | input tokens | 10.0k | 16.4k |
| qwen | output tokens | 687 | 388 (better) |

Both regressions come from one Stage 9 change: the `code` profile now offers `grep` and `edit`, as the plan's profile table says. That adds two tool schemas and two prompt sections to every request. No other part of the system prompt changed from Stage 8. The reasoning setting is unchanged (gemma4-think still sends `reasoning_effort: low`).

- **qwen:** its turn count barely moved (5.4 at Stage 8, 5.9 now). Its extra input is the larger prompt on every request, and its edits replace some bash rewrites, which is where the output tokens fell.
- **gemma4-think:** its turns rose from 10.1 to 14.9. Five runs reached the 50-turn ceiling, against none at Stage 8. In these runs it fixes the code with `edit`, the tests pass, and then it reruns the same test command until the ceiling. The doom-loop guard denies the repeats (275 `doom_loop` denials, up from 35), and the eval still counts the run a success because the work directory is correct. The denials cluster in the `fix-*` tasks, the ones `edit` applies to. Plain gemma4 is unchanged: 203 denials against 152 at Stage 8 and 198 at Stage 7.

All failures are on `find-off-by-one-loop` (both gemma4 models 0/3, qwen 2/3), which the gemma4 models have failed at every gate since the baseline.

The owner decides whether the `code` profile keeps `grep` and `edit` (one line in `profile/code/code.go`). If it keeps them, the gemma4-think rerun loop is a candidate for a stop hint in the edit section of the prompt, or for the doom-loop action ending the run rather than denying.
