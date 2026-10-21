// Package session is a fixture: a package that imports the core package.
package session

import "example.com/poo/depcheck/cycle"

// Name is the session name.
var Name = cycle.Use
