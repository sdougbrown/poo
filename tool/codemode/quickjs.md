Use codemode when one tool result decides the next call or when you need
only a small part of a large result. Intermediate results stay out of the
conversation. Only what you print and return comes back.

Write a synchronous JavaScript function body for QuickJS, not Node.js.
The runtime has no files, network, timers, console or clock: use the
session's tools for those. A slow tool call is bounded by that tool's own
timeout. For example, if `read` is available:

```js
const r = call("read", {path: "README.md"});
if (r.is_error) return {outcome: r.outcome, text: r.text};
return r.text.split("\n").filter(line => line.startsWith("## "));
```

- `call(name, args)` returns an object with `handle`, `tool`, `outcome`,
  `text`, `is_error`, and `structured` (a JSON string or `null`). A refused
  or failed tool call comes back as data in `outcome`.
- `gather([[name, args], ...])` sends one batch and returns results in
  input order. An all-read batch runs in parallel; other batches run in order.
- `print(value)` keeps a line. `return value` reports a JSON value.

Use the session's tools for files, commands, and network access.
