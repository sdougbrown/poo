package poo_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/sdougbrown/poo"
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
