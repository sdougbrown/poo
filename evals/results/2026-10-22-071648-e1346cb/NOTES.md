# Stage 5 eval gate notes

Run with `--sandbox bwrap --executor bwrap`: each task's bash ran in its own bubblewrap sandbox. Success is 95.4% over 240 runs, against 94.2% at the Stage 1 baseline.

The comparison flags one regression: gemma4 `input_tokens` rose 61%. It has two causes:

- **A longer system prompt on every request.** The new `base` text, the bash tool's own section, the read section and the jobs section add about 450 tokens per turn.
- **More turns on a few tasks.** For example, `fix-csv-skips-header` took 38 turns against 11, and `fix-cache-never-expires` took 40 against 26. Other tasks needed fewer turns than at baseline (`fix-word-count-ignores-case` took 10 against 34).

## gemma4 runs with no answer

gemma4 `find-version-mismatch` went 0/3, against 3/3 at baseline. Each run settled `end_turn` after one turn, with no assistant message. The provider (the homelab router at `localhost:4000`) streamed `finish_reason: "tool_calls"` with empty deltas, while its usage showed 18 output tokens. The server's tool parser consumed the model's call and sent nothing. `gemma4-empty-tool-calls.log` holds the raw stream, reproduced through a logging proxy.

The same pattern is in 2 of 240 Stage 4 runs, 6 of 240 Stage 5 runs, and none at baseline. The two fixes:

- **poo:** a `tool_calls` finish with no calls and no text should not settle as `end_turn`. That handling belongs to Stage 6, which owns the finish_reason mapping.
- **Server:** the gemma4 tool parser on the homelab router needs a look. That's an owner action.
