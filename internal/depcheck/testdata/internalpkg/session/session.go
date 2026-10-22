// Package session is a fixture: a package that imports only the standard
// library and is not one the core package may import.
package session

import "strings"

// Name is the session name.
var Name = strings.ToUpper("session")
