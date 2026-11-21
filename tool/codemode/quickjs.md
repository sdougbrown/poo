Use codemode when one tool result decides the next call or when you need
only a small part of a large result. Intermediate results stay out of the
conversation. Only what you print and return comes back.

Write the body of an async function for QuickJS, not Node.js. There is
no network or timers, and the guest mounts no host directory, so a file
operation in the script fails: use the session's tools for files,
commands, and network access.

Use `Promise.all` for independent reads. Use separate `await`s when one
call depends on another:

```js
const files = await Promise.all([
  tools.read({path: "README.md"}),
  tools.read({path: "go.mod"}),
]);
return files.map(r => r.is_error ? r.text : r.text.split("\n")[0]);
```

- `await tools.<name>(args)` returns an object with `handle`, `tool`,
  `outcome`, `text`, `is_error`, and `structured` (a JSON string or
  `null`). A refused or failed tool call comes back as data in `outcome`.
- Named calls submitted together and `gather([[name, args], ...])` use
  the same scheduling rule. Calls overlap only if every tool in the
  batch is declared read-only. Otherwise they run in input order.
- `parallel([[name, args], ...])` explicitly requests overlap even for
  calls that write. Use it only for independent calls. Normal tool
  permissions still apply. Batches return results in input order,
  take at most 16 calls, and do not start background jobs.
- If two Poo edits overlap on the same resolved path in one process,
  one returns `is_error` with `structured` containing
  `{"code":"edit_conflict"}`. Re-read the file before retrying. Poo
  does not detect shell command conflicts or roll back a batch.
- `print(value)` keeps a line. `return value` reports a JSON value.
