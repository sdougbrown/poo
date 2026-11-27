# Build your own binary

Poo is a library first. A consumer composes the packages it wants into a
binary of its own. It picks the tools, the executors and the profiles it
links, and the providers its settings name. What the binary leaves out is
not reachable at runtime, because it is not in the binary.

`cmd/poo` is one such binary.
`wire/conformance/testdata/readonly` is the smallest. It serves the search
profile, `read`, `grep` and the OpenAI transport, with no shell and no
tool that can change anything.

## The factory

`server.New` takes the settings and the tool set, and returns the wire
factory. Serve it with `wire.Serve`:

```go
settings, err := config.Load(*settingsPath)
if err != nil {
	return err
}
srv, err := server.New(server.Options{
	Settings:       settings,
	WorkingDir:     dir,
	DefaultProfile: search.Name,
	Tools: map[string]config.ToolFactory{
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
	},
	Profiles: []profile.Profile{search.Profile()},
	Build:    poo.BuildInfo{Name: "poo-readonly", Version: version},
})
if err != nil {
	return err
}
return wire.Serve(wire.Options{
	In:      os.Stdin,
	Out:     os.Stdout,
	Factory: srv,
	Build: protocol.Build{
		Name:       "poo-readonly",
		Version:    version,
		PooVersion: version,
	},
})
```

The factory maps the protocol onto `config`: the handshake reports the
profiles, the default profile, the capabilities and the models. A session's profile and model
pick a runtime, and the store, the prompt overrides and the permission
asks are the session's own.

`permission.askers` is the one list whose emptiness means something: an
explicit empty list is a chain with no askers, so an `ask` goes straight to
the fallback, and an absent list takes the binary's own chain (`[wire]` in
`poo serve`).

A chain that names an asker the current runtime cannot provide loses that
asker instead of the whole file failing: `poo run` builds no wire client, so
`askers: [decide, wire]` runs with the decide asker and warns where the wire
asker went. The settings schema carries the rule — a `fairWhen` on
`permission.askers` over the `has_wire_client` condition — so the reason the
warning prints is the schema's own, and one settings file serves the repl and
a headless run alike.

A chain that asks the wire only reaches a client that declared
`capabilities.permission_answers` in `poo/initialize`. A client that did
not is never asked, and every ask falls back; the session's create and
load results say so in their `warnings`, so a consumer learns the
capability it is missing with the first session it opens.

`server.Options.Overrides` carries what a command line changed. A custom
binary can set `FeedsPaths` and `CLIRegistryPaths` there to add manifests.
`server.New` also adds the environment paths; leave the variables unset to
use only the paths supplied by the binary and settings. A binary that calls
`config.Build` directly must call `config.EnvOverrides` itself to add the
environment paths. `poo run` does this automatically.
`SessionTools` changes every runtime the factory builds, before a session
is created from it. The typed result's `finish` tool is added that way,
because the settings opt into it and no profile names it.

## Your own prompt

Each prompt section lives in a markdown file beside the code that owns it, and
the package embeds it. A binary that wants its own prose embeds a directory and
hands it to the factory:

```go
//go:embed prompts
var promptDir embed.FS

prompts, err := fs.Sub(promptDir, "prompts")
if err != nil {
	return err
}
srv, err := server.New(server.Options{
	// ...
	PromptFS: prompts,
})
```

`config.BuildOptions.PromptFS` is the same directory for a binary that builds
its runtime without the factory.

Each file is named after the section it replaces. The names are the default
order's (`prompt.Order()`): `base`, `profile`, `catalog`, `environment` and
`project`, and `tools.<tool>` for a tool's own section. A directory in the path
names the section's prefix, so a tool's file is `tools/bash.md`. A variant is a
sibling whose name carries it, `tools/bash.small-local.md`.

A profile can bring a section of its own. `profile.Profile.Prompt` is the
profile's prose, and it renders in the `profile` section, right after `base`:
the code profile's coding-agent guidance lives there, and a profile that offers
no tool of its own ships none. A binary's `profile.md` replaces it, the way
`base.md` replaces the base.

```
prompts/
  base.md
  environment.md
  tools/bash.md
  tools/bash.small-local.md
```

A file is a `text/template`. The template renders with the data the default it
replaces carries. The library's environment section reads `{{.Cwd}}`,
`{{.OS}}`, `{{.Date}}` and `{{.Model}}`. The catalog section reads
`{{.Search}}` (a bool), `{{.Count}}` (an int), `{{.Call}}` and `{{.Find}}` (the two
tool names), and `{{.Entries}}`, a list whose entries have `{{.Name}}` and
`{{.Description}}`. `tools/jobs.md` reads `{{.Feeds}}`, the feeds the session
runs, each with a `{{.Name}}` and a `{{.Description}}`. A section whose prose
names no field renders the same text whatever the data is.

A file's final newline is not part of the section. Each file ends with one, and
the renderer drops it. A file that ends with a blank line keeps the newline it
meant.

A file that names a section the binary does not build is an error that names
the file. So is a file that is not markdown. A linked tool's section loads
for every profile. Only profiles that offer the tool render its section.
One directory can serve a binary with different tool profiles. A variant the directory does not
carry falls back to the section's default text. A default file replaces the
section's default text and keeps the alternatives the section already offers,
including the embedded ones: omitting a variant from the directory does not
remove a variant the section already had.

A template is checked when it loads and rendered when the session is built. The
model, tools, working directory, variant, settings and project file are all
known by then. A template that renders for one session and not another fails
the session, not the run. `config.Build` renders the prompt once, so `poo run`
fails through the settings' own error path. The failure is a `config.Error` with
the `invalid_value` code and the `system_prompt` field, and the file is named
underneath. A session built over the wire is refused at `session/create` with
the `invalid_settings` data code.

A session that changes its model or variant renders the prompt again. A prompt
that cannot render there refuses the change and leaves the session as it was.

The layers apply in this order: the embedded default, the binary's directory,
the settings (`system_prompt.*`), the user's own instructions and the project's
`AGENTS.md`. A later layer wins, so an operator's settings file still changes
the prompt the binary ships. `system_prompt.replace` clears the directory's
sections. If the session offers registry tools, the build restores the embedded
catalog after replacement, so a directory's `catalog.md` does not apply.

The skills catalog is a section of its own and not a layer: `skills.default`,
`skills.user_path` and `skills.paths` name the directories it is built from,
and a session that names none carries no section. It renders after the catalog
and before the environment.

The last two layers are opt-in, and they differ in scope. The user's
instructions are process-wide: `BuildOptions.UserPromptFile` names the path a
binary reads them from when `session.user_prompt_file` names none, and an empty
value leaves the feature with no default at all. The project's are the
session's: `Runtime.ProjectInstructions` reads the directory the session works
in, so a session created over the wire reads its own directory's file and not
the serving process's.

## Inject tools

A tool constructor takes the settings' resolved options and returns a
`poo.Tool`:

```go
type ToolFactory func(ToolOptions) poo.Tool

type ToolOptions struct {
	Executor       exec.Executor
	Scrub          exec.Scrub
	ProtectedPaths *protect.Set
	Registry       *cli.Catalog
	Search         bool
	Inputs         []poo.Modality
	CodemodeEngine string
	ToolNames      []string
	Offered        cli.Offerings
	MCP            []MCPRunning
}
```

`ToolNames` is the names the session offers, for a tool whose refusal points
at another tool: a tool the session does not offer is not named. `Offered`
reads the entries the session's calls dispatch, as they are when the tool asks
— the registry's commands and the MCP tools the session's servers listed — so
one snapshot serves a search, a refusal and the prompt. `MCP` is the servers a
session started, in selection order, as the binary's own launcher returned
them; a build holds none, because a build starts no server, and the config
layer replaces the tools over them once a session has started its own.

The map's key is the name the profile, the permission rules and the wire
use. It must be the name the tool's `Spec` reports. A profile or a
permission rule that names a tool the binary does not register is a typed
error. So is a cli registry whose `call_tool` the binary does not link.

Pass the engine the binary links to `tool/codemode`. This example pins
Starlark and ignores `tools.codemode.engine`. The `poo` command links both
engines, defaults to QuickJS, and selects one using `CodemodeEngine` (an
unset `tools.codemode.engine` reaches it as `quickjs`):

```go
"codemode": func(config.ToolOptions) poo.Tool {
	return codemode.New(starlark.New())
},
```

A binary that links no engine offers no codemode tool, and takes its schema
and its prompt section with it. `tools.disabled` leaves the tool out of
whatever profile a session runs under instead, so a light-duty project
drops it from its settings rather than from its binary.

The settings still decode and validate in `config`. `tools.bash.*`, the
executor name and `cli_registry.paths` are settings, not tools. Nothing in
`config` imports a tool package, so reading a settings file links none.

The executors are the same seam. A binary registers the ones it links, and
the session's settings name one:

```go
registry := exec.NewRegistry()
registry.Register("local", func(exec.Options) (exec.Executor, error) {
	return bash.NewLocal(), nil
})
```

The executor is resolved only for a profile that offers `bash`. A
read-only binary passes no registry, and it runs no command.

To see what a binary actually links, ask the toolchain:

```sh
mise exec -- go list -deps ./cmd/my-poo | grep 'poo/tool'
```

## Non-CLI tools

A registered tool need not be a CLI. The manifest's `bindings` accept a
`json` style: an object- or array-valued property's value is serialized
as JSON onto the script's stdin. The style requires the bound property
to be a concrete object or array in the schema, and a tool takes at
most one `stdin` or `json` binding. A `flag` is optional and is emitted
only if the property is present. A manifest with `output.mode: json`
makes `call_tool` hand the model the script's JSON result as the tool
result, bypassing the bash exit-code envelope:

```json
{
  "description": "Sum a pair",
  "command": ["python3", "fn_notes.py"],
  "bindings": [{"property": "data", "style": "json"}],
  "output": {"mode": "json"}
}
```

The contract is one sentence: stdin JSON args in, stdout JSON result
out, nonzero exit is an error with stderr as diagnostics.

cli-registry (the github.com/sdougbrown/cli-registry repository) ships
`helpers/python/poo_fn.py`, a convenience that reads the JSON from
stdin and writes the JSON to stdout; the contract itself needs nothing
but a script that does the same. Its tree carries the runnable example:
`examples/fn.json` with `examples/fn_notes.py`.

The tool gets the same machinery as any `call_tool` entry: the same
catalog and `find_tool` prompt section, the same permission and
background treatment, and arguments validated against the schema before
a process exists. It is statically side-effecting like any `call_tool`
tool, because a script can do anything.

One edge: an fn tool whose result is JSON `null` falls back to the
exit-code envelope. A tool that can return null should return an object
such as `{"result": null}` instead.

## Feeds

A feed is a long-lived source beside a session: a process whose stdout is
messages. The session starts every feed its settings name, delivers what
the feed prints as input, and stops it when the session closes. A feed the
manifest marks `optional` waits instead, and the model starts it with the
`subscribe_feed` tool.

A manifest names the feeds, and it is JSON, YAML or TOML. `feeds.paths`
lists the files and directories the session loads, and every feed under
them is active:

```json
{
  "version": 1,
  "feeds": {
    "inbox": {
      "description": "Messages from the team's queue.",
      "command": ["my-cli", "inbox"],
      "env": {"QUEUE_URL": "env:QUEUE_URL", "QUEUE_MODE": "batch"},
      "restart": true,
      "on_idle": "wake"
    }
  }
}
```

`version` is 1. A name is lowercase letters, digits, `-` and `_`, and it
names the feed everywhere its records go: its origin is `feed:<name>`.
`command` is the argv and is required. `description` is what the jobs
prompt says about the feed. `max_pending` caps the feed's undelivered
inputs at that many; past the cap the oldest undelivered input is
dropped unread (default 16). A bad version, a name outside the pattern, an
empty command, an `on_idle` that is neither `wake` nor `hold`, a negative
`max_pending`, an unknown
field or one name in two manifests fails the build, so the binary does not
start, and the failure names the file and the feed.

`feeds.paths` is a settings entry beside `cli_registry.paths` and
`mcp.paths`. poo also loads the manifests in its own directories under
the user config directory, before the settings' paths. The directories
are `$XDG_CONFIG_HOME/poo/feeds/`, `$XDG_CONFIG_HOME/poo/cli-registry/`
and `$XDG_CONFIG_HOME/poo/mcp/`. A manifest a person wants in every
session needs no settings entry. A missing directory is not an error,
and a name two directories define is the same duplicate-name error as
any other. `feeds.defaults: false`, `cli_registry.defaults: false` and
`mcp.defaults: false` turn the defaults off, and a profile's
`feeds_defaults`, `cli_registry_defaults` and `mcp_defaults` speak last.
A binary or a test then runs with exactly the manifests it names.

For binaries built on `server.New` and for `poo run` and `prompt`,
`POO_FEEDS_PATH`, `POO_MCP_PATH`, `POO_CLI_REGISTRY_PATH` and
`POO_SKILLS_PATH` add paths to those settings respectively. Each is a
path list separated by the operating system's
path-list separator. The `poo run`, `serve` and `prompt` commands also
accept repeatable `--feeds-path`, `--mcp-path` and `--cli-registry-path`
flags. Override paths add to the settings' paths. Every path is
discovered and the files
are sorted; path order affects only which missing path is reported first.
A relative path resolves against the binary's working directory, the one
it is built in, not a session's. A feed path from settings, a flag or the
environment makes a binary that links no executor fail to build, with an
error naming the source. The variables are scrubbed from commands a session
runs.

**Environment.** A feed's command gets the cli registry's benign names and
the manifest's `env` entries, and nothing else: a name the session scrubs
with `tools.env.scrub` never reaches it. An entry is a literal,
`env:NAME`, which reads the session's scrubbed environment, `session:id`,
which resolves to the session's own ID when the feed
starts — the same ID after a restore, and nothing the command says or
does can change it — or `scope:NAME`, which resolves to the value a
subscription supplied for that scope field. A reference that does not
resolve fails the attempt, and the command does not start.

**Stdout.** One JSON object per line:

```
{"text": "the message", "from": "alice"}
```

`text` is required unless the message names an image. `from` names the sender and may be
left out. A message may name an image beside its text:

```
{"text": "the look", "image": {"path": "shot.png", "media": "image/png"}}
```

A message that names an image may leave its text empty — the image is
the message. The image's `path` is required, and a relative path answers
to the directory the feed's command runs in. `media`, when the feed
names it, is the media type the file's own head must confirm: the
session reads the bytes at the acceptance, caps them at the 5 MB a model
takes, and refuses an image that does not read, is not one the head
names, contradicts the declared type, or is past the cap — and refuses
it too when the session's model reads no images, rather than spending
the wake on a picture the request would drop. The attempt fails, the
way it does for a malformed line. A message that names no `media` is
delivered with the type the head names. The image arrives beside the
text as the model's own picture, and the acceptance record carries its
bytes, so a held input is still one after a restart. A message that
carries an image is asked about its text, and the picture rides the
answer; a message whose text is empty — the image alone — is delivered
unasked, for the decider never sees a picture. The optional `id` field is reserved; poo ignores it today. A field
the object does not name is ignored, and a blank line is
skipped. A line is at most 1 MiB. The first line that is not such an
object, a longer line, or an input the session refuses is the attempt's
failure: the command is stopped, what it says afterwards is discarded, and
it is reaped anyway. The command's stderr goes to the session's log, never
to the model and never into an input.

**Restart.** A feed with `restart` runs again after its command exits or
fails. The next attempt waits 1s, and each exit or failure doubles the
wait to 60s; an attempt that ran 60s or longer starts the wait over. A cancel and
a close win over a pending restart.

**The idle policy.** `on_idle` decides only whether a feed's input may
start a run by itself. `wake` lets an idle session wake for it, `hold`
queues it until a run is going, and a feed without `on_idle` inherits
`jobs.on_idle`. An optional feed without `on_idle` holds instead, because a
feed the model chose to watch wakes an idle session only when the manifest
says so. A run that is woken or prompted takes every queued input, held
ones included.

**Feeds a session starts on demand.** An entry marked `optional: true`
never runs at a session's start. Its `scope` declares the values a
subscription supplies, one field at a time, each with a `type` of
`string` and an optional `pattern` the whole value must match:

```json
"git-feedback": {
  "description": "Feedback on one repository's pull requests.",
  "command": ["git-feedback", "--watch"],
  "optional": true,
  "scope": {"url": {"type": "string", "pattern": "^https://"}},
  "env": {"GIT_FEEDBACK_URL": "scope:url"}
}
```

The model starts one instance with
`subscribe_feed {"feed": "git-feedback", "scope": {"url": "https://…"}}`.
What it cannot carry is a command, argv, URL or credential, and every value
is held to the feed's schema. A
field the feed does not declare, a declared field the call leaves out, and
a value the pattern refuses are refusals the model sees and can correct; a
feed that declares no scope takes none. A call may also carry `prompt`, the
question the model wants asked about each record, and `min_no_probability`
beside it; the question filter section below covers what the pair does, and
the feeds that refuse it. A `scope:` reference in `env` must
name a field the feed declares, and a scope on a feed that is not optional
fails the load, as do a field type other than `string`, a pattern that
does not compile, and a field name outside `^[a-z][a-z0-9_]*$`.

An instance answers to the feed's name and the values it was started with:
its origin is `feed:<name>[<field>=<value>]`, with the fields in name order
and each value escaped as a query value — `https://github.com/o/r` becomes
`https%3A%2F%2Fgithub.com%2Fo%2Fr` — and a feed with no scope keeps
`feed:<name>`. A
call for the same feed and the same values reaches the instance that
already exists, so a repeated subscription never starts a second one, and a
subscription that stopped starts again on the next call to it. The fields
and their patterns are in the prompt, so the model knows what it may ask
for.

A session runs at most eight instances at once, counting the ones a job list
shows: running, waiting to restart and suspended. A call that would start a
ninth is refused with the cap named, because each distinct set of values is a
process of its own and a model that subscribed in a loop could grow the
session's processes without bound. Stopping an instance with `cancel_job`
makes room, and a call that reaches an instance already there is never
refused.

**Filters before delivery.** An optional `filter` names one of two kinds:
a `question`, put to the session's decision model, or a `command`, run as
one process per record and free to answer with no record or with several.
A manifest that names both fails the load. A feed with no filter delivers
what its command writes.

**Question filter.** The question applies to the text of every message,
including messages emitted while a run is active. A message whose text is
empty — an image alone — is delivered without the question. For example:

```json
"filter": {
  "question": "Does this event report a failed required check or an unclear status?",
  "min_no_probability": 0.95
}
```

Configure the question so **yes** means deliver. Set `min_no_probability` to
a value above 0.5 and at most 1. The session's decider receives the feed's
name, the sender, and the message text, and never the conversation. Enable a
filter only when the configured decider may receive that data.

poo drops a message only when the probability of **no** meets or exceeds the
threshold. When the endpoint reports a label mass, poo multiplies the
probability by that mass before the comparison. A dropped message never
enters the queue, so it cannot wake a run or appear in the history. On a
logged session, the decision record still names the question and answer.

An answer the filter cannot use leaves the message on the feed's normal
delivery path. That covers a missing decider, a failed or invalid answer,
invalid label mass, and a probability below the threshold. A session with no
decision model warns once on the feed's log, after the first message, and
delivers unfiltered from then on. Messages whose
sender and text total more than 8 KiB are delivered unasked. One decision is
bounded by a 1.5-second budget, and a decision that runs past it is
undecidable. The feed waits for each answer before it reads the next message.
A stopped feed cancels its pending decision and never delivers the message it
was assessing.

The question fails open: the only message it withholds is one it declined
with enough confidence. That is why a missing or broken decider delivers.

**The model's own question.** A session with a decision model may set the
question at runtime: the `prompt` a call names is put to the decision model
about each record of that instance, and it replaces the manifest's question
while it is set. A plain subscription names no prompt and leaves the
instance's question alone; an empty `prompt` clears the model's question, and
the manifest's applies again. The prompt belongs to the instance, so it
reaches the instance a call reaches — a running one changes the question its
next record is asked, without a restart — and a feed re-armed by a later
subscription keeps it while the session lasts. A resumed session does not
carry it: an optional feed's subscription is not resumed. Everything above
holds for a prompt too, including the fail-open answers and the messages
delivered unasked.

The threshold is the call's `min_no_probability` when it names one, else the
manifest's value when the manifest asks a question, else 0.6. The call's
value is greater than 0.5 and at most 1.

A feed has one filter kind. A feed whose manifest runs a command filter
refuses a prompt, and so does a session with no decision model: the set is a
refusal the model reads and can correct, because a filter that cannot screen
would leave the model believing it does. Clearing a prompt is never refused.
A manifest question, by contrast, still delivers unfiltered and warns,
because the operator chose it.

**Command filter.** The filter's argv runs once per record. The record
the feed wrote arrives on the filter's standard input, and the records the
filter writes to its standard output are the ones delivered, in the order
it wrote them. One source record may become any number of inputs, which is
how a filter adapts a source whose shape poo does not know. For example:

```json
"filter": {
  "command": ["/path/to/excerpt", "--max", "2000"]
}
```

A reference filter script, the shape the git-feedback trial uses — it
drops the events the operator's own login authored, bounds the excerpt it
forwards, and formats the sender as `git-feedback/<repo>#<number>`. The
feed's manifest sets `GIT_FEEDBACK_LOGIN` beside its other `env` entries,
and the filter reads the login from its environment the way the feed's
own command does:

```sh
#!/bin/sh
# excerpt — one record in on standard input, one record out on standard
# output. The feed's `from` is "git-feedback/<repo>#<number>:<author>";
# the author is the part after the last colon.
record=$(cat)
author=$(printf '%s\n' "$record" | jq -r '(.from // "") | split(":")[-1]')
if [ "$author" = "$GIT_FEEDBACK_LOGIN" ]; then
  exit 0  # nothing on stdout drops the record
fi
printf '%s\n' "$record" | jq '
  {
    text: ((.text // "")[:2000]),
    from: ((.from // "git-feedback/unknown#0") | split(":")[0])
  }'
```

The standard output is a stream of JSON values, one record each: `jq -c`'s
one-value-per-line output and jq's default pretty-printed output both
parse, and a stream may mix them. The output is all or nothing. If any
value is not a record, none of the run's records is delivered and the
attempt fails closed.

The empty standard output is the drop: a record the filter withholds is
one the session never queues. The `text` it forwards is bounded at 2000
characters, and the `from` it writes drops the author, so the model reads
the sender the way the feed's design spells it.

The output record names `text`, `from` and `image` the way a feed's own
line does, and may add a `data` object beside its text: that object is the
structured record, and the delivered message carries it. A feed that runs
no filter has no `data`, and the field is ignored there.

The filter runs the way the feed's own command runs: the same executor,
the same directory, and the same environment, the manifest's `env`
entries and the library's benign names. A record is at most 1 MiB on both
sides of the filter, and one run's whole output is at most 4 MiB and 256
records — the bound on how many records one source record becomes. Past
either of the run's caps the attempt fails closed, the way it does for a
record past 1 MiB.

The command filter fails closed. An empty standard output drops the
record, which is how a filter withholds one. A nonzero exit, output that
is not a record, or output past a cap fails the attempt, and the feed is
suspended: it does not restart, whatever `restart` says. The session wakes
with a report of its own, delivered as a feed input whose text is a
`<feed>` element with `fault` and `log` attributes: the fault's code, and
the path of a file beside the session's own that holds the filter's
diagnostics for the attempt. Those diagnostics never ride the input. The
report ends with how the feed starts again: `subscribe_feed` with the
feed's name and scope, for a feed a subscription started. One suspension
is one wake, and a further fault that lands before the report is
delivered joins it.

**The model reads a feed's input as outside input.** The message is one
XML element: `<feed name="<name>" from="<from>">` around the text, with
the `from` attribute left out when the input has no sender, and `name` is
the instance's label — `git-feedback[url=https:%2F%2Fgithub.com%2Fo%2Fr]`
for a subscription's instance, the feed's name alone otherwise. In the
text,
`&` becomes `&amp;` and `<` becomes `&lt;`, so the text cannot close the
element; an attribute escapes those two as well as `"`, and a newline or
a carriage return is written as a character reference. It is not the
user's turn, and it is untrusted like a tool result. The records are
`feed.started` and `feed.ended`; [docs/session-log-v0.md](session-log-v0.md)
lists them.

**Jobs.** A running feed is listed by `jobs` and by `session/get` as
`feed:<name>` with the tool `feed`, and an instance a subscription started
as its origin, `feed:<name>[<field>=<value>]`. `cancel_job` takes either
id and stops that feed until the session closes, and it stops a feed a
filter suspended as it stops any other. A suspended feed is listed as
`suspended`, and `subscribe_feed` starts an optional one again. A feed the
session starts itself — a configured manifest feed, or a resource feed an
`mcp.json` declares — stays suspended until the session closes, for no call
starts it. A session that loads again starts every configured feed that is
not optional; a subscription is not resumed, and the model starts one again
with `subscribe_feed`.

## MCP servers

An MCP server is a process the session starts beside itself, speaking the
Model Context Protocol on stdio. The model reaches the server's tools
through `call_mcp`, and a server may also serve a resource the session reads
as a feed.

Loading a manifest declares the servers a session *may* start. A profile's
`mcp_servers` selects the ones its sessions *do* start, and no selection
starts nothing:

```json
{
  "mcpServers": {
    "avenor": {
      "command": "avenor",
      "args": ["mcp", "--bind-host", "poo", "--bind-session", "${session:id}"]
    }
  },
  "poo": {
    "servers": {
      "avenor": {
        "feeds": {
          "avenor-inbox": {
            "resource": "avenor://inbox",
            "on_idle": "wake",
            "description": "Messages from Avenor children.",
            "filter": {"command": ["jq", "-c", "<program>"]}
          }
        }
      }
    }
  }
}
```

`mcp.paths` lists the manifest files and directories the session reads,
beside `feeds.paths` and `cli_registry.paths`, and `mcp.defaults` says
whether poo's own mcp directory loads beside them. That directory is
`$XDG_CONFIG_HOME/poo/mcp/`, it loads when the key is absent or true, and a
profile's `mcp_defaults` speaks after the settings'. `POO_MCP_PATH` and the
repeatable `--mcp-path` flag add paths the way the two other manifest kinds'
overrides do. An `mcp.json` file is JSON: a directory contributes the
`.json` files it holds and skips the rest, while a path that names a file
fails the load when it is not `.json`.

### The file

A file that names no top-level `mcpServers` is not a manifest poo can use,
and it fails the load. Each `mcpServers` entry is keyed by the server's
name — letters, digits, `-` and `_` — and poo reads `command` (required),
`args`, `env`, `type` and `url` from it. A field poo does not read is
ignored, because other harnesses keep their own inside an entry, so one
file serves poo and them alike. A top-level key poo does not name is
ignored for the same reason: poo's own additions live under `poo`, and a
harness ignores the key it does not know the way poo ignores the harness's.

A `type` of `http`, `sse` or `streamable-http`, or a `url` on an entry with
no type, names a transport poo does not start: the entry is left out with a
warning naming the server. A `type` that is neither absent nor `stdio`
fails the load, and so does an entry with no `command`.

Two files may not define one server name: the load fails naming both files,
because a selected name has to mean one server. A duplicate object key
anywhere in a file fails the load too.

### The `poo` block

The top-level `poo` key is poo's alone, so it is strict: a field poo does
not know fails the load, unlike the top level and an entry's fields.
`poo.servers.<server>.feeds.<feed>` declares one resource feed:

- `resource` is the URI the feed subscribes to, and is required.
- `description` is what the jobs prompt says about the feed.
- `on_idle` is `wake` or `hold`, and an absent one inherits `jobs.on_idle`.
- `filter` takes a feed manifest's filter shape, and the feed package
  validates it. Both kinds work: the `command` filter that fans a read out
  into records, and the `question` filter that puts each record's text to
  the decision model.

A feed's name is lowercase letters, digits, `-` and `_`. Two servers may not
declare one name, two feeds may not subscribe one server's resource — a
drained read has one reader — and a resource feed may not take the name of
a feed the settings' own feed manifests load. A `poo.servers` key that names
no `mcpServers` entry, or one whose transport poo does not start, fails the
load.

### Values a launch resolves

`command`, each argument and each `env` value are templates, resolved when
the server launches and never when the settings load: the values are the
session's, and a session that does not exist yet has none of them.

- `${NAME}` is the host environment's value.
- `${NAME:-default}` is that value, or `default` when the environment does
  not hold the name.
- `${session:id}` is the session's own ID, the same one after a resume.
- `$$` is one literal `$`.

A `${NAME}` the environment does not hold, with no default, is an error
naming the server and the variable: a command that runs with an empty
argument is worse than a server that does not launch. An unterminated `${`,
a reference with no name, a default on a `session:` reference, and any
`session:` name but `id` fail the load.

### What a session starts

A selected server is a session-owned process: it starts when the session
starts, with the session's ID and working directory, and it stops when the
session closes. It starts with the host environment minus the names
`tools.env.scrub` removes, plus the manifest's resolved `env` — so an
explicit `${NAME}` in the manifest can put a scrubbed variable back, which
is the manifest author's choice.

Servers are trusted host processes the operator selected. They start
directly, not through the session's executor or sandbox, and they run as the
user runs poo. Select one the way you would install a program.

Startup is bounded at 30 seconds per server, handshake and tool listing
included. A server that fails to start fails session creation and resume
with an error naming it. A name the profile selects that the manifests do
not load is a warning, not an error: `mcp.paths` still decides what loads.

A binary that links no `call_mcp` cannot honour a selection and fails the
build, naming the tool.

### `call_mcp` and `find_tool`

The model reaches a server's tools through one call. `call_mcp` takes
`{"name": "<server>/<tool>", "args": {...}}`: the qualified name is the
server's name, a slash, and the tool's own. The call is side-effecting — a
tool server can change anything — and its primary argument is `name`, so a
permission rule can allow one server's tools by prefix (`avenor/`) without
naming each one. It may run in the background like any side-effecting call.

The prompt carries a catalog of the names the running servers list, each
with its argument hint and, when the server claims them, `(read-only)` and
`(destructive)`. Those are the server's own claims, shown so the model can
choose; they are never the permission class of a call. A binary whose
session has a decision model offers `find_tool` beside the call, and the
search covers the MCP tools and the registered commands together: each hit
names the dispatcher that calls it and the argument that names it. The
search reads the session's live offering, so a server that is not ready
contributes no hit — the model is never pointed at a tool it cannot call.

An unknown qualified name is refused before anything runs, with the reason
and where to look: the search when the session offers one, the prompt's
catalog when it does not. A server that stopped answers `server <name> is
down` as an error result, and the session keeps running. A transport failure
mid-call is reported and never retried, because the call may have had
effects. A tool that reports its own failure answers with that failure, as
the server wrote it.

The text a tool returns is capped at the observation cap, the way a
command's output is. `structuredContent` rides along when it is valid JSON
under the same cap, and is left out whole with a line saying so when it is
not. An image becomes an attachment only when the run's model reads images;
otherwise a line says what was dropped.

### Resource feeds

A server that declares a feed must advertise `resources/subscribe` in its
handshake; a server that does not fails the session, naming the server and
the feed. The feed then:

- subscribes to the URI and waits for the grant,
- reads the resource once, which is what carries what was queued before the
  subscription, and reads it again on every update the server sends,
- emits one record per text content of each read, with the server's name as
  the sender and the content verbatim as the text. Empty text is skipped, and
  a binary content is skipped with a line in the feed's log.

A read is a drain, so a resource feed is at-most-once: a read that fails, or
an emit the session refuses, ends the attempt with the records already
emitted standing, and the rest of that read is gone. The feed's restart
policy brings the next attempt.

The declared filter runs the way a command feed's does, on the same
executor and in the same directory, with the environment a feed that declares
no `env` gets. A read whose resource carries a whole batch is therefore one
source record that the filter fans out into one input per message — and the
fan-out caps apply: a record is at most 1 MiB, and one filter run writes at
most 4 MiB and 256 records. An empty result is no input at all.

### Relaunch

A server process that exits while the session runs is relaunched. The first
attempt waits 1s, each further failure doubles the wait up to 30s, and a
generation that ran a minute or more clears the count: a server that worked
for a while gets the whole budget again.

Five consecutive failures fail the server for the session. A call then
answers `server <name> is down`, and the resource feeds that need it report
a fault that suspends them: the code is `filter_exited`, which is the fault a
filter's nonzero exit reports, reused here with the message `server <name>
stopped after 5 relaunches`. A suspended resource feed stays suspended until
the session closes; a resume starts over. A relaunch re-lists the server's
tools and the feeds subscribe again, so a search and a call see the new
generation; the prompt's MCP catalog is rendered once, when the session
starts, and is not re-rendered.

## The conformance recipe

The suite is black box. It starts the binary and drives it over stdin and
stdout. A settings file it writes points the binary at a fake endpoint.

```go
func TestMyBinarySpeaksTheProtocol(t *testing.T) {
	report := conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve"},
		Profile: "search",
		Env:     []string{"MY_POO_ID=conformance"},
	})
	if len(report.Skipped) > 0 {
		t.Logf("skipped: %v", report.Skipped)
	}
}
```

The target is a binary, not a function, so build the command first. The
suite passes `--settings <file>` on every start, and `--ephemeral` for
the ephemeral flow, so the binary takes both. It creates sessions in the
logged mode, so the binary writes session files. The profile it names must
be one the handshake offers.

A flow that needs a tool the binary does not report is skipped, and the
reason is logged. The bash-scripting flows need `bash`, and the typed
result needs the session to offer `finish`. The feeds flow needs the
handshake to announce `capabilities.feeds`. `Run` returns a `Report` of
the flows that ran and the ones that skipped, so a full binary can assert
that nothing skipped.

`Target.Env` is appended to the suite's own environment for the child. A
binary that reads an identity before it serves (a socket path, a run id)
is the case it is for. The value is the target's business, and setting it
does not change the suite's own environment.

## The fake endpoint

A client's own tests do not need the suite. The fake endpoint the suite
runs is exported, so a test can drive a real binary against scripted
turns and read back what the model was asked:

```go
endpoint := conformance.NewFakeEndpoint(
	conformance.ScriptedAnswer{Text: "hello from poo"},
	conformance.ScriptedAnswer{ToolCalls: []conformance.ToolCall{
		{Name: "bash", Arguments: map[string]any{"command": "echo hi"}},
	}},
)
defer endpoint.Close()

settings := filepath.Join(dir, "settings.json")
if err := endpoint.WriteSettings(settings, "conformance-model", 32768, nil); err != nil {
	return err
}
// start the binary with `--settings settings` and drive it over its
// pipes; the endpoint answers the turns in order.
```

The turns are the suite's `ScriptedAnswer` values: text, streamed
chunks, tool calls, provider errors, delays. The endpoint serves its
last turn again when the turns run out, so a test that does not script
every request still gets an answer. `RequestBodies` returns the bodies
the endpoint received, in order, and `Close` shuts it down.

## The closure gate

The point of composing a binary is what it does not link. Keep a test that
says so:

```go
deps := listDeps(t, "./wire/conformance/testdata/readonly")
for _, excluded := range []string{"tool/bash", "tool/edit", "tool/cliregistry", "exec/bwrap"} {
	if slices.Contains(deps, modulePath+"/"+excluded) {
		t.Errorf("the read-only binary links %s", excluded)
	}
}
```

`internal/depcheck/closure_test.go` is that test. It also checks that the
packages the binary composes are present, because a subject that stopped
importing them passes the exclusions by linking nothing.
