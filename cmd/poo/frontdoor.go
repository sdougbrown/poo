package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sdougbrown/poo/config"
)

// The ui setting names the interface a bare invocation of the binary
// launches. A binary links the interfaces it offers, and the settings
// name the one to launch: `ui` in the settings file the command
// resolves. A binary with none linked, or a name it does not offer,
// shows the usage instead.
const defaultUI = "repl"

// uis are the interfaces this binary links, by name.
var uis = map[string]func(args []string) int{
	"repl": runRepl,
}

// runUI executes a bare `poo`.
//
// The settings resolve the way every command's do: the explicit file,
// the environment's, the project's, or the user's. The interface the
// settings name — or, without them, the binary's default — runs with
// the settings file, so the session it opens reads what it read.
func runUI() int {
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
	name, err := pickUI(settings, defaultUI, uis)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo: %v\n\n%s", err, usage)
		return 2
	}
	return uis[name](args)
}

// pickUI decides which interface a bare invocation launches.
//
// The settings' name wins when they name one; the binary's default
// covers everything else. A name the binary does not link is an
// error that lists what it does offer.
func pickUI(settings *config.Settings, fallback string, uiMap map[string]func(args []string) int) (string, error) {
	name := settings.UI
	if name == "" {
		name = fallback
	}
	if _, ok := uiMap[name]; ok {
		return name, nil
	}
	known := make([]string, 0, len(uiMap))
	for ui := range uiMap {
		known = append(known, ui)
	}
	sort.Strings(known)
	return "", fmt.Errorf("no ui %q; this binary offers: %s", name, strings.Join(known, ", "))
}
