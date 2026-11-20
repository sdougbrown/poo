package poo_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
)

// Turn growth: the session's measured per-turn input growth, added to the fit
// rule when the setting asks for it.

// TestTheTurnGrowthMovesTheFitRuleByItsOwnTokens covers the budget rule: a
// turn's growth counts as prompt on both sides of the rule, so the trigger
// fires exactly that many tokens earlier.
func TestTheTurnGrowthMovesTheFitRuleByItsOwnTokens(t *testing.T) {
	const need, growth = 4096, 700
	for _, test := range []struct {
		name   string
		window int
		cap    int
		// line is the largest input that still fits: one token more is
		// short.
		line int
	}{
		{"the window side", 40000, 0, 40000 - need - poo.OutputReserve},
		{"the cap side", 400000, 200000, 200000 - poo.OutputReserve},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := poo.ContextBudget{
				ContextLimit:   test.window,
				InputLimit:     test.cap,
				EstimatedInput: test.line,
				OutputLimit:    need,
				Turns:          2,
			}
			if budget.RoomShort(need) {
				t.Errorf("the fit rule fired at %d input tokens, one under its line", test.line)
			}
			budget.EstimatedInput++
			if !budget.RoomShort(need) {
				t.Errorf("the fit rule declined at %d input tokens, on its line", test.line+1)
			}

			grown := budget
			grown.TurnGrowth = growth
			grown.EstimatedInput = test.line - growth + 1
			if !grown.RoomShort(need) {
				t.Errorf("the fit rule declined %d tokens earlier with a turn growth of %d", growth, growth)
			}
			grown.EstimatedInput--
			if grown.RoomShort(need) {
				t.Errorf("the fit rule fired %d tokens earlier with a turn growth of %d, want %d",
					growth+1, growth, growth)
			}
		})
	}
}

// The script the session-level turn-growth test runs: every turn's report adds
// growthReportStep tokens to the one before it.
const (
	// growthReportBase is the first turn's reported input.
	growthReportBase = 4000
	// growthReportStep is how many tokens each turn adds.
	growthReportStep = 2000
	// growthTurns is how many turns the script runs: enough that the fit
	// rule crosses its line mid-run.
	growthTurns = 32
	// growthOutputLimit is the output need the run's model declares.
	growthOutputLimit = 4096
	// growthContextLimit is the run's window.
	growthContextLimit = 40000
)

// growthScript is the scripted transport's turns: every turn but the last asks
// for a tool and reports more input than the turn before it.
func growthScript() []fakeResponse {
	out := make([]fakeResponse, 0, growthTurns)
	for i := 0; i < growthTurns; i++ {
		out = append(out, fakeResponse{
			calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"t"}`)}},
			usage: &poo.UsageReport{InputTokens: growthReportBase + i*growthReportStep, OutputTokens: 10},
		})
	}
	return append(out, fakeResponse{text: "the answer"})
}

// growthRun is one run of the growth script, as the probe saw it.
type growthRun struct {
	// growth is the turn growth of every budget the compactor was given.
	growth []int
	// acted is the call number of every gate the compactor compacted at.
	acted []int
}

// runGrowthScript runs the growth script with the setting on or off, and
// returns what the compactor saw.
func runGrowthScript(t *testing.T, turnGrowth bool) growthRun {
	t.Helper()
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted(growthScript()), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = growthContextLimit
	opts.Model.MaxOutput = growthOutputLimit
	opts.Compactor = probe
	opts.TurnGrowth = turnGrowth
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	return growthRun{growth: probe.growth, acted: probe.acted}
}

// TestTurnGrowthCompactsOneReportEarlier covers the session rule: with the
// setting on, a run whose turns each add the same number of tokens compacts
// one report earlier than the same run with the setting off.
func TestTurnGrowthCompactsOneReportEarlier(t *testing.T) {
	off := runGrowthScript(t, false)
	on := runGrowthScript(t, true)

	if len(off.acted) == 0 || len(on.acted) == 0 {
		t.Fatalf("the run compacted %d times off and %d times on, want at least once each",
			len(off.acted), len(on.acted))
	}
	if want := off.acted[0] - 1; on.acted[0] != want {
		t.Errorf("the run compacted at gate %d with the setting on, want %d: one report earlier than the %d the setting off uses",
			on.acted[0], want, off.acted[0])
	}

	// The setting off leaves the budget alone.
	for i, growth := range off.growth {
		if growth != 0 {
			t.Errorf("the budget at gate %d carries a turn growth of %d with the setting off, want 0", i+1, growth)
		}
	}

	// Before a difference exists the growth is the observation cap in
	// tokens, and once two reports have landed it is the measured step.
	if len(on.growth) < 3 {
		t.Fatalf("the compactor was given %d budgets, want at least 3", len(on.growth))
	}
	if want := int(math.Ceil(30000 / poo.CharsPerToken)); on.growth[0] != want {
		t.Errorf("the first budget carries a turn growth of %d, want the observation cap of %d tokens", on.growth[0], want)
	}
	// The second gate has one report, so the cap still stands in: only the
	// report after it gives the tracker a difference.
	for i, growth := range on.growth {
		if i < 2 {
			continue
		}
		if growth != growthReportStep {
			t.Errorf("the budget at gate %d carries a turn growth of %d, want the measured %d",
				i+1, growth, growthReportStep)
		}
	}
}

// liveTurnsOf returns the run of turns the view still holds, the leading
// removed turns skipped.
func liveTurnsOf(turns []poo.TurnSummary) []poo.TurnSummary {
	var live []poo.TurnSummary
	for _, turn := range turns {
		if turn.Removed {
			if len(live) > 0 {
				break
			}
			continue
		}
		live = append(live, turn)
	}
	return live
}

// growthProbe wraps a compactor and records what every budget it was given
// carried, and which gates it compacted at.
type growthProbe struct {
	// inner is the strategy the run compacts with.
	inner poo.Compactor
	// growth is the turn growth of every budget, in call order.
	growth []int
	// acted is the call number of every gate the inner strategy said yes at.
	acted []int
}

// ShouldCompact records the budget and asks the strategy it wraps.
func (p *growthProbe) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	p.growth = append(p.growth, budget.TurnGrowth)
	act := p.inner.ShouldCompact(view, budget)
	if act {
		p.acted = append(p.acted, len(p.growth))
	}
	return act
}

// Compact asks the strategy it wraps.
func (p *growthProbe) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return p.inner.Compact(ctx, in)
}

// TestAModelSwitchStartsTheTurnGrowthOver covers the switch: the new model's
// tokenizer counts the same view differently, so its first report is a baseline
// rather than growth measured by the model before it.
func TestAModelSwitchStartsTheTurnGrowthOver(t *testing.T) {
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 4000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 6000, OutputTokens: 5}},
		{text: "the first answer"},
		// The new model's first report is far above the old model's last one.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 20000, OutputTokens: 5}},
		{text: "the second answer"},
	}), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = 40000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the first run settled %q, want end_turn", res.Reason)
	}
	if len(probe.growth) < 3 || probe.growth[2] != 2000 {
		t.Fatalf("the first run's turn growth is %v, want a measured 2000 at its third gate", probe.growth)
	}

	if _, err := s.SetModel(poo.ModelInfo{
		Name: "next", ModelID: "test-model", ContextLimit: 40000,
		Ops: []poo.Op{poo.OpChat, poo.OpCompact},
	}); err != nil {
		t.Fatalf("switching models: %v", err)
	}
	if res := runSession(t, s, "do it again"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the second run settled %q, want end_turn", res.Reason)
	}
	if len(probe.growth) < 5 {
		t.Fatalf("the compactor was given %d budgets, want the second run's two", len(probe.growth))
	}
	// The second run's first gate still carries what the first model measured:
	// a switch starts the baseline over, not the differences.
	if got := probe.growth[3]; got != 2000 {
		t.Errorf("the first gate after the switch carries a turn growth of %d, want the measured 2000", got)
	}
	// The new model's first report is 14000 above the old model's last one. It
	// is the new baseline, so it records no difference.
	if got := probe.growth[4]; got != 2000 {
		t.Errorf("the gate after the new model's first report carries a turn growth of %d, want 2000: the switch starts the baseline over", got)
	}
}

// TestTheFirstTurnAfterACompactionIsTheNewBaseline covers the tracker's reset
// when a compaction installs its view. The report that follows a compaction is
// above the last one before it, and it is a baseline: the view the compaction
// left is what the run grows from, not the input it replaced.
func TestTheFirstTurnAfterACompactionIsTheNewBaseline(t *testing.T) {
	probe := &growthProbe{inner: roomCompactor{}}
	opts := testOptions(t, scripted([]fakeResponse{
		// The first two turns report the same input, so the tracker records no
		// difference and the growth stays the observation cap.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		// The report after the compaction is above the one before it.
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 31000, OutputTokens: 5}},
		{text: "the answer"},
	}), &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.ContextLimit = 34000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	if res := runSession(t, s, "do the thing"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	if len(probe.acted) == 0 || probe.acted[0] != 3 {
		t.Fatalf("the run compacted at the gates %v, want the third", probe.acted)
	}
	if len(probe.growth) < 4 {
		t.Fatalf("the compactor was given %d budgets, want the gate after the compaction", len(probe.growth))
	}
	// The growth at the gate after the compaction is the same as at the gate
	// of it: no difference was recorded in between. A tracker that measured
	// across the compaction would have recorded the 1000 tokens the report
	// grew by.
	if got := probe.growth[3]; got != probe.growth[2] {
		t.Errorf("the gate after the compaction carries a turn growth of %d, want the %d the gate of it carried",
			got, probe.growth[2])
	}
}

// runNativeGrowthScript runs the growth script with turn growth on or off and a
// native compactor, and returns what the compactor saw.
func runNativeGrowthScript(t *testing.T, turnGrowth bool) growthRun {
	t.Helper()
	tr := scripted(growthScript())
	tr.SetWindow(nativeWindowOf(`{"encrypted_content":"abc"}`))
	// The compaction reports the input its own request carried. The report is
	// the strategy's, not the view's, so it is no baseline either.
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 100, OutputTokens: 5})
	probe := &growthProbe{inner: native.New(native.Options{})}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Model.ContextLimit = growthContextLimit
	opts.Model.MaxOutput = growthOutputLimit
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = turnGrowth
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")
	return growthRun{growth: probe.growth, acted: probe.acted}
}

// TestANativeCompactionIsNotTheTurnGrowthsBaseline covers the native strategy
// with turn growth on. The compaction's own usage report is not the view's
// input, so the turn after it measures from the view the compaction left, and
// the strategy compacts one report earlier than it does with growth off.
func TestANativeCompactionIsNotTheTurnGrowthsBaseline(t *testing.T) {
	off := runNativeGrowthScript(t, false)
	on := runNativeGrowthScript(t, true)

	if len(off.acted) == 0 || len(on.acted) == 0 {
		t.Fatalf("the run compacted %d times off and %d times on, want at least once each",
			len(off.acted), len(on.acted))
	}
	if want := off.acted[0] - 1; on.acted[0] != want {
		t.Errorf("the run compacted at gate %d with the setting on, want %d: one report earlier than the %d the setting off uses",
			on.acted[0], want, off.acted[0])
	}
	// The gate after the compaction still carries the measured step. A tracker
	// that took the compaction's own 100 tokens as its baseline would measure
	// the tens of thousands between them as one turn's growth.
	if len(on.growth) <= on.acted[0] {
		t.Fatalf("the compactor was given %d budgets, want the gate after the compaction at %d", len(on.growth), on.acted[0])
	}
	if got := on.growth[on.acted[0]]; got != growthReportStep {
		t.Errorf("the gate after the compaction carries a turn growth of %d, want the measured %d", got, growthReportStep)
	}
}

// TestAFailedNativeCompactionsReportIsNotTheBaseline covers the report a
// compaction that installs no view leaves. The request was billed and it
// reported the input it carried, but the view did not move, so the turn after
// it measures from the view's last report: the compaction's own tens of tokens
// are not what the run grew by.
func TestAFailedNativeCompactionsReportIsNotTheBaseline(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 30000, OutputTokens: 5}},
		{calls: toolCallTurn().calls, usage: &poo.UsageReport{InputTokens: 31000, OutputTokens: 5}},
		{text: "the answer"},
	})
	tr.SetWindowError(errors.New("the endpoint refused the window"))
	tr.SetWindowBilled(true)
	tr.SetWindowUsage(&poo.UsageReport{InputTokens: 50, OutputTokens: 5})
	probe := &growthProbe{inner: native.New(native.Options{})}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, &recordingSink{})
	opts.Model.Ops = []poo.Op{poo.OpChat, poo.OpCompact}
	opts.Model.ContextLimit = 34000
	opts.Model.MaxOutput = 4096
	opts.Settings.ObservationCapChars = 400
	opts.Compactor = probe
	opts.TurnGrowth = true
	s := newTestSession(t, opts)

	runSession(t, s, "do the thing")
	if len(probe.acted) == 0 {
		t.Fatal("the run never asked for a window, want the first turn to fail its compaction")
	}
	if len(probe.growth) < 3 {
		t.Fatalf("the compactor was given %d budgets, want the gate after the failed compaction", len(probe.growth))
	}
	// The two turns reported the same input, so the growth is still the
	// observation cap. A tracker that took the compaction's 50 tokens as its
	// baseline would report the whole 30000 as one turn's growth.
	if got, want := probe.growth[2], probe.growth[1]; got != want {
		t.Errorf("the gate after the failed compaction carries a turn growth of %d, want the %d the gate of it carried", got, want)
	}
}

// roomCompactor compacts when the next answer no longer fits, and replaces
// every live turn but the newest with one message.
//
// It asks the model nothing, so the scripted transport's turns stay the run's
// own and every turn reports the input the script names.
type roomCompactor struct{}

// ShouldCompact reports that the next answer no longer fits and that there is
// an older turn to replace.
func (roomCompactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return budget.RoomShort(budget.OutputLimit) && len(liveTurnsOf(view.Turns())) >= 2
}

// Compact replaces every live turn but the newest.
func (roomCompactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	live := liveTurnsOf(in.View.Turns())
	if len(live) < 2 {
		return poo.Compaction{}, errors.New("there is nothing older to compact")
	}
	return poo.Compaction{
		Strategy: "cut",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: live[0].TurnIndex, Last: live[len(live)-2].TurnIndex},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary")},
		},
	}, nil
}
