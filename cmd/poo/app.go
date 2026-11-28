package main

import (
	"context"
	"fmt"
	"io"

	clicontract "github.com/sdougbrown/cli-contract"
)

// The declared command surface.
//
// Every command, its flags and its help text are declared here once.
// cli-contract builds the command tree from the declaration, so `poo
// <command> --help`, `poo help <command>` and `poo --version` say what the
// command does and which flags it takes: a flag cannot reach the parser
// without also reaching the help.

// rootLong is the root help body: the front door, the print alias, and the
// settings discovery every command shares. The declared commands and the
// shared flags follow it in the help cli-contract renders.
const rootLong = `poo runs a session against an OpenAI-compatible endpoint.

A bare poo launches the front door: the interface the binary links, the
repl, or the one the settings' ui names.

  poo -p "prompt" [flags]

runs one session, like poo run, with the run's flags before or after the
prompt. --print is the same flag.

  poo -c [flags]

reopens the most recent session in the working directory, and

  poo --resume <id> [flags]

reopens the session that id names: they are poo repl's --continue and
--resume. The session keeps its recorded working directory and model,
and the repl renders its scrollback before the first prompt.

Settings discovery:
  Without --settings, the settings are found at the file the POO_SETTINGS
  environment variable names, then the project's .poo directory (walking
  up from the current directory), then the user's config directory. On
  macOS, ~/Library/Application Support/poo is checked before ~/.config/poo.
  In each directory, poo tries settings.json, settings.yaml, settings.yml
  and settings.toml in order. It uses the first file it finds, or defaults
  if there is no file.`

// runLong is the help body of `poo run`.
const runLong = `The prompt starts at the first argument that is not one of the run's
flags. A flag written after it is prompt text, and poo -p is the way to
put the run's flags after the prompt instead.

The answer goes to stdout and the progress to stderr. The exit code says
why the run ended: 0 when it settled finished, the settle reason's own
code otherwise (the README lists them), 2 when the command refuses its
arguments or its settings, and 1 for a failure with no code of its own.

--endpoint, --model-id and --context-limit are required unless the
settings file names a chat model.`

// serveLong is the help body of `poo serve`.
const serveLong = `poo serve runs the wire protocol on stdin and stdout: one JSON-RPC 2.0
object per line. It takes the same endpoint flags as poo run, plus the
session's directory and the executor the bash tool runs commands through.`

// daemonLong is the help body of `poo daemon`.
const daemonLong = `The daemon hosts a long-lived feed-driven session: it starts the
session's feeds, and waits for a signal or a lost lease without reading
stdin. The session is durable, and a pending run resumes on load.

--session names the session to load, or create if it does not exist.
Without it, a new id is generated and printed to stderr, so a supervisor
can pin it.

The lifecycle goes to stderr, one fact per line. The exit code says why
the daemon ended: 0 after a clean shutdown, 1 when the lease was lost
(a takeover owner now holds the session), and 2 when the command refuses
its arguments or its settings.`

// replLong is the help body of `poo repl`.
const replLong = `The repl takes the same flags as poo serve, and starts a poo serve with
them. Its input reads with vi keys at a terminal, and the transcript
renders as markdown. Type /help once it is up.`

// modelsLong is the help body of `poo models`.
const modelsLong = `The command lists the effective catalog: the models the settings offer,
the models the binary knows about, and what the endpoints report.
--explain names one model and prints the layer that set each field.`

// loginLong is the help body of `poo login`.
const loginLong = `The command writes the credential to the file auth_file names, or to poo's default auth.json.
On macOS, the default is ~/Library/Application Support/poo/auth.json. If that file is absent,
poo uses an existing ~/.config/poo/auth.json instead.

A bare poo login offers the list; a name picks one:

  poo login codex         OpenAI Codex, through a browser
  poo login ollama-cloud  Ollama Cloud, with a key
  poo login opencode      OpenCode, with a key
  poo login typesafe      TypeSafe, with a key

The Codex sign-in is the legacy flow: it names the Codex CLI's public client, not poo's own, and
the credential records it so a refresh keeps working. A future first-party sign-in is another
entry in the same list.

The browser sign-in comes back to a loopback callback on the port its client is registered with.
When the browser cannot reach this machine, paste the URL it landed on: the state in its query is
what the sign-in is checked against, so the code on its own is not enough.

A key is typed into the terminal's prompt, or read from stdin. Neither leaves the key in your
shell's history or in the process list, which is why there is no flag that takes one.

An existing credential is not replaced without an explicit action: --force replaces it, and a
terminal is asked first. No token is printed.`

// rolesLong is the help body of `poo roles`.
const rolesLong = `The check an operator runs without starting a session. Each role the settings
assign is resolved against the catalog — the settings, and the models file below
them — and printed with the provider that serves it and where that provider's
key comes from. A role that does not resolve says what is known: the name as it
was written, and the models that would have served it. No key is printed and no
credential is traded, and the command exits non-zero when a role does not
resolve.`

// promptLong is the help body of `poo prompt`.
const promptLong = `The command prints the exact system prompt a session would send to the model.
Pass --pretty to print each section under a header with skills summarized for reading.`

// authImportLong is the help body of `poo auth import`.
const authImportLong = `The command copies the Codex credential pi's auth.json holds into poo's own file,
once. It reads the file --from names, then the one pi_auth_file names, then pi's own
~/.pi/agent/auth.json, and it writes the file auth_file names, or poo's own.

It is a one-way door, and it prints what it costs. A refresh rotates the refresh
token, so two tools cannot both keep one credential alive: pi's copy is stale from
poo's first refresh, and a refresh pi makes first would invalidate poo's. Run one
of them.

An existing credential is not replaced without an explicit action: --force
replaces it, and a terminal is asked first. Nothing pi's file holds is changed.`

const authRefreshLong = `Refresh the OpenAI Codex credential in the same file model requests use.
By default, trade the refresh token only when the access token has five minutes
or less left. Set --leeway to change that window, or --force to refresh now.
Success is silent, including when no refresh is due. A failure exits nonzero.`

// sessionsLong is the help body of `poo sessions`.
const sessionsLong = `The sessions the store holds. list reads the store, and prune trims it by
the settings' retention policy or by the flags.`

// versionLong is the help body of `poo version`.
const versionLong = `The values come from the binary's own build information: the module
version for a binary installed with ` + "`go install module@version`" + `, and the
revision, time and modified flag for a build from a checkout.

--version prints the one word this information renders: the release, or
dev plus the short revision of a build from a checkout.`

// replCommand declares `poo repl`, the binary's front door.
func replCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "repl",
		Short: "An interactive session in the terminal (vi keys, markdown rendering)",
		Long:  replLong,
		Flags: append(serveFlagSpecs(), reopenFlagSpecs()...),
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleRepl(inv, stdout, cmdStderr), nil
		},
	}
}

// pooApp assembles the declared CLI around the writer a command's own
// stdout goes to. Diagnostics use the writer dispatch hands the command.
func pooApp(stdout io.Writer) *clicontract.App {
	app := &clicontract.App{
		Name:    "poo",
		Version: buildVersion().String(),
		// The binary owns its signals: a run settles a cancelled run
		// through its own handlers, and the other commands keep the
		// default the shell gives them.
		HandlerOwnedSignals: true,
		RootShort:           "run a session against an OpenAI-compatible endpoint",
		RootLong:            rootLong,
		VersionTemplate:     "poo {{.Version}}\n",
		CommonFlags:         sharedFlagSpecs(),
		Commands: []*clicontract.Command{
			{
				Name:        "run",
				Short:       "Run one session: the answer goes to stdout",
				Long:        runLong,
				Positionals: []clicontract.Positional{{Name: "prompt", Variadic: true}},
				// The prompt is the rest of the line: a flag stops at the
				// first word that is not one, and a dash word after it is
				// part of the prompt.
				NoInterspersed: true,
				Flags:          runFlagSpecs(),
				Handle: func(ctx context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleRun(ctx, inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:  "serve",
				Short: "Serve the wire protocol on stdin and stdout",
				Long:  serveLong,
				Flags: serveFlagSpecs(),
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleServe(inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:  "daemon",
				Short: "Host a long-lived feed-driven session",
				Long:  daemonLong,
				Flags: daemonFlagSpecs(),
				Handle: func(ctx context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleDaemon(ctx, inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:  "models",
				Short: "List the models the settings offer, or explain one",
				Long:  modelsLong,
				Flags: []clicontract.Flag{
					{Name: "json", Kind: clicontract.Bool, Usage: "Print the models as JSON"},
					{Name: "explain", Kind: clicontract.String, Usage: "Explain one model: the fields and the layer that set each"},
				},
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleModels(inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:        "login",
				Short:       "Sign in to a provider and store the credential",
				Long:        loginLong,
				Positionals: []clicontract.Positional{{Name: "provider"}},
				Flags: []clicontract.Flag{
					{Name: "force", Kind: clicontract.Bool, Usage: "Replace a credential the file already holds, without asking"},
					{Name: "no-browser", Kind: clicontract.Bool, Usage: "Print the sign-in URL instead of opening a browser"},
					{Name: "wait", Kind: clicontract.Int, Usage: "Seconds to wait for a browser sign-in to come back (default: 300)"},
				},
				Handle: func(ctx context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleLogin(ctx, inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:  "roles",
				Short: "Resolve each role to its model, provider and credential source",
				Long:  rolesLong,
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleRoles(inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:  "prompt",
				Short: "Print the prompt a session would send",
				Long:  promptLong,
				Flags: promptFlagSpecs(),
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handlePrompt(inv, stdout, cmdStderr), nil
				},
			},
			{
				Name:        "auth",
				Short:       "Report, refresh or import stored credentials",
				Subcommands: []*clicontract.Command{authStatusCommand(stdout), authRefreshCommand(), authImportCommand(stdout)},
			},
			{
				Name:        "sessions",
				Short:       "List and prune the stored sessions",
				Long:        sessionsLong,
				Subcommands: []*clicontract.Command{sessionsListCommand(stdout), sessionsPruneCommand(stdout)},
			},
			{
				Name:  "version",
				Short: "Print the binary's version and build information",
				Long:  versionLong,
				Flags: []clicontract.Flag{
					{Name: "json", Kind: clicontract.Bool, Usage: "Print the build information as JSON"},
				},
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleVersion(inv, stdout, cmdStderr), nil
				},
			},
		},
	}
	// The front doors join the declared commands, so an interface is
	// declared and launchable in one entry.
	for _, door := range frontDoors {
		app.Commands = append(app.Commands, door(stdout))
	}
	return app
}

// authStatusCommand declares `poo auth status`.
func authStatusCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "status",
		Short: "Report the credential file, the provider and whether the token is live",
		Flags: []clicontract.Flag{
			{Name: "json", Kind: clicontract.Bool, Usage: "Print the report as JSON"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleAuthStatus(inv, stdout, cmdStderr), nil
		},
	}
}

func authRefreshCommand() *clicontract.Command {
	return &clicontract.Command{
		Name:  "refresh",
		Short: "Refresh the OpenAI Codex credential when due",
		Long:  authRefreshLong,
		Flags: []clicontract.Flag{
			{Name: "leeway", Kind: clicontract.Duration, Usage: "Refresh when this much time or less remains (default: 5m)"},
			{Name: "force", Kind: clicontract.Bool, Usage: "Refresh even when the access token is still fresh"},
		},
		Handle: func(ctx context.Context, inv clicontract.Invocation, stderr io.Writer) (any, error) {
			return handleAuthRefresh(ctx, inv, stderr), nil
		},
	}
}

// authImportCommand declares `poo auth import`.
func authImportCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "import",
		Short: "Copy a credential another tool holds into poo's file",
		Long:  authImportLong,
		Subcommands: []*clicontract.Command{
			{
				Name:  "pi",
				Short: "Copy the Codex credential pi's auth.json holds",
				Flags: []clicontract.Flag{
					{Name: "from", Kind: clicontract.String, Usage: "The auth.json to copy out of (default: pi_auth_file, then pi's own)"},
					{Name: "force", Kind: clicontract.Bool, Usage: "Replace a credential poo's file already holds, without asking"},
				},
				Handle: func(ctx context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleAuthImportPi(ctx, inv, stdout, cmdStderr), nil
				},
			},
		},
	}
}

// sessionsListCommand declares `poo sessions list`.
func sessionsListCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "list",
		Short: "List the sessions the store holds",
		Flags: []clicontract.Flag{
			{Name: "json", Kind: clicontract.Bool, Usage: "Print the sessions as JSON"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleSessionsList(inv, stdout, cmdStderr), nil
		},
	}
}

// sessionsPruneCommand declares `poo sessions prune`.
func sessionsPruneCommand(stdout io.Writer) *clicontract.Command {
	return &clicontract.Command{
		Name:  "prune",
		Short: "Delete the sessions a retention policy marks",
		Flags: []clicontract.Flag{
			{Name: "older-than", Kind: clicontract.Int, Usage: "Delete the sessions whose last activity is older than this many days"},
			{Name: "keep", Kind: clicontract.Int, Usage: "Keep this many sessions, by last activity"},
			{Name: "dry-run", Kind: clicontract.Bool, Usage: "List the sessions the prune would delete and delete nothing"},
		},
		Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
			return handleSessionsPrune(inv, stdout, cmdStderr), nil
		},
	}
}

// serverFlagSpecs are the flags the repl spells for the server it starts:
// the shared flags the child reads, then the flags it parses.
func serverFlagSpecs() []clicontract.Flag {
	return append(sharedFlagSpecs(), serveFlagSpecs()...)
}

// projectPromptFlagSpecs declares the flag that asks for the project's
// instructions in the prompt. The run and the prompt command both take it,
// and they are declared here once.
func projectPromptFlagSpecs() []clicontract.Flag {
	return []clicontract.Flag{
		{Name: "project-prompt", Kind: clicontract.Bool, Usage: "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt"},
	}
}

// promptFlagSpecs declares the flags of `poo prompt`.
func promptFlagSpecs() []clicontract.Flag {
	return append(projectPromptFlagSpecs(),
		clicontract.Flag{Name: "pretty", Kind: clicontract.Bool, Usage: "Format the prompt with section headers for human reading"},
	)
}

// serveFlagSpecs declares the flags serve and repl share: the endpoint and
// the placement of the tools.
func serveFlagSpecs() []clicontract.Flag {
	return append(endpointFlagSpecs(), toolFlagSpecs()...)
}

// reopenFlagSpecs declares the flags that open a session the store
// already holds instead of starting one.
//
// They are the repl's own: the server it starts opens the session the
// repl names, so the child parses neither of them.
func reopenFlagSpecs() []clicontract.Flag {
	return []clicontract.Flag{
		{Name: "continue", Short: "c", Kind: clicontract.Bool, Usage: "Continue the most recent session in the working directory"},
		{Name: "resume", Kind: clicontract.String, Usage: "Resume the session this id names"},
	}
}

// toolFlagSpecs declares the flags that place the tools: the directory they
// work in, and the executor the bash tool runs through. The run and the
// server both take them, so they are declared here once.
func toolFlagSpecs() []clicontract.Flag {
	return []clicontract.Flag{
		{Name: "working-dir", Kind: clicontract.String, Usage: "The directory the tools work in (default: the current one)"},
		{Name: "executor", Kind: clicontract.String, Usage: "The executor the bash tool runs commands through: local or bwrap"},
	}
}

// renderFlags spells an invocation's supplied flags back into argv, in the
// order the declaration prints them.
//
// The repl starts `poo serve` as its own child, so the flags it parsed have
// to be handed to that child again. Only the flags the caller supplied are
// rendered, and every value is written with `=`, so a value that starts
// with a dash stays that flag's value.
func renderFlags(inv clicontract.Invocation, specs []clicontract.Flag) []string {
	var args []string
	for _, spec := range specs {
		if !inv.Supplied[spec.Name] {
			continue
		}
		switch spec.Kind {
		case clicontract.Bool:
			args = append(args, fmt.Sprintf("--%s=%t", spec.Name, inv.Bools[spec.Name]))
		case clicontract.Int:
			args = append(args, fmt.Sprintf("--%s=%d", spec.Name, inv.Ints[spec.Name]))
		case clicontract.Duration:
			args = append(args, fmt.Sprintf("--%s=%s", spec.Name, inv.Durations[spec.Name]))
		case clicontract.Repeat:
			for _, value := range inv.Repeated[spec.Name] {
				args = append(args, "--"+spec.Name+"="+value)
			}
		default:
			args = append(args, "--"+spec.Name+"="+inv.Flags[spec.Name])
		}
	}
	return args
}
