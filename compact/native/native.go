// Package native is the provider-native compaction strategy.
//
// The strategy does not write a summary. It asks the session's own transport
// to compact the provider's conversation, and installs what comes back as the
// model's window. Only the provider can read that state back, so the window is
// the provider's own and nothing else can stand in for it.
//
// A native compaction costs a request, and the core charges it the way it
// charges any other request a strategy makes. It compacts when the next
// answer no longer fits, after the first request.
package native

import (
	"context"

	"github.com/sdougbrown/poo"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "native"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the prompt limit at which the strategy
	// compacts. Zero means no ratio trigger: the strategy acts when the
	// next answer no longer fits.
	Ratio float64
}

// Compactor is the native compaction strategy.
type Compactor struct {
	// ratio is the share of the window at which the strategy acts.
	ratio float64
}

// New returns the native strategy.
func New(opts Options) *Compactor {
	return &Compactor{ratio: opts.Ratio}
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// ShouldCompact reports that the run should compact the provider's state.
//
// The strategy acts when the next answer no longer fits. A configured ratio
// is an explicit override: when it is set, the strategy also acts at that
// share of the prompt limit.
//
// A compaction that cannot restore the room must not repeat every turn. When
// the input the last compaction left is still short, the strategy declines:
// the run settles no_output_room in the gate instead.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	if c.ratio > 0 && budget.Reached(c.ratio) {
		return true
	}
	if budget.CompactedInput > 0 {
		after := budget
		after.EstimatedInput = budget.CompactedInput
		if after.RoomShort(budget.OutputLimit) {
			return false
		}
	}
	return budget.RoomShort(budget.OutputLimit)
}

// Compact asks the core for a provider window.
//
// The window is the provider's own state, so only the core's bound call to
// the session's transport can produce one. The empty window is the request:
// the core asks the transport and installs what it returns.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return poo.Compaction{Strategy: Strategy, Window: &poo.ProviderWindow{}}, nil
}
