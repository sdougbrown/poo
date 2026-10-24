package openai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestAStoppedWaitDoesNotClaimARetry covers the attempt record: an
// attempt is reported as retried only once the wait is over and the
// next attempt starts. A cancellation during the wait leaves an attempt
// that says no retry followed it.
func TestAStoppedWaitDoesNotClaimARetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Second},
	})
	var mu sync.Mutex
	var attempts []poo.Attempt
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, _, err := tr.Stream(ctx, poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		OnAttempt: func(a poo.Attempt) {
			mu.Lock()
			attempts = append(attempts, a)
			mu.Unlock()
		},
	})
	if err == nil {
		t.Fatal("the request succeeded, want the wait to end it")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 1 {
		t.Fatalf("the request reported %d attempts, want 1", len(attempts))
	}
	if attempts[0].Retried {
		t.Error("the attempt claims a retry that the cancellation stopped")
	}
	if attempts[0].Delay != 0 {
		t.Errorf("the attempt waited %v, want no wait before a retry", attempts[0].Delay)
	}
}

// TestATransportTimeoutIsANetworkFailure covers the kind table: a
// timeout the transport imposed is a network failure, not the run's own
// cancellation.
func TestATransportTimeoutIsANetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{
		BaseURL: srv.URL,
		Model:   "m",
		Timeout: 30 * time.Millisecond,
	})
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want the timeout to end it")
	}
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the failure is %T (%v), want a provider error", err, err)
	}
	if perr.Kind != poo.ProviderKindNetwork {
		t.Errorf("the failure's kind is %q, want network", perr.Kind)
	}
}
