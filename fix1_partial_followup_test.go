package poo_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo/provider/openai"
)

// TestTheNextRunPairsABrokenStreamsCallsWithResults covers the wire's
// rule after a stream breaks with a call in it: the partial message keeps
// the call, and the next run's request holds a result for it, so the
// provider never sees an unanswered call.
func TestTheNextRunPairsABrokenStreamsCallsWithResults(t *testing.T) {
	var mu sync.Mutex
	n := 0
	var second []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		n++
		k := n
		if k == 2 {
			second = body
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if k == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"half an answer\"},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"text\\\":\\\"hi\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"the model crashed\",\"type\":\"server_error\"}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", Backoff: []time.Duration{time.Millisecond, time.Millisecond}})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))
	runSession(t, s, "hi")
	runSession(t, s, "again")
	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(second, &req); err != nil {
		t.Fatalf("second request: %v %s", err, second)
	}
	calls := map[string]bool{}
	for _, m := range req.Messages {
		for _, c := range m.ToolCalls {
			calls[c.ID] = true
		}
		if m.Role == "tool" {
			delete(calls, m.ToolCallID)
		}
	}
	if len(calls) > 0 {
		t.Errorf("the next request holds calls with no result: %v\n%s", calls, second)
	}
}
