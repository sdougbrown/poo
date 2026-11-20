package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
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

// gateAtTransport holds one request open until the test lets it go, so an
// input can be queued while that request is in flight.
type gateAtTransport struct {
	*pootest.Transport
	// at is the 1-based number of the request the transport holds.
	at int
	// entered closes when the held request arrives.
	entered chan struct{}
	// release lets the held request through.
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

// Stream holds the request it was told to hold and serves the script.
func (g *gateAtTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	g.mu.Lock()
	g.calls++
	held := g.calls == g.at
	g.mu.Unlock()
	if held {
		close(g.entered)
		select {
		case <-g.release:
		case <-ctx.Done():
			return nil, poo.Rendered{}, ctx.Err()
		}
	}
	return g.Transport.Stream(ctx, req)
}

// turnCounter is an AfterTurn hook that records the turns it was called for.
type turnCounter struct {
	mu    sync.Mutex
	turns []int
}

// AfterTurn records the turn.
func (c *turnCounter) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, ctx.Turn)
	return poo.TurnDecision{}, nil
}

// seen returns the turns the hook was called for.
func (c *turnCounter) seen() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.turns...)
}

// TestARefusedTurnSettlesWhenItsBudgetIsSpent covers the refused turn's own
// stop conditions: the turn ended, so the run's turn budget is spent and the
// refusal buys no compaction and no second request.
func TestARefusedTurnSettlesWhenItsBudgetIsSpent(t *testing.T) {
	tr := scripted([]fakeResponse{{err: contextLengthRejection("the prompt is too long", 0)}})
	sink := &recordingSink{}
	opts := refusalCompactionOptions(t, tr, sink)
	opts.MaxTurns = 1
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonMaxTurns {
		t.Fatalf("the run settled %q (%s), want max_turns", res.Reason, res.Detail.Text)
	}
	if got := len(compactionRecords(sink)); got != 0 {
		t.Errorf("the log holds %d compactions, want none for a turn that ended", got)
	}
	if got := len(tr.Requests()); got != 1 {
		t.Errorf("the transport got %d requests, want the refused one alone", got)
	}
}

// TestARefusedTurnRunsTheAfterTurnHooks covers the hooks' every-turn contract:
// the refused turn ends like any other, so the hooks see it.
func TestARefusedTurnRunsTheAfterTurnHooks(t *testing.T) {
	turns := append(refusedTurns(0), fakeResponse{text: "## Goal\nfinish"}, fakeResponse{text: "the answer"})
	hook := &turnCounter{}
	s := newTestSession(t, refusalCompactionOptions(t, scripted(turns), &recordingSink{}), poo.WithAfterTurn(hook))

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	want := []int{1, 2, 3, 4}
	got := hook.seen()
	if len(got) != len(want) {
		t.Fatalf("the hooks saw the turns %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the hooks saw the turns %v, want %v", got, want)
		}
	}
}

// TestASteerQueuedDuringTheRefusedRequestReachesTheResend covers the drained
// queue: the refused turn ends, so the steer it carried is in the view the
// compaction resends.
func TestASteerQueuedDuringTheRefusedRequestReachesTheResend(t *testing.T) {
	inner := scripted(append(refusedTurns(0),
		fakeResponse{text: "## Goal\nfinish"},
		fakeResponse{text: "the answer"},
	))
	tr := &gateAtTransport{Transport: inner, at: 3, entered: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = summary.New(summary.Options{KeepRecentTokens: 1})
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.entered
	if _, err := s.Send("steer the work", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	close(tr.release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	requests := inner.Requests()
	resend := -1
	for i, req := range requests {
		for _, m := range req.Messages {
			if m.Origin == poo.OriginCompaction {
				resend = i
			}
		}
		if resend >= 0 {
			break
		}
	}
	if resend < 0 {
		t.Fatal("no request carries the compaction's replacement")
	}
	found := false
	for _, m := range requests[resend].Messages {
		if strings.Contains(m.Text(), "steer the work") {
			found = true
		}
	}
	if !found {
		t.Errorf("the request after the compaction (number %d) carries no steer", resend+1)
	}
}

// interruptedOnceCompactor lets the first compaction be interrupted and
// answers the next one, so the gate has to run the forced pass twice.
type interruptedOnceCompactor struct {
	// started closes when the first compaction is running.
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

// ShouldCompact declines: only the endpoint's refusal asks for a compaction.
func (c *interruptedOnceCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return false
}

// Compact waits for its context the first time, and replaces the first turn
// afterwards.
func (c *interruptedOnceCompactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	c.mu.Lock()
	c.calls++
	calls := c.calls
	c.mu.Unlock()
	if calls == 1 {
		c.once.Do(func() { close(c.started) })
		<-ctx.Done()
		return poo.Compaction{}, ctx.Err()
	}
	return poo.Compaction{Strategy: "summary", Replace: &poo.RangeReplacement{
		Cut:      poo.TurnRange{First: 1, Last: 1},
		Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
	}}, nil
}

// callsOf returns how many compactions the compactor ran.
func (c *interruptedOnceCompactor) callsOf() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestAnInterruptThatEndsTheForcedCompactionDoesNotSpendIt covers the forced
// pass and an interrupt: the compaction the refusal asked for is ended, and
// the restarted gate compacts again instead of sending the refused request
// uncompacted.
func TestAnInterruptThatEndsTheForcedCompactionDoesNotSpendIt(t *testing.T) {
	turns := append(refusedTurns(0), fakeResponse{text: "the answer"})
	compactor := &interruptedOnceCompactor{started: make(chan struct{})}
	tr := scripted(turns)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-compactor.started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	if got := compactor.callsOf(); got != 2 {
		t.Errorf("the compactor ran %d times, want the interrupted pass and the one after it", got)
	}
	records := compactionRecords(sink)
	if len(records) != 1 {
		t.Fatalf("the log holds %d compactions, want the one the restarted gate applied", len(records))
	}
	// The resend carries the replacement, so the request that goes out is
	// the compacted one.
	requests := tr.Requests()
	last := requests[len(requests)-1]
	found := false
	for _, m := range last.Messages {
		if m.Origin == poo.OriginCompaction {
			found = true
		}
	}
	if !found {
		t.Error("the request after the interrupted compaction carries no replacement")
	}
}

// firstLiveTurnCompactor replaces the first turn the view still holds.
//
// It declines on its own and never asks the transport anything, so a
// compaction it makes is the forced one and nothing else.
func firstLiveTurnCompactor() *compactionCompactor {
	return &compactionCompactor{
		should: func(poo.View, poo.ContextBudget) bool { return false },
		compaction: func(view poo.View) (poo.Compaction, error) {
			for _, turn := range view.Turns() {
				if turn.Removed {
					continue
				}
				return poo.Compaction{Strategy: "summary", Replace: &poo.RangeReplacement{
					Cut:      poo.TurnRange{First: turn.TurnIndex, Last: turn.TurnIndex},
					Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
				}}, nil
			}
			return poo.Compaction{}, errors.New("the view holds no turn to summarize")
		},
	}
}

// TestAnInterruptOnTheResendLeavesTheNextRefusalItsOwnCompaction covers the
// refusal the run was compacting for when the interrupt landed on its resend:
// the resend did not finish, so the next refusal compacts again.
func TestAnInterruptOnTheResendLeavesTheNextRefusalItsOwnCompaction(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{err: contextLengthRejection("the prompt is too long", 0)},
		// The resend of the first refusal is request four, and the test
		// interrupts it.
		{err: contextLengthRejection("the prompt is still too long", 0)},
		{text: "the answer"},
	})
	tr := &gateAtTransport{Transport: inner, at: 4, entered: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = firstLiveTurnCompactor()
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.entered
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	if got := len(compactionRecords(sink)); got != 2 {
		t.Errorf("the log holds %d compactions, want one for each refusal", got)
	}
}

// TestASecondRefusalInTheRunCompactsAgain covers the bound's other side: the
// one forced compaction belongs to a refused request, so a request that went
// out in between leaves the next refusal its own.
func TestASecondRefusalInTheRunCompactsAgain(t *testing.T) {
	turns := []fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{err: contextLengthRejection("the prompt is too long", 0)},
		{text: "## Goal\nfinish"},
		// The request the first compaction resent, answered.
		{calls: []poo.ToolCall{{CallID: "c3", Name: "echo", Arguments: []byte(`{"text":"three"}`)}}},
		{err: contextLengthRejection("the prompt is too long again", 0)},
		{text: "## Goal\nfinish again"},
		{text: "the answer"},
	}
	sink := &recordingSink{}
	s := newTestSession(t, refusalCompactionOptions(t, scripted(turns), sink))

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	records := compactionRecords(sink)
	if len(records) != 2 {
		t.Fatalf("the log holds %d compactions, want one for each refused request", len(records))
	}
	// The second cut names the refused turn, which holds no messages, so the
	// range it replaces is the turn before it alone.
	if cut := records[1].Cut; cut == nil || cut.First != 2 {
		t.Errorf("the second compaction covers %v, want the turns from turn 2", cut)
	}
}

// TestAForcedCompactionThatFailsSettlesWithTheRefusal covers the failure: the
// endpoint's refusal is the reason the run ends with, and no request follows
// the one that was refused.
func TestAForcedCompactionThatFailsSettlesWithTheRefusal(t *testing.T) {
	tr := scripted(refusedTurns(0))
	compactor := pootest.NewCompactor(false, poo.Compaction{})
	compactor.SetError(errors.New("the summary failed"))
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Compactor = compactor
	s := newTestSession(t, opts)

	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonContextExhausted {
		t.Fatalf("the run settled %q (%s), want context_exhausted", res.Reason, res.Detail.Text)
	}
	if res.Detail.Source != "provider:context_length" {
		t.Errorf("the settle source is %q, want provider:context_length", res.Detail.Source)
	}
	if got := len(tr.Requests()); got != 3 {
		t.Errorf("the transport got %d requests, want the refused one to be the last", got)
	}
	if got := compactor.Calls(); got != 1 {
		t.Errorf("the compactor ran %d times, want the one the refusal asked for", got)
	}
}

// interruptingAfterTurn holds one turn's AfterTurn hook open until the test
// sends its interrupt.
type interruptingAfterTurn struct {
	// turn is the turn the hook holds open.
	turn int
	// entered closes when the hook reaches its turn.
	entered chan struct{}
	// release lets the hook return.
	release chan struct{}
}

// AfterTurn holds the turn open.
func (h *interruptingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	if ctx.Turn != h.turn {
		return poo.TurnDecision{}, nil
	}
	close(h.entered)
	<-h.release
	return poo.TurnDecision{}, nil
}

// TestAnInterruptDuringTheRefusedTurnsHooksStillCompacts covers the refused
// turn's own interrupt: the turn end is cut short, and the refusal it was
// compacting for is still the run's next answer. The gate compacts, and the
// request that goes out carries the interrupt's message.
func TestAnInterruptDuringTheRefusedTurnsHooksStillCompacts(t *testing.T) {
	turns := append(refusedTurns(0), fakeResponse{text: "## Goal\nfinish"}, fakeResponse{text: "the answer"})
	hook := &interruptingAfterTurn{turn: 3, entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted(turns)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.Compactor = summary.New(summary.Options{KeepRecentTokens: 1})
	s := newTestSession(t, opts, poo.WithAfterTurn(hook))

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hook.entered
	if _, err := s.Send("carry on with this", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	close(hook.release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}
	if got := len(compactionRecords(sink)); got != 1 {
		t.Fatalf("the log holds %d compactions, want the one the refusal asked for", got)
	}
	// The two turns, the refused request, the compaction's own request and the
	// resend: a run that sent the refused request again would have made four.
	requests := tr.Requests()
	if len(requests) != 5 {
		t.Fatalf("the transport got %d requests, want the compaction before the resend", len(requests))
	}
	// No request carries the interrupt's message before the compaction: the
	// refused request is not sent again uncompacted.
	for i, req := range requests {
		compacted, steered := false, false
		for _, m := range req.Messages {
			if m.Origin == poo.OriginCompaction {
				compacted = true
			}
			if strings.Contains(m.Text(), "carry on with this") {
				steered = true
			}
		}
		if !steered {
			continue
		}
		if !compacted {
			t.Fatalf("request %d carries the interrupt's message and no compaction", i+1)
		}
		return
	}
	t.Errorf("no request carries the interrupt's message; the transport got %d requests", len(requests))
}
