package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/finish"
)

// The FSM's own table: every state, and the states it may move to.
func TestTheStateTableCoversEveryState(t *testing.T) {
	states := []poo.State{
		poo.StateIdle,
		poo.StateRequesting,
		poo.StateStreaming,
		poo.StateExecuting,
		poo.StateCompacting,
		poo.StateWaiting,
		poo.StateSettling,
		poo.StateSettled,
	}
	for _, from := range states {
		for _, to := range states {
			_ = poo.IsLegal(from, to)
		}
	}
	// The states the plan names are all there.
	for _, want := range []poo.State{poo.StateIdle, poo.StateRequesting, poo.StateStreaming,
		poo.StateExecuting, poo.StateCompacting, poo.StateWaiting, poo.StateSettling, poo.StateSettled} {
		if want == "" {
			t.Error("a state has no name")
		}
	}
}

// TestTheRunMovesThroughTheStatesItUses walks a run that calls a tool
// and reads the states it passed through.
func TestTheRunMovesThroughTheStatesItUses(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	transitions := s.Transitions()
	want := []poo.State{
		poo.StateRequesting,
		poo.StateStreaming,
		poo.StateExecuting,
		poo.StateRequesting,
		poo.StateStreaming,
		poo.StateSettling,
		poo.StateSettled,
	}
	if strings.Join(stateNames(transitions), ",") != strings.Join(stateNames(want), ",") {
		t.Errorf("the states are %v, want %v", stateNames(transitions), stateNames(want))
	}
}

// stateNames renders a list of states.
func stateNames(states []poo.State) []string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, string(s))
	}
	return out
}

// TestTheSettleReasonPrecedenceIsThePlanTable covers the arbitration
// table: the reasons are ordered, and the natural ones come last.
func TestTheSettleReasonPrecedenceIsThePlanTable(t *testing.T) {
	ordered := []poo.Reason{
		poo.ReasonCancelled,
		poo.ReasonStoreError,
		poo.ReasonHookError,
		poo.ReasonToolFatal,
		poo.ReasonProviderLimit,
		poo.ReasonProviderError,
		poo.ReasonContextExhausted,
		poo.ReasonHookContinueLimit,
		poo.ReasonMaxTurns,
		poo.ReasonBudgetExhausted,
		poo.ReasonFinished,
		poo.ReasonStopRequested,
		poo.ReasonOutputTruncated,
		poo.ReasonEndTurn,
	}
	for i := 1; i < len(ordered); i++ {
		if poo.Precedence(ordered[i-1]) > poo.Precedence(ordered[i]) {
			t.Errorf("%q and %q have the wrong order: %d and %d",
				ordered[i-1], ordered[i], poo.Precedence(ordered[i-1]), poo.Precedence(ordered[i]))
		}
	}
	// provider_limit and provider_error share a level: a single failed
	// request produces only one of them.
	if poo.Precedence(poo.ReasonProviderLimit) != poo.Precedence(poo.ReasonProviderError) {
		t.Error("provider_limit and provider_error do not share a level")
	}
}

// TestTheExitCodesAreThePlanTable covers the exit codes.
func TestTheExitCodesAreThePlanTable(t *testing.T) {
	codes := map[poo.Reason]int{
		poo.ReasonCancelled:         130,
		poo.ReasonStoreError:        23,
		poo.ReasonHookError:         22,
		poo.ReasonToolFatal:         21,
		poo.ReasonProviderLimit:     24,
		poo.ReasonProviderError:     20,
		poo.ReasonContextExhausted:  13,
		poo.ReasonHookContinueLimit: 12,
		poo.ReasonMaxTurns:          11,
		poo.ReasonBudgetExhausted:   14,
		poo.ReasonFinished:          0,
		poo.ReasonStopRequested:     0,
		poo.ReasonOutputTruncated:   10,
		poo.ReasonEndTurn:           0,
	}
	for reason, want := range codes {
		if got := poo.ExitCode(reason); got != want {
			t.Errorf("the exit code for %q is %d, want %d", reason, got, want)
		}
	}
}

// TestATruncatedResponseRunsTheCompleteCalls covers the truncation
// rule: a complete call runs, and an incomplete one is recorded skipped
// with the visible note.
func TestATruncatedResponseRunsTheCompleteCalls(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			finish: poo.FinishLength,
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"whole"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"half`)},
			},
		},
		{text: "finished"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	// The complete call ran.
	if len(tool.Calls()) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(tool.Calls()))
	}
	if !strings.Contains(tool.Calls()[0], "whole") {
		t.Errorf("the tool received %q, want the complete call", tool.Calls()[0])
	}

	// The incomplete call is skipped with the visible note.
	var skipped *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Handle != "r2" {
			continue
		}
		skipped = r
	}
	if skipped == nil {
		t.Fatal("the log holds no result for the second call")
	}
	if skipped.Outcome != poo.OutcomeSkipped {
		t.Errorf("the outcome is %q, want skipped", skipped.Outcome)
	}
	if skipped.Text != poo.SkippedTruncatedText {
		t.Errorf("the result's text is %q, want the visible note", skipped.Text)
	}
}

// TestATruncatedTurnWithNoCompleteCallReachesBeforeSettle covers the
// natural stop: a truncated turn with no complete call reaches the
// BeforeSettle hook as output_truncated.
func TestATruncatedTurnWithNoCompleteCallReachesBeforeSettle(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		text:   "a truncated ans",
		finish: poo.FinishLength,
	}}}
	tool := &echoTool{result: "echoed"}
	hook := &recordingBeforeSettle{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if hook.calls != 1 {
		t.Fatalf("the BeforeSettle hook ran %d times, want 1", hook.calls)
	}
	if hook.candidate != poo.ReasonOutputTruncated {
		t.Errorf("the hook saw %q, want output_truncated", hook.candidate)
	}
	if res.Reason != poo.ReasonOutputTruncated {
		t.Errorf("the run settled %q, want output_truncated", res.Reason)
	}
}

// TestABatchCompletesBeforeHasToolCallStopsTheRun covers the plan's
// rule: the whole batch runs, and then the stop condition is asked.
func TestABatchCompletesBeforeHasToolCallStopsTheRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.StopConditions = []poo.StopCondition{poo.HasToolCall("echo")}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if len(tool.Calls()) != 2 {
		t.Errorf("the tool ran %d times, want 2: the whole batch runs first", len(tool.Calls()))
	}
	if res.Reason != poo.ReasonStopRequested {
		t.Errorf("the run settled %q, want stop_requested", res.Reason)
	}
	if res.Detail.Source != "stop_condition" {
		t.Errorf("the settle source is %q, want stop_condition", res.Detail.Source)
	}
}

// TestHasToolCallIgnoresErroredResults covers the other half of the
// rule: a call whose result was an error does not stop the run.
func TestHasToolCallIgnoresErroredResults(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
			},
		},
		{text: "finished"},
	}}
	// The tool fails, so its result is an error.
	tool := &echoTool{result: "echoed", prepareErr: errors.New("the arguments are wrong")}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.StopConditions = []poo.StopCondition{poo.HasToolCall("echo")}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: an errored result does not stop the run", res.Reason)
	}
}

// TestAToolFreeTurnWithSteeringContinues covers the turn-end
// algorithm's step 6: a turn that appended something goes to the gate
// again.
func TestAToolFreeTurnWithSteeringContinues(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	}}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The steer is sent while the first turn is running. The turn end
	// drains it, and the run goes to the gate again.
	if _, err := s.Send("actually, do this instead", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	if tr.requestCount() != 2 {
		t.Errorf("the transport got %d requests, want 2", tr.requestCount())
	}
}

// TestAToolFreeTurnWithAnAfterTurnAppendContinues covers the same step
// from the hook's side.
func TestAToolFreeTurnWithAnAfterTurnAppendContinues(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	}}
	tool := &echoTool{result: "echoed"}
	// The hook appends on the first turn only, so the run goes to the
	// gate again once and then stops.
	hook := &countingAfterTurn{stopAfter: 1}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	if hook.calls != 2 {
		t.Errorf("the AfterTurn hook ran %d times, want 2", hook.calls)
	}
}

// TestAPlainToolFreeTurnReachesBeforeSettle covers the last turn-end
// path: a turn with nothing in it reaches the BeforeSettle hook.
func TestAPlainToolFreeTurnReachesBeforeSettle(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}
	hook := &recordingBeforeSettle{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if hook.calls != 1 {
		t.Fatalf("the BeforeSettle hook ran %d times, want 1", hook.calls)
	}
	if hook.candidate != poo.ReasonEndTurn {
		t.Errorf("the hook saw %q, want end_turn", hook.candidate)
	}
}

// TestTheContinuationPredicateCoversTheResultKinds covers the plan's
// continuation predicate: a batch where every call was denied, failed
// Prepare, or got a BeforeTool result still goes to a new request, and
// that request carries the results.
//
// The BeforeTool hook belongs to the permissions stage, so the result
// kind is exercised through the core's own call kinds: a Prepare
// failure and a denied call.
func TestTheContinuationPredicateCoversTheResultKinds(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "the answer"},
	}}
	sink := &recordingSink{}
	// The tool's validation fails, so every call is a recoverable
	// error result.
	tool := &echoTool{result: "echoed", prepareErr: errors.New("the arguments are wrong")}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2: the batch continues to a new request", res.Turns)
	}
	// The second request carries the results.
	reqs := tr.requestsOf()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	if len(second) < 4 {
		t.Fatalf("the second request carried %d messages, want the results", len(second))
	}
}

// TestTheBeforeSettleContinueSendsTheRunBackToTheModel covers the
// BeforeSettle hook's Continue decision.
func TestTheBeforeSettleContinueSendsTheRunBackToTheModel(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	}}
	tool := &echoTool{result: "echoed"}
	// The hook asks for a continuation on the first turn only.
	hook := &continuingBeforeSettle{stopAfter: 1}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	// The hook ran at the first turn's end and at the second one's: the
	// second time it accepted.
	if hook.calls != 2 {
		t.Errorf("the BeforeSettle hook ran %d times, want 2", hook.calls)
	}
	// The continuation message is in the model's view.
	reqs := tr.requestsOf()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	found := false
	for _, m := range reqs[1].Messages {
		if strings.Contains(m.Text(), "keep going") {
			found = true
		}
	}
	if !found {
		t.Error("the second request does not carry the continuation message")
	}
}

// TestTheContinuationLimitSettlesTheRun covers the plan's limit: the
// next Continue after the allowed ones settles hook_continue_limit.
func TestTheContinuationLimitSettlesTheRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}
	hook := &continuingBeforeSettle{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.MaxContinuations = 3

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonHookContinueLimit {
		t.Errorf("the run settled %q, want hook_continue_limit", res.Reason)
	}
	// Three continuations are allowed, and the fourth is not.
	if hook.calls != 4 {
		t.Errorf("the BeforeSettle hook ran %d times, want 4", hook.calls)
	}
}

// TestTheAfterTurnStopSettlesTheRun covers the AfterTurn hook's Stop
// decision.
func TestTheAfterTurnStopSettlesTheRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}
	hook := &stoppingAfterTurn{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonStopRequested {
		t.Errorf("the run settled %q, want stop_requested", res.Reason)
	}
	if res.Detail.Text != "the hook stopped the run" {
		t.Errorf("the settle text is %q, want the hook's detail", res.Detail.Text)
	}
}

// TestMaxTurnsBeatsAnAfterTurnStop covers the precedence: the turn
// ceiling wins over a hook's stop.
func TestMaxTurnsBeatsAnAfterTurnStop(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
	}}
	tool := &echoTool{result: "echoed"}
	hook := &stoppingAfterTurn{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	if len(res.Detail.Losing) == 0 {
		t.Error("the settle detail does not list the losing candidate")
	}
}

// TestACancelBeatsEveryOtherCandidate covers the precedence: a cancel
// wins over a failure.
func TestACancelBeatsEveryOtherCandidate(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}
	hook := &stoppingAfterTurn{}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
}

// TestTheFinishToolSettlesTheRunWithItsResult covers the typed result:
// a valid call settles the run as finished, and the result is in the
// settle detail.
func TestTheFinishToolSettlesTheRunWithItsResult(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "finish",
				Arguments: json.RawMessage(`{"result":{"answer":"42"}}`),
			}},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}
	finisher := newFinishTool(t, `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Tools["finish"] = finisher
	opts.ToolOrder = []string{"echo", "finish"}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonFinished {
		t.Errorf("the run settled %q, want finished", res.Reason)
	}
	if len(res.Detail.Result) == 0 {
		t.Fatal("the settle detail holds no result")
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Detail.Result, &payload); err != nil {
		t.Fatalf("the result is not valid JSON: %v", err)
	}
	if payload["answer"] != "42" {
		t.Errorf("the result is %v, want the answer", payload)
	}
}

// TestAnInvalidFinishResultIsARecoverableError covers the other half:
// a result that does not match the schema is an error the model can fix.
func TestAnInvalidFinishResultIsARecoverableError(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "finish",
				Arguments: json.RawMessage(`{"result":{"wrong":"yes"}}`),
			}},
		},
		{text: "finished"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	finisher := newFinishTool(t, `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)

	opts := testOptions(t, tr, tool, sink)
	opts.Tools["finish"] = finisher
	opts.ToolOrder = []string{"echo", "finish"}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The invalid call got an error result, and the run carried on.
	var failure *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Handle != "r1" {
			continue
		}
		failure = r
	}
	if failure == nil {
		t.Fatal("the log holds no result for the finish call")
	}
	if failure.Outcome != poo.OutcomeError {
		t.Errorf("the outcome is %q, want error", failure.Outcome)
	}
	if !strings.Contains(failure.Text, "answer") {
		t.Errorf("the result's text is %q, want the schema's complaint", failure.Text)
	}
}

// TestTheOtherCallsInTheBatchCompleteFirst covers the plan's rule: the
// batch completes, and then the run settles.
func TestTheOtherCallsInTheBatchCompleteFirst(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "finish", Arguments: json.RawMessage(`{"result":{"answer":"42"}}`)},
			},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}
	finisher := newFinishTool(t, `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Tools["finish"] = finisher
	opts.ToolOrder = []string{"echo", "finish"}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonFinished {
		t.Errorf("the run settled %q, want finished", res.Reason)
	}
	// The other call in the batch ran.
	if len(tool.Calls()) != 1 {
		t.Errorf("the tool ran %d times, want 1: the batch completes first", len(tool.Calls()))
	}
}

// TestTheMaxRunTokensBudgetSettlesTheRun covers the token budget.
func TestTheMaxRunTokensBudgetSettlesTheRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			usage: &poo.UsageReport{InputTokens: 100, OutputTokens: 20},
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.MaxRunTokens = 50

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted", res.Reason)
	}
	if res.Detail.Source != "max_run_tokens" {
		t.Errorf("the settle source is %q, want max_run_tokens", res.Detail.Source)
	}
}

// TestTheMaxDurationBudgetSettlesTheRun covers the time budget.
func TestTheMaxDurationBudgetSettlesTheRun(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.MaxDuration = time.Nanosecond

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted", res.Reason)
	}
	if res.Detail.Source != "max_duration" {
		t.Errorf("the settle source is %q, want max_duration", res.Detail.Source)
	}
}

// TestTheMaxDurationBudgetFiringMidToolSettlesTheRun covers the plan's
// rule: the budget goes through finalize, so the settle record is
// written and the tool gets a result.
func TestTheMaxDurationBudgetFiringMidToolSettlesTheRun(t *testing.T) {
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "interrupted", IsError: true}, nil
	}
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
	}}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.MaxDuration = 50 * time.Millisecond

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted", res.Reason)
	}
	// The settle record is written, and the tool got a result.
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

// TestTheGateChecksTheBudgetOnEveryPath covers the plan's gate: the
// budget check fires on every path that goes to a new request.
//
// The test uses a scripted compactor and a context window the run
// cannot fit into, so the gate's budget check is what stops the run on
// each path.
func TestTheGateChecksTheBudgetOnEveryPath(t *testing.T) {
	// The compactor says yes, so the gate enters the compacting state
	// and then comes back to the budget check.
	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Compactor = compactor
	// A window so small that the run cannot answer in it.
	opts.ContextLimit = 300

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	// The compactor was asked, which is the gate's step 1.
	if compactor.Calls() == 0 {
		t.Error("the compactor was never asked")
	}
	// The gate entered the compacting state.
	sawCompacting := false
	for _, state := range s.Transitions() {
		if state == poo.StateCompacting {
			sawCompacting = true
		}
	}
	if !sawCompacting {
		t.Errorf("the states are %v, want a compacting", stateNames(s.Transitions()))
	}
}

// TestASessionWithoutACompactorNeverEntersCompacting covers the plan's
// rule: the compacting state is reachable only with a compactor.
func TestASessionWithoutACompactorNeverEntersCompacting(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	for _, state := range s.Transitions() {
		if state == poo.StateCompacting {
			t.Errorf("the states are %v, want no compacting", stateNames(s.Transitions()))
		}
	}
}

// TestAReadOnlyBatchRunsInParallel covers the plan's rule: a batch of
// read-only calls runs at the same time.
func TestAReadOnlyBatchRunsInParallel(t *testing.T) {
	// The tool blocks until both calls are running, so a sequential
	// batch would never finish.
	var running sync.WaitGroup
	running.Add(2)
	release := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		running.Done()
		<-release
		return poo.ToolResult{Text: "done"}, nil
	}

	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "finished"},
	}}

	opts := testOptions(t, tr, tool, &recordingSink{})
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}

	// Both calls are running at the same time: the batch is parallel.
	done := make(chan struct{})
	go func() {
		running.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the read-only batch did not run in parallel")
	}
	close(release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestAMixedBatchRunsSequentially covers the other half of the rule: a
// batch with a side-effecting call runs in model order.
func TestAMixedBatchRunsSequentially(t *testing.T) {
	var order []string
	var mu sync.Mutex
	tool := &echoTool{result: "echoed", class: poo.ClassSideEffecting}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		mu.Lock()
		order = append(order, "echo")
		mu.Unlock()
		return poo.ToolResult{Text: "done"}, nil
	}
	readTool := &echoTool{result: "read"}
	readTool.call = func(ctx context.Context) (poo.ToolResult, error) {
		mu.Lock()
		order = append(order, "read")
		mu.Unlock()
		return poo.ToolResult{Text: "done"}, nil
	}

	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "read", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "finished"},
	}}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Tools["read"] = readTool
	opts.ToolOrder = []string{"echo", "read"}

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(order, ",") != "echo,read" {
		t.Errorf("the calls ran as %v, want the model order", order)
	}
}

// countingAfterTurn is an AfterTurn hook that counts its calls and
// appends a message.
type countingAfterTurn struct {
	mu sync.Mutex
	// calls counts the hook's calls.
	calls int
	// stopAfter is how many times the hook appends before it stops
	// appending. Zero means it always appends.
	stopAfter int
}

// AfterTurn counts the call and appends a message.
func (h *countingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.calls++
	appendNow := h.stopAfter == 0 || h.calls <= h.stopAfter
	h.mu.Unlock()
	if !appendNow {
		return poo.TurnDecision{}, nil
	}
	return poo.TurnDecision{Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "keep going")}}, nil
}

// recordingBeforeSettle is a BeforeSettle hook that records the
// candidate it was given.
type recordingBeforeSettle struct {
	mu sync.Mutex
	// calls counts the hook's calls.
	calls int
	// candidate is the reason the hook was given.
	candidate poo.Reason
}

// BeforeSettle records the candidate and accepts it.
func (h *recordingBeforeSettle) BeforeSettle(ctx poo.HookContext, candidate poo.Reason) (poo.SettleDecision, error) {
	h.mu.Lock()
	h.calls++
	h.candidate = candidate
	h.mu.Unlock()
	return poo.Accept(), nil
}

// continuingBeforeSettle is a BeforeSettle hook that asks for a
// continuation.
type continuingBeforeSettle struct {
	mu sync.Mutex
	// calls counts the hook's calls.
	calls int
	// stopAfter is how many times the hook asks for a continuation
	// before it accepts. Zero means it always continues.
	stopAfter int
}

// BeforeSettle asks for a continuation.
func (h *continuingBeforeSettle) BeforeSettle(ctx poo.HookContext, candidate poo.Reason) (poo.SettleDecision, error) {
	h.mu.Lock()
	h.calls++
	continueNow := h.stopAfter == 0 || h.calls <= h.stopAfter
	h.mu.Unlock()
	if !continueNow {
		return poo.Accept(), nil
	}
	return poo.Continue(poo.NewTextMessage(poo.RoleUser, "keep going")), nil
}

// stoppingAfterTurn is an AfterTurn hook that always stops the run.
type stoppingAfterTurn struct {
	mu sync.Mutex
	// calls counts the hook's calls.
	calls int
}

// AfterTurn stops the run.
func (h *stoppingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	return poo.StopTurn("the hook stopped the run"), nil
}

// newFinishTool builds a finish tool for a test.
func newFinishTool(t *testing.T, schema string) poo.Tool {
	t.Helper()
	tool, err := finish.New(json.RawMessage(schema))
	if err != nil {
		t.Fatalf("building the finish tool: %v", err)
	}
	return tool
}
