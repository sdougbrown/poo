// Package handoff is the v1 compaction strategy.
//
// The strategy refuses to compact. It asks the model to write a handoff
// for whoever continues the task, and the run ends with that note: the
// consumer carries the note on and seeds a new session with it.
//
// The handoff is written by a fork of the last main request, so the
// model sees exactly what it saw before, and the run's own view is
// never changed.
//
// The instruction the fork appends is a markdown file under prompts/, so
// it is edited and reviewed as prose. scripts/lint-prompts.sh lints it
// with the rest of the session's prompt text.
package handoff

import (
	"context"
	"errors"
	"strings"

	"github.com/sdougbrown/poo"
)

// The defaults the plan fixes.
const (
	// DefaultMaxTokens is the output limit of the fork that asks for
	// the handoff.
	DefaultMaxTokens = 2048
	// Strategy is the name the compaction carries.
	Strategy = "handoff"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the prompt limit at which the strategy
	// asks for a handoff. Zero means no ratio trigger: the strategy acts
	// when the next answer no longer fits.
	Ratio float64
	// MaxTokens is the output limit of the fork that asks for the
	// handoff. It defaults to DefaultMaxTokens.
	MaxTokens int
}

// Compactor is the handoff strategy.
type Compactor struct {
	// ratio is the share of the prompt limit at which the strategy acts.
	ratio float64
	// maxTokens is the output limit of the fork.
	maxTokens int
}

// New returns the handoff strategy.
func New(opts Options) *Compactor {
	c := &Compactor{ratio: opts.Ratio, maxTokens: opts.MaxTokens}
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
// The strategy acts when the next answer no longer fits. It waits for the
// run's first request: a handoff is a note about work that happened. The note needs room of its own, so the need is at least
// the fork's output limit. The handoff ends the run, so the trigger cannot
// repeat.
//
// A configured ratio is an explicit override: when it is set, the strategy
// also acts at that share of the prompt limit.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	if c.ratio > 0 && budget.Reached(c.ratio) {
		return true
	}
	return budget.RoomShort(max(budget.OutputLimit, c.maxTokens))
}

// Compact asks the model to write a handoff.
//
// The fork sends the last main request again with the instruction
// appended, so the model reads the same conversation it was reading.
// A tool call in the reply is ignored: only the text is the handoff. A
// reply the output limit cut short is refused, because half a note
// reads as a whole one and the continuation would follow it.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	if in.Fork == nil {
		return poo.Compaction{}, errors.New("handoff: there is no fork to call")
	}
	msg := poo.NewTextMessage(poo.RoleUser, instruction)
	reply, err := in.Fork(ctx, []poo.Message{msg}, c.maxTokens)
	if err != nil {
		return poo.Compaction{}, err
	}
	if reply.Finish == poo.FinishLength {
		return poo.Compaction{}, errors.New("handoff: the note hit the output limit and is incomplete")
	}
	text := strings.TrimSpace(reply.Message.Text())
	if text == "" {
		return poo.Compaction{}, errors.New("handoff: the model wrote no handoff")
	}
	return poo.Compaction{Strategy: Strategy, End: &poo.EndRun{Handoff: text}}, nil
}
