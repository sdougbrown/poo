package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// The error-kind table.
//
// The plan names a failure class for every way a request can fail. The
// class decides two things: whether the request is worth another
// attempt, and which reason the run settles with.

// failureKind names the failure class a provider failure carries.
//
// The status gives the class, and the provider's own code and type
// refine it. A quota code is a usage limit whatever the status, a
// billing code is an inactive plan, and a context-length code is a
// context-length failure. A failure that came without a status — an
// error chunk in a stream — takes its class from its code, and the
// server's own failure is the default.
//
// The table, one row at a time:
//
//	context-length codes            → context_length
//	quota codes                     → usage_limit
//	429                             → rate_limited
//	402, or a billing code          → plan_inactive
//	401, 403                        → auth_failed
//	other 4xx                       → invalid_request
//	5xx                             → server
func failureKind(status int, code, errType, message string, fallback poo.ProviderKind) poo.ProviderKind {
	switch {
	case isContextLength(code, message):
		return poo.ProviderKindContextLength
	case isQuota(code, message):
		return poo.ProviderKindUsageLimit
	case isBilling(code, errType):
		return poo.ProviderKindPlanInactive
	case isRateLimit(code, message):
		return poo.ProviderKindRateLimited
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return poo.ProviderKindAuthFailed
	case http.StatusPaymentRequired:
		return poo.ProviderKindPlanInactive
	case http.StatusTooManyRequests:
		return poo.ProviderKindRateLimited
	}
	switch {
	case status >= 400 && status < 500:
		return poo.ProviderKindInvalidRequest
	case status >= 500:
		return poo.ProviderKindServer
	}
	return fallback
}

// isQuota reports whether a failure means the account is out of quota.
//
// The plan's row: a 429 whose code is one of the quota codes is a
// usage limit, and every other 429 is a rate limit. A failure that
// names no code is read from its message, because a server that says
// only "quota exceeded" means the same thing.
func isQuota(code, message string) bool {
	if code != "" {
		lower := strings.ToLower(code)
		return strings.Contains(lower, "quota") || strings.Contains(lower, "usage_limit")
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "quota") || strings.Contains(lower, "usage limit")
}

// isBilling reports whether a failure means the account's plan does not
// allow the request.
//
// The endpoints name it in the code ("billing_not_active") or in the
// error's type ("billing_error").
func isBilling(code, errType string) bool {
	for _, needle := range []string{"billing", "payment", "plan_inactive", "subscription", "insufficient_credit"} {
		if strings.Contains(strings.ToLower(code), needle) || strings.Contains(strings.ToLower(errType), needle) {
			return true
		}
	}
	return false
}

// isRateLimit reports whether a failure names a rate limit.
func isRateLimit(code, message string) bool {
	lower := strings.ToLower(code + " " + message)
	return strings.Contains(lower, "rate_limit") || strings.Contains(lower, "rate limit")
}

// isContextLength reports whether an error means the request was too long.
//
// The message is read as well as the code, because a server that
// reports an overflow with a plain 400 and no code still means the
// request was too long.
func isContextLength(code, message string) bool {
	text := strings.ToLower(code + " " + message)
	for _, needle := range []string{
		"context_length",
		"context length",
		"maximum context",
		"max context",
		"too many tokens",
		"reduce the length",
		"prompt is too long",
		"input is too long",
		"exceeds the context",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// retryAfter returns how long the endpoint asked the client to wait.
//
// The header is either a number of seconds or an HTTP date, and the
// plan reads a Retry-After over thirty seconds as a provider limit
// rather than a failure to retry.
func retryAfter(resp *http.Response) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	d := time.Until(when)
	if d <= 0 {
		return 0
	}
	return d
}

// resetAt returns the time the endpoint says its limit resets.
//
// The endpoints report it in a header or in the error's own fields:
// x-ratelimit-reset-requests and x-ratelimit-reset-tokens hold a
// duration ("6m0s") or a unix time, and the error body may hold a
// resets_at field with a unix time. The body's own field comes first,
// because it names the limit the request hit.
func resetAt(header http.Header, body []byte) time.Time {
	if when, ok := resetsAtField(body); ok {
		return when
	}
	for _, name := range []string{"x-ratelimit-reset-requests", "x-ratelimit-reset-tokens", "x-ratelimit-reset"} {
		raw := strings.TrimSpace(header.Get(name))
		if raw == "" {
			continue
		}
		if when, ok := parseReset(raw, time.Now()); ok {
			return when
		}
	}
	return time.Time{}
}

// resetsAtField reads the resets_at field out of an error body.
//
// The field is a unix time in seconds, or a time the endpoint wrote as
// a string.
func resetsAtField(body []byte) (time.Time, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return time.Time{}, false
	}
	var payload struct {
		ResetsAt json.RawMessage `json:"resets_at"`
		Error    struct {
			ResetsAt json.RawMessage `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return time.Time{}, false
	}
	for _, raw := range []json.RawMessage{payload.ResetsAt, payload.Error.ResetsAt} {
		if len(raw) == 0 {
			continue
		}
		var secs float64
		if err := json.Unmarshal(raw, &secs); err == nil {
			if secs <= 0 {
				continue
			}
			return time.Unix(int64(secs), 0), true
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			if when, err := time.Parse(time.RFC3339, text); err == nil {
				return when, true
			}
			if secs, err := strconv.ParseFloat(text, 64); err == nil && secs > 0 {
				return time.Unix(int64(secs), 0), true
			}
		}
	}
	return time.Time{}, false
}

// parseReset reads one reset value.
//
// The value is either a duration ("6m0s"), a number of seconds, or a
// unix time. A number too large to be a number of seconds is read as a
// unix time.
func parseReset(raw string, now time.Time) (time.Time, bool) {
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return time.Time{}, false
		}
		return now.Add(d), true
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	if secs > 1e9 {
		return time.Unix(int64(secs), 0), true
	}
	return now.Add(time.Duration(secs * float64(time.Second))), true
}

// classifyStatus turns a non-200 response into a provider error.
//
// The status gives the class, the payload refines it, and the headers
// say how long the client was asked to wait and when the limit resets.
func classifyStatus(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	er := errorResponse{}
	_ = json.Unmarshal(body, &er)

	code := codeString(er.Error.Code)

	message := er.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = resp.Status
	}

	return &poo.ProviderError{
		Kind:       failureKind(resp.StatusCode, code, er.Error.Type, message, poo.ProviderKindServer),
		Message:    message,
		Status:     resp.StatusCode,
		Code:       code,
		RetryAfter: retryAfter(resp),
		ResetsAt:   resetAt(resp.Header, body),
	}
}
