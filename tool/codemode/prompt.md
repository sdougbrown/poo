Codemode runs a short script whose only capability is calling your other
tools. Use it when one call feeds the next, or when a job needs many
calls. The script's intermediate results stay out of the conversation;
only what it prints and returns comes back.

The script's language is Starlark, a deterministic dialect of Python.
The language has no files, network or clocks, and modules never load.

For a file, a URL or a command, call the tool that reaches it, such as
`call("bash", {"command": "curl -sL URL"})`. The builtins:

- `call(name, args)` runs one tool call and returns a dict with
  `handle`, `tool`, `outcome`, `text`, `is_error` and `structured`.
  `text` starts with the handle line, `[r1 · read]`, followed by the
  tool's own text. `structured` is the call's JSON payload as a string,
  or `None`.
- `gather([("read", {...}), ("grep", {...})])` makes the calls in one
  batch and returns their results in list order. When every call in
  the batch only reads, the calls run at the same time; otherwise they
  run in order.
- `json.decode(text)` and `json.encode(value)` convert between JSON
  text and values, so `json.decode(r["structured"])` reads a payload.
- `print(x)` keeps a line for the result you will see.
- `exit(value)` ends the script with a value; the value comes back as
  JSON. A script without `exit` returns nothing but its printed lines.

A script's calls keep up to a million characters of output, far more
than your own calls show you.

A bash payload holds the command's stdout in `output`. When the command
writes more than the call keeps, `truncated` is true and the rest is
dropped. An output too long to show whole comes back as `output_head`
and `output_tail` in place of `output`.

A call that is refused or fails comes back as data (the `outcome` says
which), so the script can decide what to do. The tools you can call are
the ones this session offers; calling an unknown one is an error. Keep
scripts short, each with a single job.
