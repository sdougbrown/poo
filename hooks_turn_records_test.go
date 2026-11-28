package poo_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// A turn hook's start and end are written around the hook, so a hook a
// crash cut short leaves a start with no end.

// turnHookRecords returns the turn.hook records a session wrote, in
// order.
func turnHookRecords(sink *recordingSink) []*poo.TurnHookRecord {
	var out []*poo.TurnHookRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.TurnHookRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// hookPhases returns the phase and status of every turn.hook record, in
// order, as "phase/status".
func hookPhases(recs []*poo.TurnHookRecord) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Phase+"/"+rec.Status)
	}
	return out
}

// TestEveryTurnHookIsBracketedByItsRecords covers the normal turn end:
// each after-turn and before-settle hook gets a started record before it
// runs and an ended record after it returns, with its index in the chain.
func TestEveryTurnHookIsBracketedByItsRecords(t *testing.T) {
	sink := &recordingSink{}
	tool := &echoTool{}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{
		afterTurnFunc(func(poo.HookContext) (poo.TurnDecision, error) { return poo.TurnDecision{}, nil }),
		afterTurnFunc(func(poo.HookContext) (poo.TurnDecision, error) { return poo.TurnDecision{}, nil }),
	}
	opts.BeforeSettle = []poo.BeforeSettleHook{
		beforeSettleFunc(func(poo.HookContext, poo.Reason) (poo.SettleDecision, error) {
			return poo.Accept(), nil
		}),
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	recs := turnHookRecords(sink)
	want := []string{
		"after_turn/started", "after_turn/ended",
		"after_turn/started", "after_turn/ended",
		"before_settle/started", "before_settle/ended",
	}
	if got := hookPhases(recs); !slices.Equal(got, want) {
		t.Fatalf("the turn hook records are %v, want %v", got, want)
	}
	if recs[0].HookIndex != 0 || recs[2].HookIndex != 1 {
		t.Errorf("the after-turn hooks are numbered %d and %d, want 0 and 1", recs[0].HookIndex, recs[2].HookIndex)
	}
	if recs[4].HookIndex != 0 {
		t.Errorf("the before-settle hook is numbered %d, want 0", recs[4].HookIndex)
	}
	for _, rec := range recs {
		if rec.RunID == "" {
			t.Error("a turn hook record names no run")
		}
		if rec.Turn != 1 {
			t.Errorf("a turn hook record names the turn %d, want 1", rec.Turn)
		}
		if rec.Interrupted {
			t.Error("a turn hook record of a normal turn says it was interrupted")
		}
	}
}

// TestAnInterruptedTurnsHookRecordsSaySo covers the interrupted path: the
// hook's records name the turn's interrupt, so a reader that finds only a
// start knows what cut the hook short.
func TestAnInterruptedTurnsHookRecordsSaySo(t *testing.T) {
	sink := &recordingSink{}
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	tool := &echoTool{result: "the effect", call: func(ctx context.Context) (poo.ToolResult, error) {
		once.Do(func() { close(started) })
		<-release
		return poo.ToolResult{Text: "the effect"}, nil
	}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{
		afterTurnFunc(func(poo.HookContext) (poo.TurnDecision, error) { return poo.TurnDecision{}, nil }),
	}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The interrupt arrives while the tool runs, so the turn ends through
	// the interrupted path.
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	close(release)
	// The interrupt's message goes to the model as a steer, so the run
	// takes another turn: the test waits for the settled run and then
	// reads only the records of the turn the interrupt cut short.
	waitFor(t, "the run to settle", func() bool { return s.State() == poo.StateSettled })

	var interrupted []*poo.TurnHookRecord
	for _, rec := range turnHookRecords(sink) {
		if rec.Interrupted {
			interrupted = append(interrupted, rec)
		}
	}
	want := []string{"after_turn/started", "after_turn/ended"}
	if got := hookPhases(interrupted); !slices.Equal(got, want) {
		t.Fatalf("the interrupted turn's hook records are %v, want %v", got, want)
	}
	// A hook of the interrupted turn says so on both its records, and a
	// later turn's hooks do not.
	for _, rec := range turnHookRecords(sink) {
		if rec.Interrupted != (rec.Turn == 1) {
			t.Errorf("the turn %d %s record says interrupted=%v, want %v", rec.Turn, rec.Status, rec.Interrupted, rec.Turn == 1)
		}
	}
}
