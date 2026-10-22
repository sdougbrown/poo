// Package ignoreddep is a fixture: the core package imports a package
// whose only Go file is build-ignored, which the check must still
// report.
package ignoreddep

import "example.com/poo/depcheck/ignoreddep/gen"

// Run starts the ignored generator.
func Run() { gen.Run() }
