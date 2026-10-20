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

	tr := openai.New(openai.Config{BaseURL: url, Model: "m", Timeout: 3 * time.Second})
	_, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
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
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	_, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
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
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	_, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindContextLength {
		t.Errorf("the error kind is %q, want context_length", pe.Kind)
	}
}
