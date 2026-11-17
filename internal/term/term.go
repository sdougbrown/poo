// Package term owns what a command asks of the input a person types on: whether there is a
// terminal on the other end of it, and how to read a line without echoing what was typed.
//
// One owner, because the alternative is two: a mode check on the file and an isatty call
// disagree on a character device that is not a tty, and a command that asks one while the repl
// asks the other behaves differently on the same input. The hidden read is here for the same
// reason. It is the terminal's own echo that is turned off, so it belongs beside the question
// that says there is a terminal.
package term

import (
	"errors"

	"golang.org/x/term"
)

// descriptor is the descriptor behind f, for the calls x/term makes.
//
// A file that names no descriptor has none, which is what an in-memory buffer
// is.
func descriptor(f any) (int, bool) {
	file, ok := f.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return int(file.Fd()), true
}

// IsTerminal reports whether the file behind f is a terminal.
func IsTerminal(f any) bool {
	fd, ok := descriptor(f)
	if !ok {
		return false
	}
	return term.IsTerminal(fd)
}

// ReadPassword reads a line from the terminal behind f without echoing it.
func ReadPassword(f any) (string, error) {
	fd, ok := descriptor(f)
	if !ok {
		return "", errors.New("term: the input names no descriptor, so nothing is a terminal")
	}
	hidden, err := term.ReadPassword(fd)
	if err != nil {
		return "", err
	}
	return string(hidden), nil
}
