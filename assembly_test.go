package poo_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// The assembly of streamed tool calls: the tests the loop's writes
// carried before the state machine replaced it, now pinned against the
// session. The rules are the reader contract's: fragments with an id
// rejoin by that id, a fragment without one continues the latest call
// at its index, a call at an index in use starts a new one, and a call
// keeps the id its first fragment carried.

// eventsTransport serves the scripted call response once, and a plain
// answer after it, so the run settles after the calls are executed.
type eventsTransport struct {
	mu     sync.Mutex
	served int
	events []poo.StreamItem
}

func (t *eventsTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	t.served++
	events := t.events
	if t.served > 1 {
		events = []poo.StreamItem{
			{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "the answer"}},
			{Kind: "finish", FinishReason: poo.FinishStop},
		}
	}
	t.mu.Unlock()
	return &fakeReader{events: events}, poo.Rendered{API: poo.APIChatCompletions, Messages: len(req.Messages)}, nil
}

func (t *eventsTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// callEvents builds the events for one streamed call fragment.
func callEvents(index int, id, name string, arguments string, complete bool) []poo.StreamItem {
	ev := poo.StreamItem{Kind: "tool_call", ToolCall: &poo.ToolCallDelta{
		Index:     index,
		ID:        id,
		Name:      name,
		Arguments: json.RawMessage(arguments),
		Complete:  complete,
	}}
	return []poo.StreamItem{ev}
}

// finishEvents closes a response the way a provider does.
func finishEvents() []poo.StreamItem {
	return []poo.StreamItem{
		{Kind: "finish", FinishReason: poo.FinishToolCalls},
		{Kind: "usage", Usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 10}},
	}
}

// runOnce drives one run whose single response is the given events.
func runOnce(t *testing.T, events []poo.StreamItem) []poo.ToolCallRecord {
	t.Helper()
	tr := &eventsTransport{events: events}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	calls := make([]poo.ToolCallRecord, 0)
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.ToolCallRecord); ok {
			calls = append(calls, *c)
		}
	}
	return calls
}

func toolCallEvents(fragments ...[]poo.StreamItem) []poo.StreamItem {
	var out []poo.StreamItem
	for _, f := range fragments {
		out = append(out, f...)
	}
	return append(out, finishEvents()...)
}

// TestTwoCallsAtOneIndexAreTwoCalls covers the plan's rule that a
// provider's index is grouping, not identity: two whole calls that share
// one index are two calls, each with its own arguments and result.
func TestTwoCallsAtOneIndexAreTwoCalls(t *testing.T) {
	events := toolCallEvents(
		callEvents(0, "a", "echo", `{"text":"one"}`, true),
		callEvents(0, "b", "echo", `{"text":"two"}`, true),
	)
	calls := runOnce(t, events)
	if len(calls) != 2 {
		t.Fatalf("the log holds %d tool.call records, want 2", len(calls))
	}
	args := make(map[string]bool)
	for _, c := range calls {
		args[string(c.Arguments)] = true
	}
	if !args[`{"text":"one"}`] || !args[`{"text":"two"}`] {
		t.Errorf("the calls' arguments are %v, want one per call", args)
	}
}

// TestInterleavedFragmentsAtOneIndexRejoinTheirCalls covers the plan's
// rule that fragments rejoin their call by id, in any order: a fragment
// with an id already seen at its index continues that call, a fragment
// with a new id starts a new one, and a fragment without an id continues
// the latest call at the index.
func TestInterleavedFragmentsAtOneIndexRejoinTheirCalls(t *testing.T) {
	events := toolCallEvents(
		callEvents(0, "a", "echo", `{"text":"one`, false),
		callEvents(0, "b", "echo", `{"text":"two`, false),
		// No id: the latest call at the index is b.
		callEvents(0, "", "", `"}`, false),
		// a's id is seen, so this continues a.
		callEvents(0, "a", "", `"}`, true),
	)
	calls := runOnce(t, events)
	if len(calls) != 2 {
		t.Fatalf("the log holds %d tool.call records, want 2", len(calls))
	}
	args := make(map[string]bool)
	for _, c := range calls {
		args[string(c.Arguments)] = true
	}
	if !args[`{"text":"one"}`] {
		t.Errorf("the first call's arguments are missing: %v", args)
	}
	if !args[`{"text":"two"}`] {
		t.Errorf("the second call's arguments are missing: %v", args)
	}
}

// TestACallAtAHighProviderIndexIsNotOverwrittenBySplitCalls covers the
// plan's rule that a call's fragments keep their own call, whatever the
// provider's numbering: a call at a high index survives two whole calls
// that share a lower index.
func TestACallAtAHighProviderIndexIsNotOverwrittenBySplitCalls(t *testing.T) {
	events := toolCallEvents(
		callEvents(5, "high", "echo", `{"text":"high`, false),
		callEvents(0, "a", "echo", `{"text":"one"}`, true),
		callEvents(0, "b", "echo", `{"text":"two"}`, true),
		// The high call's continuation.
		callEvents(5, "", "", `"}`, true),
	)
	calls := runOnce(t, events)
	if len(calls) != 3 {
		t.Fatalf("the log holds %d tool.call records, want 3", len(calls))
	}
	high := false
	for _, c := range calls {
		if string(c.Arguments) == `{"text":"high"}` {
			high = true
		}
	}
	if !high {
		t.Errorf("the high-index call's fragments did not rejoin: %v", calls)
	}
}

// TestOnlyTheFirstFragmentCarriesTheID covers the plan's rule that a
// call's id is fixed by its first fragment: the fragments that follow,
// with no id of their own, extend that call and do not rename it.
func TestOnlyTheFirstFragmentCarriesTheID(t *testing.T) {
	events := toolCallEvents(
		callEvents(0, "a", "echo", `{"text":"one`, false),
		callEvents(0, "", "", `"}`, true),
	)
	calls := runOnce(t, events)
	if len(calls) != 1 {
		t.Fatalf("the log holds %d tool.call records, want 1", len(calls))
	}
	if string(calls[0].Arguments) != `{"text":"one"}` {
		t.Errorf("the call's arguments are %q, want the fragments joined", calls[0].Arguments)
	}
}
