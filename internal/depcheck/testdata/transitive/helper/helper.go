// Package helper is a fixture: a package the prompt package may not pull
// in, because it imports a package of its own that the rules forbid.
package helper

import "example.com/poo/depcheck/transitive/extra"

// Text is the text the prompt package reaches for.
const Text = extra.Text
