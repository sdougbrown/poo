package openai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestANetworkFailureIsNotReportedAsACancellation covers a request that
// never reached the endpoint: the transport's own timeout must not turn
// the failure into a cancellation.
func TestANetworkFailureIsNotReportedAsACancellation(t *testing.T) {
	// The server is closed before the request goes out, so the
	// connection is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	tr := openai.New(fastRetry(openai.Options{BaseURL: url, Model: "m", Timeout: 3 * time.Second}))
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want a network failure")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want a network failure", err)
	}
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindNetwork {
		t.Errorf("the error kind is %q, want network", pe.Kind)
	}
	// The message names the failure, so a consumer can tell a refused
	// connection from another network failure.
	if !strings.Contains(pe.Message, "connection refused") {
		t.Errorf("the error message is %q, want it to name the refused connection", pe.Message)
	}
}

// TestAnErrorBodyWithA200IsAnInvalidRequest covers a server that answers
// a rejected request with a 200 and a JSON error body: the body says
// the request was refused, and nothing else.
func TestAnErrorBodyWithA200IsAnInvalidRequest(t *testing.T) {
	srv := newSSEServer(t, `{"error":{"message":"something odd"}}`)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindInvalidRequest {
		t.Errorf("the error kind is %q, want invalid_request", pe.Kind)
	}
	if pe.Message != "something odd" {
		t.Errorf("the error message is %q, want the body's own", pe.Message)
	}
}

// TestAnErrorBodyWithItsOwnKindKeepsIt covers a body that names its own
// failure: the body's fields win over the default.
func TestAnErrorBodyWithItsOwnKindKeepsIt(t *testing.T) {
	srv := newSSEServer(t, `{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindContextLength {
		t.Errorf("the error kind is %q, want context_length", pe.Kind)
	}
}

// TestTheStatusMapsTheErrorKind covers classifyStatus's status table and
// the payload's refinement of it: the same error words classify the same
// way whichever door they came through.
func TestTheStatusMapsTheErrorKind(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   poo.ProviderKind
	}{
		{"a bare 429", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, poo.ProviderKindRateLimited},
		{"a 429 with a quota code", http.StatusTooManyRequests, `{"error":{"code":"quota_exceeded","message":"out of quota"}}`, poo.ProviderKindUsageLimit},
		{"a 429 with a usage_limit code", http.StatusTooManyRequests, `{"error":{"code":"usage_limit","message":"limit"}}`, poo.ProviderKindUsageLimit},
		{"a 402", http.StatusPaymentRequired, `{"error":{"message":"payment required"}}`, poo.ProviderKindPlanInactive},
		{"a 401", http.StatusUnauthorized, `{"error":{"message":"no key"}}`, poo.ProviderKindAuthFailed},
		{"a 400 whose payload names the rate limit", http.StatusBadRequest, `{"error":{"code":"rate_limit_exceeded","message":"limit"}}`, poo.ProviderKindRateLimited},
		{"a 400 whose payload names the context length", http.StatusBadRequest, `{"error":{"code":"context_length_exceeded","message":"too long"}}`, poo.ProviderKindContextLength},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))
			_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			pe, ok := err.(*poo.ProviderError)
			if !ok {
				t.Fatalf("the error is %T, want a provider error", err)
			}
			if pe.Kind != c.want {
				t.Errorf("the kind is %q, want %q", pe.Kind, c.want)
			}
		})
	}
}
