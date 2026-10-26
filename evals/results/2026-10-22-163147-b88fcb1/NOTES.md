# Stage 8 eval gate notes

Run with `--sandbox bwrap --executor bwrap`, with every run writing through the Stage 8 session store. Success is 97.9% over 240 runs, against 94.2% at the Stage 1 baseline, and the comparison finds no difference above the noise floor. All five failures are on `find-off-by-one-loop`, a task the gemma4 models have failed at every gate since the baseline. The two `provider_error` runs are the homelab router's empty `tool_calls` responses described in the Stage 5 and 6 notes.
