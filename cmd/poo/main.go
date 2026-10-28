// Command poo runs a poo session.
//
// The command runs one-shot sessions, lists the models it can use,
// prints the prompt it would send and prunes the sessions it kept. The
// exit code says why a run ended.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = `poo runs a session against an OpenAI-compatible endpoint.

Usage:
  poo run [flags] "prompt"
  poo -p "prompt" [flags]      poo run, with the run's flags before or after the prompt (also --print)
  poo serve [flags]
  poo models [--json] [--explain <name>]
  poo prompt [flags]
  poo sessions list [--json]
  poo sessions prune [--older-than <days>] [--keep <n>] [--dry-run]

Shared flags:
  --settings <file>            The settings file: JSON, TOML or YAML
  --system-prompt <file>       The file holding the system prompt's base section
  --append-system-prompt <t>   Text appended to the system prompt, or @file
  --permission-unmatched <a>   What to do with a side-effecting call no rule covers: allow, ask or deny
  --session-dir <dir>          The directory the session files live in
  --ephemeral                  Run without writing a session
  --profile <name>             The profile to run under: base, code or search

Run flags:
  --endpoint <url>       The model's endpoint, e.g. http://localhost:4000/v1
  --model-id <id>        The model identifier
  --context-limit <n>    The model's context window in tokens (required)
  --api-key-env <var>    The environment variable holding the API key
  --tools-mode <mode>    native (default), text or auto
  --reasoning-effort <e> reasoning_effort to send (e.g. low), for models that think only when asked
  --max-turns <n>        The turn limit; 0 means unlimited
  --max-output <n>       The model's output-token limit
  --permission-fallback  The decision when the asker chain is exhausted: allow or deny
  --log <path>           The session log path
  --working-dir <dir>    The directory the tools work in (default: the current one)
  --executor <name>      The executor the bash tool runs commands through: local (default) or bwrap
  --project-prompt       Include the working directory's AGENTS.md (or CLAUDE.md) in the prompt
  --timeout <dur>        The wall-clock limit for the run, e.g. 5m
  --no-wait-jobs         Exit at the first settle; the running jobs stop through the session's close

Serve:
  poo serve runs the wire protocol on stdin and stdout. It takes the
  same endpoint flags as poo run, plus --session-dir and --ephemeral.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the command and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "run":
		return runRun(args[1:])
	case "-p", "--print":
		return runPrint(args[1:])
	case "serve":
		return runServe(args[1:])
	case "models":
		return runModels(args[1:])
	case "prompt":
		return runPrompt(args[1:])
	case "sessions":
		return runSessions(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "poo: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// runPrint runs the print alias.
//
// The run's flags may come before or after the prompt. The arguments are
// read with the run's own flags: each flag the run defines moves ahead
// of the prompt, and every other argument is a word of the prompt, in
// order. From the first argument that is not a flag the run defines,
// or after a "--", the rest is the prompt, as it is after run. A flag
// with a bad value fails the way it does after run.
func runPrint(args []string) int {
	fs := flag.NewFlagSet("poo -p", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o runOptions
	o.register(fs)

	var flags, words []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			if !strings.HasPrefix(err.Error(), "flag provided but not defined") {
				// A bad value or a help request is the run's to
				// report.
				return runRun(append(flags, rest...))
			}
			words = append(words, rest...)
			break
		}
		consumed := rest[:len(rest)-len(fs.Args())]
		rest = fs.Args()
		if terminated(fs, consumed) {
			flags = append(flags, consumed[:len(consumed)-1]...)
			words = append(words, rest...)
			break
		}
		flags = append(flags, consumed...)
		if len(rest) == 0 {
			break
		}
		words = append(words, rest[0])
		rest = rest[1:]
	}
	return runRun(append(flags, words...))
}

// terminated reports whether a parse round ended at a "--" terminator.
//
// A "--" that follows a flag that takes a value is that flag's value,
// not a terminator.
func terminated(fs *flag.FlagSet, consumed []string) bool {
	n := len(consumed)
	if n == 0 || consumed[n-1] != "--" {
		return false
	}
	if n == 1 {
		return true
	}
	prev := consumed[n-2]
	if !strings.HasPrefix(prev, "-") || strings.Contains(prev, "=") {
		return true
	}
	f := fs.Lookup(strings.TrimLeft(prev, "-"))
	if f == nil {
		return true
	}
	if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
		return true
	}
	return false
}
