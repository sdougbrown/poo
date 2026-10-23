package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The retry policy.
//
// The plan: three attempts per request, the first try and two retries,
// with a backoff of one second and then two. A Retry-After overrides
// the backoff, capped at thirty seconds. Only a failure that arrives
// before the attempt has shown the model anything is retried, and the
// wait between the attempts is cancellable.

// countingServer answers with a body and counts the requests it got.
type countingServer struct {
	*httptest.Server
	mu sync.Mutex
	// requests counts the requests the server received.
	requests int
}

// newCountingServer starts a server that answers with a status and a
// body.
func newCountingServer(t *testing.T, status int, body string) *countingServer {
	t.Helper()
	s := &countingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

// Requests returns how many requests the server received.
func (s *countingServer) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// TestARetryBeforeTheFirstDeltaSucceeds covers the retry: a request
// that fails before the endpoint answered is tried again, and the
// second attempt's answer reaches the caller.
func TestARetryBeforeTheFirstDeltaSucceeds(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = io.Copy(io.Discard, r.Body)
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"the answer"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	if got := textOf(read(t, reader)); got != "the answer" {
		t.Errorf("the text is %q, want the answer", got)
	}
	if requests != 2 {
		t.Errorf("the endpoint got %d requests, want 2", requests)
	}
}

// TestTheBudgetIsThreeAttempts covers the budget: a request that keeps
// failing gets three attempts, and the third failure is the one the
// caller sees.
func TestTheBudgetIsThreeAttempts(t *testing.T) {
	srv := newCountingServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want a failure")
	}
	var pe *poo.ProviderError
	if !asProviderError(err, &pe) {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindServer {
		t.Errorf("the failure's kind is %q, want server", pe.Kind)
	}
	if got := srv.Requests(); got != 3 {
		t.Errorf("the endpoint got %d requests, want 3", got)
	}
}

// TestTheBudgetExhaustionSettlesTheRun covers the same budget from the
// run's side: the run settles as a provider error.
func TestTheBudgetExhaustionSettlesTheRun(t *testing.T) {
	srv := newCountingServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))
	res := settleOf(t, tr)
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if got := srv.Requests(); got != 3 {
		t.Errorf("the endpoint got %d requests, want 3", got)
	}
}

// TestAnErrorAfterADeltaIsNotRetried covers the rule: a stream that
// breaks after it has shown the model something is not tried again.
func TestAnErrorAfterADeltaIsNotRetried(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"half an answer"}}]}`) +
		chunk(`{"error":{"message":"the model crashed","type":"server_error"}}`)
	srv := newSSEServer(t, body)

	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var last error
	for {
		_, err := reader.Next()
		if err != nil {
			last = err
			break
		}
	}
	if last == nil {
		t.Fatal("the stream did not fail")
	}
	if len(srv.requests) != 1 {
		t.Errorf("the endpoint got %d requests, want 1: the stream had already started", len(srv.requests))
	}
}

// TestTheCallsOfABrokenStreamAreNotRun covers the plan's rule: a stream
// that breaks after the model asked for a tool call does not run the
// call, and the run settles as a provider error.
func TestTheCallsOfABrokenStreamAreNotRun(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"let me try"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]}}]}`) +
		chunk(`{"error":{"message":"the model crashed","type":"server_error"}}`)
	srv := newSSEServer(t, body)

	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))
	tool := &echoTool{}

	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": tool},
		ToolOrder:    []string{"echo"},
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
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if calls := tool.Calls(); len(calls) != 0 {
		t.Errorf("the tool ran %d times, want none: the stream broke", len(calls))
	}
	if len(srv.requests) != 1 {
		t.Errorf("the endpoint got %d requests, want 1: the stream had already started", len(srv.requests))
	}
}

// TestACancellationDuringTheBackoffEndsTheRun covers the cancellable
// wait: a run that is cancelled while the transport waits for the next
// attempt settles as cancelled.
func TestACancellationDuringTheBackoffEndsTheRun(t *testing.T) {
	srv := newCountingServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Second, 2 * time.Second},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

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

	if _, err := s.Prompt(ctx, "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if got := srv.Requests(); got != 1 {
		t.Errorf("the endpoint got %d requests, want 1: the wait was cancelled", got)
	}
}

// TestTheRetryAfterOverridesTheBackoff covers the rule: the wait the
// endpoint asked for is the wait the transport makes.
func TestTheRetryAfterOverridesTheBackoff(t *testing.T) {
	var requests int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"the answer"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	var delays []time.Duration
	var retried []bool
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
		OnAttempt: func(a openai.Attempt) {
			mu.Lock()
			defer mu.Unlock()
			delays = append(delays, a.Delay)
			retried = append(retried, a.Retried)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, _, err := tr.Stream(ctx, poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	if got := textOf(read(t, reader)); got != "the answer" {
		t.Errorf("the answer is %q, want the retry's answer", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(delays) == 0 {
		t.Fatal("no attempt was reported")
	}
	if delays[0] != time.Second {
		t.Errorf("the wait was %v, want the endpoint's one second", delays[0])
	}
	if !retried[0] {
		t.Error("the attempt does not say the retry followed it")
	}
}

// TestTheAttemptsAreReported covers the report: every attempt that
// failed is reported, with the wait that followed it.
func TestTheAttemptsAreReported(t *testing.T) {
	srv := newCountingServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	var mu sync.Mutex
	var attempts []openai.Attempt
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
		OnAttempt: func(a openai.Attempt) {
			mu.Lock()
			defer mu.Unlock()
			attempts = append(attempts, a)
		},
	})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want a failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 3 {
		t.Fatalf("the transport reported %d attempts, want 3: every attempt that failed is reported", len(attempts))
	}
	for i, a := range attempts {
		if a.Number != i+1 {
			t.Errorf("the attempt is %d, want %d", a.Number, i+1)
		}
		if a.Err == nil {
			t.Errorf("attempt %d has no failure", a.Number)
		}
	}
	// The last attempt is the one that ended the request, and no other
	// attempt followed it.
	if !attempts[0].Retried || !attempts[1].Retried {
		t.Errorf("the first two attempts were not retried: %+v", attempts)
	}
	if attempts[2].Retried {
		t.Errorf("the last attempt says it was retried: %+v", attempts[2])
	}
	if attempts[2].Delay != 0 {
		t.Errorf("the last attempt waited %v, want no wait", attempts[2].Delay)
	}
}

// TestTheRequestHearsTheAttempts covers the request's own reporter: it
// hears every attempt that failed, so the core can write the attempts
// to the session's record.
func TestTheRequestHearsTheAttempts(t *testing.T) {
	srv := newCountingServer(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	var mu sync.Mutex
	var attempts []poo.Attempt
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
	})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		OnAttempt: func(a poo.Attempt) {
			mu.Lock()
			defer mu.Unlock()
			attempts = append(attempts, a)
		},
	})
	if err == nil {
		t.Fatal("the request succeeded, want a failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 3 {
		t.Fatalf("the request heard %d attempts, want 3", len(attempts))
	}
	for i, a := range attempts {
		if a.Number != i+1 {
			t.Errorf("the attempt is %d, want %d", a.Number, i+1)
		}
		if a.Err == nil {
			t.Errorf("the attempt %d has no failure", a.Number)
		}
	}
}

// asProviderError reports whether the error is a provider error, and
// puts it in the target.
func asProviderError(err error, target **poo.ProviderError) bool {
	pe, ok := err.(*poo.ProviderError)
	if ok {
		*target = pe
	}
	return ok
}

// echoTool is a tool that records what it was asked to do.
type echoTool struct {
	mu sync.Mutex
	// calls holds the arguments the tool received.
	calls []string
}

// Calls returns the arguments the tool received, in order.
func (t *echoTool) Calls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts anything.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{text: string(raw)}, nil
}

// Call records the call and returns the result.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p := in.(echoPrepared)
	t.mu.Lock()
	t.calls = append(t.calls, p.text)
	t.mu.Unlock()
	return poo.ToolResult{Text: p.text}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct {
	text string
}

// Summary describes the call.
func (p echoPrepared) Summary() string { return p.text }
