package support

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

// ErrorBody is the wire shape of an error payload, without the envelope.
//
// A stream event carries the error directly rather than in the envelope,
// so both shapes decode into this one.
type ErrorBody struct {
	// Message is the endpoint's own words for the failure.
	Message string `json:"message"`
	// Type is the endpoint's own failure type.
	Type string `json:"type"`
	// Code is the endpoint's own code, which a server may send as a
	// string or as a number.
	Code any `json:"code"`
}

// ErrorResponse is the payload of a failed request.
type ErrorResponse struct {
	// Error is the failure the endpoint reported.
	Error ErrorBody `json:"error"`
}

// FailureKind names the failure class a provider failure carries.
//
// The status gives the class, and the provider's own code and type
// refine it. A quota code is a usage limit whatever the status, a
// billing code is an inactive plan, and a context-length code is a
// context-length failure. A failure that came without a status — an
// error event in a stream — takes its class from its code, and the
// server's own failure is the default.
//
// The table, one row at a time:
//
//	context-length codes                   → context_length
//	400, 422 naming a too-large max_tokens → output_limit
//	quota codes                            → usage_limit
//	429                                    → rate_limited
//	402, or a billing code                 → plan_inactive
//	401, 403                               → auth_failed
//	other 4xx                              → invalid_request
//	5xx                                    → server
func FailureKind(status int, code, errType, message string, fallback poo.ProviderKind) poo.ProviderKind {
	switch {
	case isContextLength(code, message):
		return poo.ProviderKindContextLength
	case isOutputLimit(status, message):
		return poo.ProviderKindOutputLimit
	case isQuota(code, message):
		return poo.ProviderKindUsageLimit
	case isBilling(code, errType):
		return poo.ProviderKindPlanInactive
	case isRateLimit(code, errType, message):
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

// NewProviderError classifies a failure the endpoint reported and fills in
// what its message states about the request.
//
// The kind is the class the table gives. The output cap and the prompt's
// token count come from the message, so every path that turns a wire
// failure into an error — a rejected response, an error body, an error
// event in a stream — carries the same numbers to the session.
func NewProviderError(status int, code, errType, message string, fallback poo.ProviderKind) *poo.ProviderError {
	kind := FailureKind(status, code, errType, message, fallback)
	return &poo.ProviderError{
		Kind:         kind,
		Message:      message,
		Status:       status,
		Code:         code,
		OutputLimit:  outputLimitOf(kind, message),
		PromptTokens: promptTokensOf(kind, message),
	}
}

// isQuota reports whether a failure means the account is out of quota.
//
// The plan's row: a 429 whose code is one of the quota codes is a usage
// limit, and every other 429 is a rate limit. A failure that names no
// code is read from its message, because a server that says only "quota
// exceeded" means the same thing. The Codex endpoint words its plan
// limit as a usage limit or a balance.
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

// isRateLimit reports whether a failure means the requests came too fast.
//
// The type is read as well as the code and the message, because a server
// that reports its failures as an error type — the Messages API names its
// rate limit "rate_limit_error" — carries the fact there and nowhere else.
func isRateLimit(code, errType, message string) bool {
	lower := strings.ToLower(code + " " + errType + " " + message)
	return strings.Contains(lower, "rate_limit") || strings.Contains(lower, "rate limit")
}

// isContextLength reports whether an error means the request was too long.
//
// The message is read as well as the code, because a server that reports
// an overflow with a plain 400 and no code still means the request was
// too long.
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
		"exceed context limit",
		"exceeds the context",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// outputFields are the request fields that carry the output-token limit.
var outputFields = []string{"max_tokens", "max_completion_tokens", "max_output_tokens"}

// outputLimitPhrases are the words an endpoint uses to say the output-token
// field is above what it allows.
//
// "Exceeds" alone is not one: "the input plus max_tokens exceeds the
// model's limit" is a request that is too long, not an output limit, and a
// cap read from it would be the wrong number. The phrase has to sit near the
// field name for the same reason.
var outputLimitPhrases = []string{
	"too large",
	"too high",
	"valid range",
	"range of",
	"at most",
	"maximum allowed",
	"less than or equal to",
}

// outputLimitProximity is how many characters may separate the field name
// from the phrase that calls it too large.
const outputLimitProximity = 80

// isOutputLimit reports whether a rejection says the request's output-token
// field is above what the endpoint allows.
//
// Only a 400 or a 422 is the endpoint rejecting the request itself, and the
// message has to name the output-token field and say it is too large near
// the field's name. A context overflow is checked before this and stays a
// context-length failure, even when its message names max_tokens.
func isOutputLimit(status int, message string) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	lower := strings.ToLower(message)
	for _, field := range outputFields {
		for _, phrase := range outputLimitPhrases {
			if nearText(lower, field, phrase, outputLimitProximity) {
				return true
			}
		}
	}
	return false
}

// nearText reports whether two pieces of text are within distance
// characters of each other.
func nearText(text, a, b string, distance int) bool {
	for start := 0; start <= len(text)-len(a); {
		i := strings.Index(text[start:], a)
		if i < 0 {
			return false
		}
		i += start
		lo, hi := i-distance, i+len(a)+distance
		if lo < 0 {
			lo = 0
		}
		if hi > len(text) {
			hi = len(text)
		}
		if strings.Contains(text[lo:hi], b) {
			return true
		}
		start = i + 1
	}
	return false
}

// outputLimitPatterns read the largest output-token count a rejection
// allows, each from one of the shapes the endpoints use: a range
// "[1, 8192]", "at most 16384", "32768 > 8192" and "less than or equal to
// 4096".
var outputLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[\s*\d+\s*,\s*(\d+)\s*\]`),
	regexp.MustCompile(`at most\s+(\d+)`),
	regexp.MustCompile(`>\s*(\d+)`),
	regexp.MustCompile(`less than or equal to\s+(\d+)`),
}

// outputLimitOf returns the largest output-token count a rejection
// states, or zero when it states none.
func outputLimitOf(kind poo.ProviderKind, message string) int {
	if kind != poo.ProviderKindOutputLimit {
		return 0
	}
	lower := strings.ToLower(message)
	for _, pattern := range outputLimitPatterns {
		match := pattern.FindStringSubmatch(lower)
		if match == nil {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// promptTokenPatterns read the prompt's token count a context-length
// rejection states, each from one of the shapes the endpoints use: vLLM's
// "you requested 5829 tokens (3000 in the messages, 2829 in the
// completion)" and the Messages API's "input length and max_tokens exceed
// context limit: 3000 + 2829 > 4096".
var promptTokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(\d+)\s+in the messages`),
	regexp.MustCompile(`context limit:\s*(\d+)\s*\+`),
}

// promptTokensOf returns the prompt's token count a context-length
// rejection states, or zero when it states none.
func promptTokensOf(kind poo.ProviderKind, message string) int {
	if kind != poo.ProviderKindContextLength {
		return 0
	}
	lower := strings.ToLower(message)
	for _, pattern := range promptTokenPatterns {
		match := pattern.FindStringSubmatch(lower)
		if match == nil {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// RetryAfter returns how long the endpoint asked the client to wait.
//
// The endpoint spells the wait either in milliseconds or in the standard
// header, which is a number of seconds or an HTTP date. The plan reads a
// wait over thirty seconds as a provider limit rather than a failure to
// retry.
func RetryAfter(resp *http.Response) time.Duration {
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

// ClassifyStatus turns a non-200 response into a provider error.
//
// The status gives the class, the payload refines it, and the headers
// say how long the client was asked to wait and when the limit resets.
func ClassifyStatus(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	er := ErrorResponse{}
	_ = json.Unmarshal(body, &er)

	code := CodeString(er.Error.Code)

	message := er.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = resp.Status
	}

	perr := NewProviderError(resp.StatusCode, code, er.Error.Type, message, poo.ProviderKindServer)
	perr.RetryAfter = RetryAfter(resp)
	perr.ResetsAt = resetAt(resp.Header, body)
	return perr
}

// ErrorInBody reports whether a response body is a JSON error rather than
// an SSE stream.
//
// A body that names no failure is not one: the endpoint answered with
// something the transport cannot read, and the caller says so.
func ErrorInBody(body []byte, status int, header http.Header) (*poo.ProviderError, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var payload struct {
		Error *ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload.Error == nil {
		return nil, false
	}
	code := CodeString(payload.Error.Code)
	message := payload.Error.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	perr := NewProviderError(status, code, payload.Error.Type, message, poo.ProviderKindInvalidRequest)
	perr.ResetsAt = resetAt(header, body)
	return perr, true
}

// CodeString renders an error code, which a server may send as a string
// or as a number.
func CodeString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case float64:
		return fmt.Sprintf("%v", c)
	default:
		return ""
	}
}

// ClassifyTransportError turns a request failure into a provider error.
//
// The parent is the context the transport's own timeout was applied to,
// and the request is the one that carries it. A failure the run's
// context ended is the run's, and one the transport's timeout ended is a
// network failure.
func ClassifyTransportError(parent, request context.Context, err error) error {
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if request != nil && request.Err() != nil {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: request.Err().Error()}
	}
	return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
}
