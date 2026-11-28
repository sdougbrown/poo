// Package sqlite is a fixture: the session/sqlite package, which the
// core and session packages import. It imports the standard library
// only, so the core's and session's imports of it are not import
// cycles.
package sqlite

// Use returns the text the package holds.
const Use = "sqlite"
