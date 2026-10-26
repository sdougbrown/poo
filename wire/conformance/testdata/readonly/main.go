// Command readonly serves the search profile: read and grep, and no
// tool that can change anything.
//
// The binary is the conformance suite's subject and the closure gate's:
// it composes the public API the way a custom binary does (a settings
// file, the factory, an injected tool set, wire.Serve), and it links
// neither the shell, nor edit, nor the cli registry, nor a sandbox. The
// suite runs it with the search profile, so the flows that script bash
// skip.
//
// It reads its identity from the environment before it serves: a
// binary that refuses to start without one is the case the suite's
// Target.Env covers.
package main

import (
	"embed"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// version is the build's version.
const version = "0.0.1"

// promptDir is the binary's own prompt directory.
//
// It replaces the base section with the read-only contract, so the
// prompt the model reads is the one this binary is about. The runtime
// settings still apply on top of it.
//
//go:embed prompts
var promptDir embed.FS

// identityEnv is the variable the binary's identity is read from.
const identityEnv = "POO_READONLY_ID"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run serves the protocol until the client hangs up, and returns the
// process exit code: two for a setup failure, one for a transport
// failure, and zero after a clean teardown.
func run(args []string) int {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("poo-readonly serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	settingsPath := fs.String("settings", "", "The settings file: JSON, TOML or YAML")
	workingDir := fs.String("working-dir", "", "The directory the tools work in")
	ephemeral := fs.Bool("ephemeral", false, "Keep the sessions in memory and write no files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if os.Getenv(identityEnv) == "" {
		fmt.Fprintf(os.Stderr, "poo-readonly: %s is not set\n", identityEnv)
		return 2
	}
	if *settingsPath == "" {
		fmt.Fprint(os.Stderr, "poo-readonly: no settings file\n")
		return 2
	}
	settings, err := config.Load(*settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-readonly: %v\n", err)
		return 2
	}
	dir := *workingDir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			fmt.Fprintf(os.Stderr, "poo-readonly: %v\n", err)
			return 2
		}
	}
	overrides := config.Overrides{}
	if *ephemeral {
		overrides.SessionMode = "ephemeral"
	}
	prompts, err := iofs.Sub(promptDir, "prompts")
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-readonly: %v\n", err)
		return 2
	}
	srv, err := server.New(server.Options{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     dir,
		DefaultProfile: search.Name,
		PromptFS:       prompts,
		Tools: map[string]config.ToolFactory{
			"read": func(config.ToolOptions) poo.Tool { return read.New() },
			"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
		},
		Profiles: []profile.Profile{search.Profile()},
		Build:    poo.BuildInfo{Name: "poo-readonly", Version: version},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo-readonly: %v\n", err)
		return 2
	}
	if warning := srv.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "poo-readonly: warning: %s\n", warning)
	}
	if err := wire.Serve(wire.Options{
		In:      os.Stdin,
		Out:     os.Stdout,
		Factory: srv,
		Build: protocol.Build{
			Name:       "poo-readonly",
			Version:    version,
			PooVersion: version,
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "poo-readonly: %v\n", err)
		return 1
	}
	return 0
}
