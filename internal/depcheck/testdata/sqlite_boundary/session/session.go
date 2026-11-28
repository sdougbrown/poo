// Package session is a fixture: the session package, which imports the
// session/sqlite package, a violation the session rule must catch.
package session

import "example.com/poo/depcheck/sqlite_boundary/session/sqlite"

// Use returns the value the session/sqlite package holds.
func Use() string { return sqlite.Use }
