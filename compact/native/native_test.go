package native_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/native"
)

// TestTheNativeStrategyActsAtSummaryRatio covers when the strategy asks for
// a provider window. It waits for the same share of the window summary uses,
// and only after the run has asked the model something.
func TestTheNativeStrategyActsAtSummaryRatio(t *testing.T) {
	strategy := native.New(native.Options{})
	if got := strategy.Ratio(); got != native.DefaultRatio {
		t.Errorf("the ratio is %v, want the summary ratio %v", got, native.DefaultRatio)
	}
	for _, test := range []struct {
		name  string
		input int
		turns int
		want  bool
	}{
		{"below the ratio", 849, 1, false},
		{"at the ratio", 850, 1, true},
		{"above the ratio", 900, 1, true},
		{"before any turn", 900, 0, false},
	} {
		budget := poo.ContextBudget{ContextLimit: 1000, EstimatedInput: test.input, Turns: test.turns}
		if got := strategy.ShouldCompact(nil, budget); got != test.want {
			t.Errorf("%s: the strategy compacts %v, want %v", test.name, got, test.want)
		}
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
	budget := poo.ContextBudget{ContextLimit: 1000, InputLimit: 400, EstimatedInput: 200, Turns: 1}
	if !strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy waited for half of the window, not half of the input cap")
	}
	budget.EstimatedInput = 199
	if strategy.ShouldCompact(nil, budget) {
		t.Error("the strategy compacted below half of the input cap")
	}
}
