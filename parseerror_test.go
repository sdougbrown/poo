package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The visible parse error.
//
// The plan: a tool call the model wrote as text and that could not be
// parsed is not dropped. It becomes a visible recoverable error, and
// the model is told.

// malformedEndpoint answers with a tool call that does not parse, and
// then with an answer.
//
// The endpoint keeps the requests it received, so a test can read what
// the model was told.
func malformedEndpoint(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	requests := 0
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		mu.Lock()
		requests++
		n := requests
		bodies = append(bodies, decoded)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			// The model wrote a call as text, and the call does not
			// parse.
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\""+
				"<tool_call>{\\\"name\\\": \\\"bash\\\", \\\"arguments\\\": omg</tool_call>\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

// failingTransport serves a response that reports a failure about
// itself.
type failingTransport struct {
	// err is the failure the response reports.
	err *poo.StreamError
}

// Stream serves the response.
func (t *failingTransport) Stream(context.Context, poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return &failingReader{err: t.err}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not used by the test.
func (t *failingTransport) Fork(context.Context, poo.Rendered, []poo.Message, int) (poo.StreamReader, error) {
	return nil, errors.New("the transport does not fork")
}

// failingReader is a stream that reports one failure and ends.
type failingReader struct {
	// err is the failure the stream reports.
	err *poo.StreamError
	// reported tells the failure from the end of the stream.
	reported bool
}

// Next reports the failure, and then ends.
func (r *failingReader) Next() (poo.StreamEvent, error) {
	if r.reported {
		return poo.StreamEvent{}, io.EOF
	}
	r.reported = true
	return poo.StreamEvent{Kind: "error", Error: r.err}, nil
}

// Close releases the response.
func (r *failingReader) Close() error { return nil }

// TestAFailureThatIsNotRecoverableEndsTheRun covers the flag: a
// failure the run cannot carry on ends it.
func TestAFailureThatIsNotRecoverableEndsTheRun(t *testing.T) {
	sink := &recordingSink{}
	tr := &failingTransport{err: &poo.StreamError{
		Message:     "the response is broken",
		Recoverable: false,
	}}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")

	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want %q", res.Reason, poo.ReasonProviderError)
	}
}

// TestTheParseErrorIsVisibleToTheModel covers the plan's rule: a tool
// call that could not be parsed is not dropped, and the model is told
// about it, so it can write the call again.
func TestTheParseErrorIsVisibleToTheModel(t *testing.T) {
	srv, bodies := malformedEndpoint(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want %q", res.Reason, poo.ReasonEndTurn)
	}
	// The model is told: the note is in the view, so the next request
	// carries it.
	requests := bodies()
	if len(requests) < 1 {
		t.Fatal("the endpoint got no request")
	}
	if _, err := s.Prompt(context.Background(), "again"); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the second run: %v", err)
	}
	requests = bodies()
	if len(requests) < 2 {
		t.Fatalf("the endpoint got %d requests, want 2", len(requests))
	}
	messages, _ := requests[1]["messages"].([]any)
	var note, prose string
	for _, raw := range messages {
		msg, _ := raw.(map[string]any)
		content, _ := msg["content"].(string)
		switch msg["role"] {
		case "user":
			if strings.Contains(content, "could not be parsed") {
				note = content
			}
		case "assistant":
			prose = content
		}
	}
	if note == "" {
		t.Fatal("the second request carries no note about the parse error")
	}
	// The text the model wrote is not lost: it is in the model's own
	// message.
	if !strings.Contains(prose, "tool_call") {
		t.Errorf("the model's own text was dropped: %q", prose)
	}
	// The record holds the note too.
	var recorded bool
	for _, rec := range sink.allRecords() {
		msg, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, block := range msg.Content {
			if strings.Contains(block.Text, "could not be parsed") {
				recorded = true
			}
		}
	}
	if !recorded {
		t.Error("the record holds no note about the parse error")
	}
}
