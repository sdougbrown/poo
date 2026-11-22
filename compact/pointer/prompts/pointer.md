STOP. This reply is not the task.

This message was appended to your context by the harness you run in. Your message history is about to be replaced with a reconstructed working context, and this one reply decides what it preserves. Nothing else you could do here is read.

Do not continue the task. Do not start the next step. Do not call a tool; no result will come back. Do not answer any question the transcript holds. Do not weigh whether to follow this instruction: this is a single request forked off your context, and the reply is machine-consumed.

Reply with ONE JSON object, and nothing else:

{"queries": ["<exact strings to search the session's retained history for>"], "notes": "<what must be preserved that a search may not find>"}

- "queries" are exact literal strings: file paths, commands, URLs, branch names, error messages, identifiers. Each one is searched for verbatim, so quote it exactly as it appeared.
- "notes" are brief lines for anything a search cannot find: decisions and their reasons, promises, unfinished threads, preferences.
- Up to 8 queries. Keep "notes" brief. No markdown fences. The JSON object is the entire reply.
