// Package transitive is a fixture: the core package imports the prompt
// package, which is allowed, and the prompt package imports a package the
// core rule forbids.
//
// The package that breaks the rule is not one of the packages the rules
// apply to, so the dependency closure of the core package is what exposes
// the violation.
package transitive

import "example.com/poo/depcheck/transitive/prompt"

// Use returns the prompt text.
func Use() string { return prompt.Text }
