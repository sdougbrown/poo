//go:build windows

package main

import "golang.org/x/term"

// hiddenLine reads a line from the terminal without echoing it.
func hiddenLine(fd int) (string, error) {
	hidden, err := term.ReadPassword(fd)
	if err != nil {
		return "", err
	}
	return string(hidden), nil
}
