Codemode runs a short script whose only capability is calling your other
tools. Use it when one call's result decides the next call, or when a
job needs many calls: the script's intermediate results stay out of the
conversation, and only what it prints and returns comes back.

The script's language is Starlark, a small deterministic Python dialect.
It has no files, network or clocks, and it cannot load modules. What it
has:

- `call(name, args)` runs one tool call and returns its result: a dict
  with `handle`, `tool`, `outcome`, `text`, `is_error` and
  `structured_json`.
- `gather([("read", {...}), ("grep", {...})])` runs calls at the same
  time and returns their results in order.
- `print(x)` keeps a line for the result you will see.
- `exit(value)` ends the script with a value; the value comes back as
  JSON. A script without `exit` returns nothing but its printed lines.

A refused or failed call comes back as data (`outcome` says which), so
the script can decide what to do. The tools you may call are the ones
this session offers; calling an unknown one is an error. Keep scripts
short and single-purpose: one script, one job.
