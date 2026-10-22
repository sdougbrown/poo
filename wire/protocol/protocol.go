// Package protocol holds poo's public wire contract.
//
// The contract is JSON-RPC 2.0 over a byte stream, one JSON object per
// line. The types in this package are the contract: a client depends on
// them, and a server that speaks them is a poo server.
//
// The package imports the standard library only, so a client can depend
// on the contract without pulling the rest of poo in.
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The methods a client calls and the server announces.
//
// The names are namespaced by what they act on: "poo/" is the binary
// itself, "session/" is a session, "agent/" is a run and "job/" is a
// background tool call.
const (
	// MethodInitialize is the handshake. It is the only method a client
	// may call before the handshake is done.
	MethodInitialize = "poo/initialize"
	// MethodSessionCreate starts a session.
	MethodSessionCreate = "session/create"
	// MethodSessionLoad opens a recorded session.
	MethodSessionLoad = "session/load"
	// MethodSessionGet reports a session's state.
	MethodSessionGet = "session/get"
	// MethodSessionConfigure changes a session between its runs.
	MethodSessionConfigure = "session/configure"
	// MethodSessionClose closes a session.
	MethodSessionClose = "session/close"
	// MethodSessionHistory pages a session's scrollback.
	MethodSessionHistory = "session/history"
	// MethodModelsList returns the effective model catalog.
	MethodModelsList = "models/list"
	// MethodAgentPrompt starts a run.
	MethodAgentPrompt = "agent/prompt"
	// MethodAgentMessage sends a message to the active run.
	MethodAgentMessage = "agent/message"
	// MethodAgentInspect reports a session's state and events.
	MethodAgentInspect = "agent/inspect"
	// MethodAgentCancel ends the active run.
	MethodAgentCancel = "agent/cancel"
	// MethodJobCancel stops one background job.
	MethodJobCancel = "job/cancel"
	// MethodConfigGet returns the effective settings.
	MethodConfigGet = "config/get"
	// MethodPermissionAsk is the server's ask to the client. The
	// request's id is the ask's id.
	MethodPermissionAsk = "permission/ask"
	// MethodEvent is the notification the server sends for every
	// session event.
	MethodEvent = "event"
)

// The JSON-RPC codes the server uses.
//
// The first five are the ones JSON-RPC 2.0 defines. The last three are
// in the implementation-defined range: they say what the server refused
// and why, and the data code carries the detail.
const (
	// CodeParseError means the line was not JSON.
	CodeParseError = -32700
	// CodeInvalidRequest means the line was JSON but not a request.
	CodeInvalidRequest = -32600
	// CodeMethodNotFound means the server has no such method.
	CodeMethodNotFound = -32601
	// CodeInvalidParams means the params were not what the method
	// takes.
	CodeInvalidParams = -32602
	// CodeInternalError means the server failed in a way the client
	// cannot act on.
	CodeInternalError = -32603
	// CodeServerError is the implementation-defined code for the
	// server's own refusals: a session that is not there, a session
	// that is busy, a model the settings do not name.
	CodeServerError = -32000
	// CodeVersionUnsupported means the client and the server speak no
	// common protocol version.
	CodeVersionUnsupported = -32001
	// CodeNotInitialized means the client called a method before the
	// handshake.
	CodeNotInitialized = -32002
)

// The codes the error's data carries.
//
// The data code is what a client branches on: the JSON-RPC code says
// what kind of failure it was, and the data code says which one.
const (
	// DataParseError means the line was not JSON.
	DataParseError = "parse_error"
	// DataInvalidRequest means the line was not a request.
	DataInvalidRequest = "invalid_request"
	// DataMethodNotFound means the server has no such method.
	DataMethodNotFound = "method_not_found"
	// DataInvalidParams means the params were wrong.
	DataInvalidParams = "invalid_params"
	// DataUnknownField means the params held a field the method does
	// not know.
	DataUnknownField = "unknown_field"
	// DataNotInitialized means the handshake has not happened.
	DataNotInitialized = "not_initialized"
	// DataProtocolVersionUnsupported means no version is common to the
	// client and the server.
	DataProtocolVersionUnsupported = "protocol_version_unsupported"
	// DataSessionNotFound means the store holds no such session.
	DataSessionNotFound = "session_not_found"
	// DataSessionLocked means another process holds the session's lock.
	DataSessionLocked = "session_locked"
	// DataSessionExists means the store already holds that session.
	DataSessionExists = "session_exists"
	// DataSessionBroken means the session's store failed.
	DataSessionBroken = "session_broken"
	// DataSessionClosed means the session is closing or closed.
	DataSessionClosed = "session_closed"
	// DataSessionIncompatible means the session does not fit this
	// build.
	DataSessionIncompatible = "session_incompatible"
	// DataBusy means the session already has a run.
	DataBusy = "busy"
	// DataNotRunning means the session has no run that could take the
	// request.
	DataNotRunning = "not_running"
	// DataRunNotFound means the session has no such run.
	DataRunNotFound = "run_not_found"
	// DataUnknownModel means the settings do not name that model.
	DataUnknownModel = "unknown_model"
	// DataUnknownProfile means the binary does not offer that profile.
	DataUnknownProfile = "unknown_profile"
	// DataUnsupportedMode means the persistence mode is not supported.
	DataUnsupportedMode = "unsupported_mode"
	// DataCodecMismatch means the log was written in a format the codec
	// does not know.
	DataCodecMismatch = "codec_mismatch"
	// DataNoPolicy means the prune had no policy.
	DataNoPolicy = "no_policy"
	// DataCorruptLog means the log does not hold a session.
	DataCorruptLog = "corrupt_log"
	// DataUnansweredCall means the session holds a tool call with no
	// result.
	DataUnansweredCall = "unanswered_call"
	// DataStoreError means the store could not read or write a
	// session.
	DataStoreError = "store_error"
	// DataInternalError means the server failed in a way the client
	// cannot act on.
	DataInternalError = "internal_error"
)

// Version is the protocol version this build speaks.
//
// A version is bumped when a shape changes or goes away. Within a
// version the server may add methods, event types and optional fields,
// and every addition but a capabilities key is announced in the
// handshake.
const Version = 1

// Request is one request from a client.
//
// A request without an id is a notification: the server acts on it and
// answers nothing.
type Request struct {
	// JSONRPC is the protocol version of the message. It is "2.0".
	JSONRPC string `json:"jsonrpc"`
	// ID is the request's id. The server echoes it in the response.
	ID json.RawMessage `json:"id,omitempty"`
	// Method is the method the client calls.
	Method string `json:"method"`
	// Params is the method's params object.
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one response to a client.
//
// A response carries either a result or an error, never both.
type Response struct {
	// JSONRPC is the protocol version of the message. It is "2.0".
	JSONRPC string `json:"jsonrpc"`
	// ID is the id of the request the response answers.
	ID json.RawMessage `json:"id,omitempty"`
	// Result is the result of the request.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the failure of the request.
	Error *Error `json:"error,omitempty"`
}

// Notification is one message the server sends without being asked.
//
// The server sends the event notifications and the permission asks. An
// ask is a request, though: it has an id, and the client answers it.
type Notification struct {
	// JSONRPC is the protocol version of the message. It is "2.0".
	JSONRPC string `json:"jsonrpc"`
	// Method is the method the notification carries.
	Method string `json:"method"`
	// Params is the notification's params object.
	Params json.RawMessage `json:"params,omitempty"`
}

// Error is a failure the server reports.
//
// The code says what kind of failure it was, and the data code says
// which one: a client branches on the data code.
type Error struct {
	// Code is the JSON-RPC code.
	Code int `json:"code"`
	// Message says what happened, in words.
	Message string `json:"message"`
	// Data carries the code and the detail.
	Data *ErrorData `json:"data,omitempty"`
}

// ErrorData is what an error's data carries.
type ErrorData struct {
	// Code is the machine-readable code, e.g. "session_not_found".
	Code string `json:"code"`
	// Detail says what happened, in words. It repeats the error's own
	// message when there is nothing more to say.
	Detail string `json:"detail,omitempty"`
}

// ParamsError is a params object the server refused.
//
// The code says which kind of refusal it was: an unknown field is not
// the same failure as a value of the wrong type.
type ParamsError struct {
	// Code is the data code: DataUnknownField or DataInvalidParams.
	Code string
	// Message says what was wrong.
	Message string
	// Field is the name of the unknown field, when there is one.
	Field string
}

// Error returns the failure's message.
func (e *ParamsError) Error() string { return e.Message }

// DecodeParams decodes a params object into a value, strictly.
//
// A params object that holds a field the shape does not know is an
// unknown field, and a params object that is not an object at all is
// invalid. An omitted params object takes the shape's defaults.
//
// The failure is a *ParamsError, so the caller can tell the two apart.
func DecodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if isNull(raw) {
		return &ParamsError{Code: DataInvalidParams, Message: "the params are null"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &typeErr):
			return &ParamsError{Code: DataInvalidParams, Message: err.Error()}
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
			return &ParamsError{Code: DataUnknownField, Message: err.Error(), Field: field}
		default:
			return &ParamsError{Code: DataInvalidParams, Message: err.Error()}
		}
	}
	return nil
}

// isNull reports whether a raw message is the JSON null.
func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// Errorf returns an error with a data code and a message.
//
// The wire builds its failures with it, so every failure carries the
// code a client branches on.
func Errorf(code int, dataCode, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{
		Code:    code,
		Message: msg,
		Data:    &ErrorData{Code: dataCode, Detail: msg},
	}
}
