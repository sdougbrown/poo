package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The error-kind table.
//
// The plan names a failure class for every way a request can fail, and
// the class decides the settle reason and what the record says about
// it. The table below is the plan's table, one row at a time.

// settleOf runs a session against a transport and returns the run's
// result.
//
// The session is the core's, so the settle reason and the provider
// detail are the core's own.
func settleOf(t *testing.T, tr poo.ChatTransport) poo.Result {
	t.Helper()
	s, err := poo.New(poo.Options{
		Transport:    tr,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "m", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer s.Close()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return res
}

// errorServer answers with a status and a body, and counts the
// requests it received.
func errorServer(t *testing.T, status int, body string, header map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// errorFixture reads a recorded error body.
func errorFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "errors", name))
	if err != nil {
		t.Fatalf("reading the error fixture: %v", err)
	}
	return string(raw)
}

// TestTheErrorKindTableGivesTheSettleReason covers the plan's table, one
// row at a time: the failure class the transport names, the reason the
// run settles with, and what the record says about the failure.
//
// Every row has a fixture: the error body an endpoint sent, with the
// status the row is about.
func TestTheErrorKindTableGivesTheSettleReason(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		fixture    string
		header     map[string]string
		wantKind   string
		wantReason poo.Reason
		wantCode   string
	}{
		{
			name:       "a 429 with a quota code is a usage limit",
			status:     http.StatusTooManyRequests,
			fixture:    "429-quota.json",
			wantKind:   "usage_limit",
			wantReason: poo.ReasonProviderLimit,
			wantCode:   "insufficient_quota",
		},
		{
			name:       "a 429 with a usage_limit code is a usage limit",
			status:     http.StatusTooManyRequests,
			fixture:    "429-usage-limit.json",
			wantKind:   "usage_limit",
			wantReason: poo.ReasonProviderLimit,
			wantCode:   "usage_limit",
		},
		{
			name:       "a 429 with a rate-limit quota code is a usage limit",
			status:     http.StatusTooManyRequests,
			fixture:    "429-quota-rate-limit.json",
			wantKind:   "usage_limit",
			wantReason: poo.ReasonProviderLimit,
			wantCode:   "rate_limit_exceeded_quota",
		},
		{
			name:       "another 429 is a rate limit",
			status:     http.StatusTooManyRequests,
			fixture:    "429-rate-limited.json",
			wantKind:   "rate_limited",
			wantReason: poo.ReasonProviderError,
			wantCode:   "rate_limit_exceeded",
		},
		{
			name:       "a 402 is an inactive plan",
			status:     http.StatusPaymentRequired,
			fixture:    "402-payment-required.json",
			wantKind:   "plan_inactive",
			wantReason: poo.ReasonProviderLimit,
		},
		{
			name:       "a billing code is an inactive plan",
			status:     http.StatusBadRequest,
			fixture:    "400-billing.json",
			wantKind:   "plan_inactive",
			wantReason: poo.ReasonProviderLimit,
			wantCode:   "billing_not_active",
		},
		{
			name:       "a 401 is a rejected credential",
			status:     http.StatusUnauthorized,
			fixture:    "401-unauthorized.json",
			wantKind:   "auth_failed",
			wantReason: poo.ReasonProviderError,
			wantCode:   "invalid_api_key",
		},
		{
			name:       "a 403 is a rejected credential",
			status:     http.StatusForbidden,
			fixture:    "403-forbidden.json",
			wantKind:   "auth_failed",
			wantReason: poo.ReasonProviderError,
		},
		{
			name:       "another 4xx is a rejected request",
			status:     http.StatusBadRequest,
			fixture:    "400-invalid-request.json",
			wantKind:   "invalid_request",
			wantReason: poo.ReasonProviderError,
		},
		{
			name:       "a 5xx is the server's failure",
			status:     http.StatusInternalServerError,
			fixture:    "500-server.json",
			wantKind:   "server",
			wantReason: poo.ReasonProviderError,
		},
		{
			name:       "a context-length code is a context-length failure",
			status:     http.StatusBadRequest,
			fixture:    "400-context-length.json",
			wantKind:   "context_length",
			wantReason: poo.ReasonContextExhausted,
			wantCode:   "context_length_exceeded",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := errorServer(t, c.status, errorFixture(t, c.fixture), c.header)
			tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))

			res := settleOf(t, tr)
			if res.Reason != c.wantReason {
				t.Errorf("the run settled %q, want %q", res.Reason, c.wantReason)
			}
			if res.Detail.Provider == nil {
				t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
			}
			if res.Detail.Provider.Kind != c.wantKind {
				t.Errorf("the failure's kind is %q, want %q", res.Detail.Provider.Kind, c.wantKind)
			}
			if res.Detail.Provider.Status != c.status {
				t.Errorf("the failure's status is %d, want %d", res.Detail.Provider.Status, c.status)
			}
			if c.wantCode != "" && res.Detail.Provider.Code != c.wantCode {
				t.Errorf("the failure's code is %q, want %q", res.Detail.Provider.Code, c.wantCode)
			}
		})
	}
}

// TestANetworkFailureIsItsOwnRow covers the row the table has for a
// request that never reached the endpoint.
func TestANetworkFailureIsItsOwnRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	tr := openai.New(fastRetry(openai.Options{BaseURL: url, Model: "m"}))
	res := settleOf(t, tr)
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.Kind != "network" {
		t.Errorf("the failure's kind is %q, want network", res.Detail.Provider.Kind)
	}
	if res.Detail.Provider.Status != 0 {
		t.Errorf("the failure's status is %d, want none", res.Detail.Provider.Status)
	}
}

// TestAnUnknownCodeIsKeptRaw covers the plan's rule: a code the
// transport does not know keeps its own name in the record.
func TestAnUnknownCodeIsKeptRaw(t *testing.T) {
	srv := errorServer(t, http.StatusBadRequest, `{"error":{"message":"weird","code":"model_overloaded_later"}}`, nil)
	tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))

	res := settleOf(t, tr)
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.Code != "model_overloaded_later" {
		t.Errorf("the failure's code is %q, want the provider's own", res.Detail.Provider.Code)
	}
}

// TestARateLimitOverThirtySecondsIsAProviderLimit covers the plan's
// rule: a Retry-After over thirty seconds is a provider limit, and the
// request is not retried.
func TestARateLimitOverThirtySecondsIsAProviderLimit(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Retry-After", "120")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`)
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
	res := settleOf(t, tr)
	if res.Reason != poo.ReasonProviderLimit {
		t.Errorf("the run settled %q, want provider_limit", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.RetryAfterS != 120 {
		t.Errorf("the failure's wait is %d seconds, want 120", res.Detail.Provider.RetryAfterS)
	}
	if requests != 1 {
		t.Errorf("the endpoint got %d requests, want 1: the wait is over the budget", requests)
	}
}

// TestTheResetTimeIsRecorded covers the plan's rule: the reset time the
// endpoint reports travels with the failure.
func TestTheResetTimeIsRecorded(t *testing.T) {
	srv := errorServer(t, http.StatusTooManyRequests,
		`{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`,
		map[string]string{"x-ratelimit-reset-requests": "6m0s"})

	tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))
	res := settleOf(t, tr)
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.ResetsAt == "" {
		t.Fatal("the failure has no reset time")
	}
	when, err := time.Parse(time.RFC3339, res.Detail.Provider.ResetsAt)
	if err != nil {
		t.Fatalf("the reset time %q does not parse: %v", res.Detail.Provider.ResetsAt, err)
	}
	if d := time.Until(when); d < 5*time.Minute || d > 7*time.Minute {
		t.Errorf("the reset time is in %v, want about six minutes", d)
	}
}

// TestTheResetTimeInTheBodyIsRecorded covers the reset time an endpoint
// puts in the error's own fields.
func TestTheResetTimeInTheBodyIsRecorded(t *testing.T) {
	when := time.Now().Add(90 * time.Second).Truncate(time.Second)
	body := `{"error":{"message":"slow down","code":"rate_limit_exceeded"},"resets_at":` +
		itoa(when.Unix()) + `}`
	srv := errorServer(t, http.StatusTooManyRequests, body, nil)

	tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))
	res := settleOf(t, tr)
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.ResetsAt == "" {
		t.Fatal("the failure has no reset time")
	}
	got, err := time.Parse(time.RFC3339, res.Detail.Provider.ResetsAt)
	if err != nil {
		t.Fatalf("the reset time %q does not parse: %v", res.Detail.Provider.ResetsAt, err)
	}
	if d := got.Sub(when); d > time.Second || d < -time.Second {
		t.Errorf("the reset time is %v, want %v", got, when)
	}
}

// itoa formats an int64.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
