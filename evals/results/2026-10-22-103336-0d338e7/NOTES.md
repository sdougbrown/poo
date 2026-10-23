# Stage 6 eval gate notes

Run with `--sandbox bwrap --executor bwrap`. Success is 97.1% over 240 runs, against 94.2% at the Stage 1 baseline, and the comparison finds no difference above the noise floor.

- **Tool and parse error rates.** The report's `tool err` and `parse err` columns give them per model, as providers.md Phase A asks. gemma4 and gemma4-think run in `auto` mode, which falls back to text tool calls; their parse error rate is 0.0%.
- **The homelab router's empty `tool_calls` responses**, recorded in the Stage 5 notes, now settle as `provider_error` instead of a silent `end_turn`. Four runs ended that way:
  - two with kind `server`, after the retries were spent;
  - two with kind `interrupted_stream`, because the model had streamed text before the empty finish, and a failure after a delta is not retried.

  The server-side gemma4 tool parser is still an owner action.
