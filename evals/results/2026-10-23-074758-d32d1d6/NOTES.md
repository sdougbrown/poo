# Stage 10 eval gate notes

Run with `--sandbox bwrap --executor bwrap` at `d32d1d6`. Success is 96.7% over 240 runs, the same as Stage 9. Against Stage 9 the comparison finds no difference above the noise floor. Against the Stage 1 baseline it flags one regression and one improvement:

| model | metric | baseline | Stage 9 | Stage 10 |
|---|---|---|---|---|
| gemma4 | input tokens | — | 51.5k | 39.8k |
| gemma4-think | input tokens | 25.9k | 54.9k | 39.4k (no longer flagged) |
| qwen | input tokens | 10.0k | 16.4k | 20.6k (flagged) |
| qwen | output tokens | 687 | 388 | 423 (better) |

Two Stage 10 changes reach the prompt:

- **The edit section's last line** now reads "After an edit, run the tests once. When they pass, end your turn with a short summary." It replaces "read the file again when you need to see what it holds now".
- **A refused doom-loop ask** now tells the model it repeated a call, rather than only who refused it.

The registry tools are not offered here: the eval sets no `cliregistry.paths`.

- **gemma4-think:** turns fell from 14.9 to 11.4, and `max_turns` settles from 5 to 2. Plain gemma4 fell from 13.6 to 11.9 turns. A gemma4-think experiment on the 12 `fix-*` tasks isolated each change: the doom-loop text alone took turns from 18.7 to 14.9, and adding the edit line took them to 10.9 with no `max_turns`. A shorter rewrite of the whole packaged prompt (753 to 290 words) made gemma4-think send empty tool calls and lowered success, so it was not adopted.
- **qwen:** its turns are close to the baseline (6.2 against 5.5). The extra input is per request: qwen gets no prompt cache here, so each turn resends the larger `code` profile prompt and the `grep` and `edit` schemas added in Stage 9.

Failures: `find-off-by-one-loop` for both gemma4 models (0/3; they have failed it at every gate since the baseline), one gemma4-think `fix-path-join-double-slash` run that reached `max_turns`, and one minimax `fix-retry-backoff` run.
