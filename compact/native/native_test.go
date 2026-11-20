package native_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
)

// TestTheNativeStrategyCompactsWhenTheNextAnswerDoesNotFit covers the
// trigger: the strategy acts one token after the prompt is at the line the
// window and the input cap draw, and not one token before it.
func TestTheNativeStrategyCompactsWhenTheNextAnswerDoesNotFit(t *testing.T) {
	strategy := native.New(native.Options{})
	if got := strategy.Ratio(); got != 0 {
		t.Errorf("the ratio is %v, want zero for no ratio trigger", got)
	}
	for _, test := range []struct {
		name   string
		window int
		cap    int
		limit  int
	}{
		{"a 128k window", 131072, 0, 32768},
		{"a 200k window", 200000, 0, 32768},
		{"a 1M window", 1048576, 0, 32768},
		{"an input cap under the window", 400000, 272000, 32768},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := test.window - test.limit - poo.OutputReserve
			if test.cap > 0 && test.cap-poo.OutputReserve < line {
				line = test.cap - poo.OutputReserve
			}
			below := poo.ContextBudget{ContextLimit: test.window, InputLimit: test.cap, EstimatedInput: line, OutputLimit: test.limit, Turns: 1}
			if strategy.ShouldCompact(nil, below) {
				t.Errorf("the strategy compacted one token under the line at %d input tokens", line)
			}
			on := below
			on.EstimatedInput = line + 1
			if !strategy.ShouldCompact(nil, on) {
				t.Errorf("the strategy declined on the line at %d input tokens", line+1)
			}
		})
	}
}

// TestTheNativeStrategyDoesNotRepeatACompactionThatCannotRestoreTheRoom
// covers the guard: when the input the last compaction left is still short,
// another compaction would repeat every turn.
func TestTheNativeStrategyDoesNotRepeatACompactionThatCannotRestoreTheRoom(t *testing.T) {
	strategy := native.New(native.Options{})
	budget := poo.ContextBudget{ContextLimit: 100000, EstimatedInput: 80000, OutputLimit: 32768, Turns: 2}
	if !strategy.ShouldCompact(nil, budget) {
		t.Fatal("the strategy declined with the next answer not fitting")
	}
	budget.CompactedInput = 90000
	if strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy repeated a compaction whose input is still short")
	}
	budget.CompactedInput = 60000
	if !strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy declined when the last compaction restored the room")
	}
}

// TestTheNativeStrategyHonoursACustomRatio covers a configured ratio: the
// strategy acts at the share it was given, and not at the summary ratio.
func TestTheNativeStrategyHonoursACustomRatio(t *testing.T) {
	strategy := native.New(native.Options{Ratio: 0.5})
	if got := strategy.Ratio(); got != 0.5 {
		t.Errorf("the ratio is %v, want the configured 0.5", got)
	}
	for _, test := range []struct {
		name  string
		limit int
		input int
		want  bool
	}{
		{"below the ratio", 1000, 499, false},
		{"at the ratio", 1000, 500, true},
		{"below the ratio of a wider window", 2000, 999, false},
		{"at the ratio of a wider window", 2000, 1000, true},
	} {
		budget := poo.ContextBudget{ContextLimit: test.limit, EstimatedInput: test.input, Turns: 1}
		if got := strategy.ShouldCompact(nil, budget); got != test.want {
			t.Errorf("%s: the strategy compacts %v, want %v", test.name, got, test.want)
		}
	}
}

// TestTheNativeStrategyRequestsAProviderWindow covers what the strategy hands
// back: a window request, and no form the strategy could apply itself.
func TestTheNativeStrategyRequestsAProviderWindow(t *testing.T) {
	strategy := native.New(native.Options{})
	view := poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "the instructions"),
		poo.NewTextMessage(poo.RoleUser, "do the thing"),
	}, nil, nil)

	compaction, err := strategy.Compact(context.Background(), poo.CompactionEnv{View: view})
	if err != nil {
		t.Fatalf("asking for a window: %v", err)
	}
	if compaction.Strategy != native.Strategy {
		t.Errorf("the strategy is %q, want %q", compaction.Strategy, native.Strategy)
	}
	if compaction.Window == nil {
		t.Fatal("the compaction holds no window request")
	}
	if compaction.Replace != nil || len(compaction.Rewrite) > 0 || compaction.End != nil {
		t.Errorf("the compaction holds another form: %+v", compaction)
	}
}

// TestTheNativeStrategyActsAtARatioOfTheInputCap covers an endpoint that
// takes a smaller prompt than its window: the ratio is a share of the prompt
// the endpoint accepts.
func TestTheNativeStrategyActsAtARatioOfTheInputCap(t *testing.T) {
	strategy := native.New(native.Options{Ratio: 0.5})
	budget := poo.ContextBudget{ContextLimit: 4000, InputLimit: 1000, EstimatedInput: 499, Turns: 1}
	if strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy compacted below half of the input cap")
	}
	budget.EstimatedInput = 500
	if !strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy waited for half of the window, not half of the input cap")
	}
}

// TestTheCompactedInputGuardLeavesTheTurnGrowthOut covers the guard with turn
// growth on: it asks whether a compaction restores the room for the next
// answer, so a room that fits the answer but not one more turn's growth is
// still room, and the compaction happens.
func TestTheCompactedInputGuardLeavesTheTurnGrowthOut(t *testing.T) {
	strategy := native.New(native.Options{})
	const need, growth = 4096, 2000
	budget := poo.ContextBudget{
		ContextLimit:   40000,
		EstimatedInput: 40000 - need - poo.OutputReserve + 1,
		OutputLimit:    need,
		CompactedInput: 40000 - need - poo.OutputReserve - growth/2,
		TurnGrowth:     growth,
		Turns:          2,
	}
	if !strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy declined a compaction whose input left room for the answer")
	}
	// The same budget with the input the compaction left under the line
	// altogether is the guard's own case: it declines, so the run settles
	// no_output_room instead of compacting every turn.
	budget.CompactedInput = 40000
	if strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy repeated a compaction whose input is still short")
	}
}
