package openai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestAnEventSplitOverTwoDataLinesIsRead covers a server that writes one
// event's payload over several `data:` lines, which the SSE grammar
// allows.
func TestAnEventSplitOverTwoDataLinesIsRead(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\n" +
		"data: \"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	if got := textOf(read(t, reader)); got != "hi" {
		t.Errorf("the text is %q, want hi", got)
	}
}

// TestTheTimeToTheFirstTokenIgnoresARoleOnlyChunk covers a server that
// announces the assistant's role before it sends any content.
func TestTheTimeToTheFirstTokenIgnoresARoleOnlyChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`))
		fl.Flush()
		time.Sleep(120 * time.Millisecond)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
		fl.Flush()
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	for _, ev := range read(t, reader) {
		if ev.Kind != "usage" || ev.Usage == nil {
			continue
		}
		// The role chunk carries no token, so the clock starts when the
		// content arrives.
		if ev.Usage.TTFTMS < 80 {
			t.Errorf("the time to the first token is %v ms, want the content's delay", ev.Usage.TTFTMS)
		}
		return
	}
	t.Fatal("the stream held no usage report")
}

// TestTheTimeoutBoundsTheWholeRequest covers a config timeout on a stream
// that never finishes.
func TestTheTimeoutBoundsTheWholeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"first"}}]}`))
		fl.Flush()
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, done)
		fl.Flush()
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m", Timeout: 60 * time.Millisecond})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	start := time.Now()
	var last error
	for {
		_, err := reader.Next()
		if err != nil {
			last = err
			break
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("the timeout was not enforced")
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the stream ran for %v, want the 60ms timeout to end it", elapsed)
	}
	var perr *poo.ProviderError
	if !errors.As(last, &perr) {
		t.Fatalf("the stream ended with %T (%v), want a provider error", last, last)
	}
	if perr.Kind != poo.ProviderKindInterruptedStream {
		t.Errorf("the failure's kind is %q, want interrupted_stream", perr.Kind)
	}
	if perr.Code != string(poo.ProviderKindNetwork) {
		t.Errorf("the failure's code is %q, want the original network kind", perr.Code)
	}
}

// TestACancelledStreamEndsWithTheContextsError covers a cancellation that
// arrives while the stream is open.
func TestACancelledStreamEndsWithTheContextsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"first"}}]}`))
		fl.Flush()
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, done)
		fl.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
	reader, _, err := tr.Stream(ctx, poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	if _, err := reader.Next(); err != nil {
		t.Fatalf("reading the first event: %v", err)
	}
	cancel()

	var last error
	for {
		_, err := reader.Next()
		if err != nil {
			last = err
			break
		}
	}
	if !errors.Is(last, context.Canceled) {
		t.Errorf("the stream ended with %v, want the cancellation", last)
	}
}

// TestAStreamThatFailsAfterItsDeltasStillReportsItsUsage covers a server
// that dies mid-stream.
func TestAStreamThatFailsAfterItsDeltasStillReportsItsUsage(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`) +
		chunk(`{"error":{"message":"the model crashed","type":"server_error"}}`)

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	usage := 0
	var last error
	for {
		ev, err := reader.Next()
		if err != nil {
			last = err
			break
		}
		if ev.Kind == "usage" {
			usage++
		}
	}
	if last == nil {
		t.Fatal("the stream did not fail")
	}
	if usage != 1 {
		t.Errorf("the stream held %d usage reports, want 1", usage)
	}
}

// TestTheEventAfterTheUsageIsTheEndOfTheStream covers the reader's
// contract: it returns events, then io.EOF, and nothing in between.
func TestTheEventAfterTheUsageIsTheEndOfTheStream(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) +
		chunk(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var kinds []string
	for {
		ev, err := reader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("the stream failed: %v", err)
			}
			break
		}
		if ev.Kind == "" {
			t.Fatalf("the stream returned an event with no kind: %+v", ev)
		}
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) == 0 {
		t.Fatal("the stream held no events")
	}
	if kinds[len(kinds)-1] != "usage" {
		t.Errorf("the last event is %q, want usage", kinds[len(kinds)-1])
	}
}

// TestAContextLengthErrorInAStreamChunkIsMapped covers a server that
// reports a context-length failure inside the stream.
func TestAContextLengthErrorInAStreamChunkIsMapped(t *testing.T) {
	body := chunk(`{"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error","code":"context_length_exceeded"}}`)

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	for {
		if _, err := reader.Next(); err != nil {
			pe, ok := err.(*poo.ProviderError)
			if !ok {
				t.Fatalf("the error is %T, want a provider error", err)
			}
			if pe.Kind != poo.ProviderKindContextLength {
				t.Errorf("the error kind is %q, want context_length", pe.Kind)
			}
			return
		}
	}
}

// TestAContextLengthErrorInAServerResponseIsMapped covers a server that
// reports a context-length failure with a 500.
func TestAContextLengthErrorInAServerResponseIsMapped(t *testing.T) {
	srv := newSSEServer(t, `{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`)
	srv.status = http.StatusInternalServerError
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

// TestAnErrorChunkWithoutAMessageIsStillAnError covers an endpoint that
// reports a failure with no message.
func TestAnErrorChunkWithoutAMessageIsStillAnError(t *testing.T) {
	for _, errBody := range []string{`{"error":{}}`, `{"error":{"code":1}}`} {
		t.Run(errBody, func(t *testing.T) {
			srv := newSSEServer(t, chunk(errBody))
			tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))
			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()

			var last error
			for i := 0; i < 10 && last == nil; i++ {
				if _, err := reader.Next(); err != nil {
					last = err
				}
			}
			var pe *poo.ProviderError
			if !errors.As(last, &pe) {
				t.Fatalf("the stream ended with %v, want a provider error", last)
			}
		})
	}
}

// slowErrorServer answers with a status and a JSON body in two parts,
// with a pause between them.
func slowErrorServer(t *testing.T, status int, first, rest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(first))
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(rest))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAnErrorBodyIsReadBeforeTheTimeoutIsReleased covers a request with
// a timeout whose error body arrives in two parts: the body is read in
// full, so the error keeps the endpoint's message and its kind.
func TestAnErrorBodyIsReadBeforeTheTimeoutIsReleased(t *testing.T) {
	const first = `{"error":{"message":"This model's maximum context `
	const rest = `length is 8192 tokens","code":"context_length_exceeded"}}`
	for name, status := range map[string]int{"a non-200 status": http.StatusBadRequest, "a 200 with a JSON error": http.StatusOK} {
		t.Run(name, func(t *testing.T) {
			srv := slowErrorServer(t, status, first, rest)
			tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
			_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			var pe *poo.ProviderError
			if !errors.As(err, &pe) {
				t.Fatalf("the error is %v, want a provider error", err)
			}
			if pe.Kind != poo.ProviderKindContextLength {
				t.Errorf("the kind is %q (%s), want the context-length kind", pe.Kind, pe.Message)
			}
		})
	}
}

// TestAnEventLineOver64KiBIsRead covers a single SSE data line larger
// than bufio's default token limit.
func TestAnEventLineOver64KiBIsRead(t *testing.T) {
	big := strings.Repeat("x", 100*1024)
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"`+big+`"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	got := 0
	for {
		ev, err := reader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("the stream failed: %v", err)
			}
			break
		}
		if ev.Kind == "delta" && ev.Delta != nil {
			got += len(ev.Delta.Text)
		}
	}
	if got != len(big) {
		t.Errorf("the stream delivered %d characters, want %d", got, len(big))
	}
}

// TestAFinalMessageChunkIsRead covers the chunk shape some servers send:
// a full message instead of deltas. Its content must reach the stream.
func TestAFinalMessageChunkIsRead(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"message":{"role":"assistant","content":"the answer"}}]}`))
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	if got := textOf(read(t, reader)); got != "the answer" {
		t.Errorf("the text is %q, want the message's content", got)
	}
}

// TestAStreamThatBreaksAfterItsDeltaKeepsItsKind covers the
// reclassification: a stream that breaks after a delta cannot be
// retried, so the failure says so, and the kind it had before stays in
// the code, where the plan keeps it.
func TestAStreamThatBreaksAfterItsDeltaKeepsItsKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`))
		fl.Flush()
		// The connection closes before the stream ends.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
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
	pe, ok := last.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the failure is %T, want a provider error", last)
	}
	if pe.Kind != poo.ProviderKindInterruptedStream {
		t.Errorf("the failure's kind is %q, want interrupted_stream", pe.Kind)
	}
	if pe.Code != string(poo.ProviderKindNetwork) {
		t.Errorf("the failure's code is %q, want the original kind network", pe.Code)
	}
}

// TestAStreamThatBreaksBeforeItsDeltaIsNetwork covers the other branch:
// a failure before any delta is still a network failure, which the
// retryable rule can retry, not a terminal interrupted stream.
func TestAStreamThatBreaksBeforeItsDeltaIsNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// The response starts, then a line arrives that the reader
		// cannot hold: the body fails before any data line.
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		_, _ = io.WriteString(w, strings.Repeat("x", 17*1024*1024))
	}))
	defer srv.Close()

	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})
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
	pe, ok := last.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the failure is %T, want a provider error", last)
	}
	if pe.Kind != poo.ProviderKindNetwork {
		t.Errorf("the failure's kind is %q, want network", pe.Kind)
	}
}
