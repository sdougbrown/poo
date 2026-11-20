// Package native is the provider-native compaction strategy.
//
// The strategy does not write a summary. It asks the session's own transport
// to compact the provider's conversation, and installs what comes back as the
// model's window. Only the provider can read that state back, so the window is
// the provider's own and nothing else can stand in for it.
//
// A native compaction costs a request, and the core charges it the way it
// charges any other request a strategy makes. Its default threshold matches
// summary's, but native can compact after the first request.
package native

import (
	"context"

	"github.com/sdougbrown/poo"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "native"

	// DefaultRatio is the share of the context window at which the
	// strategy compacts. It matches summary's ratio.
	DefaultRatio = 0.85
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the context window at which the strategy
	// compacts. It defaults to DefaultRatio.
	Ratio float64
}

// Compactor is the native compaction strategy.
type Compactor struct {
	// ratio is the share of the window at which the strategy acts.
	ratio float64
}

// New returns the native strategy.
func New(opts Options) *Compactor {
	ratio := opts.Ratio
	if ratio <= 0 {
		ratio = DefaultRatio
	}
	return &Compactor{ratio: ratio}
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// ShouldCompact reports that the run has reached the point that compacts
// the provider's state.
//
// The strategy waits for its own ratio only. It also waits for the run
// to have asked the model something: there is no conversation to compact
// before the first request.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return budget.Reached(c.ratio)
}

// Compact asks the core for a provider window.
//
// The window is the provider's own state, so only the core's bound call to
// the session's transport can produce one. The empty window is the request:
// the core asks the transport and installs what it returns.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	return poo.Compaction{Strategy: Strategy, Window: &poo.ProviderWindow{}}, nil
}
