// Package sqlite_boundary is a fixture: the core package of the fixture
// module, which imports the session/sqlite package, a violation the
// core rule must catch.
package sqlite_boundary

import "example.com/poo/depcheck/sqlite_boundary/session/sqlite"

// Use returns the value the session/sqlite package holds.
func Use() string { return sqlite.Use }
