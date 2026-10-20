# Observed

What the eval harness has actually seen.

This file is the plan's Stage 1 deliverable: the observed failure modes,
written down so Stage 3's failure table and defaults can be checked
against them before Stage 3 starts.

Every entry here names the run it came from, so it can be reproduced.

## Observed in the 2026-10-20 smoke test

The smoke test ran one task, `fix-division-by-zero`, against `gemma4`
and `qwen`, seed 1, and then judged both transcripts. A wider run over
five tasks and both models followed.

### The text mode doubles the model's prose

**Where:** `provider/openai/stream.go`, in the transport's `events`
function.

**What:** in the `text` and `auto` tools modes, the transport appends
the content delta as a text event *and* feeds the same text to the
parser, which emits its own text event for the prose the parser does
not consume. The reader returns both, so every character of the model's
prose reaches the model twice.

A gemma4 answer came back as:

```
TheThe unreachable unreachable handler handler is is ` handle`handle__legacylegacy__pingping` ...
```

**Why it matters:** a read-only task's check greps the answer for a
keyword, and the doubled text still holds the keyword, so the check
mostly survives. But the model sees a mangled version of its own
previous message, and a model that reads its own output is confused by
it.

The corruption goes further than the prose. On the
`long-find-error-in-log` task, gemma4 found the right error line and
answered with the request id `req--88ff22aa4411cc99`, which is
`req-8f2a41c9` with every character doubled and the hyphen doubled
too. The check compares the answer against the id derived from the
log, so the run failed: the model was right and the harness gave it a
mangled answer.

The wider smoke test also showed gemma4 re-running the same test
command from turn 9 to turn 50, without stopping, having already
solved the task at turn 7. The judge called the tail of that
transcript "corrupted, heavily duplicated summary text".

**Reproduced** with a one-line SSE body in text mode: the text comes
out as `"Hello world.Hello world."`.

**Status:** this is unit A's code. It is reported, not fixed here. The
fix is to let the parser take the text *instead of* the transport in
the modes where the parser runs, or to have the parser return only the
calls it found and leave the prose to the transport.

**What it costs the baseline:** every text-mode model's answers are
corrupted, and the read-only tasks read those answers. On the 2026-10-20
smoke test, gemma4 passed all three coding tasks and failed all three
read-only ones, and qwen, which runs in the native mode, passed all
six. The baseline has to be measured after the fix, or the read-only
success rate measures the bug rather than the model.

### gemma4 writes its tool calls as text

**Where:** the model's own behaviour, seen in the smoke test.

**What:** gemma4 answered the tool-calling prompt with a
`<tool_call>` block in its text rather than a wire-format
`tool_calls` array. The `native` tools mode therefore made no tool
call at all, and the run ended after one turn with the task undone.

**What was done:** `evals/models.yaml` gives gemma4 `tools_mode: auto`,
so the text parser reads the calls. The owner allowed this change on
the evidence of a smoke test, and the change is recorded in the
catalog's own notes.

### The models re-read what they have already read

**Where:** every model in the smoke test.

**What:** the re-read rate was 20% to 50% across the smoke test's runs.
gemma4 read `rates/__init__.py`, edited it, and read it again; qwen
re-read the source and the tests before editing.

**Why it matters:** the plan's re-read rate is a metric on the
baseline, and a high value is a signal that the harness's context
handling forces a model to re-read. It is not a bug in itself, and the
harness does not guard against it: the plan's decision 3 says v1 ships
no repeated-read guard.

### gemma4 hit the turn ceiling

**Where:** the `fix-division-by-zero` and `fix-off-by-one-slice` tasks,
gemma4, seed 1.

**What:** both runs settled `max_turns` after 50 turns. The task was
solved by turn 7, and the run kept going: it re-ran the same test
command, `python3 -m unittest discover tests`, at every turn from 9
to 50. Each of those turns produced the same summary text, doubled by
the text-mode bug above, so the model saw its own message repeated
with every word doubled and kept going.

**Why it matters:** two things are true at once. The skeleton's
`end_turn`-only vocabulary is not enough: a model that does not end
its own turn runs to the ceiling, and the plan's typed `finish` tool
and the `finished` settle reason are the answer. And the text mode's
doubling is what kept this model in the loop: qwen, which runs in the
native mode and so sees its own text once, ended the same task in 5
turns.

**What to check in Stage 3:** whether the doubling fix alone ends the
loop, and whether the `finish` tool is still needed. The plan's typed
result is the right shape either way, but the baseline should be
measured after the doubling fix, or the ceiling rate will be an
artefact of the bug rather than a fact about gemma4.

### The tool error rate was low but not zero

**Where:** the wider smoke test.

**What:** 4.2% of the tool calls failed across the smoke test's ten
runs. The failures were recoverable: a command that named `python`
where the machine has `python3`, and a `sed` whose quoting did not
survive the shell.

**Why it matters:** the tool error rate is a metric, and the errors
show the harness's own behaviour working as the plan wants: the model
saw the error and adapted.

## What the harness itself got wrong

Two things were found and fixed in the harness while it was being
written:

- The first diff implementation compared the workdir against a hash
  list, so a file the run had changed could not be diffed against its
  original content. It now keeps a pristine copy of the workdir and
  diffs against that.
- The first transcript renderer put every turn's events in one block,
  so the judge could not elide the middle of a long transcript. It now
  renders one segment per turn, which is what the summary needs.

## What is not observed yet

The long change task and the long definition task have not been run end
to end against a live model, so the harness's behaviour at the context
limit is untested. The smoke test was bounded to one easy task and one
wider run over six tasks, so the change task's many-file edit and the
context-exhausted path are still unmeasured.
