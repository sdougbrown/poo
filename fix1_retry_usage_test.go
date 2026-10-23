package poo_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// emptyToolCallsThenAnswer answers the first request with the
// recorded empty-tool_calls stream and the second one with an answer,
// so a request is retried once and then succeeds.
func emptyToolCallsThenAnswer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	requests := 0
	// The recorded stream: finish_reason tool_calls, no call, and a
	// usage chunk of 1082 prompt and 18 completion tokens.
	empty := "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":18,\"prompt_tokens\":1082,\"total_tokens\":1100}}\n\n" +
		"data: [DONE]\n\n"
	answer := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":5,\"prompt_tokens\":100,\"total_tokens\":105}}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, empty)
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
// The run's usage record holds the answer's numbers only: the failed
// attempt's usage is kept in streamWithRetry.usage, and it is reported
// only when the request ends with a failure.
func TestARetriedAttemptsUsageIsCounted(t *testing.T) {
	srv := emptyToolCallsThenAnswer(t)
	tr := openai.New(openai.Config{
		BaseURL: srv.URL,
		Model:   "m",
		Backoff: []time.Duration{time.Millisecond, time.Millisecond},
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
		usage, ok := rec.(*poo.Usage)
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
