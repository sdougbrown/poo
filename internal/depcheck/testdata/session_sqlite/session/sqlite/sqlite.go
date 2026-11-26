// Package sqlite is a fixture: the SQLite-backed store, which may
// import the core package, the session package and the packages
// outside the module, and no other package of the module.
package sqlite

import (
	"example.com/poo/depcheck/session_sqlite"
	"example.com/poo/depcheck/session_sqlite/extra"
	"example.com/poo/depcheck/session_sqlite/session"
)

// Use returns the text the packages hold.
func Use() string { return session_sqlite.Use + session.Use + extra.Text }
