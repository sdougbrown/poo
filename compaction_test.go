package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The compaction seam is frozen in this stage, and the store records what
// a compactor did, so every type the seam carries has to reach a record
// under the field names.

// TestTheCompactionUsesTheRecordVocabulary covers the compaction's own
// field names: a compaction record is snake_case, and the forms are named
// after what they do.
func TestTheCompactionUsesTheRecordVocabulary(t *testing.T) {
	compaction := poo.Compaction{
		Strategy: "handoff",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 2, Last: 3},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}
	raw, err := json.Marshal(compaction)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if decoded["strategy"] != "handoff" {
		t.Errorf("the compaction's strategy is %v, want handoff", decoded["strategy"])
	}
	replace, ok := decoded["replace"].(map[string]any)
	if !ok {
		t.Fatalf("the compaction's replace is %v, want an object", decoded["replace"])
	}
	cut, ok := replace["cut"].(map[string]any)
	if !ok {
		t.Fatalf("the replacement's cut is %v, want an object", replace["cut"])
	}
	if cut["first"] != float64(2) || cut["last"] != float64(3) {
		t.Errorf("the cut is %v, want the turns 2 to 3", cut)
	}
	if _, ok := replace["replacement"]; !ok {
		t.Errorf("the replacement has no replacement field: %v", replace)
	}
}

// TestTheCompactionFormsRoundTrip covers each of the four forms: a
// compaction that reaches a record and comes back holds the same form.
func TestTheCompactionFormsRoundTrip(t *testing.T) {
	forms := []poo.Compaction{
		{
			Strategy: "summarize",
			Replace: &poo.RangeReplacement{
				Cut:      poo.TurnRange{First: 1, Last: 2},
				Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
			},
		},
		{
			Strategy: "rewrite",
			Rewrite: []poo.ToolResultRewrite{{
				Handle:  "r1",
				Content: "[r1 · bash · compacted] the shorter result",
			}},
		},
		{
			Strategy: "window",
			Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
				Kind: poo.ContentProvider,
				Provider: &poo.ProviderBlock{
					API:      poo.APIResponses,
					Type:     "compaction",
					Data:     json.RawMessage(`{"encrypted_content":"abc"}`),
					Required: true,
				},
			}}},
		},
		{
			Strategy: "handoff",
			End:      &poo.EndRun{Handoff: "the note the next agent picks up"},
		},
	}

	for _, want := range forms {
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("encoding a %s compaction: %v", want.Strategy, err)
		}
		var back poo.Compaction
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("decoding a %s compaction: %v", want.Strategy, err)
		}
		if back.Strategy != want.Strategy {
			t.Errorf("the strategy is %q, want %q", back.Strategy, want.Strategy)
		}
		switch {
		case want.Replace != nil:
			if back.Replace == nil {
				t.Fatalf("the %s compaction came back without its replacement", want.Strategy)
			}
			if back.Replace.Cut != want.Replace.Cut {
				t.Errorf("the cut is %+v, want %+v", back.Replace.Cut, want.Replace.Cut)
			}
			if len(back.Replace.Messages) != len(want.Replace.Messages) {
				t.Errorf("the replacement holds %d messages, want %d",
					len(back.Replace.Messages), len(want.Replace.Messages))
			}
		case want.Rewrite != nil:
			if len(back.Rewrite) != len(want.Rewrite) {
				t.Fatalf("the %s compaction came back with %d rewrites, want %d",
					want.Strategy, len(back.Rewrite), len(want.Rewrite))
			}
			if back.Rewrite[0].Handle != want.Rewrite[0].Handle {
				t.Errorf("the rewrite's handle is %q, want %q",
					back.Rewrite[0].Handle, want.Rewrite[0].Handle)
			}
			if back.Rewrite[0].Content != want.Rewrite[0].Content {
				t.Errorf("the rewrite's content is %q, want %q",
					back.Rewrite[0].Content, want.Rewrite[0].Content)
			}
		case want.Window != nil:
			if back.Window == nil {
				t.Fatalf("the %s compaction came back without its window", want.Strategy)
			}
			if len(back.Window.Items) != len(want.Window.Items) {
				t.Fatalf("the window holds %d items, want %d",
					len(back.Window.Items), len(want.Window.Items))
			}
			if back.Window.Items[0].Provider == nil {
				t.Fatal("the window's item came back without its provider block")
			}
			if string(back.Window.Items[0].Provider.Data) != `{"encrypted_content":"abc"}` {
				t.Errorf("the window's payload is %s, want the original",
					back.Window.Items[0].Provider.Data)
			}
		case want.End != nil:
			if back.End == nil {
				t.Fatalf("the %s compaction came back without its end", want.Strategy)
			}
			if back.End.Handoff != want.End.Handoff {
				t.Errorf("the handoff is %q, want %q", back.End.Handoff, want.End.Handoff)
			}
		}
	}
}

// The claims under an interrupt: the compaction is claimed against the
// seal, an interrupt re-enters the gate, and a refused record closes it.

// TestInterruptDiscardsASuccessfulCompaction covers the rule that an
// interrupted compaction's successful return is discarded.
func TestInterruptDiscardsASuccessfulCompaction(t *testing.T) {
	entered := make(chan struct{})
	var used atomic.Bool
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &scriptedCompactor{
		should: func(v poo.View, b poo.ContextBudget) bool { return b.Turns == 1 && !used.Load() },
		compact: func(ctx context.Context, _ poo.CompactionEnv) (poo.Compaction, error) {
			used.Store(true)
			close(entered)
			<-ctx.Done()
			return poo.Compaction{Strategy: "interrupted", End: &poo.EndRun{Handoff: "must be discarded"}}, nil
		},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := s.Send("interrupt", poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != poo.ReasonEndTurn || sink.count("compaction") != 0 {
		t.Fatalf("interrupt applied compaction: reason=%s, compactions=%d, dropped=%v", res.Reason, sink.count("compaction"), res.Detail.UndeliveredInput)
	}
}

// TestInterruptDuringACompactionRechecksTheGate covers the rule that an
// interrupted gate re-enters the full gate, budget check included.
func TestInterruptDuringACompactionRechecksTheGate(t *testing.T) {
	entered := make(chan struct{})
	var used atomic.Bool
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Model.ContextLimit = 10000
	opts.Compactor = &scriptedCompactor{
		should: func(v poo.View, b poo.ContextBudget) bool { return b.Turns == 1 && !used.Load() },
		compact: func(ctx context.Context, _ poo.CompactionEnv) (poo.Compaction, error) {
			used.Store(true)
			close(entered)
			<-ctx.Done()
			return poo.Compaction{}, ctx.Err()
		},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := s.Send(strings.Repeat("x", 100000), poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Requests()) != 1 {
		t.Fatalf("over-budget interrupt opened request: requests=%d, max_tokens=%d, settled=%s", len(tr.Requests()), tr.Requests()[1].MaxTokens, res.Reason)
	}
}

// heldCompactionSink holds the compaction record until the test releases
// it.
type heldCompactionSink struct {
	*recordingSink
	entered, release chan struct{}
}

func (s *heldCompactionSink) Append(ctx context.Context, r poo.Record) error {
	if r.Meta().Record == "compaction" {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.recordingSink.Append(ctx, r)
	}
	return s.recordingSink.Append(ctx, r)
}

// TestCompactionWriteRespectsTheSeal covers the rule that a compaction
// is claimed against the seal like a call record.
func TestCompactionWriteRespectsTheSeal(t *testing.T) {
	inner := &recordingSink{}
	sink := &heldCompactionSink{recordingSink: inner, entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	opts := testOptions(t, tr, &echoTool{result: "output"}, inner)
	opts.Sink = sink
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return summaryCompaction(), nil },
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	// The cancel waits for the compaction's claim, so the test releases
	// the store while the cancel is under way.
	cancelled := make(chan struct{})
	go func() {
		s.Cancel("cancel:review")
		close(cancelled)
	}()
	close(sink.release)
	<-cancelled
	seenSettle := false
	for _, r := range sink.allRecords() {
		if r.Meta().Record == "run.settled" {
			seenSettle = true
		}
		if seenSettle && r.Meta().Record == "compaction" {
			t.Fatal("record order: run.settled then compaction (sink honored its uncancelled context)")
		}
	}
}

// failingCompactionSink refuses the compaction record.
type failingCompactionSink struct{ recordingSink }

func (s *failingCompactionSink) Append(ctx context.Context, r poo.Record) error {
	if r.Meta().Record == "compaction" {
		return errors.New("injected compaction write failure")
	}
	return s.recordingSink.Append(ctx, r)
}

// TestCompactionStoreErrorPreventsRequest covers the rule that a failed
// compaction record closes the gate.
func TestCompactionStoreErrorPreventsRequest(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &failingCompactionSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return summaryCompaction(), nil },
	}
	s := newTestSession(t, opts)
	res := runSession(t, s, "start")
	if len(tr.Requests()) != 1 {
		t.Fatalf("pending store_error did not close gate: requests=%d settled=%s", len(tr.Requests()), res.Reason)
	}
}

// TestTheRebaseUsesTheCharRatio covers the rule that the rebase uses 3.5
// chars/token over the whole view until the next usage report.
func TestTheRebaseUsesTheCharRatio(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 300, OutputTokens: 10}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 8000)}, sink)
	opts.Model.ContextLimit = 2000
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			c := summaryCompaction()
			c.Replace.Messages = []poo.Message{poo.NewTextMessage(poo.RoleUser, strings.Repeat("s", 4200))}
			return c, nil
		},
	}
	s := newTestSession(t, opts)
	res := runSession(t, s, "start")
	if len(tr.Requests()) != 2 {
		t.Fatalf("4200-character summary fits 2000-token window at 3.5 chars/token but settled=%s source=%s requests=%d", res.Reason, res.Detail.Source, len(tr.Requests()))
	}
}

// The rewrite's atomicity: a failed rewrite is not recorded, and the
// compaction never changes anything.

// TestFailedRewriteIsAtomic covers the rule that a rewrite resolves
// every target before it commits.
func TestFailedRewriteIsAtomic(t *testing.T) {
	tr := scripted([]fakeResponse{
		toolCallTurn(),
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "original"}, sink)
	calls := 0
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			calls++
			if calls == 1 {
				return summaryCompaction(), nil
			}
			return poo.Compaction{Strategy: "rewrite", Rewrite: []poo.ToolResultRewrite{
				{Handle: "r2", Content: poo.CompactedHandleLine("r2", "echo") + "\nunrecorded mutation"},
				{Handle: "r1", Content: poo.CompactedHandleLine("r1", "echo") + "\nmissing"},
			}}, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, m := range tr.Requests()[2].Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "unrecorded mutation") {
				t.Fatalf("failed rewrite changed view with no record: compactions=%d, result=%q", sink.count("compaction"), b.Text)
			}
		}
	}
}

// TestRewriteKeepsTheOriginalRecord covers the rule that a rewrite
// copies before it changes the message.
func TestRewriteKeepsTheOriginalRecord(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "original"}, sink)
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "rewrite", Rewrite: []poo.ToolResultRewrite{{
				Handle:  "r1",
				Content: poo.CompactedHandleLine("r1", "echo") + "\nsummary",
			}}}, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, r := range sink.messages() {
		if r.Role == poo.RoleTool && !strings.Contains(r.Content[0].Text, "original") {
			t.Fatalf("original message record mutated by rewrite: %q", r.Content[0].Text)
		}
	}
}

// TestARewriteDropsTheAttachmentsOfTheResultItRewrites covers the context an
// attachment costs: a rewritten result keeps none, because freeing that
// context is what the rewrite is for.
func TestARewriteDropsTheAttachmentsOfTheResultItRewrites(t *testing.T) {
	tool := &echoTool{call: func(context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{
			Text: "original",
			Attachments: []poo.Attachment{
				{Modality: poo.ModalityImage, MediaType: "image/png", Data: []byte{1, 2, 3}},
				{Modality: poo.ModalityAudio, MediaType: "audio/wav", Data: []byte{4, 5, 6}},
			},
		}, nil
	}}
	tr := scripted([]fakeResponse{toolCallTurn(), toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	// The model reads attachments, so the session sends them.
	opts.Model.Inputs = []poo.Modality{poo.ModalityText, poo.ModalityImage, poo.ModalityAudio}
	opts.Compactor = &compactionCompactor{
		should: afterTurns(2),
		compaction: func(poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "rewrite", Rewrite: []poo.ToolResultRewrite{{
				Handle:  "r1",
				Content: poo.CompactedHandleLine("r1", "echo") + "\nsummary",
			}}}, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	seen := map[poo.ContentKind]bool{}
	for _, req := range tr.Requests() {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Kind == poo.ContentImage || b.Kind == poo.ContentAudio {
					seen[b.Kind] = true
				}
			}
		}
	}
	if !seen[poo.ContentImage] || !seen[poo.ContentAudio] {
		t.Fatalf("the case carried %v, so it proves nothing", seen)
	}
	last := tr.Requests()[len(tr.Requests())-1]
	// r1's result is the one the rewrite targeted, so its message is the
	// one that must carry no attachment any more; r2's fresh result keeps
	// its own.
	for _, m := range last.Messages {
		rewritten := false
		for _, b := range m.Content {
			if b.Kind == poo.ContentToolResult && strings.HasPrefix(b.Text, "[r1 ·") {
				rewritten = true
			}
		}
		if !rewritten {
			continue
		}
		for _, b := range m.Content {
			if b.Kind == poo.ContentImage || b.Kind == poo.ContentAudio {
				t.Fatalf("a rewritten result still carries its %s", b.Kind)
			}
		}
	}

	// The result the rewrite did not target keeps its attachments.
	fresh := map[poo.ContentKind]bool{}
	for _, m := range last.Messages {
		for _, b := range m.Content {
			if b.Kind != poo.ContentToolResult || !strings.HasPrefix(b.Text, "[r2 ·") {
				continue
			}
			for _, attachment := range m.Content {
				if attachment.Kind == poo.ContentImage || attachment.Kind == poo.ContentAudio {
					fresh[attachment.Kind] = true
				}
			}
		}
	}
	if len(fresh) != 2 {
		t.Errorf("a result the rewrite left alone kept %v, want its image and its audio", fresh)
	}
}

// TestTokensAfterUsesTheRebasedEstimate covers the rule that
// tokens_after is measured after the rebase.
func TestTokensAfterUsesTheRebasedEstimate(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 7000, OutputTokens: 10}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 1000)}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return summaryCompaction(), nil },
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, r := range sink.allRecords() {
		if c, ok := r.(*poo.CompactionRecord); ok && c.TokensAfter >= 7000 {
			t.Fatalf("tokens_after=%d retains pre-compaction usage baseline 7000 for short summary", c.TokensAfter)
		}
	}
}

// TestReplacementMustBeVisible covers the rule that a replacement is
// non-empty visible user content.
func TestReplacementMustBeVisible(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			c := summaryCompaction()
			c.Replace.Messages = []poo.Message{{}}
			return c, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if sink.count("compaction") != 0 {
		t.Fatalf("accepted empty invisible replacement: compactions=%d, messages=%+v", sink.count("compaction"), tr.Requests()[1].Messages)
	}
}

// Window provenance: a provider window must come from the session's
// own transport.

// TestRejectWindowNotProducedByTransport covers the rule that a window
// comes from the session's own transport.
func TestRejectWindowNotProducedByTransport(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return windowCompaction(), nil },
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if sink.count("compaction") != 0 {
		t.Fatalf("accepted fabricated window without calling NativeCompactor: compactions=%d", sink.count("compaction"))
	}
}

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
	should func(view poo.View, budget poo.ContextBudget) bool
	// compaction answers Compact.
	compaction func(view poo.View) (poo.Compaction, error)
	// views holds the views the compactor saw, in order.
	views []poo.View
	// calls counts the compactions the compactor served.
	calls int
}

// ShouldCompact answers the scripted decision.
func (c *compactionCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.should == nil {
		return true
	}
	return c.should(view, budget)
}

// Compact answers the scripted compaction.
func (c *compactionCompactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	c.mu.Lock()
	c.calls++
	c.views = append(c.views, in.View)
	answer := c.compaction
	c.mu.Unlock()
	if answer == nil {
		return poo.Compaction{}, nil
	}
	return answer(in.View)
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
func afterTurns(n int) func(poo.View, poo.ContextBudget) bool {
	return func(view poo.View, budget poo.ContextBudget) bool {
		return len(view.Turns()) >= n
	}
}

// TestTheReplacementIsAppliedAndTheOriginalsAreKept covers the rule: the view changes, and the store keeps every original record.
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestAnEmptyCompactionIsRejected covers the rule: a compaction
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestANonContiguousCutIsRejected covers the rule: a
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestACutOverARemovedTurnIsRejected covers the rule: a range
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
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

// TestTheRewriteReplacesTheResultAndKeepsThePairing covers the rule: a rewrite changes the content of a tool result, the call and
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestARewriteWithoutTheMarkerIsRejected covers the rule: a
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestAWindowFromAnotherTransportIsRejected covers the rule: a
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestTheWindowBecomesTheViewPlusTheMarker covers the rule: a
// provider window becomes the view, and the marker message says so.
func TestTheWindowBecomesTheViewPlusTheMarker(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
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
	// The run's instructions, the window and the marker: a window
	// replaces the conversation, not the contract the model works under.
	if len(second) != 3 {
		t.Fatalf("the second request holds %d messages, want the instructions, the window and the marker", len(second))
	}
	if second[0].Role != poo.RoleSystem {
		t.Errorf("the first message's role is %q, want the run's instructions", second[0].Role)
	}
	if second[1].Content[0].Provider == nil {
		t.Error("the window holds no provider block")
	}
	if second[2].Text() != poo.CompactionMarkerText {
		t.Errorf("the last message holds %q, want the marker", second[2].Text())
	}
	if sink.count("compaction") != 1 {
		t.Errorf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
}

// TestTheEndSettlesWithTheHandoff covers the rule: a
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestTheBudgetIsRebasedOntoTheNewView covers the rule: a view
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
		{text: long, calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	opts.Model.ContextLimit = 4000
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

// TestTheHardLimitSettlesWithoutACompaction covers the rule
// beside the rebase: a view over the hard ratio that no compaction
// brings below it settles context_exhausted.
func TestTheHardLimitSettlesWithoutACompaction(t *testing.T) {
	long := strings.Repeat("x", 8000)
	tr := scripted([]fakeResponse{
		{text: long, calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Model.ContextLimit = 4000
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

// TestTheCompactorsFailureBelowTheHardLimitContinues covers the failure row: below the hard limit the run continues uncompacted.
func TestTheCompactorsFailureBelowTheHardLimitContinues(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{}, errors.New("the compaction failed")
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestTheCompactorsFailureAtTheHardLimitSettles covers the failure row: at or above the hard limit a compactor that failed
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
		{text: long, calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	opts.Model.ContextLimit = 4000
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

// TestTheCompactionRecordCarriesTheCutAndTheReplacement covers the record: it names the cut and the replacement, and its token
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestTheHandlesNumberTheResultsAndGrowAcrossRuns covers the rule: every tool result starts with its handle line, and the number
// grows across the runs of a session.
func TestTheHandlesNumberTheResultsAndGrowAcrossRuns(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
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

// TestAHandleSurvivesTheCompaction covers the rule: a handle
// names a call for the life of the session, so a compaction does not
// take it away.
func TestAHandleSurvivesTheCompaction(t *testing.T) {
	var second poo.View
	calls := 0
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.ContextBudget) bool {
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
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

// TestTheDeliveryNoteRendersAfterTheCompaction covers the forward note: a delivery note resolves against the full record, not
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
		{text: "the first answer", calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestAJobFinishingDuringTheCompactingIsNotInTheSnapshot covers the rule: the compactor's snapshot is the view as it was when
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
		should: func(view poo.View, budget poo.ContextBudget) bool {
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
		{calls: []poo.ToolCall{{CallID: "c1", Name: "job", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestAJobHandleSurvivesTheCompaction covers the rule: a job's
// handle survives a compaction, and the job's result still reaches the
// model.
func TestAJobHandleSurvivesTheCompaction(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	started := tool.Started()
	// The second turn's tool holds the run until the job's result is
	// queued, so the delivery is not a race.
	release := make(chan struct{})
	echo := &echoTool{result: "echoed", call: func(ctx context.Context) (poo.ToolResult, error) {
		<-release
		return poo.ToolResult{Text: "echoed"}, nil
	}}
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
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, echo, sink)
	opts.Tools["job"] = tool
	opts.ToolOrder = []string{"echo", "job"}
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	// The compaction happens at the gate after the first turn, and the
	// job is still running then.
	waitFor(t, "the compaction to happen", func() bool { return sink.count("compaction") == 1 })
	// The job finishes, and its result waits for the turn end.
	tool.ReleaseAll()
	waitFor(t, "the job's result to be queued", func() bool { return s.QueuedJobResults() > 0 })
	close(release)
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

// TestAWindowFromATransportThatCannotCompactIsRejected covers the rule: a window is accepted only when it came from the
// session's own transport, through its native compaction.
func TestAWindowFromATransportThatCannotCompactIsRejected(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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

// TestTheSessionKeepsItsJobsAfterAnEnd covers the rule: after a
// handoff end, the session's jobs stay with that session, and their
// results still arrive.
func TestTheSessionKeepsItsJobsAfterAnEnd(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	started := tool.Started()
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
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Handoff != "the handoff note" {
		t.Errorf("the settle detail's handoff is %q, want the note", res.Detail.Handoff)
	}
	// The job is still the session's, and its result still reaches the
	// model: the result wakes the session and is delivered.
	tool.ReleaseAll()
	waitFor(t, "the job's result to reach the model", func() bool {
		for _, m := range sink.messages() {
			if m.Origin == poo.OriginJob {
				return true
			}
		}
		return false
	})
}

// TestAReplacementThatStartsLaterKeepsTheEarlierOne covers the rule that
// only a cut reaching back to the earliest turn in the view supersedes
// what an earlier compaction wrote: a strategy that replaces a later
// range leaves the earlier replacement in place, because the model still
// needs it.
func TestAReplacementThatStartsLaterKeepsTheEarlierOne(t *testing.T) {
	replacement := func(cut poo.TurnRange, text string) poo.Compaction {
		return poo.Compaction{
			Strategy: "stepwise",
			Replace: &poo.RangeReplacement{
				Cut:      cut,
				Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, text)},
			},
		}
	}
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.ContextBudget) bool {
			turns := len(view.Turns())
			return turns == 1 || turns == 3
		},
	}
	compactor.compaction = func(view poo.View) (poo.Compaction, error) {
		if compactor.Calls() == 1 {
			return replacement(poo.TurnRange{First: 1, Last: 1}, "the first summary"), nil
		}
		return replacement(poo.TurnRange{First: 3, Last: 3}, "the second summary"), nil
	}
	tr := scripted([]fakeResponse{
		{text: "one", calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "two", calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "three", calls: []poo.ToolCall{{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"three"}`)}}},
		{text: "the answer"},
	})
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if got := compactor.Calls(); got != 2 {
		t.Fatalf("the compactor ran %d times, want 2", got)
	}
	reqs := tr.Requests()
	held := 0
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Origin != poo.OriginCompaction {
			continue
		}
		held++
	}
	if held != 2 {
		t.Fatalf("the last request holds %d compaction messages, want 2", held)
	}

	// The turns the two cuts covered left the view, and the view says so.
	// The turns that stayed still name their own messages: a replacement
	// moves them, and a strategy sizes the next cut by those ranges.
	removed := 0
	for _, turn := range poo.SessionView(s).Turns() {
		if turn.Removed {
			removed++
		}
	}
	if removed != 2 {
		t.Errorf("the view reports %d removed turns, want 2", removed)
	}
	requireTurnRanges(t, s, map[int]string{2: "two", 4: "the answer"})
}

// requireTurnRanges checks that every turn still in the view names its own
// messages: its range holds the turn's text and not another turn's.
//
// Comparing the range against the session's own span only catches a
// mismatch between the view's indexes and the history's; the expected
// text catches a span that moved.
func requireTurnRanges(t *testing.T, s *poo.Session, want map[int]string) {
	t.Helper()
	view := poo.SessionView(s)
	history := view.Messages()
	for _, turn := range view.Turns() {
		if turn.Removed {
			continue
		}
		got := turnSpanText(history[turn.Start:turn.End])
		span, ok := poo.TurnSpanMessages(s, turn.TurnIndex)
		if !ok {
			t.Fatalf("the session has no span for the turn %d", turn.TurnIndex)
		}
		if also := turnSpanText(span); got != also {
			t.Errorf("the turn %d's range holds %q, want %q", turn.TurnIndex, got, also)
		}
		text, ok := want[turn.TurnIndex]
		if !ok {
			continue
		}
		if !strings.Contains(got, text) {
			t.Errorf("the turn %d's range holds %q, want its own text %q", turn.TurnIndex, got, text)
		}
		for other, otherText := range want {
			if other == turn.TurnIndex || !strings.Contains(got, otherText) {
				continue
			}
			t.Errorf("the turn %d's range holds %q, which is the turn %d's text", turn.TurnIndex, otherText, other)
		}
	}
}

// turnSpanText returns every text a turn's messages show the model.
func turnSpanText(messages []poo.Message) string {
	var b strings.Builder
	for _, m := range messages {
		for _, c := range m.Content {
			if c.Kind == poo.ContentText || c.Kind == poo.ContentToolResult {
				b.WriteString(c.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// TestAReplacementLeavesProviderDataInTheView covers the rule that a
// replacement never supersedes a provider window or the marker the core
// wrote for it: only the provider can write a window again, and a view
// that lost its marker would look compacted with nothing saying so.
func TestAReplacementLeavesProviderDataInTheView(t *testing.T) {
	compactor := &compactionCompactor{
		should: func(view poo.View, budget poo.ContextBudget) bool { return len(view.Turns()) >= 1 },
	}
	compactor.compaction = func(view poo.View) (poo.Compaction, error) {
		if compactor.Calls() == 1 {
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
		}
		return poo.Compaction{
			Strategy: "stepwise",
			Replace: &poo.RangeReplacement{
				Cut:      poo.TurnRange{First: 2, Last: 2},
				Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
			},
		}, nil
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind:     poo.ContentProvider,
		Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Required: true, Data: []byte(`{"w":1}`)},
	}}})
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if got := compactor.Calls(); got != 2 {
		t.Fatalf("the compactor ran %d times, want 2", got)
	}
	keptWindow, keptMarker, keptSummary := false, false, false
	for _, m := range poo.Messages(s) {
		if m.Text() == poo.CompactionMarkerText {
			keptMarker = true
		}
		if m.Text() == "the summary" {
			keptSummary = true
		}
		for _, c := range m.Content {
			if c.Provider != nil {
				keptWindow = true
			}
		}
	}
	if !keptWindow {
		t.Error("the view lost the provider's window")
	}
	if !keptMarker {
		t.Error("the view lost the marker the core wrote for the window")
	}
	if !keptSummary {
		t.Error("the view holds no replacement")
	}
}

// TestASupersedingReplacementMovesTheTurnsBehindIt covers the shift a
// replacement makes when it also takes out the replacement before it: the
// turns the cut leaves keep ranges that name their own messages, which is
// what a strategy sizes its next cut by.
func TestASupersedingReplacementMovesTheTurnsBehindIt(t *testing.T) {
	replacement := func(cut poo.TurnRange, text string) poo.Compaction {
		return poo.Compaction{
			Strategy: "stepwise",
			Replace: &poo.RangeReplacement{
				Cut:      cut,
				Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, text)},
			},
		}
	}
	compactor := &compactionCompactor{
		// The gates after the first and the third turn compact, so the
		// second cut runs while a later turn is already in the view: the
		// turns behind the cut move by the messages it drops.
		should: func(view poo.View, budget poo.ContextBudget) bool {
			turns := len(view.Turns())
			return turns == 1 || turns == 3
		},
	}
	compactor.compaction = func(view poo.View) (poo.Compaction, error) {
		if compactor.Calls() == 1 {
			return replacement(poo.TurnRange{First: 1, Last: 1}, "the first summary"), nil
		}
		// The second cut starts at the earliest turn still in the view,
		// so it supersedes the first replacement.
		return replacement(poo.TurnRange{First: 2, Last: 2}, "the second summary"), nil
	}
	tr := scripted([]fakeResponse{
		{text: "one", calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "two", calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "three", calls: []poo.ToolCall{{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"three"}`)}}},
		{text: "the answer"},
	})
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if got := compactor.Calls(); got != 2 {
		t.Fatalf("the compactor ran %d times, want 2", got)
	}
	summaries := 0
	for _, m := range poo.SessionView(s).Messages() {
		if m.Origin == poo.OriginCompaction {
			summaries++
		}
	}
	requireTurnRanges(t, s, map[int]string{3: "three", 4: "the answer"})
	if summaries != 1 {
		t.Errorf("the view holds %d compaction messages, want 1: the second replacement supersedes the first", summaries)
	}
}

// TestTheNativeCompactorSeesTheCurrentView covers the reason the native
// compactor is handed a view: the last request's snapshot is a turn
// behind it, so a provider that compacted the snapshot would drop the
// reply and the results the run has since produced.
func TestTheNativeCompactorSeesTheCurrentView(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the first reply", calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind:     poo.ContentProvider,
		Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Required: true, Data: []byte(`{"encrypted_content":"abc"}`)},
	}}})
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			// The strategy asks for a native compaction; the core asks
			// the transport for the window.
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
		},
	}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	given := tr.NativeCompactions()
	if len(given) != 1 {
		t.Fatalf("the transport compacted %d times, want 1", len(given))
	}
	// The turn that just finished is in the view the compactor was given.
	// The last request went out before that turn existed.
	var text strings.Builder
	for _, m := range given[0].View.Messages() {
		text.WriteString(turnSpanText([]poo.Message{m}))
	}
	for _, want := range []string{"the first reply", "echoed"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the view the compactor was given holds %q, want the finished turn's %q", text.String(), want)
		}
	}
	if given[0].Model.ModelID != "test-model" {
		t.Errorf("the compaction was given the model %q, want the session's", given[0].Model.ModelID)
	}
	// The budget travels with the view: a transport that has to decide
	// how much room the window leaves needs the run's own numbers.
	if given[0].Budget.ContextLimit != 32768 {
		t.Errorf("the compaction was given the window %d, want the session's 32768", given[0].Budget.ContextLimit)
	}
	if given[0].Budget.Turns < 1 {
		t.Errorf("the compaction was given %d turns, want the run's own count", given[0].Budget.Turns)
	}
}

// TestAWindowMayCarryRetainedText covers the shape a provider returns: the
// opaque items it cannot rebuild and the visible text the model must go on
// reading, installed in the order the provider sent them.
func TestAWindowMayCarryRetainedText(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "the request the user made"},
		{Kind: poo.ContentProvider, Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Required: true, Data: []byte(`{"encrypted_content":"abc"}`)}},
		{Kind: poo.ContentProvider, Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Type: "retained", Data: []byte(`{"dropped":true}`)}},
	}})
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
		},
	}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if sink.count("compaction") != 1 {
		t.Fatalf("the log holds %d compaction records, want 1", sink.count("compaction"))
	}
	reqs := tr.Requests()
	last := reqs[len(reqs)-1].Messages
	// The instructions, the window and the marker.
	if len(last) != 3 {
		t.Fatalf("the last request holds %d messages, want 3", len(last))
	}
	window := last[1].Content
	if len(window) != 3 {
		t.Fatalf("the window holds %d items, want the provider's 3", len(window))
	}
	if window[0].Kind != poo.ContentText || window[0].Text != "the request the user made" {
		t.Errorf("the window's first item is %+v, want the retained text", window[0])
	}
	for i, item := range window[1:] {
		if item.Provider == nil {
			t.Errorf("the window's item %d is %+v, want provider data", i+1, item)
		}
	}
}

// TestAWindowWithoutRequiredDataIsRejected covers the rule: a window
// carries at least one item the provider cannot rebuild, so a window of
// retained text alone is not a compaction.
func TestAWindowWithoutRequiredDataIsRejected(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "the request the user made"},
	}})
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
		},
	}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: the refused window leaves the run alive", res.Reason)
	}
}

// TestAReplacementKeepsTheRunsInstructions covers the sibling of the
// window's rule: a replacement rewrites the conversation, and the
// instructions the model works under are not part of it.
func TestAReplacementKeepsTheRunsInstructions(t *testing.T) {
	compactor := &compactionCompactor{
		should: afterTurns(1),
		compaction: func(view poo.View) (poo.Compaction, error) {
			return poo.Compaction{
				Strategy: "stepwise",
				Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: 1, Last: 1},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				},
			}, nil
		},
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the answer"},
	})
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	second := reqs[1].Messages
	if len(second) == 0 || second[0].Role != poo.RoleSystem {
		t.Fatalf("the second request starts at %+v, want the run's instructions", second)
	}
	if second[0].Text() != "the system prompt" {
		t.Errorf("the instructions are %q, want the session's system prompt", second[0].Text())
	}
}

// TestAWindowWithAnUnknownItemIsRejected covers the other half of the
// window's rule: an item that is neither provider data nor visible text,
// and a text item with nothing in it, are both refused. Either would
// install something in the model's view the provider never compacted.
func TestAWindowWithAnUnknownItemIsRejected(t *testing.T) {
	provider := poo.ContentBlock{
		Kind:     poo.ContentProvider,
		Provider: &poo.ProviderBlock{API: poo.APIChatCompletions, Required: true, Data: []byte(`{"w":1}`)},
	}
	for _, tc := range []struct {
		// name is the item the window carries beside the required one.
		name string
		item poo.ContentBlock
	}{
		{"an image", poo.ContentBlock{Kind: poo.ContentImage, MediaType: "image/png", Data: []byte("x")}},
		{"a blank text", poo.ContentBlock{Kind: poo.ContentText, Text: "   "}},
		{"a tool result", poo.ContentBlock{Kind: poo.ContentToolResult, ToolCallID: "c1", Text: "the result"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
				{text: "the answer"},
			})
			tr.SetWindow(poo.ProviderWindow{Items: []poo.ContentBlock{provider, tc.item}})
			compactor := &compactionCompactor{
				should: afterTurns(1),
				compaction: func(view poo.View) (poo.Compaction, error) {
					return poo.Compaction{Strategy: "window", Window: &poo.ProviderWindow{}}, nil
				},
			}
			sink := &recordingSink{}
			opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
			opts.Compactor = compactor
			s := newTestSession(t, opts)
			res := runSession(t, s, "do the thing")

			if sink.count("compaction") != 0 {
				t.Errorf("the log holds %d compaction records, want none", sink.count("compaction"))
			}
			if res.Reason != poo.ReasonEndTurn {
				t.Errorf("the run settled %q, want end_turn: the refused window leaves the run alive", res.Reason)
			}
		})
	}
}

// TestACompactionsUsageIsRecorded covers the rule: a request a compaction
// made is charged to the run and written to the log as what it was, so
// the cost of compacting is a number a comparison can read rather than a
// counter nobody sees.
func TestACompactionsUsageIsRecorded(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}, usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 1}},
		{text: "the answer", usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 1}},
	})
	tr.SetForkTurns(pootest.Turn{Text: "the summary", InputTokens: 60, OutputTokens: 40})
	compactor := &forkCaptureCompactor{}
	sink := &recordingSink{}
	observer := &collectingObserver{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts, poo.WithObserver(observer))
	res := runSession(t, s, "do the thing")

	var compactionUsage *poo.UsageRecord
	for _, rec := range sink.allRecords() {
		usage, ok := rec.(*poo.UsageRecord)
		if !ok || usage.Origin != poo.OriginCompaction {
			continue
		}
		compactionUsage = usage
	}
	if compactionUsage == nil {
		t.Fatal("the log holds no usage record for the compaction's request")
	}
	if compactionUsage.InputTokens != 60 || compactionUsage.OutputTokens != 40 {
		t.Errorf("the compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			compactionUsage.InputTokens, compactionUsage.OutputTokens)
	}
	if compactionUsage.Request != 0 {
		t.Errorf("the compaction's usage names the request %d, want none: it is not a turn", compactionUsage.Request)
	}
	if compactionUsage.Turn < 1 {
		t.Errorf("the compaction's usage names the turn %d, want the run's own", compactionUsage.Turn)
	}
	// The spend is part of what the run reports having cost: the two
	// turns' 20 in and 2 out, and the compaction's 60 and 40.
	if res.Detail.Usage == nil || res.Detail.Usage.InputTokens != 80 || res.Detail.Usage.OutputTokens != 42 {
		t.Errorf("the settle detail's usage is %+v, want 80 in and 42 out", res.Detail.Usage)
	}

	// The event says what the log says, and a turn's event does not: a
	// subscriber sees the same distinction a reader of the log does. The
	// delivery is asynchronous, so the observer is waited for.
	waitFor(t, "the observer to see the compaction's usage event", func() bool {
		for _, ev := range observer.snapshot() {
			if ev.Name == poo.EventUsage && ev.Payload["origin"] == poo.OriginCompaction {
				return true
			}
		}
		return false
	})
	compactionEvents, turnEvents := 0, 0
	for _, ev := range observer.snapshot() {
		if ev.Name != poo.EventUsage {
			continue
		}
		origin, named := ev.Payload["origin"]
		if named && origin == poo.OriginCompaction {
			compactionEvents++
			continue
		}
		if named {
			t.Errorf("a turn's usage event names the origin %v, want none", origin)
		}
		turnEvents++
	}
	if compactionEvents != 1 {
		t.Errorf("the observer saw %d compaction usage events, want 1", compactionEvents)
	}
	if turnEvents == 0 {
		t.Error("the observer saw no turn's usage event")
	}
}

// The native request's accounting: a request the transport makes for a
// provider window is billed like any other, and the run's token ceiling
// counts it.

// nativeWindowOf returns the window the fake native transport returns,
// with a required payload of the given content.
func nativeWindowOf(data string) poo.ProviderWindow {
	return poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind: poo.ContentProvider,
		Provider: &poo.ProviderBlock{
			API:      poo.APIChatCompletions,
			Type:     "compaction",
			Required: true,
			Data:     []byte(data),
		},
	}}}
}

// nativeWindowRequest is a compactor that asks the core for a native
// compaction, and nothing else.
func nativeWindowRequest() *compactionCompactor {
	return &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "native", Window: &poo.ProviderWindow{}}, nil
		},
	}
}

// compactionUsageRecords returns the usage records the requests a
// compaction made wrote.
func compactionUsageRecords(sink *recordingSink) []*poo.UsageRecord {
	var out []*poo.UsageRecord
	for _, rec := range sink.allRecords() {
		usage, ok := rec.(*poo.UsageRecord)
		if !ok || usage.Origin != poo.OriginCompaction {
			continue
		}
		out = append(out, usage)
	}
	return out
}

// TestANativeCompactionsUsageIsRecorded covers the accounting of a
// provider-native request: the endpoint billed it, so its usage is
// recorded as a compaction's, names no request because it is not a turn,
// and counts against the run's token ceiling.
func TestANativeCompactionsUsageIsRecorded(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 60, OutputTokens: 40})
	observer := &collectingObserver{}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = nativeWindowRequest()
	// The compaction's own tokens are the whole of what exhausts the
	// ceiling: the two turns report no usage, and their estimates are a
	// few tokens.
	opts.MaxRunTokens = 100
	s := newTestSession(t, opts, poo.WithObserver(observer))
	res := runSession(t, s, "do the thing")

	records := compactionUsageRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d usage records for the native compaction, want 1", len(records))
	}
	usage := records[0]
	if usage.InputTokens != 60 || usage.OutputTokens != 40 {
		t.Errorf("the native compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			usage.InputTokens, usage.OutputTokens)
	}
	if usage.Request != 0 {
		t.Errorf("the native compaction's usage names the request %d, want none: it is not a turn", usage.Request)
	}
	if usage.Turn < 1 {
		t.Errorf("the native compaction's usage names the turn %d, want the run's own", usage.Turn)
	}
	if res.Detail.Usage == nil || res.Detail.Usage.InputTokens != 60 || res.Detail.Usage.OutputTokens != 40 {
		t.Errorf("the settle detail's usage is %+v, want the native compaction's 60 in and 40 out", res.Detail.Usage)
	}
	// The charge reaches the run's ceiling: 100 tokens is the whole
	// limit, and the compaction is what spent it.
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted from the native compaction's tokens", res.Reason)
	}

	waitFor(t, "the observer to see the native compaction's usage event", func() bool {
		for _, ev := range observer.snapshot() {
			if ev.Name == poo.EventUsage && ev.Payload["origin"] == poo.OriginCompaction {
				return true
			}
		}
		return false
	})
}

// TestANativeCompactionWithoutUsageIsChargedTheEstimate covers the rule:
// an endpoint that reports no usage is charged the estimate a side
// request gets, measured from the request and the window it produced. A
// compacted conversation would otherwise be free, and the ceiling would
// count a compaction as nothing.
func TestANativeCompactionWithoutUsageIsChargedTheEstimate(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}})
	// The window's payload is what the estimate prices: four thousand
	// characters are about eleven hundred tokens, so the ceiling below
	// sits between the two turns' own estimates and those estimates plus
	// the compaction's.
	tr.SetWindow(nativeWindowOf(strings.Repeat("x", 4000)))
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = nativeWindowRequest()
	opts.MaxRunTokens = 1500
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if records := compactionUsageRecords(sink); len(records) != 0 {
		t.Errorf("the log holds %d usage records for the native compaction, want none: the estimate is not a report",
			len(records))
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted from the estimated compaction", res.Reason)
	}
}

// unbilledSuccessNativeTransport is a transport whose native compaction
// answers with a window and never sets Billed.
//
// That is the shape a third party writes: the replay transport returns the
// window it read back and nothing else, because a recording says nothing
// about what the endpoint charged. A result the transport handed back without
// an error is the endpoint's answer whatever the flag says.
type unbilledSuccessNativeTransport struct {
	*pootest.Transport
	// result is what the compaction answers.
	result poo.NativeCompactionResult
}

// Compact answers with the window it was built with and no bill.
func (t *unbilledSuccessNativeTransport) Compact(ctx context.Context, in poo.NativeCompaction) (poo.NativeCompactionResult, error) {
	return t.result, nil
}

// TestASuccessfulNativeCompactionWithoutABilledFlagIsStillCharged covers a
// successful native compaction from a transport that reports no bill: the run
// is charged the counts the answer reported, or the estimate a side request
// gets when it reported none, because the endpoint read the request and
// answered it.
func TestASuccessfulNativeCompactionWithoutABilledFlagIsStillCharged(t *testing.T) {
	t.Run("reported usage", func(t *testing.T) {
		tr := &unbilledSuccessNativeTransport{
			Transport: scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}}),
			result: poo.NativeCompactionResult{
				Window: nativeWindowOf(`{"encrypted_content":"abc"}`),
				Usage:  &poo.UsageReport{InputTokens: 60, OutputTokens: 40},
			},
		}
		sink := &recordingSink{}
		opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
		opts.Compactor = nativeWindowRequest()
		// The compaction's own tokens are the whole of what exhausts the
		// ceiling: the two turns report no usage, and their estimates are a
		// few tokens.
		opts.MaxRunTokens = 100
		res := runSession(t, newTestSession(t, opts), "do the thing")

		if got := sink.count("compaction"); got != 1 {
			t.Fatalf("the log holds %d compaction records, want 1: the window was applied", got)
		}
		records := compactionUsageRecords(sink)
		if len(records) != 1 {
			t.Fatalf("the log holds %d usage records for the native compaction, want 1", len(records))
		}
		if records[0].InputTokens != 60 || records[0].OutputTokens != 40 {
			t.Errorf("the native compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
				records[0].InputTokens, records[0].OutputTokens)
		}
		if res.Detail.Usage == nil || res.Detail.Usage.InputTokens != 60 || res.Detail.Usage.OutputTokens != 40 {
			t.Errorf("the settle detail's usage is %+v, want the native compaction's 60 in and 40 out", res.Detail.Usage)
		}
		if res.Reason != poo.ReasonBudgetExhausted {
			t.Errorf("the run settled %q, want budget_exhausted from the native compaction's tokens", res.Reason)
		}
	})

	t.Run("no reported usage", func(t *testing.T) {
		tr := &unbilledSuccessNativeTransport{
			Transport: scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}}),
			// The window's payload is what the estimate prices: four
			// thousand characters are about eleven hundred tokens, so the
			// ceiling sits between the two turns' own estimates and those
			// estimates plus the compaction's.
			result: poo.NativeCompactionResult{
				Window: nativeWindowOf(strings.Repeat("x", 4000)),
			},
		}
		sink := &recordingSink{}
		opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
		opts.Compactor = nativeWindowRequest()
		opts.MaxRunTokens = 1500
		res := runSession(t, newTestSession(t, opts), "do the thing")

		if got := sink.count("compaction"); got != 1 {
			t.Fatalf("the log holds %d compaction records, want 1: the window was applied", got)
		}
		if records := compactionUsageRecords(sink); len(records) != 0 {
			t.Errorf("the log holds %d usage records for the native compaction, want none: "+
				"the estimate is not a report", len(records))
		}
		if res.Reason != poo.ReasonBudgetExhausted {
			t.Errorf("the run settled %q, want budget_exhausted from the estimated compaction", res.Reason)
		}
	})
}

// TestAFailedNativeCompactionInstallsNothing covers a native request the
// endpoint did not answer: the view keeps the turn the window would have
// replaced, no compaction record is written, and the request costs
// nothing because there is no answer to charge.
func TestAFailedNativeCompactionInstallsNothing(t *testing.T) {
	tr := scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	tr.SetWindowError(errors.New("the endpoint broke"))
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = nativeWindowRequest()
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none: the window was never produced", sink.count("compaction"))
	}
	if records := compactionUsageRecords(sink); len(records) != 0 {
		t.Errorf("the log holds %d usage records for the failed native compaction, want none", len(records))
	}
	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the transport got %d requests, want the failed compaction and the turn after it", len(reqs))
	}
	last := reqs[len(reqs)-1].Messages
	var text strings.Builder
	for _, m := range last {
		text.WriteString(turnSpanText([]poo.Message{m}))
		for _, c := range m.Content {
			if c.Provider != nil {
				t.Fatalf("the request after the failed compaction carries provider data: %+v", c)
			}
		}
	}
	for _, want := range []string{"do the thing", "echoed"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the request after the failed compaction holds %q, want the run's own history with %q",
				text.String(), want)
		}
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: a failed compaction leaves the run alive", res.Reason)
	}
}

// blockedNativeTransport is the scripted transport whose native compaction
// waits for the run's interrupt and then answers anyway, the way a
// cooperative transport may.
type blockedNativeTransport struct {
	*pootest.Transport
	// entered closes when the compaction is inside the transport.
	entered chan struct{}
	// once closes entered once.
	once sync.Once
	// result is what the compaction reports when the interrupt ends it.
	result poo.NativeCompactionResult
	// err is the failure the compaction ends with, when it is set. An
	// interrupted request ends with the interrupt's own error.
	err error
}

// Compact waits for the run's interrupt, then reports what it was given.
func (t *blockedNativeTransport) Compact(ctx context.Context, in poo.NativeCompaction) (poo.NativeCompactionResult, error) {
	t.once.Do(func() { close(t.entered) })
	<-ctx.Done()
	return t.result, t.err
}

// interruptedNativeRun starts a run whose native compaction waits for the
// run's interrupt, interrupts it once the compaction is inside the transport,
// and returns what the run settled as. The run is left for the caller to read
// the log of, so its accounting can be asserted.
func interruptedNativeRun(t *testing.T, opts poo.Options, tr *blockedNativeTransport) poo.Result {
	t.Helper()
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.entered
	if _, err := s.Send("interrupt", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return res
}

// TestAnInterruptedNativeCompactionStillCostsItsTokens covers the order the
// accounting runs in: the endpoint billed the request before the interrupt
// arrived, so the compaction's tokens are recorded even though the window
// it produced is discarded.
func TestAnInterruptedNativeCompactionStillCostsItsTokens(t *testing.T) {
	tr := &blockedNativeTransport{
		Transport: scripted([]fakeResponse{toolCallTurn(), {text: "the answer"}}),
		entered:   make(chan struct{}),
		result: poo.NativeCompactionResult{
			Window: nativeWindowOf(`{"encrypted_content":"abc"}`),
			Usage:  &poo.UsageReport{InputTokens: 60, OutputTokens: 40},
			Billed: true,
		},
	}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = nativeWindowRequest()
	res := interruptedNativeRun(t, opts, tr)

	if sink.count("compaction") != 0 {
		t.Errorf("the log holds %d compaction records, want none: the window was discarded", sink.count("compaction"))
	}
	records := compactionUsageRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d usage records for the interrupted compaction, want 1", len(records))
	}
	if records[0].InputTokens != 60 || records[0].OutputTokens != 40 {
		t.Errorf("the interrupted compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			records[0].InputTokens, records[0].OutputTokens)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn after the discarded compaction", res.Reason)
	}
}

// spentTurn is a turn whose request reports the tokens the ceiling test
// counts, so the only charge left to attribute is the compaction's.
func spentTurn(input, output int) fakeResponse {
	return fakeResponse{usage: &poo.UsageReport{InputTokens: input, OutputTokens: output}}
}

// TestANativeCompactionThatAnsweredWithoutAWindowIsStillCharged covers the
// answer the run could not install: the endpoint read the prompt and billed it
// before the window was judged, so the counts it reported are recorded and
// count against the run's ceiling. A failed window is not a free request.
func TestANativeCompactionThatAnsweredWithoutAWindowIsStillCharged(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 5}},
		spentTurn(10, 5),
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	tr.SetWindowError(errors.New("the endpoint answered with an item a window cannot hold"))
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 60, OutputTokens: 40})
	tr.SetWindowBilled(true)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = nativeWindowRequest()
	// The two turns report twenty tokens, so the compaction's own sixty and
	// forty are the whole of what exhausts the ceiling.
	opts.MaxRunTokens = 100
	res := runSession(t, newTestSession(t, opts), "do the thing")

	records := compactionUsageRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d usage records for the failed native compaction, want 1", len(records))
	}
	if records[0].InputTokens != 60 || records[0].OutputTokens != 40 {
		t.Errorf("the failed native compaction's usage is %d in and %d out, want the endpoint's 60 and 40",
			records[0].InputTokens, records[0].OutputTokens)
	}
	if got := sink.count("compaction"); got != 0 {
		t.Errorf("the log holds %d compaction records, want none: the window was refused", got)
	}
	if res.Detail.Usage == nil || res.Detail.Usage.InputTokens != 80 || res.Detail.Usage.OutputTokens != 50 {
		t.Errorf("the settle detail's usage is %+v, want the two turns' twenty plus the failure's hundred", res.Detail.Usage)
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted from the failed compaction's tokens", res.Reason)
	}
}

// TestAnInterruptedNativeCompactionWithoutUsageIsChargedTheEstimate covers the
// request an interrupt cut off before the endpoint reported anything: it was
// billed, so the run is charged the estimate a side request gets, measured from
// the view the compaction encoded: a request an interrupt cut off costs the run
// what any other request does.
func TestAnInterruptedNativeCompactionWithoutUsageIsChargedTheEstimate(t *testing.T) {
	tr := &blockedNativeTransport{
		Transport: scripted([]fakeResponse{
			{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 5}},
			spentTurn(10, 5),
		}),
		entered: make(chan struct{}),
		result:  poo.NativeCompactionResult{Billed: true},
		err:     context.Canceled,
	}
	sink := &recordingSink{}
	// The echo result is what the view's estimate prices: four thousand
	// characters are hundreds of tokens, so the ceiling sits above the two
	// turns' twenty and below what the compaction's estimate adds.
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 4000)}, sink)
	opts.Compactor = nativeWindowRequest()
	opts.MaxRunTokens = 40
	res := interruptedNativeRun(t, opts, tr)

	if records := compactionUsageRecords(sink); len(records) != 0 {
		t.Errorf("the log holds %d usage records for the interrupted compaction, want none: "+
			"an estimate is not a report", len(records))
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Errorf("the run settled %q, want budget_exhausted from the interrupted compaction's estimate", res.Reason)
	}
}

// TestANativeCompactionTheEndpointNeverAnsweredIsNotCharged covers the failure
// that never reached the endpoint's own work: the request cost nothing, so the
// same ceiling the answered one spent stays unmet.
func TestANativeCompactionTheEndpointNeverAnsweredIsNotCharged(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 5}},
		spentTurn(10, 5),
	})
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	tr.SetWindowError(errors.New("the credential could not be resolved, so no request went out"))
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 4000)}, sink)
	opts.Compactor = nativeWindowRequest()
	opts.MaxRunTokens = 40
	res := runSession(t, newTestSession(t, opts), "do the thing")

	if records := compactionUsageRecords(sink); len(records) != 0 {
		t.Errorf("the log holds %d usage records for the unbilled compaction, want none", len(records))
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: a request that never went out costs nothing", res.Reason)
	}
}
