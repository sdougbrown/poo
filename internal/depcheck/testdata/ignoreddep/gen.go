//go:build ignore

// Package gen is a fixture: the package the core imports has no
// buildable files, so the import cannot resolve.
package gen

// Run is the generator's entry point.
func Run() {}
