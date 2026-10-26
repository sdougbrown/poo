// Package conformance is a fixture: it imports a non-standard-library
// package, which breaks the wire/conformance rule.
package conformance

import "example.com/poo/depcheck/wireconf/extra"

// Run names the run.
var Run = extra.Name
