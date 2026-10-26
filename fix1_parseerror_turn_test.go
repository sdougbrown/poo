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
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\""+
			"<tool_call>{\\\"name\\\": \\\"bash\\\", \\\"arguments\\\": omg</tool_call>\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
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
// The note is appended to the view, but the turn that produced it
// processed no call, so turnEnd sees `processed == false` and
// `appended == false` and settles the run as end_turn. The note reaches
// the model only in a later run.
func TestTheParseErrorGetsASecondTurn(t *testing.T) {
	srv, requests := unparseableCallEndpoint(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "text"})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := requests(); got < 2 {
		t.Errorf("the endpoint got %d requests, want 2: the model is told about the parse error and tries again",
			got)
	}
}
