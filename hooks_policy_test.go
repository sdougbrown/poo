package poo_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The before-tool arbitration is the plan's: the policy slot runs
// first, a refusal wins over everything, and the first result in
// registration order becomes the call's result. The hooks after that
// result still run, and only to look for a refusal.

// recordingBeforeTool is a before-tool hook that records what it was
// asked and answers with a fixed decision.
type recordingBeforeTool struct {
	mu sync.Mutex
	// calls counts the calls the hook saw.
	calls int
	// decision is what the hook answers.
	decision poo.ToolDecision
	// order is the list the hooks append their names to, so a test can
	// see the order they ran in.
	order *[]string
	// name is the hook's name.
	name string
	// lastInfo is the call the hook was asked about last.
	lastInfo poo.ToolCallInfo
}

// BeforeTool records the call and answers.
func (h *recordingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	h.mu.Lock()
	h.calls++
	h.lastInfo = info
	decision := h.decision
	h.mu.Unlock()
	if h.order != nil {
		*h.order = append(*h.order, h.name)
	}
	return decision, nil
}

// lastArguments returns the arguments the hook saw last.
func (h *recordingBeforeTool) lastArguments() json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastInfo.Arguments
}

// lastCounts returns the repeat counts the hook saw last.
func (h *recordingBeforeTool) lastCounts() poo.RepeatCounts {
	h.mu.Lock()
	defer h.mu.Unlock()
	return poo.RepeatCounts{Exact: h.lastInfo.RepeatCount, Near: h.lastInfo.NearRepeatCount}
}

// Calls returns how many calls the hook saw.
func (h *recordingBeforeTool) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// slowPolicy is a policy slot that takes its time.
//
// The slot is exempt from the hook deadline, so a slot that thinks for
// longer than the deadline still answers.
type slowPolicy struct {
	// delay is how long the slot takes to answer.
	delay time.Duration
	// cancelled reports that the slot's context ended while it was
	// thinking.
	cancelled bool
}

// BeforeTool waits and then allows the call.
func (h *slowPolicy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	<-time.After(h.delay)
	if ctx.Err() != nil {
		h.cancelled = true
	}
	return poo.Allow(), nil
}

// TestThePolicySlotRunsFirst covers the plan's order: the policy slot is
// asked before the consumer's own hooks, and its refusal ends the
// evaluation.
func TestThePolicySlotRunsFirst(t *testing.T) {
	order := &[]string{}
	policy := &recordingBeforeTool{name: "policy", order: order, decision: poo.Deny("the policy says no")}
	consumer := &recordingBeforeTool{name: "consumer", order: order, decision: poo.Allow()}

	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.Policy = policy
	opts.BeforeTool = []poo.BeforeToolHook{consumer}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if got := len(*order); got != 1 || (*order)[0] != "policy" {
		t.Fatalf("the hooks ran in the order %v, want the policy slot alone", *order)
	}
	if consumer.Calls() != 0 {
		t.Errorf("the consumer hook ran %d time(s) after the policy refused, want 0", consumer.Calls())
	}
	if got := len(tool.Calls()); got != 0 {
		t.Errorf("the tool ran %d time(s) after the policy refused, want 0", got)
	}
	result := lastToolResult(t, tr)
	if want := poo.HandleLine("r1", "echo") + "\n" + "the policy says no"; result != want {
		t.Errorf("the model saw %q, want %q", result, want)
	}
}

// TestTheFirstResultBecomesTheCallsResult covers the plan's rule: the
// first result in registration order becomes the call's result, and the
// hooks after it still run.
func TestTheFirstResultBecomesTheCallsResult(t *testing.T) {
	order := &[]string{}
	first := &recordingBeforeTool{name: "first", order: order, decision: poo.ShortCircuit(poo.ToolResult{Text: "the first answer"})}
	second := &recordingBeforeTool{name: "second", order: order, decision: poo.ShortCircuit(poo.ToolResult{Text: "the second answer"})}

	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeTool = []poo.BeforeToolHook{first, second}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if got := len(*order); got != 2 {
		t.Fatalf("the hooks ran in the order %v, want both of them", *order)
	}
	if got := len(tool.Calls()); got != 0 {
		t.Errorf("the tool ran %d time(s) after a hook answered the call, want 0", got)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "the first answer"; lastToolResult(t, tr) != want {
		t.Errorf("the model saw %q, want %q", lastToolResult(t, tr), want)
	}
}

// TestARefusalWinsOverAResult covers the plan's rule: a refusal stops
// the evaluation, even when an earlier hook answered the call.
func TestARefusalWinsOverAResult(t *testing.T) {
	first := &recordingBeforeTool{name: "first", decision: poo.ShortCircuit(poo.ToolResult{Text: "the answer"})}
	second := &recordingBeforeTool{name: "second", decision: poo.Deny("not this one")}

	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeTool = []poo.BeforeToolHook{first, second}
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if got := len(tool.Calls()); got != 0 {
		t.Errorf("the tool ran %d time(s) after a hook refused the call, want 0", got)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "not this one"; lastToolResult(t, tr) != want {
		t.Errorf("the model saw %q, want %q", lastToolResult(t, tr), want)
	}
}

// TestThePolicySlotIsNotSubjectToTheHookDeadline covers the plan's
// rule: the policy slot is exempt from the hook deadline, so a slot
// that takes longer than the deadline still answers and the call runs.
func TestThePolicySlotIsNotSubjectToTheHookDeadline(t *testing.T) {
	policy := &slowPolicy{delay: 150 * time.Millisecond}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.HookTimeout = 20 * time.Millisecond
	opts.Policy = policy
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if policy.cancelled {
		t.Error("the policy slot's context ended, want it to be exempt from the hook deadline")
	}
	if got := len(tool.Calls()); got != 1 {
		t.Errorf("the tool ran %d time(s), want 1", got)
	}
}

// TestThePolicySlotsResultIsIgnored covers the policy slot's own rule:
// the slot decides whether a call runs, not what it returns.
func TestThePolicySlotsResultIsIgnored(t *testing.T) {
	policy := &recordingBeforeTool{name: "policy", decision: poo.ShortCircuit(poo.ToolResult{Text: "the policy's answer"})}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Policy = policy
	s := newTestSession(t, opts)

	runSession(t, s, "start")
	if got := len(tool.Calls()); got != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the policy slot's result is not the call's result", got)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "the effect"; lastToolResult(t, tr) != want {
		t.Errorf("the model saw %q, want %q", lastToolResult(t, tr), want)
	}
}

// TestAHookThatMissesItsDeadlineSettlesTheRun covers the plan's rule:
// an executable hook that misses its deadline settles the run with
// hook_error.
func TestAHookThatMissesItsDeadlineSettlesTheRun(t *testing.T) {
	stalling := &stallingBeforeTool{}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.HookTimeout = 30 * time.Millisecond
	opts.BeforeTool = []poo.BeforeToolHook{stalling}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonHookError {
		t.Fatalf("the run settled %q, want hook_error", res.Reason)
	}
	if res.Detail.Source != "before_tool:stallingBeforeTool" {
		t.Errorf("the settle detail's source is %q, want the hook's name", res.Detail.Source)
	}
}

// stallingBeforeTool is a before-tool hook that never answers.
type stallingBeforeTool struct{}

// BeforeTool waits for its context to end.
func (h *stallingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	<-ctx.Done()
	return poo.Allow(), nil
}

// lastToolResult returns the text of the tool result the model saw in
// the last request.
func lastToolResult(t *testing.T, tr *pootest.Transport) string {
	t.Helper()
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	messages := requests[len(requests)-1].Messages
	for i := len(messages) - 1; i >= 0; i-- {
		for _, c := range messages[i].Content {
			if c.Kind == poo.ContentToolResult {
				return c.Text
			}
		}
	}
	t.Fatalf("the last request holds no tool result: %+v", messages)
	return ""
}
