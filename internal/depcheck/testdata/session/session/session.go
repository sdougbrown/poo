// Package session is a fixture: the session package, which may import
// the core package and the decide package and no more.
package session

import (
	"example.com/poo/depcheck/session/decide"
	"example.com/poo/depcheck/session/extra"
)

// Use returns the text the packages hold.
func Use() string { return decide.Text + extra.Text }
