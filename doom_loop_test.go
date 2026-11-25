package poo_test

import (
	"fmt"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
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
			{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"a","n":1}`)},
			{CallID: "c2", Name: "echo", Arguments: []byte(`{"n":1,"text":"a"}`)},
			{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"b","n":1}`)},
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
			{CallID: "c1", Name: "echo", Arguments: []byte(`{"path":"a"}`)},
			{CallID: "c2", Name: "other", Arguments: []byte(`{"text":"x"}`)},
			{CallID: "c3", Name: "echo", Arguments: []byte(`{"path":"a"}`)},
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

// pollWork is how many calls to another tool separate the polls in the
// stale-streak tests.
//
// It is more than the streak's window, so the calls it separates are not
// consecutive repeats.
const pollWork = 6

// oneCallTurns scripts one turn per call, each turn making exactly one
// call, so a call at index i has the handle r<i+1>.
func oneCallTurns(calls ...poo.ToolCall) *pootest.Transport {
	responses := make([]fakeResponse, 0, len(calls)+1)
	for _, c := range calls {
		responses = append(responses, fakeResponse{calls: []poo.ToolCall{c}})
	}
	responses = append(responses, fakeResponse{text: "done"})
	return scripted(responses)
}

// handleOf returns the handle of the call at index i of oneCallTurns.
func handleOf(i int) string {
	return fmt.Sprintf("r%d", i+1)
}

// TestAZeroArgumentPollDoesNotAccumulateAcrossOtherCalls covers the
// streak's window: a zero-argument status poll the run makes now and
// then, with real work between the polls, is not a loop. The counts
// start over once enough calls to other tools separate one poll from
// the last.
func TestAZeroArgumentPollDoesNotAccumulateAcrossOtherCalls(t *testing.T) {
	tool := &echoTool{result: "the effect"}
	other := &namedTool{name: "other", echoTool: echoTool{result: "the other effect"}}
	calls, polls := pollWithWork(t, 3, pollWork)
	opts := testOptions(t, oneCallTurns(calls...), tool, &recordingSink{})
	opts.Tools["other"] = other
	opts.ToolOrder = []string{"echo", "other"}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	for _, i := range polls {
		if got := s.RepeatCountsFor(handleOf(i)); got.Exact != 1 || got.Near != 1 {
			t.Errorf("the poll at %d has counts %+v, want exact 1 and near 1: the work between the polls is not a repeat", i, got)
		}
	}
}

// TestPollsMaxRepeatGapApartShareAStreak covers the window's near edge:
// when exactly maxRepeatGap calls to other tools separate two polls,
// the gap is not over the window, and the streak continues.
func TestPollsMaxRepeatGapApartShareAStreak(t *testing.T) {
	tool := &echoTool{result: "the effect"}
	other := &namedTool{name: "other", echoTool: echoTool{result: "the other effect"}}
	calls, polls := pollWithWork(t, 2, 4) // 4 is maxRepeatGap
	opts := testOptions(t, oneCallTurns(calls...), tool, &recordingSink{})
	opts.Tools["other"] = other
	opts.ToolOrder = []string{"echo", "other"}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	second := s.RepeatCountsFor(handleOf(polls[1]))
	if second.Exact != 2 {
		t.Errorf("the second poll's exact count is %d, want 2: a gap of exactly the window keeps the streak", second.Exact)
	}
}

// TestAPollOneCallPastTheWindowStartsOver covers the window's far edge:
// one more separating call puts the gap over the window, and the second
// poll starts a new streak.
func TestAPollOneCallPastTheWindowStartsOver(t *testing.T) {
	tool := &echoTool{result: "the effect"}
	other := &namedTool{name: "other", echoTool: echoTool{result: "the other effect"}}
	calls, polls := pollWithWork(t, 2, 5) // maxRepeatGap + 1
	opts := testOptions(t, oneCallTurns(calls...), tool, &recordingSink{})
	opts.Tools["other"] = other
	opts.ToolOrder = []string{"echo", "other"}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	second := s.RepeatCountsFor(handleOf(polls[1]))
	if second.Exact != 1 {
		t.Errorf("the second poll's exact count is %d, want 1: a gap over the window starts a new streak", second.Exact)
	}
}

// pollWithWork returns a zero-argument poll of one tool separated from
// the next poll by gap calls to another tool, and the indexes of the
// polls.
func pollWithWork(t *testing.T, polls, gap int) ([]poo.ToolCall, []int) {
	t.Helper()
	var calls []poo.ToolCall
	var indexes []int
	for i := 0; i < polls; i++ {
		if i > 0 {
			for j := 0; j < gap; j++ {
				// The arguments are distinct, so each of these calls
				// is a call of its own and never a loop.
				calls = append(calls, poo.ToolCall{
					CallID:    fmt.Sprintf("c%d", len(calls)),
					Name:      "other",
					Arguments: []byte(fmt.Sprintf(`{"text":"step %d %d"}`, i, j)),
				})
			}
		}
		calls = append(calls, poo.ToolCall{CallID: fmt.Sprintf("c%d", len(calls)), Name: "echo", Arguments: []byte(`{}`)})
		indexes = append(indexes, len(calls)-1)
	}
	return calls, indexes
}

// TestConsecutiveZeroArgumentPollsAreALoop covers the other side of the
// window: the same poll made three times in a row, with nothing in
// between, is the loop the counts are for.
func TestConsecutiveZeroArgumentPollsAreALoop(t *testing.T) {
	tool := &echoTool{result: "the effect"}
	calls := []poo.ToolCall{
		{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)},
		{CallID: "c2", Name: "echo", Arguments: []byte(`{}`)},
		{CallID: "c3", Name: "echo", Arguments: []byte(`{}`)},
	}
	s := newTestSession(t, testOptions(t, oneCallTurns(calls...), tool, &recordingSink{}))

	runSession(t, s, "start")

	if got := s.RepeatCountsFor(handleOf(2)); got.Exact != 3 {
		t.Errorf("the third poll's exact count is %d, want 3: the polls are consecutive", got.Exact)
	}
}

// TestAnInvalidCallDoesNotAccumulateAcrossOtherCalls covers the same
// window for a call that failed Prepare: a malformed call early in a run
// does not make a malformed call much later a loop.
func TestAnInvalidCallDoesNotAccumulateAcrossOtherCalls(t *testing.T) {
	tool := &echoTool{prepareErr: poo.InvalidArgsf("the arguments are not a call")}
	other := &namedTool{name: "other", echoTool: echoTool{result: "the other effect"}}
	calls, invalid := pollWithWork(t, 3, pollWork)
	opts := testOptions(t, oneCallTurns(calls...), tool, &recordingSink{})
	opts.Tools["other"] = other
	opts.ToolOrder = []string{"echo", "other"}
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	for _, i := range invalid {
		if got := s.RepeatCountsFor(handleOf(i)); got.Exact != 1 || got.Near != 1 {
			t.Errorf("the invalid call at %d has counts %+v, want exact 1 and near 1: the work between the calls is not a repeat", i, got)
		}
	}
}

// TestConsecutiveInvalidCallsAreALoop covers the other side of the
// window: the same malformed call made three times in a row is a loop,
// even when the model rewrites the arguments each time.
func TestConsecutiveInvalidCallsAreALoop(t *testing.T) {
	tool := &echoTool{prepareErr: poo.InvalidArgsf("the arguments are not a call")}
	calls := []poo.ToolCall{
		{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"a"}`)},
		{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"b"}`)},
		{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"c"}`)},
	}
	s := newTestSession(t, testOptions(t, oneCallTurns(calls...), tool, &recordingSink{}))

	runSession(t, s, "start")

	if got := s.RepeatCountsFor(handleOf(2)); got.Exact != 3 {
		t.Errorf("the third invalid call's exact count is %d, want 3: the calls are consecutive", got.Exact)
	}
}

// TestThePolicySeesTheCountsAndTheArguments covers what the policy slot
// is given: the counts of the call and the arguments the model wrote.
func TestThePolicySeesTheCountsAndTheArguments(t *testing.T) {
	policy := &recordingBeforeTool{name: "policy", decision: poo.Allow()}
	tool := &echoTool{result: "the effect"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"a"}`)},
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"a"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"a"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))

	runSession(t, s, "start")

	second := s.RepeatCountsFor("r2")
	if second.Exact != 2 || second.Near != 2 {
		t.Errorf("the second turn's call has counts %+v, want exact 2 and near 2", second)
	}
}
