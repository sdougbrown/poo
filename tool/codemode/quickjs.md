Use codemode when one tool result decides the next call or when you need
only a small part of a large result. Intermediate results stay out of the
conversation. Only what you print and return comes back.

The code is the body of an async function run by QuickJS. There is no
Node, network, or timers: use the session's tools for those. The guest
mounts no host directory, so a file operation in the script fails. A
slow tool call is bounded by that tool's own timeout. The session's
tools are properties of `tools`, by name, so call one as
`await tools.read({...})`. For example, if `read` is available:

```js
const r = await tools.read({path: "README.md"});
if (r.is_error) return {outcome: r.outcome, text: r.text};
return r.text.split("\n").filter(line => line.startsWith("## "));
```

- `await tools.<name>(args)` returns an object with `handle`, `tool`,
  `outcome`, `text`, `is_error`, and `structured` (a JSON string or
  `null`). A refused or failed tool call comes back as data in `outcome`.
- `gather([[name, args], ...])` sends one batch and returns the results
  in input order. A batch of read-only calls runs at the same time;
  other batches run in order. Awaited calls run one at a time, so use
  gather for parallel reads.
- `print(value)` keeps a line. `return value` reports a JSON value.

The tools you can call are the ones this session offers; `tools` carries
them by name. Keep scripts short, each with a single job.
