# poo

Poo is a modular agent harness written in Go, with two deliverables from one
codebase: a **headless CLI** that runs a session to completion without a TUI,
and a **Go library** that a wrapping application links into its own binary,
injecting its own tools, providers and policies.

The customization story is "import it and make your own", not "use our binary
and our plugin surface". Tools, providers and policy modules are ordinary Go
packages wired at compile time, so a consumer compiles a harness containing
exactly what it wants — and what it leaves out cannot be reached at runtime.
[docs/build-your-own-binary.md](docs/build-your-own-binary.md) is the recipe:
the factory, the injected tools, the conformance suite and the closure gate.

**Status: experimental.** The Go APIs, settings and wire protocol may change
without a deprecation period until the TUI launches.

## Install

From a release, into `~/.local/bin` (set `BINDIR` for another directory):

```
curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | sh
```

Or from source with Go 1.26 or newer:

```
go install github.com/sdougbrown/poo/cmd/poo@latest
```

To build from a checkout instead, see [Development](#development).

## What works today

A session runs against an OpenAI-compatible endpoint and settles with a
reason:

```
poo run --endpoint http://localhost:4000/v1 --model-id gemma4 \
  --context-limit 262144 --max-turns 8 --log session.jsonl \
  "List the Go packages in this repository."
```

**The front door.** A bare `poo` launches the interface the binary
links as its front door — the repl. The settings pick another one:
set `ui` in the settings file the command resolves — the `--settings`
file, the `POO_SETTINGS` environment variable's, the project's
settings directory (found by walking up), or the user's config
file. A binary with none linked, or a name it does not offer, refuses
the bare invocation and names what it does offer.

**Settings discovery.** Every command checks these locations in order:

1. The file named by `--settings`.
2. The file named by `POO_SETTINGS`.
3. A `.poo/` directory, starting in the current directory and walking up.
4. The user's config directories:
   - Linux: `$XDG_CONFIG_HOME/poo` if set, otherwise `~/.config/poo`.
   - macOS: `~/Library/Application Support/poo` first, then `~/.config/poo`.
   - Windows: `%AppData%\poo`.

In each directory, poo tries `settings.json`, `settings.yaml`,
`settings.yml` and `settings.toml` in that order. It reads the first
file it finds and uses the extension to pick the parser. Without a
settings file, poo runs on defaults that flags can override.

On macOS, the default location for `AGENTS.md` remains
`~/Library/Application Support/poo`.

Start with `examples/code.settings.yaml`. For sandboxed bash and prompts
before side-effecting calls, use `sandboxed.settings.yaml`. For built-in
providers that use a credential file, use `codex.settings.yaml` or
`opencode.settings.yaml`. Copy the example to `.poo/settings.yaml` or
save it as `settings.yaml` in one of the user directories above.

**One help per command.** Every command declares its own usage and its
own flags, and `poo <command> --help` (or `poo help <command>`) prints
that command's section — `poo sessions prune --help` names
`--older-than`, `--keep` and `--dry-run`. The root help keeps the
shared flags, the settings discovery rules above and the front-door
note. The declaration is the source: the help, the parser and
`--version` all come from it.

**`poo version`.** The binary answers what it is from its own build
information: the module version for one installed with
`go install ...@version`, and the revision, time and modified flag for
a build from a checkout. `poo version --json` prints the fields a
script pins it by.

**A models file.** `models_file` names a models file: what can be run, and how
to reach it. It declares endpoints, each with the models it serves, and poo
reads it as a catalog layer below the settings: a field the file sets is
overridden by the settings one field at a time, and `poo models --explain` names
the file's layer `models_file`.

```yaml
endpoints:
  home:
    endpoint: http://localhost:4000/v1
    api_key_env: HOME_KEY
    models:
      qwen3:
        context_limit: 32768
        max_output: 4096
        inputs: [text, image]
        tools_mode: auto
        sampling:
          temperature: 0.7
```

The file is YAML, and JSON is read as an alternative spelling of it. Its
vocabulary is the settings' own: `endpoint`, `api`, `api_key_env`,
`skip_probe`, `instructions_per_question` and `models` on an endpoint,
and `model_id`, `ops`, `inputs`, `context_limit`, `input_limit`,
`max_output`, `reasoning`, `reasoning_levels`, `tools_mode`, `quirks`
and `sampling` on a model. A model's name is `endpoint/id`, so
`roles.chat: home/qwen3` runs a model the settings never mention. A key
this file does not have is a failure that names it.

`context_limit` is the whole window the prompt and the answer share.
`input_limit` is the largest prompt the endpoint accepts, for one that takes
less than its window: poo bounds the prompt by the smaller of the two, and
still sizes the answer from the window. It reads the same in the settings.

A literal `api_key` is refused: a models file is the one an operator shares or
checks in. Name `api_key_env` for the environment variable that holds the key,
or the credential in the auth file.

A relative path resolves against the settings file's own directory. With no
`models_file` named, a `models.yaml` beside the settings file is read, so
`~/.config/poo/settings.yaml` and `~/.config/poo/models.yaml` set up providers
without the settings naming the file. A `models.json` beside the settings is
read as well: it is the file pi writes, its providers are translated into the
same layer, and a provider whose entry carries no `apiKey` takes the key from
the credential file the settings resolved. A settings directory holding one of
each is refused rather than half read, and a models file that does not read
fails the load. A file the operator put there is never skipped in silence.

The one exception is a literal key: a key pi's file writes is copied into the
settings, where a literal key lives, and the layer never carries one.

`models_file`, `auth_file`, `pi_auth_file`, `system_prompt.base_file`,
`finish.schema_file` and the files `profiles_files` name resolve
relative paths against the settings file's directory. `cli_registry.paths`,
`feeds.paths` and `skills.paths` answer to the working directory, and a
path in them that starts with `~/` resolves under the home directory.

`poo auth import pi` copies the Codex credential pi's `auth.json` holds into poo's
file, once. It reads the file `--from` names, then the one `pi_auth_file` names,
then pi's own `~/.pi/agent/auth.json`. It is a one-way door: a refresh rotates the
refresh token, so two tools cannot both keep one credential alive. After the copy,
pi's is stale from poo's first refresh, and a refresh pi makes first would
invalidate poo's. Run one of them. Nothing pi's file holds is changed, and an
existing credential is replaced only by `--force` or by answering the prompt.

`poo roles` is the check to run before a session: each role the settings assign
is resolved against the catalog, and printed with the provider that serves it
and where that provider's key comes from. No key is printed and no credential is
traded, and a role that does not resolve exits non-zero, naming the models that
would have served it.

**Models.** The settings define the models: `endpoints` name the
servers (with `api_key_env` for the credential's environment
variable, or `api_key` for a key written in the settings), `models`
pair an endpoint with a model id, and `roles` assign a model to a
role. `roles.chat` that names an imported model runs it like any
other.

**pi's Codex provider.** The pi provider `openai-codex` is built in.
Its models are named `openai-codex/<id>`, and its credential is an
OAuth entry in a credential file. Run `poo login codex` to sign in:

```yaml
roles:
  chat: openai-codex/gpt-6.1-sol
```

On macOS, poo's default is `~/Library/Application Support/poo/auth.json`.
If that file is absent, poo uses an existing `~/.config/poo/auth.json`.
If neither exists, login creates the native file. Poo does not merge the files.
Other platforms use `auth.json` in poo's platform config directory.

Set `auth_file: auth.json` to keep the credential beside your settings.
Pass the same `--settings` path to `poo login` when using this option.
Without `auth_file` or `pi_auth_file`, a loaded models file makes poo read
`auth.json` beside that models file instead of the default. This rule does
not redirect login writes. Poo rejects settings that name both credential paths.

poo's own file is versioned, holds the credentials under it, and is written as
YAML — the format an operator edits, and the one the models file is written with.
The entry keeps pi's field names, so one file of either shape is one credential:

```yaml
version: 1
credentials:
  openai-codex:
    type: oauth
    access: …
    refresh: …
    expires: 1767225600000
    accountId: …
```

The shape is the file's own, not a setting's: poo writes back the shape it read,
so pi's `auth.json` stays a file pi can read, and a refresh of it never gains a
version. poo's own file is written as YAML; a JSON file of that shape is read as
the same document, and the first write leaves it YAML.

`pi_auth_file` names pi's `auth.json` instead, which poo reads and writes in pi's
shape and encoding so pi keeps working.

`poo auth status` reports the credential the file holds: the file, the
shape it is written with, the provider, the account the token names, and
whether the credential is one poo would use or would trade first. A key
entry has no account and no expiry, and poo sends it as it is. The report
prints no secret and trades none, so it leaves the credential as it found
it. `--json` prints the same report for a script.

`poo auth refresh` checks the same credential file that model requests use.
It refreshes the `openai-codex` credential when the access token has five minutes
or less left. Use `--leeway 30m` to check for refresh earlier, or `--force` to
refresh regardless of expiry. The command prints nothing on success and exits
nonzero on failure. It does not start an agent or make a model request.

For idle agents, run the command from a `systemd` user timer or `launchd` job.
Pass the same settings file that the agents use, with an absolute path. For example:

```sh
poo auth refresh --settings /path/to/settings.yaml --leeway 30m
```

Schedule checks more often than the leeway and leave `--force` off the timer.
If OpenAI revokes the refresh grant, the timer cannot sign in again for you.

No provider in `models.json` is needed for this. When a credential file
is there, the built-in provider adds the Codex models the built-in record
declares; a
`models_file` or the settings' own `models` still win on a name
collision, and a model the record does not name can be named under the
same provider. The record is a snapshot, so a newer id is a `models`
entry away.

The record is a data file, not code: `builtin-models.yaml`, in poo's own
models-file vocabulary. `POO_BUILTIN_MODELS` names a file read instead of
it, in the same vocabulary. A binary that ships without the newer list
points the variable at a file that holds it, and a file that does not read
fails the load.

poo reads the Codex entry for the provider id `openai-codex` only, and
sends it only to the built-in `https://chatgpt.com/backend-api`
endpoint: the provider pointed anywhere else is refused, so an OAuth
token cannot reach an endpoint the operator did not name. A token that
pi would refresh is refreshed under the lock pi takes, and pi's file is
written back with the refreshed Codex entry and every other provider's
entry as it was found. The Codex models serve chat turns, so they cannot
be the decider, and the Codex endpoint is not probed for a model list.

A key entry is read by the endpoint that declares the same environment
variable: an endpoint reading `OLLAMA_API_KEY` takes the `ollama-cloud`
key after the variable itself and ahead of a literal the settings carry.

**The OpenCode providers.** The Console providers `opencode` and
`opencode-go` are built in — pay-as-you-go and Go, at their own base
URLs — and each has a Messages sibling, `opencode-anthropic` and
`opencode-go-anthropic`, because the gateway serves the Anthropic
Messages API beside the Chat Completions one and every model answers
through both. `poo login opencode` stores the Console key, and one key
serves every endpoint, because a key belongs to one workspace and the
workspace's plan says which endpoint bills it:

```yaml
roles:
  chat: opencode-go/glm-5.3
  # or the Messages sibling:
  # chat: opencode-go-anthropic/claude-haiku-5-5
```

Each endpoint's models come from the gateway's live list, cached locally
and refreshed as the catalog is built; the built-in record names one
model per endpoint because the loader requires an endpoint to serve
something, and the probe supplies every field it reports. The gateway
requires a session header on every chat request, so the endpoints
declare `session_header: x-opencode-session` and the transports carry
the session's id — on a turn, on a fork, and on the compaction a long
run makes. An endpoint the operator declares under another name names
its own header the same way, and a model no layer gave an API to speaks
the one its endpoint declares.

The Messages transport is the Anthropic one: content blocks, tool uses
that stream their arguments, and the thinking a model did — with the
signature the response carried, returned unchanged in the history that
answers it. An effort level rides as the output config's effort with
adaptive thinking, and a budget rides in the thinking field itself.

**The TypeSafe provider.** The decisions provider `typesafe` is built
in. Jev answers the questions a run asks about its state and serves no
chat turns, so it can be the decider and not the chat model. `poo login
typesafe` stores a key TypeSafe's console issues, and the
`TYPESAFE_API_KEY` variable reaches the same endpoint from the
environment. Like every built-in provider, the models are offered once
a credential file is there:

```yaml
roles:
  chat: opencode-go/glm-5.3
  decider: typesafe/jev-latest
```

The endpoint answers what each question asks and rejects a
request-level instructions field, so it declares
`instructions_per_question` and the decider folds the call's framing
into each question when it asks. Its model list is not the OpenAI
shape the probe reads, so the endpoint is not probed: the built-in
record is the word on what it serves, and a newer model than the
record names is a `models` entry away under the same endpoint,
declaring `ops: [systemone]`.

**When compaction fires.** Every strategy compacts when the next request no
longer fits: the prompt plus the answer's output need and a small reserve is
over the context window, or the prompt plus the reserve is over the endpoint's
input cap. A `context.summary.ratio` or `context.handoff.ratio` is an optional
override that adds an earlier trigger at that share of the prompt limit. The
removed `context.notice_ratio` and `context.hard_ratio` have no effect; a
settings file that still sets them loads with a warning.

**Native compaction.** `context.strategy: native` asks the provider to compact
its own conversation and gives the model the provider's compacted state back.
It is opt-in: the default stays `summary`, which writes its own summary of the
oldest turns.

A model must declare the `compact` operation, and its provider must have a
transport that can ask for the state. Neither is an assumption about a built-in
model: declare the operation in the model's settings.

```yaml
models:
  openai-codex/gpt-6.1-sol:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    ops: [chat, compact]
roles:
  chat: openai-codex/gpt-6.1-sol
context:
  strategy: native
```

The pi Codex provider is the only transport in this repository that implements
native compaction. A model reached over Chat Completions has no transport that
can produce the state, so a build that asks for `context.strategy: native`
refuses the settings and names the missing capability. A model switch is
checked when a compaction happens: a session that moved to a model without the
`compact` operation refuses to compact natively rather than asking a provider
that cannot serve it.

**The repl.** `poo repl [flags]` takes the serve flags, starts a `poo
serve` with them, and renders the session in the terminal: replies as
markdown with highlighted code blocks, tool calls and results as
decorated blocks, and a statusline with the folder, git branch, model,
reasoning level, and context fill. Input reads with vi keys at a terminal.
Press Esc to stop a turn; Ctrl-C clears typed input or exits when the
prompt is empty. Ctrl-J breaks the draft across lines; Enter submits
the draft whole. `/less` shows the transcript through `less -R`, and
permission asks use `y/n`. `/model` lists models, and `/model <name>`
switches the chat model between runs. `/thinking` or Shift+Tab cycles
reasoning levels; `/thinking <level>` selects one. The repl is a wire
client, and the
server runs the session.

**Continuing a session.** `poo repl --continue` (`-c`) reopens the logged
session with the latest activity whose working directory is the one the
repl runs in, and `poo repl --resume <id>` reopens the session that id
names. A bare `poo -c`, `poo --continue` or `poo --resume <id>` is the
same thing, with the rest of the line as the repl's flags. The loaded
session keeps the working directory, profile and model it recorded, and
the repl renders its scrollback — the newest 500 items — before the first
prompt; none of it is sent to the model again. `poo sessions list` names
the sessions by id. A session another process holds is skipped, and a
directory with nothing to continue, a pair of flags that conflict, or an
ephemeral session is a refusal with exit 2. `--resume <id>` opens a
session that works in the repl's directory: one that recorded another is
refused, with the directory to resume it from.

The repl is a package of the root module, and `go test ./...` at the
root covers it with the rest of the tree. Its dependencies — markdown
rendering and terminal input — are imported only by the repl package,
so a binary that links the poo core without the repl carries none of
them: Go links only the packages a binary imports.

**Profiles.** The profile decides which tools a session offers. Pick one
with `--profile`; the default is `code`.

| Profile | Tools |
|---|---|
| `base` | none |
| `code` | `bash`, `read`, `grep`, `edit`, `codemode` |
| `search` | `read`, `grep` |

A settings file that names `finish.schema_file` also gets the typed
`finish` tool.

**Disabled tools.** `tools.disabled` names the tools to leave out of
whatever profile the session runs under, so one settings file can keep a
tool off every profile. A name the profile does not offer does nothing; a
name the binary does not link, or one of the core's own `jobs`,
`cancel_job` and `finish`, is an error. A selected profile left with no
tool of its own is an error too, and such a profile is not one the binary
lists.

**Named profiles.** Define a profile in settings and select it with
`--profile`, the top-level `profile` setting, or `session/create.profile`.
For example, add this to an existing settings file:

```yaml
profiles:
  reviewer:
    base: code
    prompt_file: prompts/reviewer.md
    tools:
      disabled: [edit, bash, codemode]
```

`base` names a profile linked by the binary. The named profile's tool
exclusions add to the global `tools.disabled` list. Exclusions do not
change permission rules or sandbox the remaining tools.

`prompt_file` accepts an absolute path or a path relative to the settings
file. Poo reads it when opening a new session, not when listing profiles.
It replaces the `profile` section without replacing the base or tool
instructions. An explicit `system_prompt` replacement, or a setting that
sets or removes the `profile` section, takes precedence. A resumed
session uses its recorded prompt once it has run.

`feeds` names the feeds the profile needs, by manifest name:

```yaml
profiles:
  watcher:
    base: code
    feeds: [apron-room, ticker]
```

A named profile's feeds add to the base's declared feeds. They are a
presence assertion, not a switch: the settings' `feeds.paths` still
control which feeds load, and every loaded feed still runs, whether a
profile declares it or not. A declared feed the settings do not load is
skipped with a warning, never a failure, so a binary that links a profile
without its manifests runs the rest of the session unchanged. A resumed
session runs without a declared feed the current settings no longer
load, and says so with the same warning.

**Profiles files.** `profiles_files` names the files that hold profile
definitions, so a settings file that has grown a long `profiles` map can
hand them to files of their own:

```yaml
profiles_files: [profiles.yaml, team/profiles.yaml]
```

A profiles file uses the settings' own vocabulary under a `profiles`
key, and its name's extension says its format — YAML, with JSON read
as an alternative spelling of it. A key the file does not have, a file
that does not read, and a file that declares no profiles all fail the
load.

The files layer below the settings' own profiles, in the order they are
named: the settings' word on a profile name wins over a file's, and the
first file that names one wins over the later files. A `prompt_file`
inside a profiles file resolves against the profiles file's own
directory, not the settings file's, so a file travels with its prompts.

**Exit codes.** The exit code says why a run ended:

| Code | Reason |
|---|---|
| 0 | `end_turn`, `finished`, `stop_requested` |
| 10 | `output_truncated` |
| 11 | `max_turns` |
| 12 | `hook_continue_limit` |
| 13 | `context_exhausted` |
| 14 | `budget_exhausted` |
| 20 | `provider_error` |
| 21 | `tool_fatal` |
| 22 | `hook_error` |
| 23 | `store_error` |
| 24 | `provider_limit` |
| 130 | `cancelled` |

`poo run` exits 2 when it refuses its arguments, its settings or a prompt
that does not render. It exits 1 for a failure with no code of its own.

**Permissions.** A policy checks every tool call before it runs.
- A rule matches a tool name, a tool class, a bash command prefix, or substrings
  a bash command carries.
- `permission.unmatched` (or `--permission-unmatched`) decides what happens
  to a side-effecting call no rule covers: `allow`, `ask` or `deny`.
- An `ask` goes through the asker chain. The `decide` asker asks a decision
  model. The `wire` asker asks the client of `poo serve`.
- `--permission-fallback` is the answer when no asker answers.
- A call repeated too often is a doom loop, and the file tools refuse the
  globs in `tools.protected_paths`.

File tools can reach paths outside the working directory by default. To restrict
individual tools, set:

```yaml
tools:
  read: {restrict_to_working_dir: true}
  grep: {restrict_to_working_dir: true}
  edit: {restrict_to_working_dir: true}
```

Restricted `read` can also reach the session's skill directories. Protected
paths still apply in either mode. These tool restrictions do not confine `bash`;
use a whole-process sandbox when isolation is required.

To keep the default reach but ask before a call uses it, set
`ask_outside_working_dir` on the same tools:

```yaml
tools:
  read: {ask_outside_working_dir: true}
  grep: {ask_outside_working_dir: true}
  edit: {ask_outside_working_dir: true}
```

A call one of these tools makes to a path outside the working directory then
goes through the asker chain after the policy's rules, whatever the unmatched
action says. The asker sees the call and its path. A tool that is also
restricted refuses paths outside the working directory before the policy runs,
so only the read roots a restricted `read` still accepts — the session's skill
directories — reach the ask.

The policy is in `poo/perm`.

**Sessions.** A logged session writes one JSONL file under `session.dir`,
one record per line. [docs/session-log-v0.md](docs/session-log-v0.md) lists
the records.
- `--log <path>` writes the log to a new file of your choice. It never
  extends an existing file.
- `--ephemeral` writes nothing.
- `poo sessions list` and `poo sessions prune` read and trim the store.
- `session/load` over the wire (or `session.Resume` in Go) resumes a
  stored session. A torn tail is recovered, an unfinished run is marked
  `interrupted`, and undelivered job results are queued.

**The wire.** `poo serve` speaks JSON-RPC 2.0 on stdin and stdout, one
object per line.
- Sessions: `session/create`, `session/load`, `session/configure`,
  `session/history`, `session/close`.
- Runs: `run/prompt`, `run/message`, `run/inspect`, `run/cancel`.
- The server also sends events, permission asks and job updates.

The contract is `poo/wire/protocol`. The conformance suite in
`poo/wire/conformance` drives any binary through it. Its fake endpoint
is exported (`conformance.NewFakeEndpoint`), so a client's own tests
can run a real binary against scripted turns.

**Prompts.** The system prompt is a set of named markdown sections, each
embedded beside the package that owns it.
- A binary can replace sections with its own directory
  (`server.Options.PromptFS`).
- The `system_prompt.*` settings, `--system-prompt` and
  `--append-system-prompt` (`@file` reads a file) apply on top.
- `session.user_prompt` adds the user's own instructions, read once from
  `session.user_prompt_file` or the binary's `BuildOptions.UserPromptFile`;
  `session.project_prompt` adds the working directory's `AGENTS.md`, read
  per session from the directory that session works in.
- `skills.paths`, `skills.default` and `skills.user_path` name directories of
  skills — each a folder with a `SKILL.md` — and the prompt carries a catalog
  line per skill. The guidance is a file the model reads with the read tool
  when a description fits, so a session offering thirty skills pays for thirty
  lines. `disable-model-invocation` keeps a skill out of the model's catalog.
- `poo prompt` prints the sections a session would send.

**CLI registry.** When the settings list manifests in `cli_registry.paths`,
a profile with a side-effecting tool also offers `call_tool`. It runs one
registered command through the session's executor. Above 30 entries, with
a decision model configured, the profile also offers `find_tool` to search
the catalog. The code is in `poo/cli` and `poo/tool/cliregistry`.

A manifest can gate entries with an `availability` block. poo supplies the
condition `profile_side_effecting`, and the entries the rules leave are the
ones the profile offers: a profile with no side-effecting tool earns the
registry once the manifests declare conditions, and gets only the entries
that remain available. A manifest that reads another condition fails the
build naming it, because poo has no value for it.

**Non-CLI tools.** A registered tool need not be a CLI: a manifest's
`json` binding puts an object- or array-valued property's JSON on the
script's stdin, and `output.mode: json` makes the script's JSON result
the tool result. The tool joins the same catalog, permission and
background machinery as any `call_tool` entry, and the script can be
written in any language.

**Codemode.** When the binary links the `codemode` tool and an engine,
the profile offers it beside the others: the model writes one script,
the script makes the tool calls, and only the script's output and
return value enter the context. The calls go through the nested-call
seam, so the permission engine and the doom loop judge every one of
them, and the session log records each with its parent's handle. The
engine the `poo` CLI runs is QuickJS (`poo/codemode/quickjs`), and the
first engine, Starlark (`poo/codemode/starlark`), is one setting away. The
seam is `poo/codemode.Engine`, and a binary that links no engine offers no
codemode tool. The settings are `tools.codemode.*`. A script may run in the
background like any other call: pass `background: true`, and its result
arrives as a job's. The script's own tool calls keep running after the run
that started it ends, until the job's timeout.

QuickJS runs as WebAssembly and does not require Node.js. A script is the
body of an async JavaScript function: `await tools.<name>(args)` chains
calls, and `Promise.all` overlaps independent reads. Use `timeout_s` to
bound runtime. `exec_steps` is only supported by Starlark, so a settings
file that sets it must also name that engine:

```yaml
tools:
  codemode:
    engine: starlark
    exec_steps: 250000
```

**Feeds.** When the settings name feed manifests in `feeds.paths` (a
path that starts with `~/` resolves under the home directory), the
session runs a long-lived command beside itself and delivers the messages
it prints as input. A feed is listed as `feed:<name>` and stopped with
`cancel_job`. Its manifest names the command, the environment, the restart
policy and whether it may wake an idle session. The code is in `poo/feed`;
[docs/build-your-own-binary.md](docs/build-your-own-binary.md#feeds) has
the manifest and the protocol. The [Avenor example](examples/avenor/README.md)
uses a feed and the CLI registry to delegate and reply through a local supervisor.

**Your own binary.** A consumer links only the packages it wants, hands
its tools, profiles and executors to `server.New`, and serves the result
with `wire.Serve`. What it leaves out is not in the binary.
[docs/build-your-own-binary.md](docs/build-your-own-binary.md) is the
recipe, including the prompt directory, the conformance suite and the
closure gate.

## Layout

| Package | Role |
|---|---|
| `poo` | Core: agent loop FSM, tool registry, hooks, event stream, session lifecycle |
| `poo/decide` | Decision-model types: questions, answers, the decider, the recording wrapper |
| `poo/prompt` | The system prompt's vocabulary: named sections, the alternatives they offer, and the environment they read |
| `poo/pootest` | Exported test helpers: a scripted transport, an in-memory sink, a stub decider, a scripted compactor |
| `poo/tool/*` | Opt-in tools: `read`, `edit`, `grep`, `bash`, `cliregistry`, `codemode`, `fetch` |
| `poo/cli` | The cli registry's catalog: the manifests a session may call |
| `poo/feed` | The feed manifests a session runs: the loader, the environment, and the runner over a command's stdout |
| `poo/exec` | The executor contract a binary fills: a command, its environment, and where it runs |
| `poo/protect` | The protected-path matcher: the paths a tool must not touch |
| `poo/server` | The `wire.Factory` a `serve` command is built from |
| `poo/provider/*` | Provider seam and transports |
| `poo/perm` | Permission engine: the policy over tool calls, the doom loop's action, and the asker chain that answers a call the policy asks about |
| `poo/perm/decide` | An asker that answers a permission call with a decision model |
| `poo/session` | Session seam: store, versioned codec, projection, authority |
| `poo/wire` | JSON-RPC 2.0 control protocol over stdio |
| `poo/wire/protocol` | The public contract: the method names, the error codes, and the request and result types a client depends on |
| `poo/wire/conformance` | The black-box suite that drives any binary through the contract, and the scripted fake endpoint its tests and a client's tests share |
| `poo/config` | Opt-in settings loading and validation |
| `poo/cmd/poo` | The headless CLI |

The core depends on the standard library only (plus `poo/prompt` and
`poo/decide`). `internal/depcheck` enforces this in `go test`.

The message model is provider-neutral and carries what the three wire
APIs need. The round trip through Chat Completions, OpenAI Responses and
Anthropic Messages is written down in
[docs/wire-api-mapping.md](docs/wire-api-mapping.md).

## Development

Requires [mise](https://mise.jdx.dev/) and Go 1.26 (see `mise.toml`).

```
mise exec -- go vet ./...
mise exec -- go test -race ./...
```

## License

Apache-2.0. See [LICENSE](LICENSE).
