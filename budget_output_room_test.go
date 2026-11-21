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

// TestRoomShortAfterLeavesTheTurnGrowthOut covers the check a strategy makes
// about a compaction's own input: the budget is read with the input the caller
// names and with no turn growth, so one more turn is never what keeps a
// compaction from happening.
func TestRoomShortAfterLeavesTheTurnGrowthOut(t *testing.T) {
	const need, growth = 4096, 2000
	// line is the largest input that fits the window with the reserve and the
	// output need, and no turn growth.
	line := 40000 - need - poo.OutputReserve
	budget := poo.ContextBudget{
		ContextLimit:   40000,
		EstimatedInput: line - growth + 1,
		OutputLimit:    need,
		TurnGrowth:     growth,
		Turns:          2,
	}
	if !budget.RoomShort(need) {
		t.Fatal("the budget is not short with the turn growth on, want the test's setup to be short")
	}
	if budget.RoomShortAfter(line, need) {
		t.Error("RoomShortAfter counted the turn growth, so a compaction that restores the room for the answer was refused")
	}
	if !budget.RoomShortAfter(line+1, need) {
		t.Error("RoomShortAfter declined an input one token over the line")
	}
}

// TestTheRequestIsBlockedOnlyWhenNoRoomIsLeft covers the gate's block
// decision on both lines it draws: the window's room under the smallest
// max_tokens, and the prompt over the endpoint's input cap.
func TestTheRequestIsBlockedOnlyWhenNoRoomIsLeft(t *testing.T) {
	const window, cap = 100000, 8000
	cases := []struct {
		name   string
		budget poo.ContextBudget
		want   bool
		source string
		room   int
	}{
		{
			name:   "the window leaves exactly the minimum",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window - poo.OutputReserve - poo.MinOutputTokens, OutputLimit: 4096},
			room:   poo.MinOutputTokens,
		},
		{
			name:   "the window leaves one token less",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window - poo.OutputReserve - poo.MinOutputTokens + 1, OutputLimit: 4096},
			want:   true,
			source: "no_output_room",
			room:   poo.MinOutputTokens - 1,
		},
		{
			// The output limit is the default for the window, and the
			// room the window leaves is wider than it.
			name:   "an output limit of zero",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: 80000},
			room:   poo.DefaultMaxOutputFor(window),
		},
		{
			name:   "no window",
			budget: poo.ContextBudget{EstimatedInput: 1 << 20, OutputLimit: 4096},
			room:   4096,
		},
		{
			name:   "no window and no output limit",
			budget: poo.ContextBudget{EstimatedInput: 1 << 20},
			room:   poo.DefaultMaxOutputFor(0),
		},
		{
			name:   "the cap is exactly the reserve away",
			budget: poo.ContextBudget{ContextLimit: window, InputLimit: cap, EstimatedInput: cap - poo.OutputReserve, OutputLimit: 4096},
		},
		{
			name:   "the cap is one token over",
			budget: poo.ContextBudget{ContextLimit: window, InputLimit: cap, EstimatedInput: cap - poo.OutputReserve + 1, OutputLimit: 4096},
			want:   true,
			source: "prompt_limit",
		},
		{
			// The turn growth is the strategy's to plan for, not the
			// gate's: a request with the minimum room left goes out
			// whatever the next turn is expected to add.
			name:   "a large turn growth with the minimum room left",
			budget: poo.ContextBudget{ContextLimit: window, EstimatedInput: window - poo.OutputReserve - poo.MinOutputTokens, OutputLimit: 4096, TurnGrowth: 20000},
			room:   poo.MinOutputTokens,
		},
		{
			name:   "a large turn growth just under the input cap",
			budget: poo.ContextBudget{ContextLimit: window, InputLimit: cap, EstimatedInput: cap - poo.OutputReserve, OutputLimit: 4096, TurnGrowth: 20000},
			room:   4096,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blocked, source, text := c.budget.RequestBlocked()
			if blocked != c.want {
				t.Errorf("RequestBlocked is %v, want %v", blocked, c.want)
			}
			if source != c.source {
				t.Errorf("the source is %q, want %q", source, c.source)
			}
			if c.want && text == "" {
				t.Error("the blocked request has no text")
			}
			if c.room != 0 {
				if got := c.budget.OutputRoom(); got != c.room {
					t.Errorf("the output room is %d, want %d", got, c.room)
				}
			}
		})
	}
}
