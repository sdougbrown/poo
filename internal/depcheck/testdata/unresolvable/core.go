// Package unresolvable is a fixture: the core package imports a path inside
// the module that has no directory, so the toolchain cannot resolve it.
package unresolvable

import "example.com/poo/depcheck/unresolvable/missing"

// Use returns the value of the missing package.
func Use() string { return missing.Value }
