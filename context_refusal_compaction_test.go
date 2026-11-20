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

// endedTurns checks the run's turn events: every turn that started ended, in
// order, and no turn started while another was still running.
//
// The turns that ended are returned, so a caller can say which turn each one
// was.
func endedTurns(t *testing.T, s *poo.Session) []int {
	t.Helper()
	var ended []int
	running := 0
	for _, ev := range s.Events() {
		turn, _ := ev.Payload["turn"].(int)
		switch ev.Name {
		case poo.EventTurnStarted:
			if running != 0 {
				t.Errorf("the turn %d started while the turn %d had not ended", turn, running)
			}
			running = turn
		case poo.EventTurnEnded:
			if running == 0 {
				t.Errorf("the turn %d ended without starting", turn)
			} else if turn != running {
				t.Errorf("the turn %d ended, but the turn %d was the one running", turn, running)
			}
			running = 0
			ended = append(ended, turn)
		}
	}
	if running != 0 {
		t.Errorf("the turn %d started and never ended", running)
	}
	return ended
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
	// The refused turn ended too: a client that saw it start sees it end, and
	// the turn after it is the one the compaction sent.
	ended := endedTurns(t, s)
	if len(ended) != 4 || ended[2] != 3 {
		t.Fatalf("the turns that ended are %v, want the four turns with the refused turn 3 among them", ended)
	}
	records := compactionRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d compactions, want 1", len(records))
	}
	if records[0].Strategy != summary.Strategy || records[0].Form != "replace" {
		t.Errorf("the compaction is a %q %q, want a %q replace",
			records[0].Strategy, records[0].Form, summary.Strategy)
	}
	// The refused turn carries no messages, so the cut stops at the turn
	// before it: a range that reached into an empty turn would name a turn
	// the replacement cannot carry.
	if cut := records[0].Cut; cut == nil || *cut != (poo.TurnRange{First: 1, Last: 1}) {
		t.Errorf("the compaction covers %v, want the turn 1 alone", cut)
	}
	// The empty turn keeps its span: the view still knows it happened.
	refused := false
	for _, turn := range poo.SessionView(s).Turns() {
		if turn.TurnIndex != 3 {
			continue
		}
		refused = true
		if turn.Removed {
			t.Error("the refused turn was removed from the view")
		}
		if turn.Start != turn.End {
			t.Errorf("the refused turn holds the messages %d to %d, want none", turn.Start, turn.End)
		}
	}
	if !refused {
		t.Error("the view holds no turn for the refused request")
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
