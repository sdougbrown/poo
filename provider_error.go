package poo

// ProviderKind classifies a provider failure.
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
)

// ProviderError is a failure a provider reported.
//
// The skeleton has no retry budget, so every provider failure ends the run.
// The kind still travels with the error, so the settle record says what
// happened.
type ProviderError struct {
	// Kind classifies the failure.
	Kind ProviderKind
	// Message is the human-readable explanation.
	Message string
	// Status is the HTTP status, 0 when the failure was not a response.
	Status int
	// Code is the provider's own error code, when it sent one.
	Code string
}

func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return e.Message
	}
	return e.Message
}
