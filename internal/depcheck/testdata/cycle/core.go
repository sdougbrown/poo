// Package cycle is a fixture: the core package imports a package that
// imports the core package back, so the toolchain reports an import cycle on
// the core package. The offending import must still be named.
package cycle

import "example.com/poo/depcheck/cycle/session"

// Use returns the session name.
func Use() string { return session.Name }
