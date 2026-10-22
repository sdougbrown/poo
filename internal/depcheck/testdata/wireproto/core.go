// Package wireproto is a fixture: its wire/protocol package imports a
// non-standard-library package, which breaks the wire/protocol rule.
package wireproto

// Shape returns a shape.
func Shape() string { return "chat" }
