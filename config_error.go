package poo

import "fmt"

// ConfigError is a configuration mistake.
//
// A setting that cannot be honoured is a configuration error, and it
// names the setting that is at fault, so the reader knows what to
// change.
type ConfigError struct {
	// Field names the setting at fault.
	Field string
	// Message explains what is wrong with it.
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("poo: %s: %s", e.Field, e.Message)
}
