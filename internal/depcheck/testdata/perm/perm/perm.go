// Package perm is a fixture: the permission package, which may import the
// core package and no more.
package perm

import "example.com/poo/depcheck/perm/extra"

// Use returns the extra text.
func Use() string { return extra.Text }
