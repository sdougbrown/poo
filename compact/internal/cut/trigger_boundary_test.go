package cut_test

// The trigger boundary tests run through the two block strategies: the
// strategies wire the same knobs (ratio, keep-recent, pointer limit)
// into the same Trigger, so the fit rule's arithmetic is asserted once,
// through a table of constructors, and a change to the shared logic
// lands in one place. The strategies' own defaults and their fallback
// wiring stay in the strategies' own test files.

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/mine"
	"github.com/sdougbrown/poo/compact/pointer"
	"github.com/sdougbrown/poo/compact/summary"
)

// subject is the surface the boundary tests read on a strategy.
type subject interface {
	Ratio() float64
	PointerMaxTokens() int
	ShouldCompact(view poo.View, budget poo.ContextBudget) bool
}

// factory builds one strategy under the configurations the tests use.
// A strategy with a fork sets hasFork and builds under a pointer limit;
// the summary makes no fork, so its need is the run's output limit.
type factory struct {
	name        string
	hasFork     bool
	defaulted   func() subject
	withRatio   func(ratio float64) subject
	pointerNeed func(limit int) subject
}

var factories = []factory{
	{
		name:        "mine",
		hasFork:     true,
		defaulted:   func() subject { return mine.New(mine.Options{KeepRecentTokens: 1}) },
		withRatio:   func(ratio float64) subject { return mine.New(mine.Options{Ratio: ratio, KeepRecentTokens: 1}) },
		pointerNeed: func(limit int) subject { return mine.New(mine.Options{KeepRecentTokens: 1, PointerMaxTokens: limit}) },
	},
	{
		name:      "pointer",
		hasFork:   true,
		defaulted: func() subject { return pointer.New(pointer.Options{KeepRecentTokens: 1}) },
		withRatio: func(ratio float64) subject { return pointer.New(pointer.Options{Ratio: ratio, KeepRecentTokens: 1}) },
		pointerNeed: func(limit int) subject {
			return pointer.New(pointer.Options{KeepRecentTokens: 1, PointerMaxTokens: limit})
		},
	},
	{
		name:      "summary",
		defaulted: func() subject { return summary.New(summary.Options{KeepRecentTokens: 1}) },
		withRatio: func(ratio float64) subject { return summary.New(summary.Options{Ratio: ratio, KeepRecentTokens: 1}) },
	},
}

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

// TestTheStrategyCompactsWhenTheNextAnswerDoesNotFit covers the
// computed rule: with no ratio configured, the strategy acts one token
// after the prompt is at the line the window, the larger of the run's
// output limit and the pointer pass, and the reserve draw.
func TestTheStrategyCompactsWhenTheNextAnswerDoesNotFit(t *testing.T) {
	view := triggerView()
	const limit = 8192
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s := f.defaulted()
			if got := s.Ratio(); got != 0 {
				t.Fatalf("the ratio is %v, want zero for no ratio trigger", got)
			}
			for _, test := range []struct {
				name   string
				window int
			}{
				{"a 128k window", 131072},
				{"a 200k window", 200000},
				{"a 1M window", 1048576},
			} {
				t.Run(test.name, func(t *testing.T) {
					need := limit
					if f.hasFork {
						need = max(limit, s.PointerMaxTokens())
					}
					line := test.window - need - poo.OutputReserve
					below := poo.ContextBudget{
						ContextLimit:   test.window,
						EstimatedInput: line,
						OutputLimit:    limit,
						CharsPerToken:  4,
						Turns:          2,
					}
					if s.ShouldCompact(view, below) {
						t.Errorf("the strategy compacted one token under the line at %d input tokens", line)
					}
					on := below
					on.EstimatedInput = line + 1
					if !s.ShouldCompact(view, on) {
						t.Errorf("the strategy declined on the line at %d input tokens", line+1)
					}
				})
			}
		})
	}
}

// TestTheStrategyDoesNotCompactAOneMillionWindowAt86Percent covers the
// regression the ratio brought back: a 1M window at 86% fill still has
// room for the answer, so the computed rule leaves it alone.
func TestTheStrategyDoesNotCompactAOneMillionWindowAt86Percent(t *testing.T) {
	const window = 1048576
	budget := poo.ContextBudget{
		ContextLimit:   window,
		EstimatedInput: window * 86 / 100,
		OutputLimit:    8192,
		CharsPerToken:  4,
		Turns:          2,
	}
	for _, f := range factories {
		if f.defaulted().ShouldCompact(triggerView(), budget) {
			t.Errorf("the %s strategy compacted at 86%% of a 1M window", f.name)
		}
	}
}

// TestAnExplicitRatioCompactsEarly covers the override: a configured
// ratio fires at that share of the prompt limit even when the next
// answer still fits.
func TestAnExplicitRatioCompactsEarly(t *testing.T) {
	for _, f := range factories {
		s := f.withRatio(0.5)
		below := poo.ContextBudget{
			ContextLimit:   400000,
			EstimatedInput: 199999,
			OutputLimit:    8192,
			CharsPerToken:  4,
			Turns:          2,
		}
		if s.ShouldCompact(triggerView(), below) {
			t.Errorf("the %s strategy compacted under half of the prompt limit", f.name)
		}
		on := below
		on.EstimatedInput = 200000
		if !s.ShouldCompact(triggerView(), on) {
			t.Errorf("the %s strategy declined at half of the prompt limit", f.name)
		}
	}
}

// TestALargerForkNeedMovesTheTriggerEarlier covers the fork's need: a
// pointer pass larger than the run's output limit moves the line the
// fit rule draws earlier by the difference.
func TestALargerForkNeedMovesTheTriggerEarlier(t *testing.T) {
	view := triggerView()
	const window = 131072
	const limit = 8192
	for _, f := range factories {
		if !f.hasFork {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			small := f.pointerNeed(16384)
			large := f.pointerNeed(32768)
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
		})
	}
}

// TestTheNeedTakesTheRunLimit covers the other side of the need: a run
// output limit larger than the fork's need draws the line at the run's
// own limit, and the default fork need stays the pinned 16384.
func TestTheNeedTakesTheRunLimit(t *testing.T) {
	view := triggerView()
	const window = 131072
	const runLimit = 32768
	// The run limit dominates the fork's need, so the line is the run
	// limit's own: 131072 - 32768 - 256 = 98048.
	line := window - runLimit - poo.OutputReserve
	for _, f := range factories {
		if !f.hasFork {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			s := f.defaulted()
			if got := s.PointerMaxTokens(); got != 16384 {
				t.Fatalf("the default fork need is %d, want the pinned 16384", got)
			}
			below := poo.ContextBudget{
				ContextLimit:   window,
				EstimatedInput: line,
				OutputLimit:    runLimit,
				CharsPerToken:  4,
				Turns:          2,
			}
			if s.ShouldCompact(view, below) {
				t.Errorf("the strategy compacted one token under the run limit's line at %d input tokens", line)
			}
			on := below
			on.EstimatedInput = line + 1
			if !s.ShouldCompact(view, on) {
				t.Errorf("the strategy declined on the run limit's line at %d input tokens", line+1)
			}
		})
	}
}

// TestTheStrategyDeclinesAReconstructionThatCannotPayForItself covers
// the non-convergence guard: on a window too small for the verbatim
// tail, the replacement and the need, a reconstruction gains nothing,
// so the strategy leaves the run alone.
func TestTheStrategyDeclinesAReconstructionThatCannotPayForItself(t *testing.T) {
	view := poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the system prompt"),
		poo.NewTextMessage(poo.RoleAssistant, strings.Repeat("x", 4000)),
		poo.NewTextMessage(poo.RoleAssistant, "the second answer"),
	}, nil, []poo.TurnSummary{
		{TurnIndex: 1, Start: 1, End: 2},
		{TurnIndex: 2, Start: 2, End: 3},
	})
	budget := poo.ContextBudget{
		ContextLimit:   40000,
		EstimatedInput: 30000,
		OutputLimit:    8192,
		CharsPerToken:  4,
		Turns:          2,
	}
	for _, f := range factories {
		if f.defaulted().ShouldCompact(view, budget) {
			t.Errorf("the %s strategy compacted a window no reconstruction can pay for", f.name)
		}
	}
}
