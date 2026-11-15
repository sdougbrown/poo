package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	clicontract "github.com/sdougbrown/cli-contract"

	"github.com/sdougbrown/poo/repl"
)

// handleRepl executes `poo repl`.
//
// The flags are the serve flags, and they pass through to the server
// the repl starts: the repl holds no configuration of its own, so one
// flag set says what both sides run. The parse and the validation run
// here too, so a bad flag is refused before the server starts.
func handleRepl(inv clicontract.Invocation, stdout, stderr io.Writer) exit {
	var o serveOptions
	o.bind(inv)
	if !o.validate("poo repl", stderr) {
		return exit{usageExitCode}
	}

	workingDir, err := workingDirectory(o.workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "poo repl: %v\n", err)
		return exit{usageExitCode}
	}
	// The repl runs the binary again as `poo serve`, so the flags it
	// parsed are spelled a second time for the child.
	serverArgs := append([]string{"serve"}, renderFlags(inv, append(sharedFlagSpecs(), serveFlagSpecs()...))...)
	return exit{repl.Run(repl.Options{
		ServerArgs:  serverArgs,
		WorkingDir:  workingDir,
		HistoryFile: historyPath(),
	})}
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
