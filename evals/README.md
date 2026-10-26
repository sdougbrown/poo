# The eval set

Twenty tasks: twelve small coding fixes, five find-and-explain
questions, and three long tasks that stress the context window.

Every task lives in `evals/tasks/<id>/`, with a `task.json` and a
`check.sh`. The `check.sh` runs in the workdir after the run, and exit
zero means the task was solved. The model catalog is
`evals/models.yaml`, and the fixtures the tasks work in are under
`evals/fixtures/`.

Run the set with:

    poo-eval run --models gemma4,qwen --seeds 3

## Isolation

`poo-eval run` uses `--isolation bwrap` by default, which needs bubblewrap;
`--isolation none` turns it off, and the model's commands then run with
your full privileges. Each run's
poo process sees the system's read-only `/usr` and `/etc`, its own workdir
at `/work`, its log directory at `/log`, and a private `/tmp` that is
also its `$HOME`, and nothing else: not the real home directory, the
repository or the task set. What a tool installs into `$HOME`, such as
`pip install --user`, stays out of the run's diff. The network is shared, so the
model's endpoint is still reachable. `check.sh` runs outside the sandbox,
because it needs the task set. With `--isolation none`, a model that searches
the filesystem can find the task set's checks, as minimax did in the
first baseline with `find /`, and its result does not measure the task.

The workdirs are made under `<user cache dir>/poo-eval/work`, outside
the repository. `--work-root` overrides it, and the runner refuses a
work root that holds the task set or lies inside it, so the task set
is never an ancestor of a workdir.

Each invocation copies the fixtures its tasks use into
`<work root>/<invocation>/fixtures/` before the first run and makes
every workdir from that snapshot, so a model that edits
`evals/fixtures` cannot change the start of a later run. The runner
also hashes each fixture, its snapshot and each task directory at the start and again
after each run. A run that finds one changed is marked invalid
(`source_changed`, success false, an error saying so), a warning
naming the run goes to stderr, and `poo-eval run` exits non-zero. The
source is not restored; that is the operator's call.

The sandboxed process can write its own session log: the log directory
is writable. Without the sandbox, a process of the same user can also
reach the open log file through `/proc`; the sandbox has its own pid
namespace, so that route is closed inside it. The metrics and the judge's transcripts come from that
log, so they come from data the model could alter. Success does not:
it comes from `check.sh`, which runs outside the sandbox.

The limit that remains: a model can still search the filesystem, and
the derivations in `check_answer.py` and the checks are files on the
same disk. Real isolation is a sandbox's job. A transcript that reads
outside its workdir should be treated as invalid.

## How a task is measured

**A coding task** is a Python package with a bug and a `unittest` suite
that fails because of it. The check runs the suite. The harness puts
the fixture's own tests back into the workdir before the check runs, so
a run that rewrote the tests cannot make the check pass. A change to a
test file is reported as an unexpected change.

**A find-and-explain task** is a repository with one thing to find and
no file to change. The check holds that the run changed nothing, and
that the run's last message names every fact the task asked about. The
facts are derived from the workdir by `evals/tasks/check_answer.py`,
which lives beside the task set and never in the workdir: a model that
reads the task's own directory learns nothing from it. The derivation
runs against the workdir as the model found it, so a run that edited
the workdir to change the answer has not answered the question.

**A long task** is one whose working set is past the 32,768-token
window of the low-budget model and well under the 262,144-token window
of the other. The sizes are measured below.

## The long tasks

The three long tasks are sized so the *whole* tree is a large working
set. A model that reads the whole thing needs more than 32,768 tokens,
so it has to search instead. The token estimate is the harness's own:
3.5 characters per token, so a task's estimated tokens are its
characters times ten over thirty-five.

| task | files | characters | lines | estimated tokens |
|---|---|---|---|---|
| `long-find-definition-in-tree` | 130 | 361,236 | 12,446 | 103,210 |
| `long-find-error-in-log` | 3 | 217,277 | 1,805 | 62,079 |
| `long-change-timeout-everywhere` | 85 | 163,492 | 5,873 | 46,712 |

What each of them actually tests:

### `long-find-definition-in-tree`

The symbol `parse_manifest` is mentioned in 129 files and defined in
one of them, at `src/pipeline/ingest/handlers/loader.py`. The
mentions are decoys: every decoy file carries a comment saying "This
is not parse_manifest; it only shares its prefix."

This measures **search strategy and the discipline of narrowing down**.
A model that greps for the definition finds the file in one call and
uses a small fraction of the window; a model that reads the tree
instead fills the window and hits the budget. Both are real
behaviours, and the run's own metrics show which one happened: the
input tokens, the reads and the re-read rate. The task does not
require the whole tree in the context, and it is not built so that it
does — a competent model should solve it with a search.

### `long-find-error-in-log`

`logs/service.log` holds 1,102 lines, of which exactly one has the
level `ERROR`. The line carries the request id `req-8f2a41c9`, which
appears nowhere else in the file, so the answer is unambiguous.

This measures **narrowing a large file to one line**. The prompt asks
for a grep or a narrow read rather than the whole file, and a model
that reads all 62,079 estimated tokens hits the 32,768-token window of
the low-budget model. The access log beside it is a second, larger
file with no error in it, so reading the wrong file is also a way to
waste the budget.

### `long-change-timeout-everywhere`

Every package under `shop/` owns its own request-timeout constant, and
all 74 of them are 30 seconds. The task is to raise every one of them
to 45.

This measures **many edits across many files, without a blanket
replacement**. Two decoys sit in `shop/settings.py` — the payment
timeout and the index timeout — and a blind search-and-replace over
`= 30` moves them and fails the check. The check requires every
request timeout to be exactly 45, so a timeout set to 46 fails too.

This is the one long task that cannot be solved without many edits: a
model that only reads cannot solve it, and a model that edits one file
cannot solve it either. The fixture's own tests assert the old value
for two of the packages, so the run has to update the tests as well —
this is the only task in the set whose prompt asks the run to change
the fixture's tests, and the only one where a test change is expected
rather than reported.

## The read-only questions

| task | the question | the facts the answer must name |
|---|---|---|
| `find-dead-code` | which handler no route reaches, and which file would have to change | the handler, and `routes.py` |
| `find-off-by-one-loop` | which helper drops the last item, and which comparison is off by one | the helper, and the loop's condition |
| `find-secret-in-config` | where the API token is read from | the function, and the environment variable |
| `find-slow-loop` | which lookup walks the records, and what would replace the walk | the method, and the data structure |
| `find-version-mismatch` | which two declarations disagree, and which file is out of step | both versions, and the file |

Each of these has exactly one right answer, and the check verifies
every fact the prompt asks for. An answer that names one of the facts
fails.
