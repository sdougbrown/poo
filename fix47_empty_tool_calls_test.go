package poo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/eval"
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
// invalid_turns, the reason the bounds on unusable turns give.
func TestAServerThatKeepsSendingTheEmptyTurnSettles(t *testing.T) {
	srv, bodies := streamEndpoint(t, emptyToolCallsStream)
	tr := openai.New(openai.Options{
		Endpoint: srv.URL,
		Model:    "m",
		Backoff:  []time.Duration{time.Millisecond, time.Millisecond},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q (%s), want invalid_turns", res.Reason, res.Detail.Source)
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

// TestAConsecutiveRunOfEmptyTurnsSettlesInvalidTurns covers the bound: a
// model that keeps answering with no turn at all is asked twice, and then
// the run settles invalid_turns.
func TestAConsecutiveRunOfEmptyTurnsSettlesInvalidTurns(t *testing.T) {
	tr := emptyToolCallTurns(3)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q, want invalid_turns", res.Reason)
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

// TestTheReplayOfARecoveredRunReportsTheShift covers what a replay sees
// in a log that holds a recovered empty turn: an empty turn records no
// assistant message, so the script has no turn for it and every later
// turn replays one request early. The replay must report that rather
// than a match.
func TestTheReplayOfARecoveredRunReportsTheShift(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Finish: "tool_calls", InputTokens: 1082, OutputTokens: 18},
		pootest.Turn{Calls: []pootest.Call{{Name: "echo", Arguments: []byte(`{"text":"hi"}`)}},
			InputTokens: 100, OutputTokens: 5},
		pootest.Turn{Text: "done", InputTokens: 110, OutputTokens: 4},
	)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := len(tr.Requests()); got != 3 {
		t.Fatalf("the recorded run made %d requests, want 3", got)
	}

	path := writeSessionLog(t, append([]poo.Record{header("s1")}, sink.allRecords()...))

	log, err := eval.ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := eval.ScriptFromLog(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Turns) != 2 {
		t.Errorf("the script holds %d turns, want 2: the empty turn records no assistant message", len(script.Turns))
	}

	result, err := eval.Replay(path, eval.ReplayOptions{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("the replay failed: %v", err)
	}
	if len(result.Recorded) != 1 || len(result.Replayed) != 1 {
		t.Fatalf("the recording holds %d calls and the replay made %d, want one each",
			len(result.Recorded), len(result.Replayed))
	}
	recorded, replayed := result.Recorded[0], result.Replayed[0]
	if recorded.Turn != 2 || replayed.Turn != 1 {
		t.Fatalf("the recorded call was at turn %d and the replayed one at turn %d, want 2 and 1",
			recorded.Turn, replayed.Turn)
	}
	if result.Match {
		t.Errorf("the replay reports a match although it sent the call at turn %d and the recording at turn %d",
			replayed.Turn, recorded.Turn)
	}
	if len(result.Differences) == 0 {
		t.Error("the replay reported no difference for a shifted call")
	}
	var named bool
	for _, difference := range result.Differences {
		if strings.Contains(difference, "no assistant message") {
			named = true
		}
	}
	if !named {
		t.Errorf("the replay's differences do not name the request the script cannot replay: %v", result.Differences)
	}
}

// TestAReplayOfABoundedRunHasNoScript covers the other side: a run that
// settled at the empty-turn bound holds no assistant message at all, so
// the replay cannot build a script and says so.
func TestAReplayOfABoundedRunHasNoScript(t *testing.T) {
	tr := emptyToolCallTurns(3)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q, want invalid_turns", res.Reason)
	}

	path := writeSessionLog(t, append([]poo.Record{header("s1")}, sink.allRecords()...))

	log, err := eval.ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eval.ScriptFromLog(log); err == nil {
		t.Error("the script was built from a run whose model never said anything")
	}
}

// writeSessionLog writes records as a session log and returns its path.
func writeSessionLog(t *testing.T, records []poo.Record) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
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
