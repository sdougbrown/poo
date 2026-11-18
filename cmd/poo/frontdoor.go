package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/config"
)

// The ui setting names the interface a bare invocation of the binary
// launches. A binary links the interfaces it offers, and the settings
// name the one to launch: `ui` in the settings file the command
// resolves. A binary with none linked, or a name it does not offer,
// refuses the bare invocation.
const defaultUI = "repl"

// frontDoors are the interfaces this binary links: the name a bare
// invocation may launch, and the command that launch dispatches.
//
// The two are one entry, so an interface cannot be declared without being
// launchable or launchable without being declared: the declaration in
// app.go takes its commands from this map.
var frontDoors = map[string]func(io.Writer) *clicontract.Command{
	"repl": replCommand,
}

// runUI executes a bare `poo`.
//
// The settings resolve the way every command's do: the explicit file, the
// environment's, the project's, or the user's. The interface the
// settings name — or, without them, the binary's default — runs with the
// settings file, so the session it opens reads what it read.
func runUI() int {
	return runUIWith(nil)
}

// runUIWith executes the front door with the words the invocation added
// to it: `poo -c` and `poo --resume <id>` are `poo repl --continue` and
// `poo repl --resume <id>`, and the rest of the line goes with them.
//
// The words are also where the settings come from: a line that names the
// settings file or the working directory launches the interface with it,
// so `poo -c --working-dir <dir>` reads the settings of the directory it
// continues the session in.
func runUIWith(extra []string) int {
	explicitSettings, workingDir := aliasLine(extra)
	path, settings, err := resolveSettings(explicitSettings, workingDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n", err)
		return usageExitCode
	}
	name, err := pickUI(settings, defaultUI, frontDoors)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n", err)
		return usageExitCode
	}
	return run(frontDoorLine(name, path, extra))
}

// frontDoorAlias returns the words a front-door alias carries and
// whether the invocation is one: `poo -c`, `poo --continue` and `poo
// --resume <id>` open a logged session in the repl, and the rest of the
// line is the repl's flags. Every other leading flag is not the front
// door's, so it keeps the behaviour it has.
func frontDoorAlias(args []string) ([]string, bool) {
	switch args[0] {
	case "-c", "--continue", "--resume":
		return args, true
	}
	return nil, false
}

// frontDoorLine is the argv a bare invocation dispatches: the interface
// the settings named, and the settings file itself, so the session it
// opens reads what the settings said.
//
// The settings file is the one the invocation's own words do not name: a
// line that spells --settings carries it through, and one prepended here
// would be a second answer to a question the person already answered.
func frontDoorLine(name, discoveredPath string, extra []string) []string {
	args := []string{name}
	if explicit, _ := aliasLine(extra); explicit == "" && discoveredPath != "" {
		args = append(args, "--settings", discoveredPath)
	}
	return append(args, extra...)
}

// aliasLine returns the settings file and the working directory the
// alias's words name, each empty when they name none.
//
// The words are read, not removed: the interface parses them itself, and
// they have to reach it as the person spelled them. A terminator ends the
// flags, so a word after it is never read as one.
func aliasLine(extra []string) (explicitSettings, workingDir string) {
	for i := 0; i < len(extra); i++ {
		word := extra[i]
		if word == "--" {
			break
		}
		name, value, hasValue := strings.Cut(word, "=")
		switch name {
		case "--settings":
			if hasValue {
				explicitSettings = value
				continue
			}
			if i+1 < len(extra) {
				i++
				explicitSettings = extra[i]
			}
		case "--working-dir":
			if hasValue {
				workingDir = value
				continue
			}
			if i+1 < len(extra) {
				i++
				workingDir = extra[i]
			}
		}
	}
	return explicitSettings, workingDir
}

// pickUI decides which interface a bare invocation launches.
//
// The settings' name wins when they name one; the binary's default
// covers everything else. A name the binary does not link is an
// error that lists what it does offer.
func pickUI(settings *config.Settings, fallback string, doors map[string]func(io.Writer) *clicontract.Command) (string, error) {
	name := settings.UI
	if name == "" {
		name = fallback
	}
	if _, ok := doors[name]; ok {
		return name, nil
	}
	known := make([]string, 0, len(doors))
	for door := range doors {
		known = append(known, door)
	}
	sort.Strings(known)
	return "", fmt.Errorf("no ui %q; this binary offers: %s", name, strings.Join(known, ", "))
}
