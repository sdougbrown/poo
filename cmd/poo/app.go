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

Settings discovery:
  Without --settings, the settings are found at the file the POO_SETTINGS
  environment variable names, then the project's .poo directory (walking
  up from the current directory), then the user's config directory. Each
  is searched for settings.json, settings.yaml, settings.yml and
  settings.toml, and the extension picks the parser. A command that finds
  none runs on the defaults.`

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

// replLong is the help body of `poo repl`.
const replLong = `The repl takes the same flags as poo serve, and starts a poo serve with
them. Its input reads with vi keys at a terminal, and the transcript
renders as markdown. Type /help once it is up.`

// modelsLong is the help body of `poo models`.
const modelsLong = `The command lists the effective catalog: the models the settings offer,
the models the binary knows about, and what the endpoints report.
--explain names one model and prints the layer that set each field.`

// promptLong is the help body of `poo prompt`.
const promptLong = `The command prints the prompt a session would send, with the sections
marked, so an operator can see what the model is told.`

// sessionsLong is the help body of `poo sessions`.
const sessionsLong = `The sessions the store holds. list reads the store, and prune trims it by
the settings' retention policy or by the flags.`

// versionLong is the help body of `poo version`.
const versionLong = `The values come from the binary's own build information: the module
version for a binary installed with ` + "`go install module@version`" + `, and the
revision, time and modified flag for a build from a checkout.

--version prints the one word this information renders: the release, or
dev plus the short revision of a build from a checkout.`

// pooApp assembles the declared CLI around the writer a command's own
// stdout goes to. Diagnostics use the writer dispatch hands the command.
func pooApp(stdout io.Writer) *clicontract.App {
	return &clicontract.App{
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
				Name:  "repl",
				Short: "An interactive session in the terminal (vi keys, markdown rendering)",
				Long:  replLong,
				Flags: serveFlagSpecs(),
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handleRepl(inv, stdout, cmdStderr), nil
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
				Name:  "prompt",
				Short: "Print the prompt a session would send",
				Long:  promptLong,
				Flags: []clicontract.Flag{
					{Name: "project-prompt", Kind: clicontract.Bool, Usage: "Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt"},
				},
				Handle: func(_ context.Context, inv clicontract.Invocation, cmdStderr io.Writer) (any, error) {
					return handlePrompt(inv, stdout, cmdStderr), nil
				},
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
				Handle: func(_ context.Context, inv clicontract.Invocation, _ io.Writer) (any, error) {
					return handleVersion(inv, stdout), nil
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

// serveFlagSpecs declares the flags serve and repl share: the endpoint and
// the placement of the tools.
func serveFlagSpecs() []clicontract.Flag {
	return append(endpointFlagSpecs(), toolFlagSpecs()...)
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
