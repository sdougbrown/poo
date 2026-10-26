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
profiles, the capabilities and the models. A session's profile and model
pick a runtime, and the store, the prompt overrides and the permission
asks are the session's own.

`permission.askers` is the one list whose emptiness means something: an
explicit empty list is a chain with no askers, so an `ask` goes straight to
the fallback, and an absent list takes the binary's own chain (`[wire]` in
`poo serve`).

`server.Options.Overrides` carries what a command line changed.
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
order's (`prompt.Order()`): `base`, `catalog`, `environment` and `project`, and
`tools.<tool>` for a tool's own section. A directory in the path names the
section's prefix, so a tool's file is `tools/bash.md`. A variant is a sibling
whose name carries it, `tools/bash.small-local.md`.

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
`{{.Description}}`. A section whose prose names no field renders the same text
whatever the data is.

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
the settings (`system_prompt.*`) and the project's `AGENTS.md`. A later layer
wins, so an operator's settings file still changes the prompt the binary ships.
`system_prompt.replace` clears the directory's sections. If the session offers
registry tools, the build restores the embedded catalog after replacement, so
a directory's `catalog.md` does not apply.
The project layer is the consumer's to add: `server.New` loads no project file,
and `poo prompt` includes one only when it is run with `--project-prompt`.

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
}
```

The map's key is the name the profile, the permission rules and the wire
use. It must be the name the tool's `Spec` reports. A profile or a
permission rule that names a tool the binary does not register is a typed
error. So is a cli registry whose `call_cli` the binary does not link.

The settings still decode and validate in `config`. `tools.bash.*`, the
executor name and `cliregistry.paths` are settings, not tools. Nothing in
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
result needs the session to offer `finish`. `Run` returns a `Report` of
the flows that ran and the ones that skipped, so a full binary can assert
that nothing skipped.

`Target.Env` is appended to the suite's own environment for the child. A
binary that reads an identity before it serves (a socket path, a run id)
is the case it is for. The value is the target's business, and setting it
does not change the suite's own environment.

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
