package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// serveUsage is the usage of `poo serve`.
const serveUsage = `poo serve runs the wire protocol on stdin and stdout.

Usage:
  poo serve [flags]

Serve flags:
  --settings <file>            The settings file: JSON, TOML or YAML
  --system-prompt <file>       The file holding the system prompt's base section
  --append-system-prompt <t>   Text appended to the system prompt, or @file
  --permission-unmatched <a>   What to do with a side-effecting call no rule covers
  --session-dir <dir>          The directory the session files live in
  --ephemeral                  Keep the sessions in memory and write no files
  --profile <name>             The profile to run under: base, code or search
  --base-url <url>             The endpoint's base URL, e.g. http://localhost:4000/v1
  --model-id <id>              The model identifier
  --context-limit <n>          The model's context window in tokens
  --api-key-env <var>          The environment variable holding the API key
  --tools-mode <mode>          native (default), text or auto
  --working-dir <dir>          The directory the tools work in (default: the current one)
  --executor <name>            The executor the bash tool runs commands through: local or bwrap
`

// serveOptions are the flags of `poo serve`.
type serveOptions struct {
	sharedFlags
	endpointFlags
	// workingDir is the directory the tools work in.
	workingDir string
	// executor is the executor the bash tool runs commands through.
	executor string
}

// register adds the serve flags to a flag set.
func (o *serveOptions) register(fs *flag.FlagSet) {
	o.sharedFlags.register(fs)
	o.endpointFlags.register(fs)
	fs.StringVar(&o.workingDir, "working-dir", "", "The directory the tools work in")
	fs.StringVar(&o.executor, "executor", "", "The executor the bash tool runs commands through")
}

// runServe executes `poo serve`.
//
// The server reads the client's requests from stdin and writes its
// answers to stdout. It returns the process exit code: zero after a
// clean teardown, and one after a transport failure.
func runServe(args []string) int {
	fs := flag.NewFlagSet("poo serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, serveUsage) }

	var o serveOptions
	o.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}
	if err := o.endpointFlags.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}

	settings, err := o.settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}
	o.endpointFlags.apply(settings)
	if o.executor != "" {
		executor := o.executor
		settings.Tools.Bash.Executor = &executor
	}
	workDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}
	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}
	srv, err := server.New(server.Options{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Executors:      executorRegistry(),
		Profiles:       linkedProfiles(),
		Build:          poo.BuildInfo{Name: "poo", Version: version},
		SessionTools:   addFinishTool,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 2
	}
	if warning := srv.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "poo serve: warning: %s\n", warning)
	}
	err = wire.Serve(wire.Options{
		In:      os.Stdin,
		Out:     os.Stdout,
		Factory: srv,
		Build: protocol.Build{
			Name:       "poo",
			Version:    version,
			PooVersion: version,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo serve: %v\n", err)
		return 1
	}
	return 0
}
