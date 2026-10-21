# Observed

What the eval harness has seen. This is the Stage 1 deliverable named in
the plan: the observed failure modes, written down so that Stage 3's
failure table and defaults can be checked against them before Stage 3
starts.

Every entry names the runs it came from. The raw transcripts are not
committed (`evals/results/**/runs/` is ignored); the report, the
summary and the judge's verdicts are.

## The baseline

`evals/results/2026-10-21-003728-f6a832f/`: 20 tasks × 4 models × 3
seeds = 240 runs at `f6a832f`, each inside the bwrap sandbox
(`--sandbox bwrap`), `--max-turns 50`.

| model | endpoint | context | success | turns | tool err | reread | settle |
|---|---|---|---|---|---|---|---|
| qwen | grace router | 262144 | 100.0% | 5.5 | 0.5% | 12.4% | 60 end_turn |
| minimax-m3.1-flash | Ollama cloud | 262144 | 100.0% | 8.2 | 6.9% | 17.7% | 60 end_turn |
| gemma4 | grace router | 32768 | 90.0% | 13.3 | 2.2% | 25.7% | 58 end_turn, 2 max_turns |
| gemma4-think | grace router, `reasoning_effort: low` | 32768 | 86.7% | 10.8 | 1.5% | 21.3% | 58 end_turn, 2 max_turns |

No run settled `context_exhausted` or `provider_error`.

The report's mean diff size for gemma4 and gemma4-think is inflated:
three runs (`fix-json-missing-key` gemma4 seeds 1–2,
`fix-stack-push-order` gemma4-think seed 1) ran `pip install --user
pytest`, and with the sandbox's `$HOME` at `/work` the installed files
counted as the run's diff, about 178000 lines each. Their success is
unaffected. The sandbox now puts `$HOME` in its private `/tmp`, so later
baselines do not count it. The text
parse-failure rate is 0% for every model: the parser never failed on a
block it recognized. Malformed calls it did not recognize show up as a
premature `end_turn` instead (entry 3).

All 14 failed runs are gemma4 or gemma4-think runs. 6 of them are
`find-off-by-one-loop`, which no gemma run passed.

## Failure modes

### 1. A model edits what the task said not to touch

**Runs:** `find-off-by-one-loop` gemma4 seeds 1–2 and gemma4-think
seeds 2–3; `fix-json-missing-key` gemma4 seed 3; `fix-off-by-one-slice`
gemma4-think seed 1; `fix-unicode-decode-error` minimax seeds 1–3;
`fix-stack-push-order` minimax seed 1; `fix-path-join-double-slash`
minimax seed 2.

**What:** on a read-only task that says "do not change any file", four
of the six gemma runs wrote a `test_chunks.py` to confirm their
diagnosis, then gave the right answer. The check fails them for the
unexpected file. (The other two never answered: one made no call at
all, and one is entry 3.) On coding tasks, gemma left a scratch
`fix_require.py` or `fix_attempt.py` behind, and minimax rewrote
`README.md` or added tests it was not asked for after the fix passed.
None of the coding-task edits failed a check.

**Harness relevance:** the harness can't stop this, and v1 shouldn't
try. The diff-sanity metric (`unexpected_files`) and the judge's
`off_task_edit` both catch it, which is what they are for. A
permission policy (Stage 7) could deny writes on a read-only task, but
that is a consumer's choice, not a default.

### 2. A model calls a tool poo does not have

**Runs:** minimax across the set: 34 calls to `edit` and 5 to `write`,
of 655 calls. qwen made 1 call to `call`.

**What:** minimax-m3.1-flash reaches for an `edit` tool even though the
request offers only `bash` and `read`. Each call gets a recoverable
error result, and minimax then edits with a heredoc or `sed`. It never
failed a task because of it, but it is most of its 6.9% tool-error
rate.

**Harness relevance:** matches the failure table's recoverable-error
row. Stage 5's `edit` tool will absorb most of these. Worth a
tool-error-rate check after Stage 5.

### 3. A malformed call ends the run as a premature `end_turn`

**Runs:** the assistant text of 18 gemma4-think runs and 3 gemma4 runs
holds Gemma's own channel or tool tokens (`<|channel>batch` followed by
a JSON command, `decalration:bash{command:<|"|>ls -R<|"|>}`). When that
text was the model's attempted call, the run ended there. Examples:
`find-dead-code` gemma4-think seed 2 and `find-off-by-one-loop`
gemma4-think seed 1.

**What:** the response has text and no recognized call, so the run
settles `end_turn` on that turn with the task undone.

**Harness relevance:** two separate issues.

- The leaked tokens come from the gateway's chat template for gemma4,
  mostly with thinking on. That is endpoint configuration, not poo.
- The settle is poo's, and it is the case the plan's `BeforeSettle`
  hook (Stage 4) exists for: a turn that ends in something that looks
  like an attempted call can be continued with a nudge instead of
  accepted. The plan already says `end_turn` is "accepted by
  `BeforeSettle`", so the table needs no change.

### 4. A model does not stop after the task is done

**Runs:** 4 runs hit `max_turns` (50): `fix-path-join-double-slash`
gemma4 seed 1, `fix-word-count-ignores-case` gemma4 seed 2 and
gemma4-think seed 2, `fix-off-by-one-slice` gemma4-think seed 2. Three
of the four still passed the check, because the fix was in place long
before turn 50.

**What:** the model fixes the bug, runs the tests, and then keeps
re-running the same test command and restating its summary until the
ceiling.

**Harness relevance:** `loop_or_reread` is gemma's most common
pathology (see the judge table). The plan answers it twice: the
doom-loop repeat counting in Stage 3 (exact and near keys) and the typed
`finish` tool. This baseline has no loop guard at all, so these runs
show what an unguarded loop costs: most of the 50 turns.

### 5. A model wanders outside its working directory

**Runs:** minimax ran 37 bash commands that searched outside the
workdir (`find / -maxdepth 3 -name ...`); qwen ran 15 (`cd /home/user`,
a `find /` for the package it was editing).

**What:** in an earlier, unsandboxed baseline, minimax found the task
set through `find /`, read `check_answer.py`, and patched the source
fixtures of two tasks. That led to the fixture snapshot guard and then
the bwrap sandbox. Inside the sandbox the same searches find nothing
outside `/work`, and the source fixtures stayed unchanged for all 240
runs.

**Harness relevance:** this is why `poo-eval run --sandbox bwrap`, which
wraps the whole `poo` process, is the eval isolation (owner decision,
2026-10-21). An executor that confines only bash would leave `read`,
which runs inside poo's own process, able to open the answer key. The
plan's Stage 5 bwrap executor is about keeping credentials away from
model-chosen commands, not about eval isolation.

### 6. Re-reads are common

**Runs:** every model. The re-read rate runs from 12.4% (qwen) to 25.7%
(gemma4).

**What:** a model reads a file, edits it, and reads the same range
again, or reads source and tests twice before editing.

**Harness relevance:** a measurement, not a defect. Decision 3 says v1
ships no repeated-read guard. It is a baseline to compare Stage 4's
prompt builder against.

### 7. TTFT and the prefix cache

The prefix-cache hit ratio is 71% (minimax) to 83% (gemma4); qwen's
endpoint reports none. The judge flagged no `ttft_growth`. Nothing here
for Stage 3 to change.

## What the harness got wrong along the way

Found while taking the baseline, and fixed before it was taken:

- **The text mode doubled the model's prose.** The transport appended a
  content delta as text and also fed it to the parser, which emitted it
  again. gemma4's answers came back with every word twice, and that kept
  a gemma4 run in a 50-turn loop. A streamed fenced block that was not a
  call was re-emitted the same way. Both are fixed and tested.
- **Parallel calls at one index merged.** minimax sends every parallel
  call at index 0, each with its own id. The transport merged them into
  one call with both names and both arguments, and the provider
  answered 400. Fragments are now routed by index and id.
- **The judge lost answers.** The grace gateway cuts a deepseek-flash
  reply at the first literal `<tool_call>` the model writes, even inside
  its reasoning, and returns empty content. The judge now shows the tag
  as `⟨tool_call⟩`, asks once more after an empty answer, and reports
  the finish reason if the answer is still empty.
- **Reports held local paths, a run could edit a source fixture, and a
  run could read the answer key.** Fixed by relative report paths and
  `$WORKDIR` rewriting, the snapshot guard, and the sandbox.

## Judge

deepseek-flash judged all 240 transcripts after the judge fix above.
`judgments/summary.md` lists the 10 it flagged for the owner:

- 4 are the `find-off-by-one-loop` gemma runs that wrote `test_chunks.py`
  (entry 1).
- 3 are minimax runs that edited `README.md` or added tests after the
  fix passed (entry 1).
- 3 are judge errors: the request timed out at the judge's 120 s limit
  (`find-off-by-one-loop` minimax seed 1, `fix-path-join-double-slash`
  minimax seed 2, `fix-json-missing-key` gemma4 seed 1). The gateway was
  slow on long transcripts. These runs passed or failed on `check.sh`
  as the report says; they are unjudged, not suspect.

The pathology counts per model:

| model | tool-call parse failure | premature end_turn | loop or re-read | off-task edit | other |
|---|---|---|---|---|---|
| gemma4 | 13 | 3 | 33 | 3 | 5 |
| gemma4-think | 16 | 6 | 34 | 3 | 0 |
| minimax-m3.1-flash | 37 | 0 | 1 | 6 | 2 |
| qwen | 2 | 0 | 1 | 0 | 0 |

No transcript showed a context blowup or TTFT growth. The judge counts a
call to a tool poo does not have as a parse failure, which is most of
minimax's 37 (entry 2).

The judge is not perfectly consistent. Two runs with the same behaviour
can get different `needs_owner` flags, so the flagged list is a place
to start reading, not a complete list.

## Checked against Stage 3

Stage 3's failure table (`v1-plan.md`, "Failure and finalization") and
its defaults were read against the entries above:

- **Unknown tool** (entry 2): recoverable error result. Matches.
- **Premature `end_turn` on a malformed call** (entry 3): `end_turn` is
  "accepted by `BeforeSettle`". No change; Stage 4's hook is where a
  nudge belongs.
- **Not stopping** (entry 4): doom-loop counting and `finish` are both
  planned. No change. The baseline gives Stage 3 a number to beat: 4 of
  120 gemma runs at the ceiling.
- **`provider_error`:** none in this baseline. An earlier minimax run
  hit one Ollama HTTP/2 timeout, which the Stage 6 retry budget covers.
- **`context_exhausted`:** none at gemma's 32768 limit, so the 80%
  notice and the 97% hard limit are untested by real runs. Stage 3
  keeps its own injected tests for that path.

No change to the Stage 3 table or defaults follows from this baseline.
