# Prior art

What poo copies from other work, and what it rejects. This is a short list, not
an essay. Each source records exactly what was read and when.

## mini-swe-agent

- **Source:** <https://github.com/SWE-agent/mini-swe-agent>
- **Commit read:** `04d809ceab9df28f9adaed044884180159172930` (branch `main`,
  package version 2.4.6)
- **Read:** `src/minisweagent/agents/default.py`, `src/minisweagent/agents/interactive.py`,
  `src/minisweagent/environments/local.py`, `src/minisweagent/exceptions.py`,
  `src/minisweagent/__init__.py`, `src/minisweagent/config/default.yaml`,
  `src/minisweagent/config/mini.yaml`,
  `src/minisweagent/models/utils/actions_toolcall.py`, `docs/advanced/control_flow.md`.

### Copied

- **One tool, bash, as the primary verb.** mini's answer to "how do I let the
  model edit a file" is "it runs `sed`". Poo keeps bash as the primary tool in
  the `code` profile and adds `read`/`grep`/`edit` only because a read-only
  build cannot link bash at all.
- **A stateless shell per call.** Every command runs in a fresh subshell; `cd`
  and exported variables do not persist. Poo's system prompt says so explicitly
  (plan, §Simple defaults, `bash` tool).
- **A linear, append-only history.** The trajectory and the messages sent to the
  model are the same list. Poo makes this a governing rule: nothing rewrites
  the model's view of history silently.
- **The observation envelope and head/tail elision.** mini's `observation_template`
  renders `{returncode, output}` below a cap and `{output_head, output_tail,
  elided_chars}` above it. Poo uses the same shape with a context-relative cap
  and a hint pointing at `grep`/`sed`/`head`/`tail`.
- **The "output too long" guidance text.** mini's system template tells the
  model to use `head`, `tail` or `sed` to view a smaller range. Poo reuses
  that advice verbatim in spirit in the bash hint.
- **The prompt structure.** A system template that states the operating
  contract, plus an instance template that states the task, the recommended
  workflow, and the command-execution rules. Poo splits this into named
  prompt sections (plan, §System prompt) and drafts the `base` section from
  mini's wording in Stage 5.
- **The "fresh shell" and "your response must contain a tool call" rules.**
  mini's `mini.yaml` instance template states that each response must include
  reasoning text and at least one bash call, and that directory changes are not
  persistent. Poo keeps the substance, drops the exact-output-format
  requirement (poo uses native tool calls where the model supports them).
- **Process-group kill on timeout.** mini's `environments/local.py` `_run` uses
  `start_new_session=True` and `os.killpg` so a timed-out command leaves no
  orphaned children. Poo does the same, and adds a grace period before
  `SIGKILL` plus cleanup of survivors in the call's own process group.

### Rejected

- **Bash as the only tool.** mini's "no tools other than bash" stance does not
  survive the read-only build requirement (umpire-bot's proving build must not
  be able to run arbitrary commands).
- **The finish-by-magic-string convention.** mini ends a run when the shell
  prints `COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT`. Poo's `finish` tool carries a
  typed, schema-validated result instead, and the sentinel is not part of the
  protocol.
- **Exception-driven control flow.** mini's loop is a `while True` that
  catches `InterruptAgentFlow` subclasses (`Submitted`, `LimitsExceeded`,
  `TimeExceeded`, `UserInterruption`, `FormatError`) and turns them into
  messages. Poo uses an explicit FSM with a closed settle-reason enum, because
  the wire protocol, `agent/inspect` and session replay all depend on a known
  state set.
- **Format-error retries as a loop counter.** mini tolerates up to
  `max_consecutive_format_errors` malformed responses. Poo treats a malformed
  tool call as a recoverable validation error returned to the model, and bounds
  continuation attempts with `MaxContinuations` instead.
- **`role: "exit"` messages in the transcript.** mini mixes control messages
  into the message list. Poo keeps the transcript to model and tool messages
  and records control outcomes as typed records and events.
- **A rich terminal UI.** `InteractiveAgent` renders to the console with
  `rich`. Poo v1 has no TUI; a future TUI is a wire client.
- **Third-party dependencies.** mini depends on `litellm`, `pydantic`, `jinja2`,
  `rich` and `dotenv`. Poo's core, `poo/prompt`, `poo/decide` and
  `poo/wire/protocol` depend on the standard library only, and CI checks
  that. The other opt-in packages may take dependencies.

## pi-durable

- **Source:** <https://earendil.com/posts/pi-durable/> ("Pi Durable", Earendil
  Engineering, dated Thu, 01 Oct 2026)
- **Retrieved (UTC):** 2026-10-20T15:38:42Z
- **sha256 of the fetched page bytes:**
  `38965254d2517030aaf2b7735b3b5c3882cfae6998209358ad5037cecfe1710c`
  (57,375 bytes, `text/html`)
- **Read:** the whole post, including the storage, extension, hook, task,
  compaction, document and multiplayer sections.

### Copied

- **Storage backends, and one owner per store.** pi-durable ships memory,
  SQLite and JSONL storage, and "one process owns a storage at a time" while
  other clients attach to that process. Poo takes the ownership half: the
  `flock` authority in `poo/session` (plan, §Package layout).
- **The state/session model: a harness owns conversations, each conversation
  owns an agent, and a run is a sequence of durable tasks.** poo maps this
  onto a session that holds a sequence of runs, one active run at a time,
  with the store, the projection and the authority lock as the session seam.
- **The task-and-checkpoint idea as the durability model.** pi-durable stores a
  checkpoint before every step, so a new process can find unfinished work. Poo
  takes the v1 subset: every record is written as it happens, and Stage 8
  recovery marks interrupted runs rather than continuing them.
- **Sections, not a monolithic prompt.** pi-durable rebuilds the system prompt
  from the sections of a conversation's extensions and records what changed.
  Poo's prompt builder is section-based too (plan, §System prompt), with the
  difference that poo freezes the rendered prompt for the length of a run so
  the provider's prompt cache stays valid.
- **Extensions as named bundles of prompt sections, tools and hooks.** poo's
  equivalent is a build-time composition of packages plus Go hook interfaces,
  wired at compile time rather than installed at runtime.
- **Tool calls as durable tasks.** pi-durable runs every tool call as its own
  durable task, and stores the call's intent before it runs. Poo's equivalent
  is a tool call as a record with an outcome, written as it happens.
- **A fork that shares history.** pi-durable forks a conversation at a point
  without copying it. Poo keeps the idea at the transport level only
  (`ForkFunc`, plan, §Compaction seam), where a fork re-sends a byte-identical
  request prefix.
- **A handoff note as the end of a run.** pi-durable's `reset(note)` starts a
  new context from a note, keeping the old messages searchable. Poo's
  `handoff` compaction strategy is the same shape, and it is the v1 default
  for `cmd/poo`.

### Rejected

- **The durable session mode.** pi-durable's SQLite-backed durable sessions
  and its "resume the interrupted run" behavior are out of scope for v1.
  Poo's `logged` mode records everything and always starts a new run on load;
  `durable` is a reserved mode that returns `unsupported_mode`.
- **A task graph with ownership trees and abort propagation.** pi-durable
  models conversations, tasks and their ownership as one tree, with bottom-up
  abort. Poo runs one agent loop per session and has no task graph.
- **Subagents.** pi-durable builds a subagent in a few lines because a
  conversation can own another conversation. Poo's v1 explicitly excludes
  subagents; orchestration belongs to avenor.
- **Forked conversations as a first-class session concept.** poo v1 has no
  conversation fork.
- **A registry that can be mutated while conversations run.** pi-durable
  installs extensions at runtime, and a running call finishes on the code it
  started with. Poo's customization is compile-time, so the question does not
  arise.
- **A declared replay policy for tool calls.** pi-durable tags each tool
  `replay: "safe"` or not, deciding whether a crash may re-run it. Poo does
  not re-run anything: Stage 8 recovery records an interrupted outcome and
  leaves the decision to the consumer. The plan records this as a deliberate
  divergence, not an omission.
- **Background tasks and durable timers.** Out of scope for v1.
- **Application state as typed documents.** pi-durable's `defineDoc` model for
  todos, plans and sandboxes is a nice idea, but v1 has no application state
  layer, and adding one would be a new locked-decision surface.
- **Compaction as a background task that summarizes older messages.** Poo's
  governing rule is that nothing rewrites the model's history silently. The
  `handoff` strategy that ships in v1 forks the request and ends the run with
  a note; it never rewrites or summarizes the history it leaves behind. No
  strategy that rewrites or summarizes older messages ships in v1; only the
  seam, and the `handoff` strategy on top of it.

### FSM state changes adopted from pi-durable

**None.**

The study was read against the plan's state set (`idle`, `requesting`,
`streaming`, `executing`, `compacting`, `settling`, `settled`) and did not
suggest a state change worth making. pi-durable's state model is a task and
ownership graph rather than a session FSM, so it offers no directly
translatable states; its concrete differences (durable resume, forked
conversations, background tasks) are all out of scope for v1.

Locked decision 8 constrains this study: it may reshape states, but it cannot
add a dependency, and it must not add a state that only pi-durable's durable
model needs. No state change is adopted; one question is raised for the owner
below.

**Proposal for the owner (not adopted, not approved).** pi-durable separates
"the conversation is idle" from "the conversation has nothing in flight" by
treating background work as part of the conversation but not part of its
current work: the conversation goes idle while a background task runs, and an
ordinary abort leaves it alone. Poo's v1 equivalent question is whether an
`idle` session with outstanding background work is reported as available for
a new run, and whether `agent/cancel` reaches that work. Poo v1 has no
background tasks, so there is nothing to model today. This is raised for the
owner's judgment, not decided here.
