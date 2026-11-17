// Package term answers the one question a command asks about its input: is there a
// terminal on the other end of it.
//
// One answer, because the alternative is two: a mode check on the file and an isatty call
// disagree on a character device that is not a tty, and a command that asks one while the
// repl asks the other behaves differently on the same input.
package term

import "golang.org/x/term"

// IsTerminal reports whether the file behind f is a terminal.
//
// A file that names no descriptor is not one, which is what an in-memory buffer and a
// typed nil are.
func IsTerminal(f any) bool {
	fd, ok := f.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(fd.Fd()))
}
