package wire

import (
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	storesession "github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/wire/protocol"
)

// The core's and the store's failures carry the code a client
// branches on.
func TestCodeOf(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"busy", poo.ErrBusy, protocol.DataBusy},
		{"not running", poo.ErrNotRunning, protocol.DataNotRunning},
		{"closed", poo.ErrSessionClosed, protocol.DataSessionClosed},
		{"broken", poo.ErrSessionBroken, protocol.DataSessionBroken},
		{"invalid input", &poo.ErrInvalidInput{Message: "no such message"}, protocol.DataInvalidParams},
		{"unknown profile", poo.ErrUnknownProfile, protocol.DataUnknownProfile},
		{"unknown model", poo.ErrUnknownModel, protocol.DataUnknownModel},
		{"incompatible", poo.ErrSessionIncompatible, protocol.DataSessionIncompatible},
		{"unanswered call", poo.ErrUnansweredCall, protocol.DataUnansweredCall},
		{"unsupported mode", poo.ErrUnsupportedMode, protocol.DataUnsupportedMode},
		{"prompt render", poo.ErrPromptRender, protocol.DataInvalidSettings},
		{"no header", poo.ErrNoSessionHeader, protocol.DataCodecMismatch},
		{"codec version", poo.ErrCodecVersion, protocol.DataCodecMismatch},
		{"locked", storesession.ErrSessionLocked, protocol.DataSessionLocked},
		{"not found", storesession.ErrSessionNotFound, protocol.DataSessionNotFound},
		{"unsupported store mode", storesession.ErrUnsupportedMode, protocol.DataUnsupportedMode},
		{"no policy", storesession.ErrNoPolicy, protocol.DataNoPolicy},
		{"codec mismatch", storesession.ErrCodecMismatch, protocol.DataCodecMismatch},
		{"corrupt log", &storesession.Error{Code: storesession.CodeCorruptLog}, protocol.DataCorruptLog},
		{"session exists", &storesession.Error{Code: storesession.CodeSessionExists}, protocol.DataSessionExists},
		{"store error", &storesession.Error{Code: storesession.CodeStoreError}, protocol.DataStoreError},
		{"plain", errors.New("something went wrong"), protocol.DataInternalError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codeOf(tc.err); got != tc.want {
				t.Errorf("the code = %q, want %q", got, tc.want)
			}
		})
	}
}

// The JSON-RPC code says what kind of failure it was.
func TestRPCCode(t *testing.T) {
	cases := []struct {
		data string
		want int
	}{
		{protocol.DataParseError, protocol.CodeParseError},
		{protocol.DataInvalidRequest, protocol.CodeInvalidRequest},
		{protocol.DataMethodNotFound, protocol.CodeMethodNotFound},
		{protocol.DataInvalidParams, protocol.CodeInvalidParams},
		{protocol.DataUnknownField, protocol.CodeInvalidParams},
		{protocol.DataNotInitialized, protocol.CodeNotInitialized},
		{protocol.DataProtocolVersionUnsupported, protocol.CodeVersionUnsupported},
		{protocol.DataBusy, protocol.CodeServerError},
		{protocol.DataSessionNotFound, protocol.CodeServerError},
	}
	for _, tc := range cases {
		t.Run(tc.data, func(t *testing.T) {
			if got := rpcCode(tc.data); got != tc.want {
				t.Errorf("the code = %d, want %d", got, tc.want)
			}
		})
	}
}
