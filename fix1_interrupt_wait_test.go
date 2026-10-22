package poo_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestAnInterruptDuringTheRetryWait covers the plan's cancellable
// wait from the run's side: an interrupt that arrives while the
// transport waits for the next attempt aborts the wait, and the run goes
// on with the interrupt delivered rather than settling as a provider
// failure.
func TestAnInterruptDuringTheRetryWait(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.MaxTurns = 1
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "hi"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// Wait until the first attempt has been reported, which means the
	// transport is in its backoff.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(fix1AttemptRecords(sink)) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the transport never reported an attempt")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled cancelled: the interrupt is a turn boundary, not a cancel")
	}
	var interrupted bool
	for _, rec := range sink.allRecords() {
		if rec.Meta().Record == "turn.interrupted" {
			interrupted = true
		}
	}
	if !interrupted {
		t.Errorf("the run recorded no interrupted turn; the records are %v", sink.names())
	}
	t.Logf("the run settled %q after %d requests", res.Reason, requests.Load())
}

// fix1AttemptRecords returns the attempt records the sink kept.
func fix1AttemptRecords(sink *recordingSink) []*poo.ProviderAttempt {
	var out []*poo.ProviderAttempt
	for _, rec := range sink.allRecords() {
		if a, ok := rec.(*poo.ProviderAttempt); ok {
			out = append(out, a)
		}
	}
	return out
}
