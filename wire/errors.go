package wire

import (
	"encoding/json"
	"errors"

	"github.com/sdougbrown/poo"
	storesession "github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/wire/protocol"
)

// codeOf returns the data code an error carries.
//
// The core's failures and the store's failures are the two families
// the wire answers: a client branches on the code, not on the message.
func codeOf(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, poo.ErrBusy):
		return protocol.DataBusy
	case errors.Is(err, poo.ErrNotRunning):
		return protocol.DataNotRunning
	case errors.Is(err, poo.ErrSessionClosed):
		return protocol.DataSessionClosed
	case errors.Is(err, poo.ErrSessionBroken):
		return protocol.DataSessionBroken
	case errors.Is(err, poo.ErrUnknownProfile):
		return protocol.DataUnknownProfile
	case errors.Is(err, poo.ErrUnknownModel):
		return protocol.DataUnknownModel
	case errors.Is(err, poo.ErrSessionIncompatible):
		return protocol.DataSessionIncompatible
	case errors.Is(err, poo.ErrUnansweredCall):
		return protocol.DataUnansweredCall
	case errors.Is(err, poo.ErrUnsupportedMode):
		return protocol.DataUnsupportedMode
	case errors.Is(err, poo.ErrPromptRender):
		return protocol.DataInvalidSettings
	case errors.Is(err, poo.ErrNoSessionHeader), errors.Is(err, poo.ErrCodecVersion):
		return protocol.DataCodecMismatch
	case errors.Is(err, storesession.ErrInvalidSessionID):
		return protocol.DataInvalidParams
	}
	// The store's failures carry their own code: session_locked,
	// session_not_found, session_exists, unsupported_mode, no_policy,
	// corrupt_log and store_error.
	if code, ok := storesession.CodeOf(err); ok {
		return code
	}
	// An input the session refused: an unknown message id, or a
	// delivery note that does not describe a number of characters.
	var invalid *poo.ErrInvalidInput
	if errors.As(err, &invalid) {
		return protocol.DataInvalidParams
	}
	// A provider error that says the session does not fit this build.
	var perr *poo.ProviderError
	if errors.As(err, &perr) && perr.Kind == poo.ProviderKindSessionIncompatible {
		return protocol.DataSessionIncompatible
	}
	return protocol.DataInternalError
}

// rpcCode returns the JSON-RPC code a data code travels with.
//
// The codes of JSON-RPC itself say what kind of failure it was, and the
// implementation-defined ones say the server refused.
func rpcCode(dataCode string) int {
	switch dataCode {
	case protocol.DataParseError:
		return protocol.CodeParseError
	case protocol.DataInvalidRequest:
		return protocol.CodeInvalidRequest
	case protocol.DataMethodNotFound:
		return protocol.CodeMethodNotFound
	case protocol.DataInvalidParams, protocol.DataUnknownField:
		return protocol.CodeInvalidParams
	case protocol.DataNotInitialized:
		return protocol.CodeNotInitialized
	case protocol.DataProtocolVersionUnsupported:
		return protocol.CodeVersionUnsupported
	}
	return protocol.CodeServerError
}

// failure maps an error to the failure the client sees.
func failure(err error) *protocol.Error {
	if err == nil {
		return nil
	}
	code := codeOf(err)
	return protocol.Errorf(rpcCode(code), code, "%s", err.Error())
}

// paramsFailure maps a refused params object to the failure the client
// sees.
//
// An unknown field and a value of the wrong type are different
// failures, and the client can act on the difference.
func paramsFailure(err error) *protocol.Error {
	var perr *protocol.ParamsError
	if errors.As(err, &perr) {
		return protocol.Errorf(rpcCode(perr.Code), perr.Code, "%s", perr.Message)
	}
	return protocol.Errorf(protocol.CodeInvalidParams, protocol.DataInvalidParams, "%s", err.Error())
}

// invalidParams returns the failure for a params object that is wrong
// in a way the shape cannot say.
func invalidParams(format string, args ...any) *protocol.Error {
	return protocol.Errorf(protocol.CodeInvalidParams, protocol.DataInvalidParams, format, args...)
}

// idKey returns the key a request id is filed under.
//
// A string id is keyed by its text, so a client may answer with the
// same string the server sent. Any other id is keyed by its raw form.
func idKey(raw []byte) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return "s:" + text
	}
	return "r:" + string(raw)
}
