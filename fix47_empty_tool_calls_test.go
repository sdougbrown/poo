package poo_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
)

// The recorded stream the local gemma4 server sends: a finish reason of
// tool_calls, no call, and the usage of the request.
const emptyToolCallsStream = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":18,\"prompt_tokens\":1082,\"total_tokens\":1100}}\n\n" +
	"data: [DONE]\n\n"

// answerStream is an ordinary answer, so a request after the empty one
// has something to read.
const answerStream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{}}],\"usage\":{\"completion_tokens\":5,\"prompt_tokens\":100,\"total_tokens\":105}}\n\n" +
	"data: [DONE]\n\n"

// streamEndpoint answers each request with the next stream, and repeats
// the last one.
func streamEndpoint(t *testing.T, streams ...string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		n := len(bodies)
		mu.Unlock()
		stream := streams[min(n, len(streams))-1]
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// TestTheRecordedEmptyTurnIsRecovered covers the stream the local gemma4
// server sends: the run carries on, and the request that follows it holds
// the notice.
func TestTheRecordedEmptyTurnIsRecovered(t *testing.T) {
	srv, bodies := streamEndpoint(t, emptyToolCallsStream, answerStream)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	got := bodies()
	if len(got) != 2 {
		t.Fatalf("the endpoint got %d requests, want 2: the empty turn is a turn, not a failed request", len(got))
	}
	if !strings.Contains(got[1], poo.EmptyToolCallsText) {
		t.Errorf("the second request does not carry the notice:\n%s", got[1])
	}
}

// TestAServerThatKeepsSendingTheEmptyTurnSettles covers the endpoint
// whose every answer is the empty stream: the run asks twice and settles
// as a natural stop, the way the recoverable-note bound settles.
func TestAServerThatKeepsSendingTheEmptyTurnSettles(t *testing.T) {
	srv, bodies := streamEndpoint(t, emptyToolCallsStream)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Source)
	}
	if got := len(bodies()); got != 2 {
		t.Errorf("the endpoint got %d requests, want 2", got)
	}
}

// emptyToolCallTurns returns a script of turns that ask for tool calls and
// send none, followed by the turns that answer.
//
// The endpoint's own stream is a finish reason of "tool_calls" with no
// call in it, which is what the local gemma4 server sends.
func emptyToolCallTurns(empty int, then ...pootest.Turn) *pootest.Transport {
	turns := make([]pootest.Turn, 0, empty+len(then))
	for i := 0; i < empty; i++ {
		turns = append(turns, pootest.Turn{Finish: "tool_calls"})
	}
	return pootest.NewTransport(append(turns, then...)...)
}

// TestATurnThatAsksForToolCallsAndSendsNoneIsRecovered covers the model
// that answers with finish_reason "tool_calls" and no call in the
// response.
//
// The turn is the model's slip, not the endpoint's failure: the model is
// told what its turn looked like and gets another turn. The turn is also
// in the session's records, so a resumed session reads the same history.
func TestATurnThatAsksForToolCallsAndSendsNoneIsRecovered(t *testing.T) {
	tr := emptyToolCallTurns(1, pootest.Turn{Text: "the answer"})
	sink := &recordingSink{}
	tool := &echoTool{}
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn: the model answered after the notice", res.Reason)
	}

	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the endpoint got %d requests, want 2: the model is told and tries again", len(requests))
	}
	got := requests[1].Messages
	if len(got) == 0 {
		t.Fatalf("the second request carries no message")
	}
	last := got[len(got)-1]
	if last.Role != poo.RoleUser || last.Text() != poo.EmptyToolCallsText {
		t.Errorf("the second request ends with %s %q, want the notice %q",
			last.Role, last.Text(), poo.EmptyToolCallsText)
	}

	// The empty turn is in the records: the notice it opened is a
	// message record of that turn.
	var notes int
	for _, rec := range sink.messages() {
		if rec.Turn != 1 || rec.Role != poo.RoleUser {
			continue
		}
		for _, block := range rec.Content {
			if block.Kind == poo.ContentText && block.Text == poo.EmptyToolCallsText {
				notes++
			}
		}
	}
	if notes != 1 {
		t.Errorf("the records hold the notice %d times, want 1", notes)
	}
	if calls := tool.Calls(); len(calls) != 0 {
		t.Errorf("the tool ran %d times, want 0", len(calls))
	}
}

// TestAConsecutiveRunOfEmptyTurnsSettlesNaturally covers the bound: a
// model that keeps answering with no turn at all is asked twice, and then
// the run settles the way the recoverable-note bound settles, as a
// natural stop.
func TestAConsecutiveRunOfEmptyTurnsSettlesNaturally(t *testing.T) {
	tr := emptyToolCallTurns(3)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the endpoint got %d requests, want 2: the model is asked twice", got)
	}
	var notes int
	for _, rec := range sink.messages() {
		if rec.Role == poo.RoleUser && rec.Turn > 0 {
			notes++
		}
	}
	if notes != 2 {
		t.Errorf("the records hold %d notices, want 2", notes)
	}
}

// TestTheRestoredSessionKeepsTheEmptyTurnNotice covers the replay: the
// notice the empty turn opened is in the records, so the request the
// restored session sends is the request the original session sent after
// it.
func TestTheRestoredSessionKeepsTheEmptyTurnNotice(t *testing.T) {
	original := emptyToolCallTurns(1,
		pootest.Turn{Text: "the answer"},
		pootest.Turn{Text: "ok"},
	)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, original, &echoTool{}, sink))
	runSession(t, s, "hello")
	firstRun := append([]poo.Record{header("s1")}, sink.allRecords()...)
	runSession(t, s, "more")

	requests := original.Requests()
	if len(requests) != 3 {
		t.Fatalf("the original made %d requests, want 3", len(requests))
	}

	restoredTransport := scripted([]fakeResponse{{text: "ok"}})
	session, _, err := poo.Restore(restoreOptions(t, restoredTransport, &recordingSink{}), firstRun)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	if _, err := session.Prompt(context.Background(), "more"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}

	restoredRequests := restoredTransport.Requests()
	if len(restoredRequests) != 1 {
		t.Fatalf("the restored session made %d requests, want 1", len(restoredRequests))
	}
	want := requests[2].Messages
	got := restoredRequests[0].Messages
	if len(want) != len(got) {
		t.Fatalf("the restored request holds %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if want[i].Role != got[i].Role || want[i].Text() != got[i].Text() {
			t.Errorf("the message %d is %s %q, want %s %q",
				i, got[i].Role, got[i].Text(), want[i].Role, want[i].Text())
		}
	}
	if !bytes.Equal(original.Rendered()[2].Body, restoredTransport.Rendered()[0].Body) {
		t.Errorf("the restored body is\n%s\nwant\n%s",
			restoredTransport.Rendered()[0].Body, original.Rendered()[2].Body)
	}
}
