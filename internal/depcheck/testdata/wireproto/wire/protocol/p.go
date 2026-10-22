// Package protocol is a fixture: it imports a non-standard-library
// package, which breaks the wire/protocol rule.
package protocol

import "example.com/poo/depcheck/wireproto/extra"

// Shape names the wire shape.
var Shape = extra.Name
