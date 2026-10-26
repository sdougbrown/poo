package poo_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// brokenStreamServer answers with a delta, a tool call and then a
// broken connection.
func brokenStreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"half an answer\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"text\\\":\\\"hi\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"the model crashed\",\"type\":\"server_error\"}}\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestThePartialAssistantMessageAfterADeltaIsRecorded covers the plan's
// sentence for a stream that breaks after its first delta: "the partial
// assistant message is recorded with partial: true, cause:
// provider_error, its tool calls are not executed, and the run settles
// provider_error".
//
// The settle reason and "no executed calls" are there. The message is
// not recorded: readResponse hands the partial response back, and the
// loop drops it on the settle path.
func TestThePartialAssistantMessageAfterADeltaIsRecorded(t *testing.T) {
	srv := brokenStreamServer(t)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	tool := &echoTool{}
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonProviderError {
		t.Fatalf("the run settled %q, want provider_error", res.Reason)
	}
	if calls := tool.Calls(); len(calls) != 0 {
		t.Errorf("the tool ran %d times, want none", len(calls))
	}

	var partial *poo.MessageRecord
	for _, rec := range sink.messages() {
		if rec.Role == poo.RoleAssistant && rec.Partial {
			partial = rec
		}
	}
	if partial == nil {
		t.Fatalf("the record holds no partial assistant message; the records are %v", sink.Names())
	}
	if partial.Cause != poo.CauseProviderError {
		t.Errorf("the partial message's cause is %q, want %q", partial.Cause, poo.CauseProviderError)
	}
	var text string
	for _, block := range partial.Content {
		if block.Kind == poo.ContentText {
			text += block.Text
		}
	}
	if text != "half an answer" {
		t.Errorf("the partial message holds %q, want the text the stream carried", text)
	}
}

// TestTheAttemptAfterADeltaKeepsItsKind covers the failure class
// of an attempt that carried a delta before it broke.
//
// The stream reports its failure in an error chunk, and the transport
// classifies the chunk by its code and message: the kind is "server",
// which is a kind the retry treats as worth another attempt. The
// attempt is not retried (streamWithRetry.seen stops it), but the
// record says the failure was the server's, and no record says the
// stream had already shown the model part of the answer.
func TestTheAttemptAfterADeltaKeepsItsKind(t *testing.T) {
	srv := brokenStreamServer(t)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonProviderError {
		t.Fatalf("the run settled %q, want provider_error", res.Reason)
	}
	var kind string
	for _, rec := range sink.allRecords() {
		if a, ok := rec.(*poo.ProviderAttemptRecord); ok {
			kind = a.Kind
		}
	}
	if kind != string(poo.ProviderKindInterruptedStream) {
		t.Errorf("the attempt is recorded as %q, want %q: the stream had already sent a delta",
			kind, poo.ProviderKindInterruptedStream)
	}
}
