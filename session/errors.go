package session

import "errors"

// The codes the store's failures carry.
//
// A client reports the code, not the message: the wire layer maps it to
// the protocol's error code, and the command line maps it to an exit
// code.
const (
	// CodeCodecMismatch means the log was written in a format the
	// codec does not know.
	CodeCodecMismatch = "codec_mismatch"
	// CodeSessionLocked means another process holds the session's lock.
	CodeSessionLocked = "session_locked"
	// CodeSessionNotFound means the store holds no such session.
	CodeSessionNotFound = "session_not_found"
	// CodeSessionExists means the store already holds that session.
	CodeSessionExists = "session_exists"
	// CodeUnsupportedMode means the session's persistence mode is not
	// supported by this build.
	CodeUnsupportedMode = "unsupported_mode"
	// CodeNoPolicy means the prune had no policy: retention is opt-in.
	CodeNoPolicy = "no_policy"
	// CodeCorruptLog means the log does not hold a session.
	CodeCorruptLog = "corrupt_log"
	// CodeStoreError means the store could not read or write a
	// session.
	CodeStoreError = "store_error"
	// CodeInvalidSessionID means the id could reach outside the session
	// directory.
	CodeInvalidSessionID = "invalid_session_id"
)

// The failures the store reports.
//
// They are values, so a caller can test for one with errors.Is.
var (
	// ErrSessionLocked means another process holds the session's lock.
	ErrSessionLocked = &Error{Code: CodeSessionLocked, Message: "another process holds the session's lock"}
	// ErrSessionNotFound means the store holds no such session.
	ErrSessionNotFound = &Error{Code: CodeSessionNotFound, Message: "no such session"}
	// ErrUnsupportedMode means the persistence mode is not supported by
	// this build.
	ErrUnsupportedMode = &Error{Code: CodeUnsupportedMode, Message: "the persistence mode is not supported"}
	// ErrNoPolicy means the prune had no policy.
	ErrNoPolicy = &Error{Code: CodeNoPolicy, Message: "no retention policy"}
	// ErrCodecMismatch means the log was written in a format the codec
	// does not know.
	ErrCodecMismatch = &Error{Code: CodeCodecMismatch, Message: "the log's format does not match the codec"}
	// ErrInvalidSessionID means the id names something outside the
	// session directory.
	ErrInvalidSessionID = &Error{Code: CodeInvalidSessionID, Message: "the session id is not a plain name"}
)

// Error is a store failure with a code.
//
// The code is what a client reports. The message says what happened, and
// the wrapped failure says why.
type Error struct {
	// Code is the failure's code, e.g. "session_locked".
	Code string
	// Message describes the failure.
	Message string
	// Err is the failure underneath, when there is one.
	Err error
}

// Error returns the failure's message.
func (e *Error) Error() string {
	switch {
	case e.Message != "" && e.Err != nil:
		return "session: " + e.Code + ": " + e.Message + ": " + e.Err.Error()
	case e.Message != "":
		return "session: " + e.Code + ": " + e.Message
	case e.Err != nil:
		return "session: " + e.Code + ": " + e.Err.Error()
	}
	return "session: " + e.Code
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
// An error that is not a store error carries no code.
func CodeOf(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return "", false
}
