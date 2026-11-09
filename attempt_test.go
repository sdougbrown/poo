package poo_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
)

// The attempts of a request.
//
// Each attempt is written to the record with its error, for
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
func attemptRecords(sink *recordingSink) []*poo.ProviderAttemptRecord {
	var out []*poo.ProviderAttemptRecord
	for _, rec := range sink.allRecords() {
		if a, ok := rec.(*poo.ProviderAttemptRecord); ok {
			out = append(out, a)
		}
	}
	return out
}

// TestTheFailedAttemptsAreWrittenToTheRecord covers the audit
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

// TestTheRetryIsAnnounced covers the event: a request that is
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
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
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
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
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

// brokenAttemptThenAnswer answers the first attempt with the usage the
// attempt spent and then an endpoint failure, and the second one with an
// answer, so a request is retried once and then succeeds.
func brokenAttemptThenAnswer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	attempts := 0
	// The recorded stream: the usage chunk of 1082 prompt and 18
	// completion tokens, then the failure that ends the attempt.
	broken := "data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":18,\"prompt_tokens\":1082,\"total_tokens\":1100}}\n\n" +
		"data: {\"error\":{\"message\":\"the endpoint failed\",\"type\":\"server_error\",\"code\":\"internal_error\"}}\n\n" +
		"data: [DONE]\n\n"
	answer := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":5,\"prompt_tokens\":100,\"total_tokens\":105}}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, broken)
			return
		}
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestARetriedAttemptsUsageIsCounted covers the accounting of a
// request that is retried: the attempt that failed spent 1082 prompt and
// 18 completion tokens, and the request that answered spent 100 and 5.
//
// The run's usage record holds both attempts' numbers, so the request
// that broke is not counted as free.
func TestARetriedAttemptsUsageIsCounted(t *testing.T) {
	srv := brokenAttemptThenAnswer(t)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn: the retry answered", res.Reason)
	}
	var input, output int
	var records int
	for _, rec := range sink.allRecords() {
		usage, ok := rec.(*poo.UsageRecord)
		if !ok {
			continue
		}
		records++
		input += usage.InputTokens
		output += usage.OutputTokens
	}
	if records != 1 {
		t.Fatalf("the run wrote %d usage records, want 1", records)
	}
	if input != 1182 || output != 23 {
		t.Errorf("the run counted %d input and %d output tokens, want 1182 and 23: every attempt spent tokens",
			input, output)
	}
}

// TestTheMessageDeltaNamesTheAttempt covers the streaming event after a
// retry: the delta names the attempt that produced it, not the request.
func TestTheMessageDeltaNamesTheAttempt(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"the answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))
	runSession(t, s, "hi")

	deltas := 0
	for _, ev := range s.Events() {
		if ev.Name != poo.EventMessageDelta {
			continue
		}
		deltas++
		if ev.Payload["attempt"] != 2 {
			t.Errorf("the delta names the attempt %v, want the second one", ev.Payload["attempt"])
		}
	}
	if deltas == 0 {
		t.Fatal("the run emitted no delta")
	}
}

// lateAttemptTransport reports an attempt after the run has settled.
type lateAttemptTransport struct {
	// reported closes once the late attempt has been reported.
	reported chan struct{}
}

// Stream serves an answer and reports an attempt after the run ends.
func (t *lateAttemptTransport) Stream(_ context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		if req.OnAttempt != nil {
			req.OnAttempt(poo.Attempt{
				Number: 1,
				Err:    &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "too late"},
			})
		}
		close(t.reported)
	}()
	return &fakeReader{events: []poo.StreamItem{
		{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "done"}},
		{Kind: "finish", FinishReason: poo.FinishStop},
	}}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not used by the test.
func (t *lateAttemptTransport) Fork(context.Context, poo.Rendered, []poo.Message, int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// TestAnAttemptAfterTheSealIsDropped covers the seal for the attempt
// record: a report that lands after run.settled writes nothing.
func TestAnAttemptAfterTheSealIsDropped(t *testing.T) {
	tr := &lateAttemptTransport{reported: make(chan struct{})}
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	runSession(t, s, "hi")
	<-tr.reported

	settled := -1
	late := -1
	for i, rec := range sink.allRecords() {
		switch rec.Meta().Record {
		case "run.settled":
			settled = i
		case "provider.attempt":
			late = i
		}
	}
	if settled < 0 {
		t.Fatal("the run wrote no settle record")
	}
	if late > settled {
		t.Error("a provider.attempt record followed run.settled")
	}
}

// TestAnInterruptDuringTheRetryWait covers the cancellable
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

	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
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
		if len(attemptRecords(sink)) > 0 {
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
		t.Errorf("the run recorded no interrupted turn; the records are %v", sink.Names())
	}
	t.Logf("the run settled %q after %d requests", res.Reason, requests.Load())
}
