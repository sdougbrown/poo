package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
)

// The two bounds on a model that keeps producing turns the run cannot
// use: the consecutive empty tool-call turns, and the consecutive turns
// whose calls did not parse. Both settle invalid_turns, and the settle
// detail names the bound and the number of turns.

// invalidTurnsExitCode is the exit code of an invalid_turns settle.
const invalidTurnsExitCode = 15

// parseableCallStream is a text-mode tool call that parses, so the turn
// that carries it resets the run's invalid-turn counters.
const parseableCallStream = `data: {"choices":[{"index":0,"delta":{"content":"<tool_call>{\"name\": \"echo\", \"arguments\": {\"text\": \"hi\"}}</tool_call>"},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
	"data: [DONE]\n\n"

// TestTheEmptyTurnBoundSettlesInvalidTurns covers the first bound: a
// model that keeps answering with tool calls it never sends is asked
// twice, and then the run settles invalid_turns.
func TestTheEmptyTurnBoundSettlesInvalidTurns(t *testing.T) {
	tr := emptyToolCallTurns(3)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q, want invalid_turns", res.Reason)
	}
	if got := poo.ExitCode(res.Reason); got != invalidTurnsExitCode {
		t.Errorf("the exit code is %d, want %d", got, invalidTurnsExitCode)
	}
	if res.Detail.Source != "empty_turns" {
		t.Errorf("the settle source is %q, want empty_turns", res.Detail.Source)
	}
	if !strings.Contains(res.Detail.Text, "2 turns") {
		t.Errorf("the settle text is %q, want it to name the two turns", res.Detail.Text)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the endpoint got %d requests, want the two-turn budget", got)
	}
	var notes int
	for _, rec := range sink.messages() {
		if rec.Role == poo.RoleUser && rec.Turn > 0 {
			notes++
		}
	}
	if notes != 2 {
		t.Errorf("the records hold %d notices, want 2: the last turn's note is still recorded", notes)
	}
}

// TestTheUnparsedCallBoundSettlesInvalidTurns covers the second bound:
// the consecutive turns whose calls did not parse.
func TestTheUnparsedCallBoundSettlesInvalidTurns(t *testing.T) {
	srv, requests := unparseableCallEndpoint(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "text"})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonInvalidTurns {
		t.Fatalf("the run settled %q, want invalid_turns", res.Reason)
	}
	if got := poo.ExitCode(res.Reason); got != invalidTurnsExitCode {
		t.Errorf("the exit code is %d, want %d", got, invalidTurnsExitCode)
	}
	if res.Detail.Source != "unparsed_calls" {
		t.Errorf("the settle source is %q, want unparsed_calls", res.Detail.Source)
	}
	if !strings.Contains(res.Detail.Text, "3 turns") {
		t.Errorf("the settle text is %q, want it to name the three turns", res.Detail.Text)
	}
	if got := requests(); got != 3 {
		t.Errorf("the endpoint got %d requests, want the three-note budget", got)
	}
	var notes int
	for _, rec := range sink.messages() {
		if rec.Role == poo.RoleUser && rec.Turn > 0 {
			notes++
		}
	}
	if notes != 3 {
		t.Errorf("the records hold %d notices, want 3: the last turn's note is still recorded", notes)
	}
}

// TestAValidTurnResetsTheEmptyTurnCount covers the counter's reset: the
// second empty turn would reach the bound if the tool call between them
// had not reset the count, so the run settles naturally instead.
func TestAValidTurnResetsTheEmptyTurnCount(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Finish: "tool_calls"},
		pootest.Turn{Calls: []pootest.Call{{Name: "echo", Arguments: []byte(`{"text":"hi"}`)}}},
		pootest.Turn{Finish: "tool_calls"},
		pootest.Turn{Text: "the answer"},
	)
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the call reset the count",
			res.Reason, res.Detail.Source)
	}
	if got := len(tr.Requests()); got != 4 {
		t.Errorf("the endpoint got %d requests, want 4", got)
	}
}

// TestAValidTurnResetsTheUnparsedCallCount covers the other counter's
// reset: two unparsed calls, a parseable one, then two more. Without the
// reset the fourth request would reach the bound; with it the run answers
// and settles naturally.
func TestAValidTurnResetsTheUnparsedCallCount(t *testing.T) {
	srv, bodies := streamEndpoint(t, unparseableCallStream, parseableCallStream,
		unparseableCallStream, unparseableCallStream, answerStream)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "text"})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	res := runSession(t, s, "hi")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the parseable call reset the count",
			res.Reason, res.Detail.Source)
	}
	if got := len(bodies()); got != 5 {
		t.Errorf("the endpoint got %d requests, want 5", got)
	}
}
