// Command poo runs a poo session.
//
// The command runs one-shot sessions, lists the models it can use,
// prints the prompt it would send and prunes the sessions it kept. The
// exit code says why a run ended. The command surface — every command,
// its flags and its help — is the declaration in app.go, and cli-contract
// renders the help, the help command and --version from it.
package main

import (
	"os"
	"strings"

	clicontract "github.com/sdougbrown/cli-contract"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches the command and returns the process exit code.
func run(args []string) int {
	if len(args) == 0 {
		return runUI()
	}
	return clicontract.Run(pooApp(os.Stdout), pooAdapter{}, printAlias(args), os.Stdout, os.Stderr)
}

// printAlias rewrites the print alias into the command it aliases.
//
// `poo -p "prompt" [flags]` is `poo run`, and the alias takes the run's
// flags before or after the prompt: they are moved ahead of it, and only
// the flags the run declares move. A help request is the run's help,
// wherever it is written.
//
// The words follow a terminator, so a prompt the caller wrote after `--`
// stays the prompt even when it is spelled like a flag.
func printAlias(args []string) []string {
	if args[0] != "-p" && args[0] != "--print" {
		return args
	}
	flags, words := hoistRunFlags(args[1:])
	aliased := make([]string, 0, 2+len(flags)+len(words))
	aliased = append(aliased, "run")
	aliased = append(aliased, flags...)
	if len(words) > 0 {
		aliased = append(aliased, "--")
	}
	return append(aliased, words...)
}

// hoistRunFlags splits the alias's arguments into the run's flags and the
// prompt's words.
//
// A token naming a flag the run declares is a flag, and a flag that is not
// a bool takes the token after it as its value. Every other token is a
// word of the prompt, and a dash token the run does not declare makes
// everything from it on words: a prompt word that looks like a flag stays
// in the prompt.
func hoistRunFlags(args []string) (flags, words []string) {
	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "--":
			// Everything after the terminator is the prompt.
			return flags, append(words, args[i+1:]...)
		case len(words) == 0 && helpToken(token):
			// A help request before the prompt starts is the run's own.
			// Once the prompt has started, a dash word is prompt text, as
			// it is after run.
			return append(flags, "--help"), words
		}
		spec, declared := declaredRunFlag(token)
		if !declared {
			if flagToken(token) {
				// Before the prompt starts, a dash word the run does not
				// declare is the parser's to refuse, so a mistyped flag
				// fails fast instead of becoming the prompt. After it, the
				// word is prompt text, as it is after run.
				if len(words) == 0 {
					return append(flags, args[i:]...), words
				}
				return flags, append(words, args[i:]...)
			}
			words = append(words, token)
			continue
		}
		flags = append(flags, token)
		if spec.Kind == clicontract.Bool || strings.ContainsRune(token, '=') {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, words
}

// helpToken reports a request for the run's help in every spelling the
// parser accepts: the shorthand, the long flag, and the single-dash long
// form the parser rewrites.
func helpToken(token string) bool {
	if !flagToken(token) {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimLeft(token, "-"), "=")
	return name == "h" || name == "help"
}

// declaredRunFlag answers the declaration of a token that names one of the
// run's flags.
func declaredRunFlag(token string) (clicontract.Flag, bool) {
	if !flagToken(token) {
		return clicontract.Flag{}, false
	}
	name, _, _ := strings.Cut(strings.TrimLeft(token, "-"), "=")
	for _, spec := range append(sharedFlagSpecs(), runFlagSpecs()...) {
		if spec.Name == name {
			return spec, true
		}
	}
	return clicontract.Flag{}, false
}

// flagToken reports whether a token is spelled as a flag.
func flagToken(token string) bool {
	return len(token) > 1 && token[0] == '-'
}
