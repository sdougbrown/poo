package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sdougbrown/poo/config"
)

// A door is the interface a bare invocation of the binary launches. A
// binary links the doors it offers, and the settings name the one to
// launch: `door` in the settings file the command resolves. A binary
// with no door linked, or a name it does not offer, shows the usage
// instead.
const defaultDoor = "repl"

// doors are the doors this binary links, by name.
var doors = map[string]func(args []string) int{
	"repl": runRepl,
}

// runDoor executes a bare `poo`.
//
// The settings resolve the way every command's do: the explicit file,
// the environment's, the project's, or the user's. The door the
// settings name — or, without them, the binary's default — runs with
// the settings file, so the session it opens reads what the door read.
func runDoor() int {
	path, err := config.FindSettings("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n", err)
		return 2
	}
	settings := &config.Settings{Version: 1}
	var args []string
	if path != "" {
		settings, err = config.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo: %v\n", err)
			return 2
		}
		args = []string{"--settings", path}
	}
	name, err := pickDoor(settings, defaultDoor, doors)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n\n%s", err, usage)
		return 2
	}
	return doors[name](args)
}

// pickDoor decides which door a bare invocation launches.
//
// The settings' name wins when they name one; the binary's default
// covers everything else. A name the binary does not link is an
// error that lists what it does offer.
func pickDoor(settings *config.Settings, fallback string, doorMap map[string]func(args []string) int) (string, error) {
	name := settings.Door
	if name == "" {
		name = fallback
	}
	if _, ok := doorMap[name]; ok {
		return name, nil
	}
	known := make([]string, 0, len(doorMap))
	for door := range doorMap {
		known = append(known, door)
	}
	sort.Strings(known)
	return "", fmt.Errorf("no door %q; this binary offers: %s", name, strings.Join(known, ", "))
}
