// Package prompt is a fixture: an allowed package of the core package that
// breaks the rule of the prompt package itself.
package prompt

import (
	"example.com/poo/depcheck/transitive/decide"
	"example.com/poo/depcheck/transitive/helper"
)

// Text is the text the core package reaches for.
const Text = helper.Text

// Answer is the answer the core package reaches for.
const Answer = decide.Answer
