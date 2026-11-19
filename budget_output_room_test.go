package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestTheOutputRoomIsShortWhenTheRoomClampWouldBite covers the rule a
// compaction strategy reads: the room left after the input and the reserve
// is under the output limit the request would carry, so max_tokens would be
// clamped to the room.
func TestTheOutputRoomIsShortWhenTheRoomClampWouldBite(t *testing.T) {
	const limit = 32768
	window := 131072
	// The room is exactly the output limit at this input.
	edge := window - poo.OutputReserve - limit

	cases := []struct {
		name   string
		budget poo.ContextBudget
		want   bool
	}{
		{
			name:   "the room equals the output limit",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: edge, OutputLimit: limit, Turns: 1},
			want:   false,
		},
		{
			name:   "the room is one token under the output limit",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: edge + 1, OutputLimit: limit, Turns: 1},
			want:   true,
		},
		{
			name:   "no request has been made",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window, OutputLimit: limit},
			want:   false,
		},
		{
			name:   "the window is unknown",
			budget: poo.ContextBudget{EstimatedInput: 1 << 20, OutputLimit: limit, Turns: 1},
			want:   false,
		},
		{
			name:   "the output limit is unknown",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window, Turns: 1},
			want:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.budget.OutputRoomShort(); got != c.want {
				t.Errorf("OutputRoomShort is %v, want %v", got, c.want)
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
