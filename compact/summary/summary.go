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
// It also keeps the request inside the window it summarizes. The model is
// told to write a summary instead of continuing the work, tool results are
// capped, and reasoning blocks are left out.
//
// Every replacement the strategy writes starts with a marker line naming
// the turns it replaced, so the model reads that its history was
// compacted. The store keeps the original turns: a compaction changes
// what the model sees, not what the session did.
package summary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sdougbrown/poo"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "summary"

	// DefaultRatio is the share of the context window at which the
	// strategy summarizes. It sits under the handoff strategy's ratio,
	// so a summary has room to be written before the window is full.
	DefaultRatio = 0.85

	// DefaultKeepRecentTokens is how much of the newest history the
	// strategy keeps verbatim.
	DefaultKeepRecentTokens = 20000

	// DefaultMaxTokens is the output limit of the summarization request.
	DefaultMaxTokens = 4096

	// tailShare is the share of a window a verbatim tail may fill. A
	// window smaller than twice the configured tail keeps this share
	// instead, so a small-window model still compacts.
	tailShare = 0.5

	// markerPrefix starts every replacement the strategy writes. The
	// model reads it as the sign that its history was replaced, and the
	// strategy recognizes its own previous summary by it.
	markerPrefix = "[Context compacted:"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the context window at which the strategy
	// summarizes. It defaults to DefaultRatio.
	Ratio float64
	// KeepRecentTokens is how much of the newest history the strategy
	// keeps verbatim. It defaults to DefaultKeepRecentTokens.
	KeepRecentTokens int
	// MaxTokens is the output limit of the summarization request. It
	// defaults to DefaultMaxTokens.
	MaxTokens int
	// Files reports the file a tool call names. It defaults to the tools
	// poo ships: read, edit and write, with the path in a "path"
	// argument. A consumer whose tools are named otherwise sets it, so
	// the summary states the files the work touched.
	Files FileOp
}

// FileOp reports the file a tool call names.
//
// path is the file the call names, and changed reports whether the call
// changed it. A call the function does not recognize returns an empty
// path, and the file is not listed.
type FileOp func(name string, arguments json.RawMessage) (path string, changed bool)

// Compactor is the summary strategy.
type Compactor struct {
	// ratio is the share of the window at which the strategy acts.
	ratio float64
	// keepRecent is how much of the newest history the strategy keeps.
	keepRecent int
	// maxTokens is the output limit of the summarization request.
	maxTokens int
	// files reports the file a tool call names.
	files FileOp
}

// New returns the summary strategy.
func New(opts Options) *Compactor {
	c := &Compactor{ratio: opts.Ratio, keepRecent: opts.KeepRecentTokens, maxTokens: opts.MaxTokens, files: opts.Files}
	if c.ratio <= 0 {
		c.ratio = DefaultRatio
	}
	if c.keepRecent <= 0 {
		c.keepRecent = DefaultKeepRecentTokens
	}
	if c.maxTokens <= 0 {
		c.maxTokens = DefaultMaxTokens
	}
	if c.files == nil {
		c.files = defaultFileOp
	}
	return c
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// KeepRecentTokens returns how much of the newest history the strategy
// keeps verbatim.
func (c *Compactor) KeepRecentTokens() int { return c.keepRecent }

// MaxTokens returns the output limit of the summarization request.
func (c *Compactor) MaxTokens() int { return c.maxTokens }

// ShouldCompact reports whether the view should be summarized.
//
// The strategy acts when the window is nearly full, and only after the
// run has asked the model something. A summary is about work that
// happened.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	if budget.Turns < 1 || budget.ContextLimit <= 0 {
		return false
	}
	return float64(budget.EstimatedInput) >= c.ratio*float64(budget.ContextLimit)
}

// Compact summarizes the turns the view can spare and replaces them with
// the summary.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	if in.Ask == nil {
		return poo.Compaction{}, errors.New("summary: there is no request to make")
	}
	messages := in.View.Messages()
	cut, ok := c.cut(messages, in.View.Turns(), in.Budget)
	if !ok {
		return poo.Compaction{}, errors.New("summary: the newest history fills the tail, so there is nothing older to summarize")
	}
	covered := messages[cut.Start:cut.End]
	request := summarizationRequest(covered, previousSummary(messages[:cut.Start]))
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
	replacement := marker(cut.Cut) + "\n\n" + text
	if files := fileList(c.files, covered); files != "" {
		replacement += "\n\n" + files
	}
	return poo.Compaction{
		Strategy: Strategy,
		Replace: &poo.RangeReplacement{
			Cut:      cut.Cut,
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, replacement)},
		},
	}, nil
}

// cutRange is the range of turns a compaction replaces.
type cutRange struct {
	// Cut is the range of turns, by session turn number.
	Cut poo.TurnRange
	// Start and End are the indexes of the messages it covers in the
	// view: Start is the first, End the index after the last.
	Start int
	End   int
}

// cut returns the turns the strategy summarizes.
//
// The tail is the newest turns of the view, as many as the budget keeps.
// The cut covers everything before it. It starts at the earliest turn
// still in the view and stops before any turn a compaction removed. The
// range the core is asked to replace is therefore one contiguous run.
func (c *Compactor) cut(messages []poo.Message, turns []poo.TurnSummary, budget poo.ContextBudget) (cutRange, bool) {
	live := liveTurns(turns)
	if len(live) < 2 {
		// A single turn is all the view holds, so there is nothing older
		// to summarize. Replacing it would summarize the work the run is
		// in the middle of.
		return cutRange{}, false
	}
	keep := c.tailTokens(budget)
	tail := len(live) - 1
	tokens := 0
	for i := len(live) - 1; i >= 0; i-- {
		tokens += turnTokens(messages[live[i].Start:live[i].End], budget)
		tail = i
		if tokens >= keep {
			break
		}
	}
	if tail == 0 {
		return cutRange{}, false
	}
	return cutRange{
		Cut:   poo.TurnRange{First: live[0].TurnIndex, Last: live[tail-1].TurnIndex},
		Start: live[0].Start,
		End:   live[tail-1].End,
	}, true
}

// liveTurns returns the run of turns the view still holds.
//
// A compaction removes a run of turns and leaves the rest where they
// were. The turns the strategy may cut are therefore the leading removed
// turns skipped, and the contiguous run that follows them.
func liveTurns(turns []poo.TurnSummary) []poo.TurnSummary {
	live := make([]poo.TurnSummary, 0, len(turns))
	started := false
	for _, t := range turns {
		if t.Removed {
			if started {
				break
			}
			continue
		}
		started = true
		live = append(live, t)
	}
	return live
}

// tailTokens returns how many tokens the verbatim tail may hold.
func (c *Compactor) tailTokens(budget poo.ContextBudget) int {
	keep := c.keepRecent
	if budget.ContextLimit > 0 {
		if share := int(float64(budget.ContextLimit) * tailShare); share < keep {
			keep = share
		}
	}
	if keep < 1 {
		keep = 1
	}
	return keep
}

// turnTokens estimates what a turn's messages cost in the window.
func turnTokens(messages []poo.Message, budget poo.ContextBudget) int {
	ratio := budget.CharsPerToken
	if ratio <= 0 {
		ratio = poo.CharsPerToken
	}
	return int(float64(poo.EstimateHistoryChars(messages)) / ratio)
}

// marker names the turns a replacement covers.
//
// The model reads it as the sign that its history was replaced rather
// than silently dropped.
func marker(cut poo.TurnRange) string {
	return fmt.Sprintf("%s turns %d-%d summarized below]", markerPrefix, cut.First, cut.Last)
}

// previousSummary returns the summary an earlier compaction left in the
// view, so the request updates it instead of summarizing that work
// again from nothing.
//
// Any compaction's visible text counts, not only this strategy's: a run
// whose strategy changed keeps the checkpoint the earlier one wrote.
// A provider window and the marker the core writes for it are not
// summaries, and neither is an empty message.
func previousSummary(messages []poo.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Origin != poo.OriginCompaction {
			continue
		}
		text := strings.TrimSpace(m.Text())
		if text == "" || text == poo.CompactionMarkerText {
			continue
		}
		// A replacement this strategy wrote carries its marker line
		// first: the summary is what follows it.
		if strings.HasPrefix(text, markerPrefix) {
			if _, rest, ok := strings.Cut(text, "\n\n"); ok {
				return strings.TrimSpace(rest)
			}
		}
		return text
	}
	return ""
}
