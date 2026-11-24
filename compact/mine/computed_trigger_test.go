package mine_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/mine"
)

// triggerView builds the two-turn view the trigger tests read: a large
// covered turn and a small newest one, so a reconstruction has room to
// give back.
func triggerView() poo.View {
	return poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the system prompt"),
		poo.NewTextMessage(poo.RoleAssistant, strings.Repeat("x", 400000)),
		poo.NewTextMessage(poo.RoleAssistant, "the second answer"),
	}, nil, []poo.TurnSummary{
		{TurnIndex: 1, Start: 1, End: 2},
		{TurnIndex: 2, Start: 2, End: 3},
	})
}

// TestTheMineStrategyCompactsWhenTheNextAnswerDoesNotFit covers the
// computed rule: with no ratio configured, the strategy acts one token
// after the prompt is at the line the window, the larger of the run's
// output limit and the pointer pass, and the reserve draw.
func TestTheMineStrategyCompactsWhenTheNextAnswerDoesNotFit(t *testing.T) {
	view := triggerView()
	c := mine.New(mine.Options{KeepRecentTokens: 1})
	if got := c.Ratio(); got != 0 {
		t.Fatalf("the ratio is %v, want zero for no ratio trigger", got)
	}
	const limit = 8192
	for _, test := range []struct {
		name   string
		window int
	}{
		{"a 128k window", 131072},
		{"a 200k window", 200000},
		{"a 1M window", 1048576},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := test.window - max(limit, c.PointerMaxTokens()) - poo.OutputReserve
			below := poo.ContextBudget{
				ContextLimit:   test.window,
				EstimatedInput: line,
				OutputLimit:    limit,
				CharsPerToken:  4,
				Turns:          2,
			}
			if c.ShouldCompact(view, below) {
				t.Errorf("the strategy compacted one token under the line at %d input tokens", line)
			}
			on := below
			on.EstimatedInput = line + 1
			if !c.ShouldCompact(view, on) {
				t.Errorf("the strategy declined on the line at %d input tokens", line+1)
			}
		})
	}
}

// TestTheMineStrategyDoesNotCompactAOneMillionWindowAt86Percent covers
// the regression the ratio brought back: a 1M window at 86% fill still
// has room for the answer, so the computed rule leaves it alone.
func TestTheMineStrategyDoesNotCompactAOneMillionWindowAt86Percent(t *testing.T) {
	c := mine.New(mine.Options{KeepRecentTokens: 1})
	const window = 1048576
	budget := poo.ContextBudget{
		ContextLimit:   window,
		EstimatedInput: window * 86 / 100,
		OutputLimit:    8192,
		CharsPerToken:  4,
		Turns:          2,
	}
	if c.ShouldCompact(triggerView(), budget) {
		t.Error("the strategy compacted at 86% of a 1M window")
	}
}

// TestAnExplicitMineRatioCompactsEarly covers the override: a configured
// ratio fires at that share of the prompt limit even when the next
// answer still fits.
func TestAnExplicitMineRatioCompactsEarly(t *testing.T) {
	c := mine.New(mine.Options{Ratio: 0.5, KeepRecentTokens: 1})
	budget := poo.ContextBudget{
		ContextLimit:   400000,
		EstimatedInput: 199999,
		OutputLimit:    8192,
		CharsPerToken:  4,
		Turns:          2,
	}
	if c.ShouldCompact(triggerView(), budget) {
		t.Error("the strategy compacted under half of the prompt limit")
	}
	budget.EstimatedInput = 200000
	if !c.ShouldCompact(triggerView(), budget) {
		t.Error("the strategy declined at half of the prompt limit")
	}
}

// TestALargerMineForkNeedMovesTheTriggerEarlier covers the fork's need:
// a pointer pass larger than the run's output limit moves the line the
// fit rule draws earlier by the difference.
func TestALargerMineForkNeedMovesTheTriggerEarlier(t *testing.T) {
	view := triggerView()
	const window = 131072
	const limit = 8192
	small := mine.New(mine.Options{KeepRecentTokens: 1, PointerMaxTokens: 16384})
	large := mine.New(mine.Options{KeepRecentTokens: 1, PointerMaxTokens: 32768})
	smallLine := window - max(limit, small.PointerMaxTokens()) - poo.OutputReserve
	largeLine := window - max(limit, large.PointerMaxTokens()) - poo.OutputReserve
	if got := smallLine - largeLine; got != 16384 {
		t.Fatalf("the larger fork need moved the line by %d tokens, want 16384", got)
	}
	budget := poo.ContextBudget{
		ContextLimit:   window,
		EstimatedInput: largeLine,
		OutputLimit:    limit,
		CharsPerToken:  4,
		Turns:          2,
	}
	if large.ShouldCompact(view, budget) {
		t.Errorf("the strategy compacted one token under its line at %d input tokens", largeLine)
	}
	if small.ShouldCompact(view, budget) {
		t.Errorf("the strategy with the smaller fork need compacted at %d input tokens", largeLine)
	}
	budget.EstimatedInput++
	if !large.ShouldCompact(view, budget) {
		t.Errorf("the strategy declined on its line at %d input tokens", largeLine+1)
	}
}
