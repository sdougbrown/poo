// Command poo runs a poo session.
//
// The command runs one-shot sessions, lists the models it can use,
// prints the prompt it would send and prunes the sessions it kept. The
// exit code says why a run ended.
package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `poo runs a session against an OpenAI-compatible endpoint.

Usage:
  poo run [flags] "prompt"
  poo -p "prompt" [flags]      An alias for poo run; --print is the same flag
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
// A prompt followed by flags moves behind them, because the flag set
// stops at the first argument that is not a flag. Any other order is
// already the run's own, and passes through unchanged.
func runPrint(args []string) int {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || !strings.HasPrefix(args[1], "-") {
		return runRun(args)
	}
	rest := make([]string, 0, len(args))
	rest = append(rest, args[1:]...)
	rest = append(rest, args[0])
	return runRun(rest)
}
