// Package mine is the mine-and-continue compaction strategy.
//
// The strategy replaces the oldest turns of the view with a context a
// pair of fresh requests constructs: a fork of the last main request
// asks the model what its history must preserve, and a request of the
// strategy's own reads the covered transcript and writes the working
// context a fresh start needs. The conventional strategies summarize
// the conversation forward; this one reconstructs the work, so what
// replaces the cut is shaped by the task rather than by the order it
// happened in. It follows the harnesses that hand a new session a
// pointer at the prior session's transcript and let it mine that
// transcript for what it needs.
//
// The fork is the pointer pass. The model sees the window it just
// worked in, so what it names — the turns, the file paths, the error
// strings, the open threads — steers the mining request's reading. The
// pass is advisory: a reply the output limit cut short still steers,
// because pointers that stop early lose nothing the transcript does not
// still hold, and a fork that fails mines without the pass, because
// the transcript holds what the pass would have named.
//
// The mining request is where the compaction is decided. The model is
// told to construct the context instead of continuing the work, tool
// results are capped, and reasoning blocks are left out. A reply the
// output limit cut short is refused, as a reconstruction that ends
// mid-sentence is not a context to continue from.
//
// The replacement ends with a retention pointer: the store keeps every
// original message, so the text the cut removed is recoverable, and the
// pointer names the session and the turns to recover it from. A
// consumer whose model can read the store serves the recovery; the
// history tool ships beside it for exactly that.
//
// The prompts the requests carry are markdown files under prompts/, so
// they are edited and reviewed as prose. scripts/lint-prompts.sh lints
// them as agent instructions with the rest of the session's prompt text.
package mine

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "mine"

	// DefaultRatio is the share of the prompt limit at which the
	// strategy mines. It sits under the handoff strategy's ratio, so the
	// two requests have room to run before the prompt is full.
	DefaultRatio = 0.85

	// DefaultKeepRecentTokens is how much of the newest history the
	// strategy keeps verbatim. It is under the summary strategy's
	// default: the reconstruction carries the long view, so the verbatim
	// tail only has to hold the live edge of the work.
	DefaultKeepRecentTokens = 12000

	// DefaultMaxTokens is the output limit of the mining request. It
	// sits over the summary strategy's default: a reconstruction of a
	// long session carries the working state of hundreds of turns, and
	// a reconstruction the limit cut short is refused, so the room has
	// to be there.
	DefaultMaxTokens = 32768

	// DefaultPointerMaxTokens is the output limit of the fork's pointer
	// pass.
	DefaultPointerMaxTokens = 1024

	// markerVerb is what the replacement's marker line says the turns
	// were replaced with.
	markerVerb = "reconstructed"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the prompt limit at which the strategy
	// mines. It defaults to DefaultRatio.
	Ratio float64
	// KeepRecentTokens is how much of the newest history the strategy
	// keeps verbatim. It defaults to DefaultKeepRecentTokens.
	KeepRecentTokens int
	// MaxTokens is the output limit of the mining request. It defaults
	// to DefaultMaxTokens.
	MaxTokens int
	// PointerMaxTokens is the output limit of the fork's pointer pass.
	// It defaults to DefaultPointerMaxTokens.
	PointerMaxTokens int
	// Files reports the file a tool call names. It defaults to the two
	// file tools poo ships, read and edit, with the path in a "path"
	// argument. A consumer whose tools are named otherwise sets it, so
	// the replacement states the files the work touched.
	Files FileOp
}

// FileOp reports the file a tool call names.
type FileOp = cut.FileOp

// Compactor is the mine strategy.
type Compactor struct {
	// ratio is the share of the prompt limit at which the strategy acts.
	ratio float64
	// keepRecent is how much of the newest history the strategy keeps.
	keepRecent int
	// maxTokens is the output limit of the mining request.
	maxTokens int
	// pointerMaxTokens is the output limit of the pointer pass.
	pointerMaxTokens int
	// files reports the file a tool call names.
	files FileOp
}

// New returns the mine strategy.
func New(opts Options) *Compactor {
	c := &Compactor{ratio: opts.Ratio, keepRecent: opts.KeepRecentTokens, maxTokens: opts.MaxTokens, pointerMaxTokens: opts.PointerMaxTokens, files: opts.Files}
	if c.ratio <= 0 {
		c.ratio = DefaultRatio
	}
	if c.keepRecent <= 0 {
		c.keepRecent = DefaultKeepRecentTokens
	}
	if c.maxTokens <= 0 {
		c.maxTokens = DefaultMaxTokens
	}
	if c.pointerMaxTokens <= 0 {
		c.pointerMaxTokens = DefaultPointerMaxTokens
	}
	if c.files == nil {
		c.files = cut.DefaultFileOp
	}
	return c
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// KeepRecentTokens returns how much of the newest history the strategy
// keeps verbatim.
func (c *Compactor) KeepRecentTokens() int { return c.keepRecent }

// MaxTokens returns the output limit of the mining request.
func (c *Compactor) MaxTokens() int { return c.maxTokens }

// PointerMaxTokens returns the output limit of the pointer pass.
func (c *Compactor) PointerMaxTokens() int { return c.pointerMaxTokens }

// ShouldCompact reports whether the view should be mined.
//
// The strategy acts when the window is nearly full and the view holds a
// turn older than the newest one, and before the ratio when the room
// left in the window is under the output limit and a compaction would
// give the room back. It also acts when the conversation alone exceeds
// the window: a run that starts over the window cannot wait for a turn,
// because the ratio trigger needs a request behind it. The guard is the
// summary strategy's: a window whose tail fills everything has nothing
// older to reconstruct, and starting a compaction would only fail.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	if len(cut.LiveTurns(view.Turns())) < 2 {
		return false
	}
	return budget.OverWindow() || budget.Reached(c.ratio) || c.restoresOutputRoom(view, budget)
}

// restoresOutputRoom reports that the output room is short and a
// reconstruction would end the shortage.
func (c *Compactor) restoresOutputRoom(view poo.View, budget poo.ContextBudget) bool {
	if !budget.RoomShort(budget.OutputLimit) {
		return false
	}
	messages := view.Messages()
	r, ok := cut.Select(messages, view.Turns(), budget, c.keepRecent)
	if !ok {
		return false
	}
	covered := messages[r.Start:r.End]
	after := cut.RoomAfter(budget, cut.TurnTokens(covered, budget),
		c.maxTokens+c.replacementCarriageTokens(r, covered, budget))
	return !after.RoomShort(after.OutputLimit)
}

// replacementCarriageTokens estimates what the replacement carries
// besides the reconstruction: the marker line, the retention pointer
// and the file list.
func (c *Compactor) replacementCarriageTokens(r cut.Range, covered []poo.Message, budget poo.ContextBudget) int {
	text := cut.Marker(r, markerVerb) + "\n\n" + retention(r.Cut, "", true) +
		"\n\n" + cut.FileList(c.files, covered)
	return cut.TurnTokens([]poo.Message{poo.NewTextMessage(poo.RoleUser, text)}, budget)
}

// Compact reconstructs the turns the view can spare and replaces them
// with the reconstruction.
//
// The pointer pass runs first, on a fork of the last main request. The
// pass is best-effort: a fork that fails, or a model that answers with
// a tool call, leaves the reconstruction without steering rather than
// fails the compaction. The transcript holds what the pointers would
// have named, and the cap on the pass's output already bounds what a
// pointer reply can add, so mining blind loses less than not mining at
// all.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	if in.Ask == nil {
		return poo.Compaction{}, errors.New("mine: there is no request to make")
	}
	messages := in.View.Messages()
	r, ok := cut.Select(messages, in.View.Turns(), in.Budget, c.keepRecent)
	if !ok {
		return poo.Compaction{}, errors.New("mine: the newest history fills the tail, so there is nothing older to reconstruct")
	}
	covered := messages[r.Start:r.End]

	pointers := ""
	var fork *poo.ForkSide
	if in.Fork != nil {
		reply, err := in.Fork(ctx, pointerRequest(), c.pointerMaxTokens)
		// The pass is advisory, so a fork that fails mines without it.
		// A tool call is not pointers either: a model that answers with
		// one has not read the ask, and the transcript holds what its
		// reply would have named.
		if err == nil && len(reply.Message.ToolCalls()) == 0 {
			pointers = strings.TrimSpace(reply.Message.Text())
		}
		fork = &poo.ForkSide{}
		if err != nil {
			fork.Error = err.Error()
		} else {
			fork.Bytes = len(pointers)
			fork.Truncated = reply.Finish == poo.FinishLength
			fork.ToolCall = len(reply.Message.ToolCalls()) > 0
		}
	}

	request := miningRequest(covered, pointers, cut.PreviousSummary(messages[:r.Start]))
	reply, err := in.Ask(ctx, request, c.maxTokens)
	if err != nil {
		return poo.Compaction{}, err
	}
	// A reconstruction the output limit cut short is not a context: it
	// ends mid-sentence, and the run that continues from it trusts what
	// it reads. A tool call is not one either.
	if reply.Finish == poo.FinishLength {
		return poo.Compaction{}, errors.New("mine: the reconstruction hit the output limit and is incomplete")
	}
	if len(reply.Message.ToolCalls()) > 0 {
		return poo.Compaction{}, errors.New("mine: the mining request called a tool")
	}
	text := strings.TrimSpace(reply.Message.Text())
	if text == "" {
		return poo.Compaction{}, errors.New("mine: the model wrote no reconstruction")
	}
	replacement := cut.Marker(r, markerVerb) + "\n\n" + text
	if files := cut.FileList(c.files, covered); files != "" {
		replacement += "\n\n" + files
	}
	replacement += "\n\n" + retention(r.Cut, in.SessionID, in.HistoryTool)
	return poo.Compaction{
		Strategy: Strategy,
		Replace: &poo.RangeReplacement{
			Cut:      r.Cut,
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, replacement)},
		},
		Side: &poo.SideRequests{Fork: fork},
	}, nil
}

// retention is the pointer the replacement ends with.
//
// The store keeps every original message, so the text the cut removed
// is not gone. The pointer names the turns and the session they belong
// to, so a consumer whose model can read the store knows what to
// recover and from where.
// The recovery sentence names the history tool only when the session
// offers it: a profile without the tool would send the model looking
// for a call it cannot make, so the pointer then says the recovery is
// the reader's own.
func retention(turns poo.TurnRange, sessionID string, historyTool bool) string {
	where := ""
	if sessionID != "" {
		where = " of session " + sessionID
	}
	recover := ""
	if historyTool {
		recover = " The history tool reads them: search the retained turns with a pattern, or read one whole with its turn number."
	}
	return "[The turns replaced above are retained in this session's store" + where +
		" (turns " + strconv.Itoa(turns.First) + "-" + strconv.Itoa(turns.Last) + "): nothing was deleted." + recover + "]"
}
