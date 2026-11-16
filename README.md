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

**Settings discovery.** Every command resolves its settings file the
same way: the explicit `--settings` file, then the file the
`POO_SETTINGS` environment variable names, then the project's
`.poo/` directory walking up from the current directory, then the
user's config directory — `~/.config/poo` on Linux,
`~/Library/Application Support/poo` on macOS, `%AppData%\poo` on
Windows. Each of those directories is searched for `settings.json`,
then `settings.yaml`, `settings.yml` and `settings.toml`, and the
extension picks the parser. A directory holding more than one is
read through the first name it holds. A command that finds none runs
on the defaults, and the flags change them.
`examples/code.settings.yaml` is the file to start from.

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
`skip_probe` and `models` on an endpoint, and `model_id`, `inputs`,
`context_limit`, `max_output`, `reasoning`, `reasoning_levels`, `tools_mode`,
`quirks` and `sampling` on a model. A model's name is `endpoint/id`, so
`roles.chat: home/qwen3` runs a model the settings never mention. A key this
file does not have is a failure that names it.

A literal `api_key` is refused: a models file is the one an operator shares or
checks in. Name `api_key_env` for the environment variable that holds the key,
or the credential in the auth file.

A relative path resolves against the settings file's own directory. With no
`models_file` named, a `models.yaml` beside the settings file is read, so
`~/.config/poo/settings.yaml` and `~/.config/poo/models.yaml` set up providers
without the settings naming the file. A `models.json` beside the settings is
read as well: it is the file pi writes, its providers are imported, and a
provider whose entry carries no `apiKey` takes the key from the credential file
the settings resolved. A settings directory holding one of each is refused
rather than half read, and a models file that does not read fails the load. A
file the operator put there is never skipped in silence.

`models_file`, `auth_file` and `pi_auth_file` resolve relative paths
against the settings file's directory. `cli_registry.paths` and
`feeds.paths` answer to the working directory.

**Models.** The settings define the models: `endpoints` name the
servers (with `api_key_env` for the credential's environment
variable, or `api_key` for a key written in the settings), `models`
pair an endpoint with a model id, and `roles` assign a model to a
role. `roles.chat` that names an imported model runs it like any
other.

**pi's Codex provider.** The pi provider `openai-codex` is built in.
Its models are named `openai-codex/<id>`, and its credential is an
OAuth entry in a credential file. `auth_file` names poo's own file, and
a relative path resolves against the settings file's own directory,
like `models_file`:

```yaml
auth_file: ~/.config/poo/auth.json
roles:
  chat: openai-codex/gpt-6.1-sol
```

With no `auth_file` named, poo reads an `auth.json` beside the models
file when that file is there — the arrangement pi's own files already
have — and otherwise its own `~/.config/poo/auth.json`. Naming both
`auth_file` and `pi_auth_file` is refused: poo reads one credential at
a time.

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

`poo auth status` reports the file the settings resolved, the shape it
is written with, the provider, the account the token names, and whether
the token is one poo would use or would trade first. It prints no token
value and trades none, so a report leaves the credential as it found
it. `--json` prints the same report for a script.

No provider in `models.json` is needed for this. When a credential file
is there, the built-in provider adds the Codex models pi bundles; a
`models_file` or the settings' own `models` still win on a name
collision, and a model pi adds later can be named under the same
provider. The built-in list is a snapshot, so a newer id is a `models`
entry away.

poo reads the credential only for the provider id `openai-codex`, and
sends it only to the built-in `https://chatgpt.com/backend-api`
endpoint: the provider pointed anywhere else is refused, so an OAuth
token cannot reach an endpoint the operator did not name. A token that
pi would refresh is refreshed under the lock pi takes, and pi's file is
written back with the refreshed Codex entry and every other provider's
entry as it was found. The Codex models serve chat turns, so they cannot
be the decider, and the Codex endpoint is not probed for a model list.

**The repl.** `poo repl [flags]` takes the serve flags, starts a `poo
serve` with them, and renders the session in the terminal: replies as
markdown with highlighted code blocks, tool calls and results as
decorated blocks, and a statusline with the folder, git branch, model,
reasoning level, and context fill. Input reads with vi keys at a terminal.
Press Esc to stop a turn; Ctrl-C clears typed input or exits when the
prompt is empty. `/less` shows the transcript through `less -R`, and
permission asks use `y/n`. `/model` lists models, and `/model <name>`
switches the chat model between runs. `/thinking` or Shift+Tab cycles
reasoning levels; `/thinking <level>` selects one. The repl is a wire
client, and the
server runs the session.

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
- A rule matches a tool name, a tool class or a bash command prefix.
- `permission.unmatched` (or `--permission-unmatched`) decides what happens
  to a side-effecting call no rule covers: `allow`, `ask` or `deny`.
- An `ask` goes through the asker chain. The `decide` asker asks a decision
  model. The `wire` asker asks the client of `poo serve`.
- `--permission-fallback` is the answer when no asker answers.
- A call repeated too often is a doom loop, and the file tools refuse the
  globs in `tools.protected_paths`.

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
a profile with a side-effecting tool also offers `call_cli`. It runs one
registered command through the session's executor. Above 30 entries, with
a decision model configured, the profile also offers `find_tool` to search
the catalog. The code is in `poo/cli` and `poo/tool/cliregistry`.

A manifest can gate entries with an `availability` block. poo supplies the
condition `profile_side_effecting`, and the entries the rules leave are the
ones the profile offers: a profile with no side-effecting tool earns the
registry once the manifests declare conditions, and gets only the entries
that remain available. A manifest that reads another condition fails the
build naming it, because poo has no value for it.

**Codemode.** When the binary links the `codemode` tool and an engine,
the profile offers it beside the others: the model writes one script,
the script makes the tool calls, and only the script's output and
return value enter the context. The calls go through the nested-call
seam, so the permission engine and the doom loop judge every one of
them, and the session log records each with its parent's handle. The
first engine is Starlark (`poo/codemode/starlark`); the seam is
`poo/codemode.Engine`, and a binary that links no engine offers no
codemode tool. The settings are `tools.codemode.*`.

**Feeds.** When the settings name feed manifests in `feeds.paths`, the
session runs a long-lived command beside itself and delivers the messages
it prints as input. A feed is listed as `feed:<name>` and stopped with
`cancel_job`. Its manifest names the command, the environment, the restart
policy and whether it may wake an idle session. The code is in `poo/feed`;
[docs/build-your-own-binary.md](docs/build-your-own-binary.md#feeds) has
the manifest and the protocol.

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

Requires [mise](https://mise.jdx.dev/) and Go 1.25 (see `mise.toml`).

```
mise exec -- go vet ./...
mise exec -- go test -race ./...
```

## License

Apache-2.0. See [LICENSE](LICENSE).
