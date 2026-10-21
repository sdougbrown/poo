package poo

import (
	"fmt"
	"time"
)

// ProviderKind classifies a provider failure.
//
// The kinds are the plan's: a settle reason is derived from them, so the
// set is closed and every kind means one thing.
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
	// Code is the provider's own error code, when it sent one.
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
// A rate limit, a server failure and a network failure are worth another
// attempt, as long as the attempt has not spent the budget. A rejection
// is not: the same request would be refused again.
func (e *ProviderError) Retryable() bool {
	switch e.Kind {
	case ProviderKindRateLimited, ProviderKindServer, ProviderKindNetwork:
		return true
	default:
		return false
	}
}
