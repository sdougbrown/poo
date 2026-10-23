package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// The doom loop's detection is the core's: two counters of consecutive
// repeats, an exact one and a near one. The policy decides what a loop
// does; the core only counts.

// TestTheExactKeyIsTheCanonicalCall covers the exact key: the tool plus
// its canonical input, so two calls that spell the same arguments in a
// different order share a count, and a call with other arguments does
// not.
func TestTheExactKeyIsTheCanonicalCall(t *testing.T) {
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"a","n":1}`)},
			{ID: "c2", Name: "echo", Arguments: []byte(`{"n":1,"text":"a"}`)},
			{ID: "c3", Name: "echo", Arguments: []byte(`{"text":"b","n":1}`)},
		}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	first := s.RepeatCountsFor("r1")
	if first.Exact != 1 {
		t.Errorf("the first call's exact count is %d, want 1", first.Exact)
	}
	second := s.RepeatCountsFor("r2")
	if second.Exact != 2 {
		t.Errorf("the second call's exact count is %d, want 2: the same arguments share a key", second.Exact)
	}
	third := s.RepeatCountsFor("r3")
	if third.Exact != 1 {
		t.Errorf("the third call's exact count is %d, want 1: other arguments are another key", third.Exact)
	}
}

// TestAnotherToolDoesNotBreakTheStreak covers the word "consecutive": a
// call to another tool in the same batch does not reset the count.
func TestAnotherToolDoesNotBreakTheStreak(t *testing.T) {
	tool := &echoTool{result: "the effect", primaryArg: "path"}
	other := &namedTool{name: "other", echoTool: echoTool{result: "the other effect"}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{ID: "c1", Name: "echo", Arguments: []byte(`{"path":"a"}`)},
			{ID: "c2", Name: "other", Arguments: []byte(`{"text":"x"}`)},
			{ID: "c3", Name: "echo", Arguments: []byte(`{"path":"a"}`)},
		}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Tools["other"] = other
	opts.ToolOrder = []string{"echo", "other"}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	third := s.RepeatCountsFor("r3")
	if third.Exact != 2 {
		t.Errorf("the third call's exact count is %d, want 2: the other tool does not break the streak", third.Exact)
	}
	if third.Near != 2 {
		t.Errorf("the third call's near count is %d, want 2: the other tool does not break the streak", third.Near)
	}
}

// TestThePolicySeesTheCountsAndTheArguments covers what the policy slot
// is given: the counts of the call and the arguments the model wrote.
func TestThePolicySeesTheCountsAndTheArguments(t *testing.T) {
	policy := &recordingBeforeTool{name: "policy", decision: poo.Allow()}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"a"}`)},
		}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Policy = policy
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	if policy.Calls() != 1 {
		t.Fatalf("the policy slot ran %d time(s), want 1", policy.Calls())
	}
	if got := string(policy.lastArguments()); got != `{"text":"a"}` {
		t.Errorf("the policy saw the arguments %q, want the model's own arguments", got)
	}
	if got := policy.lastCounts().Exact; got != 1 {
		t.Errorf("the policy saw the exact count %d, want 1", got)
	}
}

// TestTheStreakContinuesAcrossTurns covers "consecutive within a run":
// the same call in the next turn continues the streak, because the
// counters belong to the run and not to one batch.
func TestTheStreakContinuesAcrossTurns(t *testing.T) {
	tool := &echoTool{result: "the effect", primaryArg: "text"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"a"}`)}}},
		{calls: []poo.ToolCall{{ID: "c2", Name: "echo", Arguments: []byte(`{"text":"a"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))

	runSession(t, s, "start")

	second := s.RepeatCountsFor("r2")
	if second.Exact != 2 || second.Near != 2 {
		t.Errorf("the second turn's call has counts %+v, want exact 2 and near 2", second)
	}
}
