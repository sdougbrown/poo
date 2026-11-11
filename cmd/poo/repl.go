package main

import (
	"flag"
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
	fs := flag.NewFlagSet("poo repl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, replUsage) }

	var o serveOptions
	o.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// The same refusal as serve's: the flags say what the session
	// runs, and the raw arguments pass through to the server, so a
	// stray word would be its confusion, not the repl's.
	if len(fs.Args()) != 0 {
		fmt.Fprintf(os.Stderr, "poo repl: unexpected argument %q\n", fs.Args()[0])
		return 2
	}
	if err := o.checkUnmatched(); err != nil {
		fmt.Fprintf(os.Stderr, "poo repl: %v\n", err)
		return 2
	}
	if err := o.endpointFlags.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "poo repl: %v\n", err)
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
	return filepath.Join(cache, "poo", "repl-history")
}
