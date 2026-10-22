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

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// TestAForksTimeToTheFirstTokenIncludesTheWaitForHeaders covers a server
// that holds the response headers back: a fork is timed from the moment
// the request goes out, as a main request is.
func TestAForksTimeToTheFirstTokenIncludesTheWaitForHeaders(t *testing.T) {
	var delay time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}

	delay = 100 * time.Millisecond
	reader, err := tr.Fork(context.Background(), rendered, nil, 16)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()
	for _, ev := range read(t, reader) {
		if ev.Kind != "usage" || ev.Usage == nil {
			continue
		}
		if ev.Usage.TTFTMS < 70 {
			t.Errorf("the fork's time to the first token is %v ms, want at least the header delay", ev.Usage.TTFTMS)
		}
		return
	}
	t.Fatal("the fork held no usage report")
}

// brokenBodyServer answers 200 with a JSON body that stops short of its
// declared length and drops the connection.
func brokenBodyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijacking: %v", err)
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"error\":{\"message\":\"par")
		_ = buf.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestABodyThatBreaksMidReadIsANetworkFailure covers a 200 JSON body that
// the connection cuts short.
func TestABodyThatBreaksMidReadIsANetworkFailure(t *testing.T) {
	srv := brokenBodyServer(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	check := func(t *testing.T, err error) {
		t.Helper()
		var pe *poo.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("the error is %v, want a provider error", err)
		}
		if pe.Kind != poo.ProviderKindNetwork {
			t.Errorf("the kind is %q, want network", pe.Kind)
		}
	}

	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	check(t, err)

	_, err = tr.Fork(context.Background(), rendered, nil, 16)
	check(t, err)
}

// slowTwoWriteErrorServer answers with a JSON error that arrives in two writes,
// with a pause between them, under the given status.
func slowTwoWriteErrorServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"slow `)
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, `body","code":"x"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestASlowErrorBodyIsReadUnderTheTimeout covers an error body that
// arrives late while Config.Timeout is set: the timeout must not be
// released before the body is read.
func TestASlowErrorBodyIsReadUnderTheTimeout(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		srv := slowTwoWriteErrorServer(t, status)
		tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second})
		_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
		var pe *poo.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("status %d: the error is %v, want a provider error", status, err)
		}
		if pe.Kind != poo.ProviderKindInvalidRequest || pe.Message != "slow body" || pe.Status != status {
			t.Errorf("status %d: got kind %q, message %q, status %d; want invalid_request, %q, %d",
				status, pe.Kind, pe.Message, pe.Status, "slow body", status)
		}
	}
}
