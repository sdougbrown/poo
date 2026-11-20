package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/summary"
	"github.com/sdougbrown/poo/pootest"
)

// The endpoint's own refusal: a request the endpoint refuses for its length is
// compacted for once and sent again, because a smaller max_tokens cannot make
// a prompt smaller.

// refusalCompactionOptions is a session that summarizes, with a window wide
// enough that the fit rule never fires by itself: only the endpoint's refusal
// asks for the compaction.
func refusalCompactionOptions(t *testing.T, tr *pootest.Transport, sink poo.RecordSink) poo.Options {
	t.Helper()
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	// The tail is one token, so the oldest of two turns is the one the
	// summary replaces.
	opts.Compactor = summary.New(summary.Options{KeepRecentTokens: 1})
	return opts
}

// refusedTurns is the script of a run that does two turns and is then refused
// for the prompt's length.
func refusedTurns(prompt int) []fakeResponse {
	return []fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{err: contextLengthRejection("the prompt is too long", prompt)},
	}
}

// compactionRecords returns the compaction records the sink holds, in order.
func compactionRecords(sink *recordingSink) []*poo.CompactionRecord {
	var out []*poo.CompactionRecord
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.CompactionRecord); ok {
			out = append(out, c)
		}
	}
	return out
}

// TestARefusedRequestCompactsOnceAndIsSentAgain covers the rule: the endpoint
// refused the prompt's length, the run summarized the oldest turn, and the
// request went out again and was answered.
func TestARefusedRequestCompactsOnceAndIsSentAgain(t *testing.T) {
	turns := append(refusedTurns(0), fakeResponse{text: "## Goal\nfinish"}, fakeResponse{text: "the answer"})
	tr := scripted(turns)
	sink := &recordingSink{}
	s := newTestSession(t, refusalCompactionOptions(t, tr, sink))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	records := compactionRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d compactions, want 1", len(records))
	}
	if records[0].Strategy != summary.Strategy || records[0].Form != "replace" {
		t.Errorf("the compaction is a %q %q, want a %q replace",
			records[0].Strategy, records[0].Form, summary.Strategy)
	}
	// The request after the compaction carries the summary.
	requests := tr.Requests()
	last := requests[len(requests)-1]
	found := false
	for _, m := range last.Messages {
		if m.Origin == poo.OriginCompaction {
			found = true
		}
	}
	if !found {
		t.Error("the request after the compaction carries no summary")
	}
}

// TestARefusedRequestCompactsOnlyOnce covers the bound: a request refused
// again after its one compaction settles the run with the endpoint's refusal.
func TestARefusedRequestCompactsOnlyOnce(t *testing.T) {
	turns := append(refusedTurns(0), fakeResponse{text: "## Goal\nfinish"}, fakeResponse{err: contextLengthRejection("the prompt is still too long", 0)})
	tr := scripted(turns)
	sink := &recordingSink{}
	s := newTestSession(t, refusalCompactionOptions(t, tr, sink))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "provider:context_length" {
		t.Errorf("the settle source is %q, want provider:context_length", res.Detail.Source)
	}
	if got := len(compactionRecords(sink)); got != 1 {
		t.Errorf("the log holds %d compactions, want the one the first refusal bought", got)
	}
}

// TestARefusedRequestWithoutACompactorSettlesAsBefore covers the session with
// no compactor: nothing can shrink the prompt, so the refusal settles the run
// and no request follows it.
func TestARefusedRequestWithoutACompactorSettlesAsBefore(t *testing.T) {
	tr := scripted(refusedTurns(0))
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "provider:context_length" {
		t.Errorf("the settle source is %q, want provider:context_length", res.Detail.Source)
	}
	if got := len(tr.Requests()); got != 3 {
		t.Errorf("the transport got %d requests, want the three the script holds", got)
	}
}

// TestARefusedPromptCountCalibratesBeforeTheCompaction covers the count: the
// endpoint's own number is the prompt's size, so the compaction is sized
// against it rather than against the estimate the refusal proved wrong.
func TestARefusedPromptCountCalibratesBeforeTheCompaction(t *testing.T) {
	const prompt = 30000
	turns := append(refusedTurns(prompt), fakeResponse{text: "## Goal\nfinish"}, fakeResponse{text: "the answer"})
	tr := scripted(turns)
	sink := &recordingSink{}
	s := newTestSession(t, refusalCompactionOptions(t, tr, sink))

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	records := compactionRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d compactions, want 1", len(records))
	}
	if got := records[0].TokensBefore; got != prompt {
		t.Errorf("the compaction starts at %d tokens, want the endpoint's own %d", got, prompt)
	}
}

// TestARefusedNativeRequestCompactsAgainstItsOwnGuard covers the forced pass
// the native strategy cannot make for itself: the input the last compaction
// left is still short, so the strategy declines, and the endpoint's refusal
// compacts all the same.
func TestARefusedNativeRequestCompactsAgainstItsOwnGuard(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}, usage: &poo.UsageReport{InputTokens: 3600, OutputTokens: 5}},
		// The report after the compaction leaves the input short of the
		// window, which is what arms the strategy's guard...
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}, usage: &poo.UsageReport{InputTokens: 3300, OutputTokens: 5}},
		// ...and the endpoint then refuses the request the guard let out.
		{err: contextLengthRejection("the prompt is too long", 0)},
		{text: "the answer"},
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	s := newTestSession(t, nativeOptions(t, tr, "echoed", &recordingSink{}))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	if got := tr.Compactions(); got != 2 {
		t.Fatalf("the transport served %d native compactions, want the fit rule's and the refusal's", got)
	}
}
