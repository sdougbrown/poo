// Package internalpkg is a fixture: the core package imports a package of the
// module that itself imports only the standard library. The core rule still
// forbids it, because only prompt and decide are allowed.
package internalpkg

import "example.com/poo/depcheck/internalpkg/session"

// Use returns the session name.
func Use() string { return session.Name }
