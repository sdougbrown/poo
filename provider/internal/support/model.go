package support

import (
	"errors"
	"fmt"

	"github.com/sdougbrown/poo"
)

// ConfigErrorf is a failure in a transport's own configuration.
//
// It is a rejected request: the endpoint never saw it, and the run
// settles with the message.
func ConfigErrorf(format string, args ...any) error {
	return &poo.ProviderError{
		Kind:    poo.ProviderKindInvalidRequest,
		Message: fmt.Sprintf(format, args...),
	}
}

// ReasonError renders a configuration error for a request's failure.
//
// The error names the setting that is at fault, so the reader knows what
// to change.
func ReasonError(err error) string {
	var cfg *poo.ConfigError
	if errors.As(err, &cfg) {
		return cfg.Field + ": " + cfg.Message
	}
	return err.Error()
}
