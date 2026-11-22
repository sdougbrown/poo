Use codemode when one tool result decides the next call or when you need
only a small part of a large result. Intermediate results stay out of the
conversation. Only what you print and return comes back. A call the
script makes keeps up to a million characters of output, far more than a
call of your own shows you.

Write the body of an async function for QuickJS, not Node.js. There is
no network or timers. The guest mounts no host directory, so a file
operation in the script fails. Use the session's tools for files,
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
- Named calls submitted together form a batch. Calls overlap only if
  every tool in the batch is declared read-only. Otherwise they run in
  input order.
- Use `await parallel(() => [...])` to overlap independent calls,
  including writes. Create the tool promises in the synchronous
  callback and return each one exactly once:

  ```js
  const [a, b] = await parallel(() => [
    tools.edit({path: "a.txt", old: "draft", new: "final"}),
    tools.edit({path: "b.txt", old: "draft", new: "final"}),
  ]);
  ```

  Results follow the order of the returned array. If the callback throws
  or returns an invalid array, none of its captured calls run. A
  permission denial appears as a result in the array.
- If two Poo edits overlap on the same resolved path in one process,
  one returns `is_error` with `structured` containing
  `{"code":"edit_conflict"}`. Re-read the file before retrying. Poo
  does not detect shell command conflicts or roll back a batch.
- `print(value)` keeps a line. `return value` reports a JSON value.
- A bash payload holds the command's stdout in `output`. When the
  output is too long to show whole, `output_head` and `output_tail`
  stand in its place, and `truncated` is true.

Batches run in submission order. Use at most 16 calls per batch. Do
not request background jobs in a batch.
