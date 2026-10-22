package poo_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
)

// The attempts of a request.
//
// The plan: each attempt is written to the record with its error, for
// audit, and the retry is announced.

// retryingTransport is a transport whose first attempts fail.
//
// The transport reports every attempt to the request's own reporter,
// the way a transport that retries does, and then serves the scripted
// response.
type retryingTransport struct {
	*pootest.Transport
	// failures is how many attempts fail before the request is served.
	failures int
	// attempts counts the attempts the transport made.
	attempts int
}

// Stream serves the request, after the scripted failures.
//
// The transport owns the retry loop, so the failures are tried again
// here and the request only fails when the budget is spent.
func (t *retryingTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	for {
		t.attempts++
		if t.attempts > t.failures {
			return t.Transport.Stream(ctx, req)
		}
		err := &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "the endpoint is unwell"}
		if req.OnAttempt != nil {
			req.OnAttempt(poo.Attempt{Number: t.attempts, Err: err, Delay: time.Second, Retried: true})
		}
	}
}

// attemptRecords returns the attempt records the sink kept.
func attemptRecords(sink *recordingSink) []*poo.ProviderAttempt {
	var out []*poo.ProviderAttempt
	for _, rec := range sink.allRecords() {
		if a, ok := rec.(*poo.ProviderAttempt); ok {
			out = append(out, a)
		}
	}
	return out
}

// TestTheFailedAttemptsAreWrittenToTheRecord covers the plan's audit
// rule: every attempt that failed is written to the record, with its
// error.
func TestTheFailedAttemptsAreWrittenToTheRecord(t *testing.T) {
	sink := &recordingSink{}
	tr := &retryingTransport{
		Transport: pootest.NewTransport(pootest.Turn{Text: "hello"}),
		failures:  2,
	}
	opts := testOptions(t, tr, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hi")

	records := attemptRecords(sink)
	if len(records) != 2 {
		t.Fatalf("the sink kept %d attempt records, want 2", len(records))
	}
	for i, rec := range records {
		if rec.Attempt != i+1 {
			t.Errorf("the attempt is %d, want %d", rec.Attempt, i+1)
		}
		if rec.Error == "" {
			t.Errorf("the attempt %d has no error", rec.Attempt)
		}
		if rec.Kind != string(poo.ProviderKindServer) {
			t.Errorf("the attempt %d is a %q, want %q", rec.Attempt, rec.Kind, poo.ProviderKindServer)
		}
		if rec.DelayMS != 1000 {
			t.Errorf("the attempt %d waited %dms, want 1000", rec.Attempt, rec.DelayMS)
		}
		if rec.RunID == "" {
			t.Errorf("the attempt %d names no run", rec.Attempt)
		}
		if rec.Turn != 1 {
			t.Errorf("the attempt %d is in turn %d, want 1", rec.Attempt, rec.Turn)
		}
	}
}

// TestTheRetryIsAnnounced covers the plan's event: a request that is
// tried again announces the retry.
func TestTheRetryIsAnnounced(t *testing.T) {
	sink := &recordingSink{}
	tr := &retryingTransport{
		Transport: pootest.NewTransport(pootest.Turn{Text: "hello"}),
		failures:  1,
	}
	opts := testOptions(t, tr, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hi")

	var retries int
	for _, ev := range s.Events() {
		if ev.Name != poo.EventProviderRetry {
			continue
		}
		retries++
		if ev.Payload["attempt"] != 1 {
			t.Errorf("the event names the attempt %v, want 1", ev.Payload["attempt"])
		}
		if ev.Payload["error"] == "" {
			t.Errorf("the event names no error: %v", ev.Payload)
		}
		if ev.Payload["delay_ms"] != 1000 {
			t.Errorf("the event waits %vms, want 1000", ev.Payload["delay_ms"])
		}
	}
	if retries != 1 {
		t.Errorf("the session announced %d retries, want 1", retries)
	}
}

// TestARequestThatDoesNotFailWritesNoAttempt covers the quiet case: a
// request that answers the first time leaves no attempt behind.
func TestARequestThatDoesNotFailWritesNoAttempt(t *testing.T) {
	sink := &recordingSink{}
	tr := pootest.NewTransport(pootest.Turn{Text: "hello"})
	opts := testOptions(t, tr, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hi")

	if records := attemptRecords(sink); len(records) != 0 {
		t.Errorf("the sink kept %d attempt records, want none", len(records))
	}
}

// failingEndpoint answers with a failure, until the last answer is a
// stream.
func failingEndpoint(t *testing.T, failures int) *httptest.Server {

	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheRealTransportReportsItsAttempts covers the whole path: the
// transport that retries reports every attempt, and the core writes them
// to the record.
func TestTheRealTransportReportsItsAttempts(t *testing.T) {
	srv := failingEndpoint(t, 2)
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	runSession(t, s, "hi")

	records := attemptRecords(sink)
	if len(records) != 2 {
		t.Fatalf("the sink kept %d attempt records, want 2", len(records))
	}
	for i, rec := range records {
		if rec.Attempt != i+1 {
			t.Errorf("the attempt is %d, want %d", rec.Attempt, i+1)
		}
		if rec.Kind != string(poo.ProviderKindServer) {
			t.Errorf("the attempt %d is a %q, want %q", rec.Attempt, rec.Kind, poo.ProviderKindServer)
		}
	}
}

// TestTheFailedRequestKeepsEveryAttempt covers the request that never
// answers: every attempt it made is in the record, the last one
// included.
func TestTheFailedRequestKeepsEveryAttempt(t *testing.T) {
	srv := failingEndpoint(t, 10)
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")

	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want %q", res.Reason, poo.ReasonProviderError)
	}
	records := attemptRecords(sink)
	if len(records) != 3 {
		t.Fatalf("the sink kept %d attempt records, want 3", len(records))
	}
	if records[2].Kind != string(poo.ProviderKindServer) {
		t.Errorf("the last attempt is a %q, want %q", records[2].Kind, poo.ProviderKindServer)
	}
}
