package config

import (
	"errors"
	"fmt"
	"strings"
)

// The codes the settings failures carry.
//
// A client reports the code, not the message: the command line turns it
// into an exit code, and a wire client turns it into an error data
// code.
const (
	// CodeDecode means the settings did not decode: the document is
	// malformed, or a key is not one the settings have.
	CodeDecode = "decode_error"
	// CodeUnknownField means the settings hold a key the settings do
	// not have.
	CodeUnknownField = "unknown_field"
	// CodeUnavailable means a field is set but is not available: the
	// settings ask for something the other settings do not allow.
	CodeUnavailable = "unavailable_field"
	// CodeInvalid means a field holds a value the settings do not
	// allow.
	CodeInvalid = "invalid_value"
	// CodeUnsupported means the settings ask for something this build
	// does not have.
	CodeUnsupported = "unsupported"
)

// Error is a settings failure with a code.
//
// The code is what a client reports. The field is the setting the
// failure is about, and the challenge explains why: it holds the
// generated check's own account of the field.
type Error struct {
	// Code is the failure's code, e.g. "unavailable_field".
	Code string
	// Field is the setting the failure is about, named as the settings
	// name it, e.g. "permission.decide.approve".
	Field string
	// Message describes the failure.
	Message string
	// Challenge explains the failure. It holds the generated check's
	// account of the field, one line per rule that had a say.
	Challenge []string
	// Err is the failure underneath, when there is one.
	Err error
}

// Error returns the failure's message.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("config: ")
	b.WriteString(e.Code)
	if e.Field != "" {
		b.WriteString(": the field ")
		b.WriteString(fmt.Sprintf("%q", e.Field))
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	for _, line := range e.Challenge {
		b.WriteString("\n  ")
		b.WriteString(line)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the failure underneath.
func (e *Error) Unwrap() error { return e.Err }

// Is reports whether the failure is one of the given failures.
//
// The failures are compared by their code: a failure that carries a
// code is the failure that code names, whatever the message says.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return other.Code == e.Code
}

// CodeOf returns the code an error carries, and whether it carried one.
//
// An error that is not a settings error carries no code.
func CodeOf(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return "", false
}

// FieldOf returns the field an error is about, and whether it named
// one.
func FieldOf(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Field, true
	}
	return "", false
}
