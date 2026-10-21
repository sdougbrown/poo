// Package direct is a fixture: the core package imports a package outside
// the standard library, which the core rule forbids.
package direct

import "example.com/poo/depcheck/direct/forbidden"

// Use returns the forbidden value.
func Use() string { return forbidden.Value }
