package poo

import (
	"fmt"
	"time"
)

// ProviderKind classifies a provider failure.
//
// A settle reason is derived from the kind, and every kind means one
// thing. Not every kind appears in the plan's settle table:
// session_incompatible currently settles as provider_error, and its
// settle placement is pending an owner decision.
type ProviderKind string

const (
	// ProviderKindContextLength means the request exceeded the model's
	// context window.
	ProviderKindContextLength ProviderKind = "context_length"
	// ProviderKindAuthFailed means the endpoint rejected the credentials.
	ProviderKindAuthFailed ProviderKind = "auth_failed"
	// ProviderKindInvalidRequest means the endpoint rejected the request.
	ProviderKindInvalidRequest ProviderKind = "invalid_request"
	// ProviderKindServer means the endpoint failed.
	ProviderKindServer ProviderKind = "server"
	// ProviderKindNetwork means the request never reached the endpoint.
	ProviderKindNetwork ProviderKind = "network"
	// ProviderKindRateLimited means the endpoint asked the client to slow
	// down.
	ProviderKindRateLimited ProviderKind = "rate_limited"
	// ProviderKindUsageLimit means the account is out of quota.
	ProviderKindUsageLimit ProviderKind = "usage_limit"
	// ProviderKindPlanInactive means the account's plan does not allow the
	// request.
	ProviderKindPlanInactive ProviderKind = "plan_inactive"
	// ProviderKindInterruptedStream means the stream broke after it had
	// already sent a delta. The request cannot be retried, because the
	// model has already seen part of the answer.
	ProviderKindInterruptedStream ProviderKind = "interrupted_stream"
	// ProviderKindOutputTruncated means the provider stopped on the
	// output-token limit.
	ProviderKindOutputTruncated ProviderKind = "output_truncated"
	// ProviderKindSessionIncompatible means the history holds a required
	// provider block that belongs to another wire API, so this transport
	// cannot make the request.
	ProviderKindSessionIncompatible ProviderKind = "session_incompatible"
)

// ProviderError is a failure a provider reported.
//
// The kind travels with the error, so the settle record says what
// happened and the loop can tell a retryable failure from a terminal one.
type ProviderError struct {
	// Kind classifies the failure.
	Kind ProviderKind
	// Message is the human-readable explanation.
	Message string
	// Status is the HTTP status, 0 when the failure was not a response.
	Status int
	// Code is the provider's own error code, when it sent one. It also
	// holds the kind a failure had before it was reclassified: an
	// interrupted stream keeps the kind it broke in here.
	Code string
	// RetryAfter is how long the endpoint asked the client to wait, when
	// it said so.
	RetryAfter time.Duration
}

func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s (status %d)", e.Message, e.Status)
	}
	return e.Message
}

// Retryable reports whether the failure is worth retrying.
//
// The plan's rule: a rate limit is worth another attempt only when the
// endpoint asked for a wait of thirty seconds or less, and a wait of
// none means it asked for no wait at all. A server failure and a network
// failure always are. A rejection is not: the same request would be
// refused again.
func (e *ProviderError) Retryable() bool {
	switch e.Kind {
	case ProviderKindRateLimited:
		return e.RetryAfter <= MaxRetryAfter
	case ProviderKindServer, ProviderKindNetwork:
		return true
	default:
		return false
	}
}

// MaxRetryAfter is the longest wait a rate limit may ask for and still
// be worth retrying.
//
// A limit that asks for longer is a provider limit, not a transient
// failure: the run settles and a later run may succeed.
const MaxRetryAfter = 30 * time.Second
