// Package summary is the summary-and-continue compaction strategy.
//
// The strategy replaces the oldest turns of the view with a model-written
// summary and keeps the newest turns verbatim. The run carries on with a
// shorter view of the same work. It is the conventional strategy:
// pi, Claude Code's /compact and OpenHands' condensers all read a
// conversation and write a structured checkpoint of it.
//
// The summary request is the strategy's own, built from a serialization
// of the turns the cut covers rather than from the run's conversation.
// That is what lets it read the whole current view. A fork re-sends the
// window of the last main request, which can be a turn older than the
// view.
//
// It also keeps the request inside the window it summarizes. The model
// is told to write a summary instead of continuing the work, tool results are
// capped, and reasoning blocks are left out.
//
// Every replacement the strategy writes starts with a marker line naming
// the turns it replaced, so the model reads that its history was
// compacted. The store keeps the original turns: a compaction changes
// what the model sees, not what the session did.
//
// The prompts the request carries are markdown files under prompts/, so
// they are edited and reviewed as prose. scripts/lint-prompts.sh lints
// them as agent instructions with the rest of the session's prompt text.
package summary

import (
	"context"
	"errors"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "summary"

	// DefaultKeepRecentTokens is how much of the newest history the
	// strategy keeps verbatim.
	DefaultKeepRecentTokens = 20000

	// DefaultMaxTokens is the output limit of the summarization request.
	DefaultMaxTokens = 4096

	// markerVerb is what the replacement's marker line says the turns
	// were replaced with.
	markerVerb = "summarized"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the prompt limit at which the strategy
	// summarizes. Zero means no ratio trigger: the strategy acts when
	// the next answer no longer fits.
	Ratio float64
	// KeepRecentTokens is how much of the newest history the strategy
	// keeps verbatim. It defaults to DefaultKeepRecentTokens.
	KeepRecentTokens int
	// MaxTokens is the output limit of the summarization request. It
	// defaults to DefaultMaxTokens.
	MaxTokens int
	// Files reports the file a tool call names. It defaults to the two
	// file tools poo ships, read and edit, with the path in a "path"
	// argument. A consumer whose tools are named otherwise sets it, so
	// the summary states the files the work touched.
	Files FileOp
}

// FileOp reports the file a tool call names.
type FileOp = cut.FileOp

// Compactor is the summary strategy.
type Compactor struct {
	// ratio is the share of the prompt limit at which the strategy acts.
	ratio float64
	// keepRecent is how much of the newest history the strategy keeps.
	keepRecent int
	// maxTokens is the output limit of the summarization request.
	maxTokens int
	// files reports the file a tool call names.
	files FileOp
	// trigger is the reconstruction trigger the block strategies share.
	trigger cut.Trigger
}

// New returns the summary strategy.
func New(opts Options) *Compactor {
	c := &Compactor{ratio: opts.Ratio, keepRecent: opts.KeepRecentTokens, maxTokens: opts.MaxTokens, files: opts.Files}
	if c.keepRecent <= 0 {
		c.keepRecent = DefaultKeepRecentTokens
	}
	if c.maxTokens <= 0 {
		c.maxTokens = DefaultMaxTokens
	}
	if c.files == nil {
		c.files = cut.DefaultFileOp
	}
	// The summary makes no fork, so the fork's own need is zero and the
	// run's output limit is the whole need. The replacement carries no
	// retention pointer: the summary itself is the checkpoint.
	c.trigger = cut.Trigger{Ratio: c.ratio, KeepRecent: c.keepRecent, MaxTokens: c.maxTokens, Files: c.files, Marker: markerVerb}
	return c
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// KeepRecentTokens returns how much of the newest history the strategy
// keeps verbatim.
func (c *Compactor) KeepRecentTokens() int { return c.keepRecent }

// MaxTokens returns the output limit of the summarization request.
func (c *Compactor) MaxTokens() int { return c.maxTokens }

// PointerMaxTokens returns the output limit of the fork's pointer
// pass. The summary makes no fork, so it is zero.
func (c *Compactor) PointerMaxTokens() int { return 0 }

// ShouldCompact reports whether the view should be summarized.
//
// The strategy acts when the next answer no longer fits and the view holds
// a turn older than the newest one. A single turn has nothing older to
// summarize, so starting a compaction would only fail: the same guard
// Compact applies belongs here, before the run is told one started.
//
// It also acts when the room the window leaves is under the minimum
// output: a run that starts there cannot wait for a turn, because the
// ratio trigger needs a request behind it. The rule is the budget's
// own, shared with the other strategies and the request gate.
//
// A configured ratio is an explicit override: when it is set, the strategy
// also acts at that share of the prompt limit.
//
// The room trigger fires only when a summary would give the room back. On
// a window so small that the verbatim tail and the summary alone leave less
// room than the output limit, a summary on every turn would gain nothing.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return c.trigger.ShouldCompact(view, budget)
}

// Compact summarizes the turns the view can spare and replaces them with
// the summary.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	if in.Ask == nil {
		return poo.Compaction{}, errors.New("summary: there is no request to make")
	}
	messages := in.View.Messages()
	r, ok := cut.Select(messages, in.View.Turns(), in.Budget, c.keepRecent)
	if !ok {
		return poo.Compaction{}, errors.New("summary: the newest history fills the tail, so there is nothing older to summarize")
	}
	covered := messages[r.Start:r.End]
	request := summarizationRequest(covered, cut.PreviousSummary(messages[:r.Start]))
	reply, err := in.Ask(ctx, request, c.maxTokens)
	if err != nil {
		return poo.Compaction{}, err
	}
	// A summary the output limit cut short is not a checkpoint: it ends
	// mid-sentence, and the run that continues from it trusts what it
	// reads. A tool call is not one either, and a model that answers
	// with one has not summarized anything.
	if reply.Finish == poo.FinishLength {
		return poo.Compaction{}, errors.New("summary: the summary hit the output limit and is incomplete")
	}
	if len(reply.Message.ToolCalls()) > 0 {
		return poo.Compaction{}, errors.New("summary: the summarization request called a tool")
	}
	text := strings.TrimSpace(reply.Message.Text())
	if text == "" {
		return poo.Compaction{}, errors.New("summary: the model wrote no summary")
	}
	replacement := cut.Marker(r, markerVerb) + "\n\n" + text
	if files := cut.FileList(c.files, covered); files != "" {
		replacement += "\n\n" + files
	}
	return poo.Compaction{
		Strategy: Strategy,
		Replace: &poo.RangeReplacement{
			Cut:      r.Cut,
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, replacement)},
		},
	}, nil
}
