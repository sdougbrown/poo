package poo_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// unparseableCallStream is a text-mode tool call that does not parse.
const unparseableCallStream = `data: {"choices":[{"index":0,"delta":{"content":"<tool_call>{\"name\": \"bash\", \"arguments\": omg</tool_call>"},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
	"data: [DONE]\n\n"

// unparseableCallEndpoint answers every request with the same
// text-mode tool call that does not parse.
func unparseableCallEndpoint(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, unparseableCallStream)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return requests
	}
}

// TestTheParseErrorGetsASecondTurn covers the plan's recoverable
// failure: "your tool call could not be parsed: <reason>" is a failure
// the run can carry on from, so the model is told and writes the call
// again.
//
// This endpoint never writes a call that parses, so the run reaches the
// consecutive-note bound and settles invalid_turns.
func TestTheParseErrorGetsASecondTurn(t *testing.T) {
	srv, requests := unparseableCallEndpoint(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "text"})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q, want invalid_turns", res.Reason)
	}
	if got := requests(); got != 3 {
		t.Errorf("the endpoint got %d requests, want the three-note budget", got)
	}
}
