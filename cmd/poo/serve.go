package main

import (
	"fmt"
	"io"
	"os"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// serveOptions are the flags of `poo serve`.
type serveOptions struct {
	sharedFlags
	endpointFlags
	// workingDir is the directory the tools work in.
	workingDir string
	// executor is the executor the bash tool runs commands through.
	executor string
}

// bind reads the serve flags out of a parsed invocation.
func (o *serveOptions) bind(inv clicontract.Invocation) {
	o.sharedFlags.bind(inv)
	o.endpointFlags.bind(inv)
	o.workingDir = inv.Flags["working-dir"]
	o.executor = inv.Flags["executor"]
}

// validate checks the flags the server's two front doors share: a bad
// policy value or half a model is refused before the server starts.
func (o *serveOptions) validate(name string, stderr io.Writer) bool {
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return false
	}
	if err := o.endpointFlags.validate(); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return false
	}
	return true
}

// handleServe executes `poo serve`.
//
// The server reads the client's requests from stdin and writes its
// answers to stdout. It returns the process exit code: zero after a
// clean teardown, and one after a transport failure.
func handleServe(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o serveOptions
	o.bind(inv)
	if !o.validate("poo serve", stderr) {
		return exit{usageExitCode}
	}

	workDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo serve: %v\n", err)
		return exit{usageExitCode}
	}
	settings, err := o.settings(workDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo serve: %v\n", err)
		return exit{usageExitCode}
	}
	o.endpointFlags.apply(settings)
	if o.executor != "" {
		executor := o.executor
		settings.Tools.Bash.Executor = &executor
	}
	overrides, err := o.overrides()
	if err != nil {
		fmt.Fprintf(stderr, "poo serve: %v\n", err)
		return exit{usageExitCode}
	}
	srv, err := server.New(server.Options{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     workDir,
		DefaultProfile: defaultProfile,
		UserPromptFile: defaultUserPromptFile(),
		Tools:          linkedTools(),
		Executors:      executorRegistry(),
		Profiles:       linkedProfiles(),
		Build:          poo.BuildInfo{Name: "poo", Version: buildVersion().String()},
		SessionTools:   addFinishTool,
	})
	if err != nil {
		fmt.Fprintf(stderr, "poo serve: %v\n", err)
		return exit{usageExitCode}
	}
	if warning := srv.Warning(); warning != "" {
		fmt.Fprintf(stderr, "poo serve: warning: %s\n", warning)
	}
	err = wire.Serve(wire.Options{
		In:      os.Stdin,
		Out:     stdout,
		Factory: srv,
		Build: protocol.Build{
			Name:       "poo",
			Version:    buildVersion().String(),
			PooVersion: buildVersion().String(),
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "poo serve: %v\n", err)
		return exit{operationalExitCode}
	}
	return exit{}
}
