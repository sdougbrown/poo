package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

func TestARunThatEndsOnItsOwnSettlesEndTurn(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "all done"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 1 {
		t.Errorf("the run made %d turns, want 1", res.Turns)
	}

	names := sink.names()
	// The system message, the user's prompt, the model's answer and the
	// settle record.
	want := []string{"run.started", "message", "message", "message", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the records are %v, want %v", names, want)
	}
}

func TestARunThatCallsAToolExecutesIt(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	if len(tool.Calls()) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(tool.Calls()))
	}
	if !strings.Contains(tool.Calls()[0], "hello") {
		t.Errorf("the tool received %q, want the call's arguments", tool.Calls()[0])
	}

	// The tool call and its result are both recorded.
	if sink.count("tool.call") != 1 {
		t.Errorf("the log holds %d tool.call records, want 1", sink.count("tool.call"))
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheSecondRequestCarriesTheToolResult(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	tool := &echoTool{result: "the echoed text"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	// system, user, assistant (with its tool call), tool result
	if len(second) != 4 {
		t.Fatalf("the second request carried %d messages, want 4", len(second))
	}
	last := second[3]
	if last.Role != poo.RoleTool {
		t.Errorf("the last message's role is %q, want tool", last.Role)
	}
	if last.Content[0].Text != "the echoed text" {
		t.Errorf("the last message holds %q, want the tool's output", last.Content[0].Text)
	}
	// The result carries the provider's call id, because that is the id
	// the provider matches it against.
	if last.Content[0].ToolCallID != "call_1" {
		t.Errorf("the result's tool call id is %q, want call_1", last.Content[0].ToolCallID)
	}
	// The assistant's tool call is in the history, so the provider can
	// match the result to it.
	assistant := second[2]
	if len(assistant.ToolCalls()) != 1 {
		t.Fatalf("the assistant message holds %d tool calls, want 1", len(assistant.ToolCalls()))
	}
	if assistant.ToolCalls()[0].ToolCallID != "call_1" {
		t.Errorf("the tool call's id is %q, want call_1", assistant.ToolCalls()[0].ToolCallID)
	}
}

func TestTheMaxTurnsCeilingSettlesMaxTurns(t *testing.T) {
	// Every response asks for another tool call, so the run only stops
	// at its ceiling.
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"again"}`),
		}},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.MaxTurns = 3

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	if res.Turns != 3 {
		t.Errorf("the run made %d turns, want 3", res.Turns)
	}
	if len(tool.Calls()) != 3 {
		t.Errorf("the tool ran %d times, want 3", len(tool.Calls()))
	}
	// The turn ceiling is a candidate of its own.
	if res.Detail.Source != "max_turns" {
		t.Errorf("the settle source is %q, want max_turns", res.Detail.Source)
	}
}

func TestACancelledContextSettlesCancelled(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(ctx, "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
}

func TestAProviderErrorSettlesProviderError(t *testing.T) {
	tr := scripted([]fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint is down",
			Status:  500,
		},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatal("the settle detail has no provider information")
	}
	if res.Detail.Provider.Kind != string(poo.ProviderKindServer) {
		t.Errorf("the provider kind is %q, want server", res.Detail.Provider.Kind)
	}
}

func TestAContextLengthErrorSettlesContextExhausted(t *testing.T) {
	tr := scripted([]fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindContextLength,
			Message: "the prompt is too long",
			Status:  400,
		},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
}

func TestAFinishReasonOfLengthReachesBeforeSettle(t *testing.T) {
	// A truncated response is a natural stop, and the plan's
	// `output_truncated` reason is what it settles as.
	tr := scripted([]fakeResponse{{
		text:   "a truncated ans",
		finish: poo.FinishLength,
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonOutputTruncated {
		t.Errorf("the run settled %q, want output_truncated", res.Reason)
	}
}

func TestNoOutputRoomSettlesContextExhaustedWithoutSending(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	// A context limit this small leaves less than 256 tokens of room
	// once the prompt is counted.
	opts.ContextLimit = 300

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	if len(tr.Requests()) != 0 {
		t.Errorf("the transport got %d requests, want none", len(tr.Requests()))
	}
}

func TestTheMaxTokensIsComputedFromTheContext(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 4096
	opts.MaxOutput = 0

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	mt := reqs[0].MaxTokens
	// The room is the limit minus the estimated input minus 256, and the
	// model's own ceiling is 8192 by default. The estimate covers the
	// history at request time: the system message ("the system prompt")
	// and the user message ("a short prompt") hold 23 and 18 chars, and
	// the plan estimates at 3.5 chars per token, so 41*10/35 = 11.
	estimate := (len("system") + len("the system prompt") + len("user") + len("a short prompt")) * 10 / 35
	want := 4096 - estimate - 256
	if mt != want {
		t.Errorf("max_tokens is %d, want %d (context limit minus the %d-token estimate minus the 256-token reserve)", mt, want, estimate)
	}
}

func TestTheMaxOutputCapsTheMaxTokens(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 262144
	opts.MaxOutput = 512

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	if got := tr.Requests()[0].MaxTokens; got != 512 {
		t.Errorf("max_tokens is %d, want 512", got)
	}
}

func TestTheDefaultMaxTokensIsEightThousandAndNinetyTwo(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 262144
	opts.MaxOutput = 0

	s := newTestSession(t, opts)
	runSession(t, s, "a short prompt")
	if got := tr.Requests()[0].MaxTokens; got != 8192 {
		t.Errorf("max_tokens is %d, want 8192", got)
	}
}

func TestAnUnknownToolIsARecoverableError(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "nope",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheSessionMovesThroughTheLoopStates(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if s.State() != poo.StateIdle {
		t.Errorf("a new session is in %q, want idle", s.State())
	}
	runSession(t, s, "do the thing")
	if s.State() != poo.StateSettled {
		t.Errorf("a finished session is in %q, want settled", s.State())
	}
}

func TestTheContextRunningOutMidRunSettlesContextExhausted(t *testing.T) {
	// The model asks for a call whose result is large, so the next
	// request has no room left.
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"hello"}`),
		}},
	}})
	tool := &echoTool{result: string(make([]byte, 20000))}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.ContextLimit = 2000

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	// The first request went out, and the second did not.
	if len(tr.Requests()) != 1 {
		t.Errorf("the transport got %d requests, want 1", len(tr.Requests()))
	}
}

func TestTheSettleRecordCarriesTheReason(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")
	records := sink.allRecords()
	last := records[len(records)-1]
	settled, ok := last.(*poo.RunSettled)
	if !ok {
		t.Fatalf("the last record is %T, want a run.settled", last)
	}
	if settled.Reason != poo.ReasonEndTurn {
		t.Errorf("the settle reason is %q, want end_turn", settled.Reason)
	}
	if settled.RunID == "" {
		t.Error("the settle record has no run id")
	}
	if settled.Seq == 0 {
		t.Error("the settle record has no sequence number")
	}
	if settled.TS == "" {
		t.Error("the settle record has no timestamp")
	}
}

func TestMalformedArgumentsAreRecordedAsText(t *testing.T) {
	// The model sent arguments that are not valid JSON. The record
	// still holds valid JSON, so a consumer can decode it without a
	// special case, and the raw text is preserved.
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text": broken`),
			}},
		},
		{text: "finished"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	var call *poo.ToolCallRecord
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.ToolCallRecord); ok {
			call = c
		}
	}
	if call == nil {
		t.Fatal("the log holds no tool.call record")
	}
	if !json.Valid(call.Arguments) {
		t.Fatalf("the recorded arguments are not valid JSON: %s", call.Arguments)
	}
	var text string
	if err := json.Unmarshal(call.Arguments, &text); err != nil {
		t.Fatalf("the recorded arguments are not a JSON string: %s", call.Arguments)
	}
	if text != `{"text": broken` {
		t.Errorf("the recorded arguments are %q, want the raw text", text)
	}
}

func TestASessionWithoutATransportIsAConstructionError(t *testing.T) {
	_, err := poo.New(poo.Options{ContextLimit: 1024})
	if err == nil {
		t.Fatal("a session with no transport was built")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the error is %T, want an ErrSessionInit", err)
	}
}

func TestASessionWithoutAContextLimitIsAConstructionError(t *testing.T) {
	_, err := poo.New(poo.Options{Transport: pootest.NewTransport()})
	if err == nil {
		t.Fatal("a session with no context limit was built")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the error is %T, want an ErrSessionInit", err)
	}
}

// asSessionInit reports that the error is an ErrSessionInit.
func asSessionInit(err error, target **poo.ErrSessionInit) bool {
	for err != nil {
		if e, ok := err.(*poo.ErrSessionInit); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestTheSessionRefusesASecondPromptWhileARunIsActive(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hello"}})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "first"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Prompt(context.Background(), "second"); err == nil {
		t.Fatal("the session accepted a second prompt")
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}
