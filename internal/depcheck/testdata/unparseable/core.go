// Package unparseable is a fixture: the core package imports a package whose
// file the toolchain cannot load, so the package fails to load.
package unparseable

import "example.com/poo/depcheck/unparseable/broken"

// Use returns the value of the broken package.
func Use() string { return broken.Value }
