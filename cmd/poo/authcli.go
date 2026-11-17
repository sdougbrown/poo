package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sdougbrown/poo/internal/term"
	"github.com/sdougbrown/poo/provider/auth"
)

// replace reports whether a credential the file already holds may be replaced.
//
// The store's rule is that nothing replaces a credential without an explicit action: --force is
// one, and answering the prompt is the other. A run with no terminal to ask on has neither, so it
// refuses. The question the rule asks is whether an entry is there, not whether it decodes — an
// entry poo cannot read is still an entry.
//
// The caller reports a failure with its own name, so one rule serves every command that writes a
// credential.
func replace(credentials auth.Credentials, provider auth.Provider, force bool, command string, stderr io.Writer) (bool, bool) {
	held, err := credentials.Holds(provider.ID)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
		return false, true
	}
	if !held {
		return false, false
	}
	if force {
		return true, false
	}
	signedIn := ""
	if status, err := credentials.Status(provider); err == nil && status.AccountID != "" {
		signedIn = " as " + status.AccountID
	}
	if !interactive() {
		fmt.Fprintf(stderr, "%s: %s already holds a credential for %s: pass --force to replace it\n",
			command, credentials.Path, provider.ID)
		return false, true
	}
	fmt.Fprintf(stderr, "%s already holds a credential for %s%s.\n", credentials.Path, provider.ID, signedIn)
	fmt.Fprint(stderr, "Replace it? [y/N] ")
	if !confirmed(os.Stdin) {
		fmt.Fprintf(stderr, "%s: the credential was left as it was\n", command)
		return false, true
	}
	return true, false
}

// interactive reports whether there is a terminal to ask on.
//
// The question is internal/term's, so this and the repl answer it the same way.
func interactive() bool {
	return term.IsTerminal(os.Stdin)
}

// confirmed reads one answer, and reads anything but yes as no.
func confirmed(in io.Reader) bool {
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}
