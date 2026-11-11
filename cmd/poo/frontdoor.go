package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sdougbrown/poo/config"
)

// A front door is the interface a bare invocation of the binary
// launches. A binary links the doors it offers, and the settings name
// the one to launch: `front_door` in the settings file the
// POO_SETTINGS environment variable points at. A binary with no door
// linked, or a name it does not offer, shows the usage instead.
const defaultFrontDoor = "repl"

// frontDoors are the doors this binary links, by name.
var frontDoors = map[string]func(args []string) int{
	"repl": runRepl,
}

// runFrontDoor executes a bare `poo`.
//
// The settings come from the file POO_SETTINGS names, when the
// environment points at one; a settings file that fails to load is a
// refusal, not a silent fallthrough to the default door. The door the
// settings name — or, without them, the binary's default — runs with
// the settings file, so the session it opens reads what the door read.
func runFrontDoor() int {
	path := os.Getenv("POO_SETTINGS")
	var settings *config.Settings
	var args []string
	if path != "" {
		var err error
		settings, err = config.Load(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poo: %v\n", err)
			return 2
		}
		args = []string{"--settings", path}
	} else {
		settings = &config.Settings{Version: 1}
	}
	name, err := pickFrontDoor(settings, defaultFrontDoor, frontDoors)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n\n%s", err, usage)
		return 2
	}
	return frontDoors[name](args)
}

// pickFrontDoor decides which door a bare invocation launches.
//
// The settings' name wins when they name one; the binary's default
// covers everything else. A name the binary does not link is an
// error that lists what it does offer.
func pickFrontDoor(settings *config.Settings, fallback string, doors map[string]func(args []string) int) (string, error) {
	name := settings.FrontDoor
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
	return "", fmt.Errorf("no front door %q; this binary offers: %s", name, strings.Join(known, ", "))
}
