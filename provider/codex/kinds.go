package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// errorBody is the error an endpoint reports in its own payload.
type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// errorResponse is the payload of a failed request.
type errorResponse struct {
	Error errorBody `json:"error"`
}

// failureKind names the failure class a provider failure carries.
//
// The status gives the class, and the provider's own code and type refine it.
// A quota code is a usage limit whatever the status, a billing code is an
// inactive plan, and a context-length code is a context-length failure. A
// failure that came without a status — an error event in a stream — takes its
// class from its code. The server's own failure is the default.
//
// The table, one rule per line:
//
//   - context-length codes map to context_length.
//   - quota codes map to usage_limit.
//   - 429 maps to rate_limited.
//   - 402, or a billing code, maps to plan_inactive.
//   - 401 and 403 map to auth_failed.
//   - other 4xx map to invalid_request.
//   - 5xx map to server.
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
// A failure that names no code is read from its message, because a
// server that says only "quota exceeded" means the same thing. The Codex
// endpoint words its plan limit as a usage limit or a balance.
func isQuota(code, message string) bool {
	if code != "" {
		lower := strings.ToLower(code)
		return strings.Contains(lower, "quota") ||
			strings.Contains(lower, "usage_limit") ||
			strings.Contains(lower, "usage_not_included")
	}
	lower := strings.ToLower(message)
	for _, needle := range []string{"quota", "usage limit", "usage_limit", "available balance", "out of budget"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// isBilling reports whether a failure means the account's plan does not
// allow the request.
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
// The endpoint spells the wait either in milliseconds or in the
// standard header, which is a number of seconds or an HTTP date.
func retryAfter(resp *http.Response) time.Duration {
	if raw := strings.TrimSpace(resp.Header.Get("Retry-After-Ms")); raw != "" {
		if ms, err := strconv.ParseFloat(raw, 64); err == nil && ms > 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
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
// The body's own field comes first, because it names the limit the
// request hit; the headers follow.
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
// The field is a unix time in seconds, or a time the endpoint wrote as a
// string.
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
// The status gives the class, and the payload refines it. The headers say
// how long the client was asked to wait, and when the limit resets.
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

// errorInBody reports whether a response body is a JSON error rather than
// an SSE stream.
//
// A body that names no failure is not one: the endpoint answered with
// something the transport cannot read, and the caller says so.
func errorInBody(body []byte, status int, header http.Header) (*poo.ProviderError, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var payload struct {
		Error *errorBody `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload.Error == nil {
		return nil, false
	}
	code := codeString(payload.Error.Code)
	message := payload.Error.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return &poo.ProviderError{
		Kind:     failureKind(status, code, payload.Error.Type, message, poo.ProviderKindInvalidRequest),
		Message:  message,
		Code:     code,
		Status:   status,
		ResetsAt: resetAt(header, body),
	}, true
}

// codeString renders an error code, which a server may send as a string
// or as a number.
func codeString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case float64:
		return fmt.Sprintf("%v", c)
	default:
		return ""
	}
}

// classifyTransportError turns a request failure into a provider error.
func classifyTransportError(parent, request context.Context, err error) error {
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if request != nil && request.Err() != nil {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: request.Err().Error()}
	}
	return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
}
