package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestTheRoomIsShortWhenTheNextAnswerDoesNotFit covers the rule a
// compaction strategy reads: the prompt plus the output need and the reserve
// is over the window, or the prompt plus the reserve is over the input cap.
func TestTheRoomIsShortWhenTheNextAnswerDoesNotFit(t *testing.T) {
	const limit = 32768
	window := 131072
	// The window's line is exactly at this input.
	edge := window - poo.OutputReserve - limit
	cap := 12000

	cases := []struct {
		name   string
		budget poo.ContextBudget
		need   int
		want   bool
	}{
		{
			name:   "the window has exactly the need and the reserve",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: edge, OutputLimit: limit, Turns: 1},
			need:   limit,
			want:   false,
		},
		{
			name:   "the window is one token short",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: edge + 1, OutputLimit: limit, Turns: 1},
			need:   limit,
			want:   true,
		},
		{
			name:   "no request has been made",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window, OutputLimit: limit},
			need:   limit,
			want:   false,
		},
		{
			name:   "no window and no cap",
			budget: poo.ContextBudget{EstimatedInput: 1 << 20, OutputLimit: limit, Turns: 1},
			need:   limit,
			want:   false,
		},
		{
			name:   "the input cap has exactly the reserve",
			budget: poo.ContextBudget{ContextLimit: window, InputLimit: cap, EstimatedInput: cap - poo.OutputReserve, OutputLimit: limit, Turns: 1},
			need:   limit,
			want:   false,
		},
		{
			name:   "the input cap is one token short",
			budget: poo.ContextBudget{ContextLimit: window, InputLimit: cap, EstimatedInput: cap - poo.OutputReserve + 1, OutputLimit: limit, Turns: 1},
			need:   limit,
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.budget.RoomShort(c.need); got != c.want {
				t.Errorf("RoomShort is %v, want %v", got, c.want)
			}
		})
	}
}

// TestTheSessionsBudgetCarriesTheOutputLimitBeforeTheRoomClamp covers the
// field the strategies read: the model's own limit, or the default for its
// window, and never the clamped value.
func TestTheSessionsBudgetCarriesTheOutputLimitBeforeTheRoomClamp(t *testing.T) {
	cases := []struct {
		name  string
		model poo.ModelInfo
		want  int
	}{
		{"a declared limit", poo.ModelInfo{ContextLimit: 131072, MaxOutput: 4096}, 4096},
		{"a declared limit above the default", poo.ModelInfo{ContextLimit: 200000, MaxOutput: 64000}, 32768},
		{"the default for a large window", poo.ModelInfo{ContextLimit: 131072}, 32768},
		{"the default for a small window", poo.ModelInfo{ContextLimit: 16384}, 8192},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probe := &budgetProbe{}
			opts := testOptions(t, scripted([]fakeResponse{{text: "hi"}}), &echoTool{}, &recordingSink{})
			opts.Model = c.model
			opts.Model.ModelID = "m"
			opts.Compactor = probe
			s := newTestSession(t, opts)
			runSession(t, s, "hello")
			if probe.last.OutputLimit != c.want {
				t.Errorf("the budget's output limit is %d, want %d", probe.last.OutputLimit, c.want)
			}
		})
	}
}

// budgetProbe is a compactor that never compacts and keeps the last budget
// it was asked about.
type budgetProbe struct {
	last poo.ContextBudget
}

func (p *budgetProbe) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	p.last = budget
	return false
}

func (p *budgetProbe) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return poo.Compaction{}, nil
}
