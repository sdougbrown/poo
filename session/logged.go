package session

import (
	"net/url"

	"github.com/sdougbrown/poo"
)

// NewID returns a fresh identifier for a session or a run.
//
// The identifier is a ULID: it sorts by creation time, so the store's
// id-ordered list presents sessions in the order they were created.
func NewID() string {
	return poo.NewULID()
}

// LoggedModel returns the model a log records.
//
// A URL may carry credentials, and the log is a file a consumer reads,
// so the userinfo is stripped before the model is written.
func LoggedModel(model poo.ModelInfo) poo.ModelInfo {
	model.Endpoint = LoggedEndpoint(model.Endpoint)
	return model
}

// LoggedEndpoint returns the base URL a log records.
//
// A URL may carry credentials, and the log is a file a consumer reads, so
// the userinfo is stripped before it is written.
func LoggedEndpoint(raw string) string {
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
