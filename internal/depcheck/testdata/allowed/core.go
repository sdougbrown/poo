// Package allowed is a fixture: the core package imports the prompt package,
// which imports only the standard library, so nothing breaks the rules.
package allowed

import "example.com/poo/depcheck/allowed/prompt"

// Use returns the prompt text.
func Use() string { return prompt.Text }
