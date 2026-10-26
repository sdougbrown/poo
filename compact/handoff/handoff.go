// Package handoff is the v1 compaction strategy.
//
// The strategy refuses to compact. It asks the model to write a handoff
// for whoever continues the task, and the run ends with that note: the
// consumer carries the note on and seeds a new session with it.
//
// The handoff is written by a fork of the last main request, so the
// model sees exactly what it saw before, and the run's own view is
// never changed.
package handoff

import (
	"context"
	"errors"
	"strings"

	"github.com/sdougbrown/poo"
)

// The defaults the plan fixes.
const (
	// DefaultRatio is the share of the context window at which the
	// strategy asks for a handoff.
	DefaultRatio = 0.90
	// DefaultMaxTokens is the output limit of the fork that asks for
	// the handoff.
	DefaultMaxTokens = 2048
	// Strategy is the name the compaction carries.
	Strategy = "handoff"
	// Instruction is the message the fork appends to the last request.
	Instruction = "[Context limit reached. Write a handoff for whoever continues this task: what you did, what you learned, what remains, and the next concrete step. Reply with text only.]"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the context window at which the strategy
	// asks for a handoff. It defaults to DefaultRatio.
	Ratio float64
	// MaxTokens is the output limit of the fork that asks for the
	// handoff. It defaults to DefaultMaxTokens.
	MaxTokens int
}

// Compactor is the handoff strategy.
type Compactor struct {
	// ratio is the share of the window at which the strategy acts.
	ratio float64
	// maxTokens is the output limit of the fork.
	maxTokens int
}

// New returns the handoff strategy.
func New(opts Options) *Compactor {
	c := &Compactor{ratio: opts.Ratio, maxTokens: opts.MaxTokens}
	if c.ratio <= 0 {
		c.ratio = DefaultRatio
	}
	if c.maxTokens <= 0 {
		c.maxTokens = DefaultMaxTokens
	}
	return c
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// MaxTokens returns the output limit of the fork.
func (c *Compactor) MaxTokens() int { return c.maxTokens }

// ShouldCompact reports whether the run should ask for a handoff.
//
// The strategy acts when the window is nearly full, and only after the
// run has asked the model something: a handoff is a note about work
// that happened.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.Budget) bool {
	if budget.Turns < 1 {
		// The run has not asked the model anything yet, so there is
		// nothing to hand over.
		return false
	}
	if budget.ContextLimit <= 0 {
		return false
	}
	return float64(budget.EstimatedInput) >= c.ratio*float64(budget.ContextLimit)
}

// Compact asks the model to write a handoff.
//
// The fork sends the last main request again with the instruction
// appended, so the model reads the same conversation it was reading.
// A tool call in the reply is ignored: only the text is the handoff.
func (c *Compactor) Compact(ctx context.Context, view poo.View, fork poo.ForkFunc) (poo.Compaction, error) {
	if fork == nil {
		return poo.Compaction{}, errors.New("handoff: there is no fork to call")
	}
	msg := poo.NewTextMessage(poo.RoleUser, Instruction)
	reply, _, err := fork(ctx, []poo.Message{msg}, c.maxTokens)
	if err != nil {
		return poo.Compaction{}, err
	}
	text := strings.TrimSpace(reply.Text())
	if text == "" {
		return poo.Compaction{}, errors.New("handoff: the model wrote no handoff")
	}
	return poo.Compaction{Strategy: Strategy, End: &poo.EndRun{Handoff: text}}, nil
}
