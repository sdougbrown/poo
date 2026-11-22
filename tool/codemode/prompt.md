Codemode runs a short script whose only capability is calling your other
tools. Reach for it when a tool returns far more text than you need to
act on. The script reads the bulk, and only what it prints and returns
comes back. Its calls keep up to a million characters of output, far
more than your own calls show you.

It also chains calls: one call's result decides the next, without a
round trip through you. The script's intermediate results stay out of
the conversation.

The script's language is Starlark: Python-like, but not Python. The
language has no files, network or clocks, and modules never load. There
are no f-strings, so format with `"... % (a, b)"`. There is no
try/except: a refused or failed call comes back as data in its
`outcome` field.

For a file, a URL or a command, call the tool that reaches it, such as
`call("bash", {"command": "curl -sL URL"})`. The builtins:

- `call(name, args)` runs one tool call and returns a dict with
  `handle`, `tool`, `outcome`, `text`, `is_error` and `structured`.
  `text` starts with the handle line, `[r1 · read]`, followed by the
  tool's own text. `structured` holds the call's JSON payload: a dict
  when the payload is a JSON object, its text when it is not, or
  `None`.
- `gather([("read", {...}), ("grep", {...})])` makes the calls in one
  batch and returns their results in list order. When every call in
  the batch only reads, the calls run at the same time; otherwise they
  run in order.
- `json.decode(text)` and `json.encode(value)` convert between JSON
  text and values; `json.decode` reads a `structured` payload that is
  not an object.
- `print(x)` keeps a line for the result you will see.
- `exit(value)` ends the script with a value; the value comes back as
  JSON. A script without `exit` returns nothing but its printed lines.

A bash payload holds the command's stdout in `output`. When the command
writes more than the call keeps, `truncated` is true and the rest is
dropped. An output too long to show whole comes back as `output_head`
and `output_tail` in place of `output`.

The tools you can call are the ones this session offers; calling an
unknown one is an error. Keep scripts short, each with a single job.
