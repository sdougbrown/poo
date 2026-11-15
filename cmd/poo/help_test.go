package main

import (
	"io"
	"strings"
	"testing"

	clicontract "github.com/sdougbrown/cli-contract"
)

// The help is derived from the command declaration, so a flag cannot reach
// the parser without reaching the help. These tests hold the derived help
// to the declaration: every command prints its own section, and that
// section names every flag the command takes.

// commandHelp is one declared command's path and the flags its help must
// name.
type commandHelp struct {
	path  []string
	flags []clicontract.Flag
}

// commandHelps walks the declaration and returns every command's help
// expectations, common flags included: cli-contract renders those in each
// command's global-flags block.
func commandHelps(app *clicontract.App) []commandHelp {
	var helps []commandHelp
	var walk func(commands []*clicontract.Command, prefix []string)
	walk = func(commands []*clicontract.Command, prefix []string) {
		for _, command := range commands {
			path := append(append([]string{}, prefix...), command.Name)
			if len(command.Subcommands) > 0 {
				walk(command.Subcommands, path)
				continue
			}
			flags := append(append([]clicontract.Flag{}, app.CommonFlags...), command.Flags...)
			helps = append(helps, commandHelp{path: path, flags: flags})
		}
	}
	walk(app.Commands, nil)
	return helps
}

// TestEachCommandHelpPrintsItsOwnSection covers the help every command
// owns: `poo <command> --help` prints the command's own usage and flags,
// not the root block.
func TestEachCommandHelpPrintsItsOwnSection(t *testing.T) {
	app := pooApp(io.Discard)
	for _, help := range commandHelps(app) {
		path := strings.Join(help.path, " ")
		t.Run(path, func(t *testing.T) {
			args := append(append([]string{}, help.path...), "--help")
			code, _, stdout := runCLIWithStdout(t, args...)
			if code != 0 {
				t.Fatalf("poo %s --help exited %d, want 0", path, code)
			}
			if !strings.Contains(stdout, "poo "+path) {
				t.Errorf("the help does not show the command's usage line:\n%s", stdout)
			}
			for _, flag := range help.flags {
				if !strings.Contains(stdout, "--"+flag.Name) {
					t.Errorf("the help does not name --%s:\n%s", flag.Name, stdout)
				}
			}
		})
	}
}

// TestTheHelpCommandPrintsACommandsSection covers `poo help <command>`: it
// answers with the same section as the command's own --help.
func TestTheHelpCommandPrintsACommandsSection(t *testing.T) {
	code, _, stdout := runCLIWithStdout(t, "help", "sessions", "prune")
	if code != 0 {
		t.Fatalf("poo help sessions prune exited %d, want 0", code)
	}
	for _, name := range []string{"--older-than", "--keep", "--dry-run"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("the help does not name %s:\n%s", name, stdout)
		}
	}
}

// TestTheRootHelpKeepsTheSharedRules covers the root block's own content:
// the front door, the print alias, the settings discovery and the shared
// flags stay in the root help.
func TestTheRootHelpKeepsTheSharedRules(t *testing.T) {
	code, _, stdout := runCLIWithStdout(t, "--help")
	if code != 0 {
		t.Fatalf("poo --help exited %d, want 0", code)
	}
	for _, want := range []string{
		"front door",
		"poo -p",
		"POO_SETTINGS",
		"settings.json",
		"--settings",
		"--session-dir",
		"version",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the root help does not hold %q:\n%s", want, stdout)
		}
	}
}

// TestAnUnknownHelpTopicIsARefusal covers the help command's failure: a
// topic the binary does not declare is the caller's error.
func TestAnUnknownHelpTopicIsARefusal(t *testing.T) {
	code, stderr, _ := runCLIWithStdout(t, "help", "fly")
	if code != 2 {
		t.Errorf("poo help fly exited %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown help topic") {
		t.Errorf("the refusal is %q, want it to name the unknown topic", stderr)
	}
}
