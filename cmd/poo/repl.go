package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sdougbrown/poo/repl"
)

// replUsage is the usage of `poo repl`.
const replUsage = `poo repl runs an interactive session in the terminal.

Usage:
  poo repl [flags]

The repl takes the same flags as poo serve, and starts a poo serve
with them. Its input reads with vi keys at a terminal, and the
transcript renders as markdown. Type /help once it is up.
`

// runRepl executes `poo repl`.
//
// The flags are the serve flags, and they pass through to the server
// the repl starts: the repl holds no configuration of its own, so one
// flag set says what both sides run. The parse and the validation run
// here too, so a bad flag is refused before the server starts.
func runRepl(args []string) int {
	o, ok := parseServeFlags("poo repl", replUsage, args)
	if !ok {
		return 2
	}

	workingDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "poo repl: %v\n", err)
		return 2
	}
	return repl.Run(repl.Options{
		ServerArgs:  append([]string{"serve"}, args...),
		WorkingDir:  workingDir,
		HistoryFile: historyPath(),
	})
}

// historyPath is where the repl keeps its input history between
// sessions. A cache directory the system does not name means no
// history, not a failure.
func historyPath() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	history := filepath.Join(cache, "poo", "repl-history")
	// readline's history save does not create directories: the parent
	// is here, once, or the save fails quietly every session.
	if err := os.MkdirAll(filepath.Dir(history), 0o755); err != nil {
		return ""
	}
	return history
}
