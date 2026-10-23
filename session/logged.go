package session

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"

	"github.com/sdougbrown/poo"
)

// NewID returns a fresh identifier for a session or a run.
//
// The identifier is random: ids are opaque to the protocol, and nothing
// reads an order out of one.
func NewID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error (it ends the program if the
	// system's source fails).
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// LoggedModel returns the model a log records.
//
// A URL may carry credentials, and the log is a file a consumer reads,
// so the userinfo is stripped before the model is written.
func LoggedModel(model poo.ModelInfo) poo.ModelInfo {
	model.Endpoint = LoggedBaseURL(model.Endpoint)
	return model
}

// LoggedBaseURL returns the base URL a log records.
//
// A URL may carry credentials, and the log is a file a consumer reads, so
// the userinfo is stripped before it is written.
func LoggedBaseURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
