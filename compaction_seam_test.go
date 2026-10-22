package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The compaction seam: a compaction is validated, applied to the
// model's view and recorded, and the store keeps every original
// record.

// compactionCompactor is a compactor the test drives.
//
// It answers the decision the test scripted, and it records the views
// it saw, so a test can look at what the compactor was given.
type compactionCompactor struct {
	mu sync.Mutex
	// should answers ShouldCompact.
	should func(view poo.View, budget poo.Budget) bool
	// compaction answers Compact.
	compaction func(view poo.View) (poo.Compaction, error)
	// views holds the views the compactor saw, in order.
	views []poo.View
	// calls counts the compactions the compactor served.
	calls int
}

// ShouldCompact answers the scripted decision.
func (c *compactionCompactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.should == nil {
		return true
	}
	return c.should(view, budget)
}

// Compact answers the scripted compaction.
func (c *compactionCompactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	c.mu.Lock()
	c.calls++
	c.views = append(c.views, view)
	answer := c.compaction
	c.mu.Unlock()
	if answer == nil {
		return poo.Compaction{}, nil
	}
	return answer(view)
}

// Calls returns how many times the compactor compacted.
func (c *compactionCompactor) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// View returns the view the compactor saw on its last call.
func (c *compactionCompactor) View() poo.View {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.views) == 0 {
		return nil
	}
	return c.views[len(c.views)-1]
}

// afterTurns makes a compactor answer "yes" once the view holds the
// given number of turns.
//
// A compaction that replaces a range of turns needs the turns to
// exist, and the gate is only reached after a turn that did something.
func afterTurns(n int) func(poo.View, poo.Budget) bool {
	return func(view poo.View, budget poo.Budget) bool {
		return len(view.Turns()) >= n
	}
}

// TestTheReplacementIsAppliedAndTheOriginalsAreKept covers the plan's
// rule: the view changes, and the store keeps every original record.
func TestTheReplacementIsAppliedAndTheOriginalsAreKept(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	// The second request carries the replacement and not the turn it
	// replaced.
	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	found := false
	for _, m := range second {
		if m.Origin != poo.OriginCompaction {
			continue
		}
		found = true
		if m.Role != poo.RoleUser {
			t.Errorf("the replacement's role is %q, want user", m.Role)
		}
		if m.Text() != "the summary" {
			t.Errorf("the replacement holds %q, want the summary", m.Text())
		}
	}
	if !found {
		t.Errorf("the second request holds no compaction message: %+v", second)
	}
	for _, m := range second {
		if strings.Contains(m.Text(), "echoed") {
			t.Errorf("the second request still holds the replaced turn: %q", m.Text())
		}
	}
	// The store kept the original records: the result the compaction
	// replaced is still in the log.
	kept := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		kept = true
		if !strings.Contains(r.Text, "echoed") {
			t.Errorf("the log's result holds %q, want the tool's own output", r.Text)
		}
	}
	if !kept {
		t.Error("the log holds no tool result record: the compaction took the original")
	}
}

// TestAnEmptyCompactionIsRejected covers the plan's rule: a compaction
// carries exactly one form, and a compaction that carries none is
// rejected.
func TestAnEmptyCompactionIsRejected(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "empty"}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: a rejected compaction is not a failure below the hard limit", res.Reason)
	}
	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
	// The run carried on and made its second request.
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2", got)
	}
	// The compaction's end event says why it failed.
	found := false
	for _, ev := range s.Events() {
		if ev.Name != poo.EventCompactionEnded {
			continue
		}
		if msg, ok := ev.Payload["error"].(string); ok && msg != "" {
			found = true
		}
	}
	if !found {
		t.Error("the compaction's end event carries no error")
	}
}

// TestANonContiguousCutIsRejected covers the plan's rule: a
// replacement covers a contiguous range of completed turns, and a cut
// that names turns the session does not have is rejected.
func TestANonContiguousCutIsRejected(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					// The session has one turn, and the cut names
					// the turns four to five.
					Cut:      poo.TurnRange{First: 4, Last: 5},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
}

// TestACutOverARemovedTurnIsRejected covers the plan's rule: a range
// that reaches over a turn a compaction removed is not contiguous, and
// the core rejects it.
func TestACutOverARemovedTurnIsRejected(t *testing.T) {
	// The first compaction replaces turn one, and the second one asks
	// for a range that reaches over it.
	calls := 0
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			calls++
			if calls == 1 {
				return poo.Compaction{
					Strategy: "summarize",
					Replace: &poo.RangeReplacement{
						Cut:      poo.TurnRange{First: 1, Last: 1},
						Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
					},
				}, nil
			}
			// The range reaches over the turn the first compaction
			// removed.
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 2},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the second summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{ID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	if calls < 2 {
		t.Fatalf("the compactor ran %d time(s), want 2", calls)
	}
	// Only the first compaction was applied: the second one reached
	// over a turn that is gone.
	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
}

// TestTheRewriteReplacesTheResultAndKeepsThePairing covers the plan's
// rule: a rewrite changes the content of a tool result, the call and
// the result stay paired, and the handle and the compacted marker are
// there.
func TestTheRewriteReplacesTheResultAndKeepsThePairing(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "rewrite",
				Rewrite: []poo.ToolResultRewrite{{
					Handle:  "r1",
					Content: poo.CompactedHandleLine("r1", "echo") + "\nthe shorter result",
				}},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "the long result"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	var result *poo.Message
	for i := range reqs[1].Messages {
		m := reqs[1].Messages[i]
		if m.Role == poo.RoleTool {
			result = &reqs[1].Messages[i]
		}
	}
	if result == nil {
		t.Fatal("the second request holds no tool result")
	}
	if result.Content[0].Text != poo.CompactedHandleLine("r1", "echo")+"\nthe shorter result" {
		t.Errorf("the rewritten result holds %q, want the compacted marker and the new content", result.Content[0].Text)
	}
	// The call and the result are still paired: the result answers the
	// call the assistant made.
	if result.Content[0].ToolCallID != "c1" {
		t.Errorf("the result's call id is %q, want c1", result.Content[0].ToolCallID)
	}
	var call *poo.Message
	for i := range reqs[1].Messages {
		if reqs[1].Messages[i].Role == poo.RoleAssistant {
			call = &reqs[1].Messages[i]
		}
	}
	if call == nil {
		t.Fatal("the second request holds no assistant message")
	}
	if len(call.ToolCalls()) != 1 {
		t.Errorf("the assistant message holds %d tool calls, want 1", len(call.ToolCalls()))
	}
}

// TestARewriteWithoutTheMarkerIsRejected covers the plan's rule: a
// rewrite keeps the handle and the compacted marker, and a rewrite
// without the marker is rejected.
func TestARewriteWithoutTheMarkerIsRejected(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "rewrite",
				Rewrite: []poo.ToolResultRewrite{{
					Handle:  "r1",
					Content: "the shorter result",
				}},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "the long result"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
	// The view still holds the original result.
	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	found := false
	for _, m := range reqs[1].Messages {
		if m.Role != poo.RoleTool {
			continue
		}
		found = true
		if !strings.Contains(m.Content[0].Text, "the long result") {
			t.Errorf("the second request holds %q, want the original result", m.Content[0].Text)
		}
	}
	if !found {
		t.Error("the second request holds no tool result")
	}
}

// TestAWindowFromAnotherTransportIsRejected covers the plan's rule: a
// provider window is the provider's own state, so it is accepted only
// when it came from the session's own transport.
func TestAWindowFromAnotherTransportIsRejected(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "window",
				Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
					Kind: poo.ContentProvider,
					Provider: &poo.ProviderBlock{
						API:      poo.APIResponses,
						Type:     "compaction",
						Data:     []byte(`{"encrypted_content":"abc"}`),
						Required: true,
					},
				}}},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
}

// TestTheWindowBecomesTheViewPlusTheMarker covers the plan's rule: a
// provider window becomes the view, and the marker message says so.
func TestTheWindowBecomesTheViewPlusTheMarker(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind: poo.ContentProvider,
		Provider: &poo.ProviderBlock{
			API:      poo.APIChatCompletions,
			Type:     "compaction",
			Data:     []byte(`{"encrypted_content":"abc"}`),
			Required: true,
		},
	}}})
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			window, err := tr.Compact(context.Background(), poo.Rendered{})
			if err != nil {
				return poo.Compaction{}, err
			}
			return poo.Compaction{Strategy: "window", Window: &window}, nil
		},
	}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	if len(second) != 2 {
		t.Fatalf("the second request holds %d messages, want the window and the marker", len(second))
	}
	if second[0].Content[0].Provider == nil {
		t.Error("the first message holds no provider block")
	}
	if second[1].Text() != poo.CompactionMarkerText {
		t.Errorf("the second message holds %q, want the marker", second[1].Text())
	}
	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
}

// TestTheEndSettlesWithTheHandoff covers the plan's rule: a
// compaction that refuses to compact ends the run with the handoff it
// wrote, and the view is unchanged.
func TestTheEndSettlesWithTheHandoff(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "handoff",
				End:      &poo.EndRun{Handoff: "the handoff note"},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Handoff != "the handoff note" {
		t.Errorf("the settle detail's handoff is %q, want the note", res.Detail.Handoff)
	}
	if res.Detail.Source != "compactor:handoff" {
		t.Errorf("the settle detail's source is %q, want the compactor's", res.Detail.Source)
	}
	// The handoff is recorded.
	handoffs := 0
	for _, rec := range sink.allRecords() {
		if h, ok := rec.(*poo.HandoffRecord); ok {
			handoffs++
			if h.Handoff != "the handoff note" {
				t.Errorf("the handoff record holds %q, want the note", h.Handoff)
			}
		}
	}
	if handoffs != 1 {
		t.Errorf("the log holds %d handoff records, want 1", handoffs)
	}
	// The view is unchanged: the run made no request after the
	// compaction.
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want 1: the view is unchanged", got)
	}
}

// TestTheBudgetIsRebasedOntoTheNewView covers the plan's rule: a view
// over the hard ratio that a compaction brings below it goes on to
// request, and it does not settle context_exhausted.
func TestTheBudgetIsRebasedOntoTheNewView(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	// The first answer is long enough to fill the window.
	long := strings.Repeat("x", 8000)
	tr := scripted([]fakeResponse{
		{text: long, calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	opts.ContextLimit = 4000
	opts.HardRatio = 0.5
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn: the compaction brought the view below the hard ratio", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2: the run went on to request", got)
	}
}

// TestTheHardLimitSettlesWithoutACompaction covers the plan's rule
// beside the rebase: a view over the hard ratio that no compaction
// brings below it settles context_exhausted.
func TestTheHardLimitSettlesWithoutACompaction(t *testing.T) {
	long := strings.Repeat("x", 8000)
	tr := scripted([]fakeResponse{
		{text: long, calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.ContextLimit = 4000
	opts.HardRatio = 0.5
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "hard_limit" {
		t.Errorf("the settle detail's source is %q, want hard_limit", res.Detail.Source)
	}
}

// TestTheCompactorsFailureBelowTheHardLimitContinues covers the plan's
// failure row: below the hard limit the run continues uncompacted.
func TestTheCompactorsFailureBelowTheHardLimitContinues(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{}, errors.New("the compaction failed")
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if got := len(tr.Requests()); got != 2 {
		t.Errorf("the transport got %d requests, want 2: the run continued", got)
	}
}

// TestTheCompactorsFailureAtTheHardLimitSettles covers the plan's
// failure row: at or above the hard limit a compactor that failed
// settles the run as context_exhausted.
func TestTheCompactorsFailureAtTheHardLimitSettles(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{}, errors.New("the compaction failed")
		},
	}
	long := strings.Repeat("x", 8000)
	tr := scripted([]fakeResponse{
		{text: long, calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	opts.ContextLimit = 4000
	opts.HardRatio = 0.5
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "compaction_failed" {
		t.Errorf("the settle detail's source is %q, want compaction_failed", res.Detail.Source)
	}
}

// TestTheCompactionRecordCarriesTheCutAndTheReplacement covers the
// plan's record: it names the cut and the replacement, and its token
// estimates are the ones of the view before and after the compaction.
func TestTheCompactionRecordCarriesTheCutAndTheReplacement(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	var rec *poo.CompactionRecord
	for _, r := range sink.allRecords() {
		if c, ok := r.(*poo.CompactionRecord); ok {
			rec = c
		}
	}
	if rec == nil {
		t.Fatal("the log holds no compaction record")
	}
	if rec.Cut == nil || *rec.Cut != (poo.TurnRange{First: 1, Last: 1}) {
		t.Errorf("the compaction record's cut is %+v, want the turn 1", rec.Cut)
	}
	if len(rec.Replacement) != 1 || rec.Replacement[0].Text() != "the summary" {
		t.Errorf("the compaction record's replacement is %+v, want the summary", rec.Replacement)
	}
	if rec.TokensAfter >= rec.TokensBefore {
		t.Errorf("the compaction record's tokens_after is %d, want less than the %d before it",
			rec.TokensAfter, rec.TokensBefore)
	}
	if rec.Compaction.Strategy != "summarize" {
		t.Errorf("the compaction record's strategy is %q, want summarize", rec.Compaction.Strategy)
	}
}

// TestTheHandlesNumberTheResultsAndGrowAcrossRuns covers the plan's
// rule: every tool result starts with its handle line, and the number
// grows across the runs of a session.
func TestTheHandlesNumberTheResultsAndGrowAcrossRuns(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
		{calls: []poo.ToolCall{{ID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	s := newTestSession(t, opts)

	runSession(t, s, "the first run")
	runSession(t, s, "the second run")

	var handles []string
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		handles = append(handles, r.Handle)
		if !strings.HasPrefix(r.Text, "["+r.Handle+" · echo]") {
			t.Errorf("the result %q holds %q, want the handle line", r.Handle, r.Text)
		}
	}
	if len(handles) != 2 {
		t.Fatalf("the log holds %d results, want 2", len(handles))
	}
	if handles[0] != "r1" || handles[1] != "r2" {
		t.Errorf("the handles are %v, want r1 then r2", handles)
	}
}

// TestAHandleSurvivesTheCompaction covers the plan's rule: a handle
// names a call for the life of the session, so a compaction does not
// take it away.
func TestAHandleSurvivesTheCompaction(t *testing.T) {
	var second poo.View
	calls := 0
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.Budget) bool {
			return len(view.Turns()) >= 1
		},
		compaction: func(view poo.View) (poo.Compaction, error) {
			calls++
			if calls == 1 {
				// The first compaction replaces the turn that made
				// the call.
				return poo.Compaction{
					Strategy: "summarize",
					Replace: &poo.RangeReplacement{
						Cut:      poo.TurnRange{First: 1, Last: 1},
						Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
					},
				}, nil
			}
			// The second compaction sees the session after the first
			// one, and the handle is still there.
			second = view
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 2, Last: 2},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the second summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{ID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")

	if second == nil {
		t.Fatal("the compactor compacted once, want twice")
	}
	result, ok := second.Result("r1")
	if !ok {
		t.Fatal("the view holds no result for r1 after the compaction")
	}
	if !strings.Contains(result.Text, "echoed") {
		t.Errorf("the result for r1 holds %q, want the tool's output", result.Text)
	}
}

// TestTheDeliveryNoteRendersAfterTheCompaction covers the plan's
// forward note: a delivery note resolves against the full record, not
// the compacted view.
func TestTheDeliveryNoteRendersAfterTheCompaction(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{text: "the first answer", calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	// The first run's reply is compacted away.
	runSession(t, s, "do the thing")

	// The next run refers to the reply the compaction removed, and the
	// note still renders from the record.
	if _, err := s.Prompt(context.Background(), "more please", poo.WithDelivered("m1", 4)); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	found := false
	for _, m := range sink.messages() {
		if m.DeliveryNote == nil {
			continue
		}
		found = true
		if !strings.Contains(m.DeliveryNote.Text, "m1") {
			t.Errorf("the delivered message's note is %q, want the note of m1", m.DeliveryNote.Text)
		}
		if !strings.Contains(m.DeliveryNote.Text, "the") {
			t.Errorf("the delivered message's note is %q, want the presented part of the reply", m.DeliveryNote.Text)
		}
	}
	if !found {
		t.Error("the log holds no delivery note")
	}
}

// TestAJobFinishingDuringTheCompactingIsNotInTheSnapshot covers the
// plan's rule: the compactor's snapshot is the view as it was when
// the compaction started, and a job that finishes during the
// compaction is not in it.
func TestAJobFinishingDuringTheCompactingIsNotInTheSnapshot(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	started := tool.Started()
	release := make(chan struct{})
	compacting := make(chan struct{})
	var snapshot poo.View
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.Budget) bool {
			return len(view.Turns()) >= 1
		},
		compaction: func(view poo.View) (poo.Compaction, error) {
			// The job finishes while the compaction runs.
			close(compacting)
			<-release
			snapshot = view
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "job", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, nil, sink)
	opts.Tools = map[string]poo.Tool{"job": tool}
	opts.ToolOrder = []string{"job"}
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	<-compacting
	// The job finishes while the compaction is running.
	tool.ReleaseAll()
	close(release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	if snapshot == nil {
		t.Fatal("the compactor saw no view")
	}
	for _, m := range snapshot.Messages() {
		if strings.Contains(m.Text(), "the job's output") {
			t.Errorf("the compactor's snapshot holds the job's result: %q", m.Text())
		}
	}
}

// TestAJobHandleSurvivesTheCompaction covers the plan's rule: a job's
// handle survives a compaction, and the job's result still reaches the
// model.
func TestAJobHandleSurvivesTheCompaction(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	started := tool.Started()
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			// The compaction replaces the turn that started the job.
			return poo.Compaction{
				Strategy: "summarize",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := jobOptions(t, tr, tool, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	// The job finishes while the run is on its way.
	tool.ReleaseAll()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The compaction really happened, and it replaced the turn that
	// started the job.
	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
	// The job's result reached the model, and the job's handle is
	// still the one the job started with.
	found := false
	for _, m := range sink.messages() {
		if m.Origin != poo.OriginJob {
			continue
		}
		found = true
		if !strings.Contains(m.Content[0].Text, "the job's output") {
			t.Errorf("the job's message holds %q, want the job's output", m.Content[0].Text)
		}
	}
	if !found {
		t.Error("the log holds no job message: the job's result did not reach the model")
	}
	handles := 0
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Handle == "r1" {
			handles++
		}
	}
	if handles != 1 {
		t.Errorf("the log holds %d results for r1, want 1", handles)
	}
}

// plainTransport is a chat transport that cannot compact natively.
//
// It carries the scripted transport's behaviour and none of its native
// compaction, so a window cannot have come from it.
type plainTransport struct {
	// inner is the scripted transport.
	inner *pootest.Transport
}

// Stream serves the next scripted response.
func (t *plainTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return t.inner.Stream(ctx, req)
}

// Fork serves the next forked response.
func (t *plainTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return t.inner.Fork(ctx, prev, appended, maxTokens)
}

// TestAWindowFromATransportThatCannotCompactIsRejected covers the
// plan's rule: a window is accepted only when it came from the
// session's own transport, through its native compaction.
func TestAWindowFromATransportThatCannotCompactIsRejected(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	tr := &plainTransport{inner: inner}
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "window",
				Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
					Kind: poo.ContentProvider,
					Provider: &poo.ProviderBlock{
						API:      poo.APIChatCompletions,
						Type:     "compaction",
						Data:     []byte(`{"encrypted_content":"abc"}`),
						Required: true,
					},
				}}},
			}, nil
		},
	}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
}
