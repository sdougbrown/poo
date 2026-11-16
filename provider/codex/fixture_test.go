package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/codex"
)

// The test fixtures: an endpoint that answers with recorded streams and
// keeps what the requests carried, and the small readers the tests share.

// frame renders one SSE event.
func frame(payload string) string {
	return "data: " + payload + "\n\n"
}

// done is the SSE terminator some servers send after the last event.
const done = "data: [DONE]\n\n"

// captured is one request the endpoint received.
type captured struct {
	// path is the request's path.
	path string
	// header is the request's headers.
	header http.Header
	// body is the request's payload, as it went out.
	body []byte
	// fields are the payload's top-level fields.
	fields map[string]json.RawMessage
}

// streamServer answers requests with recorded streams and keeps what
// each request carried.
type streamServer struct {
	*httptest.Server
	mu sync.Mutex
	// streams are the answers, in order. The last one answers every
	// request after it.
	streams []string
	// requests are the requests the endpoint received, in order.
	requests []captured
	// failures are the answers that are a status instead of a stream, by
	// the request's index.
	failures map[int]int
}

// newServer starts an endpoint that serves the streams.
func newServer(t *testing.T, streams ...string) *streamServer {
	t.Helper()
	s := &streamServer{streams: streams}
	s.failures = map[int]int{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(body, &fields)
		s.mu.Lock()
		index := len(s.requests)
		s.requests = append(s.requests, captured{
			path:   r.URL.Path,
			header: r.Header.Clone(),
			body:   body,
			fields: fields,
		})
		stream := s.streams[len(s.streams)-1]
		if index < len(s.streams) {
			stream = s.streams[index]
		}
		status, fails := s.failures[index]
		s.mu.Unlock()

		if fails {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"the endpoint is busy","type":"server_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, stream)
	}))
	t.Cleanup(s.Close)
	return s
}

// failRequest makes the request at an index answer with a status instead of a
// stream, so a test can drive a failure the retry policy repeats.
func (s *streamServer) failRequest(index, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[index] = status
}

// request returns the request the endpoint received at an index.
func (s *streamServer) request(t *testing.T, index int) captured {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.requests) {
		t.Fatalf("the endpoint received %d requests, wanted number %d", len(s.requests), index+1)
	}
	return s.requests[index]
}

// count returns how many requests the endpoint received.
func (s *streamServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// field decodes one top-level field of a captured request.
func field[T any](t *testing.T, req captured, name string) T {
	t.Helper()
	var out T
	raw, ok := req.fields[name]
	if !ok {
		t.Fatalf("the request carries no %q field: %s", name, req.body)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the request's %q field does not decode: %v", name, err)
	}
	return out
}

// credential is a resolver that hands out a fixed credential.
func credential(token, account string) func(context.Context) (codex.Credential, error) {
	return func(context.Context) (codex.Credential, error) {
		return codex.Credential{Token: token, AccountID: account}, nil
	}
}

// newTransport returns a transport that talks to the endpoint.
func newTransport(endpoint string, opts ...func(*codex.Options)) *codex.Transport {
	cfg := codex.Options{
		Endpoint:    endpoint,
		Model:       "gpt-6.1-sol",
		Credentials: credential("test-token", "acct-1"),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return codex.New(cfg)
}

// read drains a stream.
func read(t *testing.T, reader poo.StreamReader) []poo.StreamItem {
	t.Helper()
	var events []poo.StreamItem
	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		events = append(events, ev)
	}
}

// textOf joins the text deltas of a stream.
func textOf(events []poo.StreamItem) string {
	out := ""
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "text" {
			out += ev.Delta.Text
		}
	}
	return out
}

// thinkingOf joins the thinking deltas of a stream.
func thinkingOf(events []poo.StreamItem) string {
	out := ""
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "thinking" {
			out += ev.Delta.Text
		}
	}
	return out
}

// callsOf returns the tool calls a stream reported.
func callsOf(events []poo.StreamItem) []poo.ToolCallDelta {
	var out []poo.ToolCallDelta
	for _, ev := range events {
		if ev.Kind == "tool_call" && ev.ToolCall != nil {
			out = append(out, *ev.ToolCall)
		}
	}
	return out
}

// finishOf returns the finish reason a stream reported.
func finishOf(t *testing.T, events []poo.StreamItem) poo.FinishReason {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == "finish" {
			return ev.FinishReason
		}
	}
	t.Fatalf("the stream reported no finish reason: %v", events)
	return ""
}

// usageOf returns the usage a stream reported.
func usageOf(t *testing.T, events []poo.StreamItem) *poo.UsageReport {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == "usage" {
			return ev.Usage
		}
	}
	t.Fatalf("the stream reported no usage: %v", events)
	return nil
}
