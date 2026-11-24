// Package pointer is the pointer-and-extract compaction strategy.
//
// The strategy keeps the mine strategy's fork — the chat model names,
// from its own window, what the replacement must preserve — and makes
// the pointers load-bearing: each item is a query over the retained
// history, the strategy runs the queries itself, and the compaction
// model composes the working context from what they hit. It never reads
// the covered transcript whole, so the compaction model's window bounds
// the compose ask's output, not the transcript's size.
//
// The fork replies with one JSON object: the queries to search for and
// the notes a search may not find. The ask is request-side JSON, when
// the endpoint takes it, and the parse is lenient: a reply that does
// not parse is the notes, and a strategy that extracts nothing composes
// from the orientation seed alone.
//
// The seed orients the compose ask: the first user message and the last
// few, plus the last turn, say what the session is and where it stands.
// The seed, the matches and the notes are the whole input, and a
// fallback to the mine strategy's full-span read covers the case where
// the selective pass cannot produce a context at all.
//
// The replacement ends with the retention pointer: the store keeps every
// original message, and the history tool reads what a query missed on
// demand.
package pointer

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/internal/cut"
	"github.com/sdougbrown/poo/compact/mine"
)

// The strategy's constants.
const (
	// Strategy is the name the compaction carries.
	Strategy = "pointer"

	// DefaultKeepRecentTokens is how much of the newest history the
	// strategy keeps verbatim.
	DefaultKeepRecentTokens = 12000

	// DefaultMaxTokens is the output limit of the compose request.
	DefaultMaxTokens = 32768

	// DefaultPointerMaxTokens is the output limit of the fork's pointer
	// pass.
	DefaultPointerMaxTokens = 2048

	// DefaultSnippetBudget is the most text the queries may collect.
	DefaultSnippetBudget = 32768

	// DefaultQueryCount is the most queries the pointer pass may name.
	DefaultQueryCount = 8

	// markerVerb is what the replacement's marker line says the turns
	// were replaced with.
	markerVerb = "reconstructed"
)

// Options configure the strategy.
type Options struct {
	// Ratio is the share of the prompt limit at which the strategy
	// acts. Zero means no ratio trigger: the strategy acts when the
	// next answer no longer fits.
	Ratio float64
	// KeepRecentTokens is how much of the newest history the strategy
	// keeps verbatim. It defaults to DefaultKeepRecentTokens.
	KeepRecentTokens int
	// MaxTokens is the output limit of the compose request. It defaults
	// to DefaultMaxTokens.
	MaxTokens int
	// PointerMaxTokens is the output limit of the fork's pointer pass.
	// It defaults to DefaultPointerMaxTokens.
	PointerMaxTokens int
	// SnippetBudget is the most text the queries may collect, in bytes.
	// It defaults to DefaultSnippetBudget.
	SnippetBudget int
	// QueryCount is the most queries the pointer pass may name. It
	// defaults to DefaultQueryCount.
	QueryCount int
	// EscalateAfter is the number of the strategy's own compactions a
	// session runs before the rest are the full-span fallback's. The
	// guard is opt-in: when the settings do not name it, or name it
	// zero or less, the strategy never escalates, and a session that
	// keeps refilling its window keeps compacting selectively for as
	// long as it runs.
	EscalateAfter int
	// Files reports the file a tool call names. It defaults to the two
	// file tools poo ships, read and edit, with the path in a "path"
	// argument.
	Files FileOp
	// Fallback is the full-span strategy a selective pass that cannot
	// produce a context falls back to. It defaults to the mine strategy
	// with the same ratio, tail and output limit.
	Fallback poo.Compactor
}

// FileOp reports the file a tool call names.
type FileOp = cut.FileOp

// Compactor is the pointer strategy.
type Compactor struct {
	// ratio is the share of the prompt limit at which the strategy acts.
	ratio float64
	// keepRecent is how much of the newest history the strategy keeps.
	keepRecent int
	// maxTokens is the output limit of the compose request.
	maxTokens int
	// pointerMaxTokens is the output limit of the pointer pass.
	pointerMaxTokens int
	// snippetBudget is the most text the queries may collect.
	snippetBudget int
	// queryCount is the most queries the pointer pass may name.
	queryCount int
	// escalateAfter is the number of the strategy's own compactions
	// before the rest are the fallback's.
	escalateAfter int
	// files reports the file a tool call names.
	files FileOp
	// trigger is the reconstruction trigger the block strategies
	// share: the cut package's own arithmetic.
	trigger cut.Trigger
	// fallback is the full-span strategy the selective pass falls back
	// to.
	fallback poo.Compactor
}

// New returns the pointer strategy.
func New(opts Options) *Compactor {
	c := &Compactor{
		ratio:            opts.Ratio,
		keepRecent:       opts.KeepRecentTokens,
		maxTokens:        opts.MaxTokens,
		pointerMaxTokens: opts.PointerMaxTokens,
		snippetBudget:    opts.SnippetBudget,
		queryCount:       opts.QueryCount,
		escalateAfter:    opts.EscalateAfter,
		files:            opts.Files,
		fallback:         opts.Fallback,
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
	if c.snippetBudget <= 0 {
		c.snippetBudget = DefaultSnippetBudget
	}
	if c.queryCount <= 0 {
		c.queryCount = DefaultQueryCount
	}

	if c.files == nil {
		c.files = cut.DefaultFileOp
	}
	c.trigger = cut.Trigger{Ratio: c.ratio, KeepRecent: c.keepRecent, MaxTokens: c.maxTokens, Files: c.files}
	if c.fallback == nil {
		c.fallback = mine.New(mine.Options{
			Ratio:            c.ratio,
			KeepRecentTokens: c.keepRecent,
			MaxTokens:        c.maxTokens,
			Files:            c.files,
		})
	}
	return c
}

// Ratio returns the share of the window at which the strategy acts.
func (c *Compactor) Ratio() float64 { return c.ratio }

// KeepRecentTokens returns how much of the newest history the strategy
// keeps verbatim.
func (c *Compactor) KeepRecentTokens() int { return c.keepRecent }

// MaxTokens returns the output limit of the compose request.
func (c *Compactor) MaxTokens() int { return c.maxTokens }

// PointerMaxTokens returns the output limit of the pointer pass.
func (c *Compactor) PointerMaxTokens() int { return c.pointerMaxTokens }

// SnippetBudget returns the most text the queries may collect.
func (c *Compactor) SnippetBudget() int { return c.snippetBudget }

// EscalateAfter returns the number of the strategy's own compactions a
// session runs before the rest are the full-span fallback's.
func (c *Compactor) EscalateAfter() int { return c.escalateAfter }

// ShouldCompact reports whether the view should be reconstructed: the
// conversation alone exceeds the window, the fill share the ratio names
// is reached, or the room left is under the compose limit and a
// replacement would give it back. The trigger is the cut package's,
// shared with the other block strategies.
func (c *Compactor) ShouldCompact(view poo.View, budget poo.ContextBudget) bool {
	return c.trigger.ShouldCompact(view, budget)
}

// Compact reconstructs the turns the view can spare from what the
// pointers select.
//
// The pass runs on a fork of the last main request, asked for JSON. A
// fork that fails, or a reply that does not parse, leaves the queries
// empty and the notes as they were said: the pass is steering, and the
// compose ask still reads the seed. A compose ask that fails falls back
// to the full-span strategy, whose compaction is returned as it is.
func (c *Compactor) Compact(ctx context.Context, in poo.CompactionEnv) (poo.Compaction, error) {
	if in.Ask == nil {
		return poo.Compaction{}, errors.New("pointer: there is no request to make")
	}
	if c.escalateAfter > 0 && in.Compactions >= c.escalateAfter {
		// The cheap path has had its rounds: a session that keeps
		// refilling the window is one the reconstruction is not
		// converging on, so the rest are the fallback's.
		return c.fallback.Compact(ctx, in)
	}
	messages := in.View.Messages()
	r, ok := cut.Select(messages, in.View.Turns(), in.Budget, c.keepRecent)
	if !ok {
		return poo.Compaction{}, errors.New("pointer: the newest history fills the tail, so there is nothing older to reconstruct")
	}
	covered := messages[r.Start:r.End]

	var queries []string
	notes := ""
	fork := &poo.ForkSide{}
	if in.Fork != nil {
		reply, err := in.Fork(ctx, pointerRequest(), c.pointerMaxTokens, poo.ForkOptions{JSON: true})
		// The reply is asked for as JSON; an endpoint that cannot be
		// asked gets one more fork without the ask, and the reply is
		// parsed leniently either way.
		if err != nil {
			// The endpoint refused the response-format ask: one more fork
			// without it, and the reply is parsed leniently either way.
			reply, err = in.Fork(ctx, pointerRequest(), c.pointerMaxTokens, poo.ForkOptions{})
		}
		if err != nil {
			// Both passes failed: the record says so, the way the mine
			// strategy's does, instead of a side that looks successful.
			fork.Error = err.Error()
		} else {
			fork.Truncated = reply.Finish == poo.FinishLength
			fork.ToolCall = len(reply.Message.ToolCalls()) > 0
			if !fork.ToolCall {
				var parsed bool
				queries, notes, parsed = parsePointers(reply.Message.Text())
				// A reply that did not parse is one malformed pass away
				// from machine-readable: the fork paid the window's
				// prefill, so a small reformat ask is cheap next to
				// doing the pass again. A reply that parsed needs
				// nothing, even when it named no queries.
				if !parsed && reformationWanted(reply.Message.Text()) {
					queries, notes = c.reform(ctx, in, reply.Message.Text())
					fork.Reformed = queries != nil
				}
			}
			fork.Bytes = len(notes)
		}
	}

	snippets := c.extract(ctx, in.History, c.queryLimit(queries))
	request := composeRequest(seed(in.View), snippets, notes, cut.PreviousSummary(messages[:r.Start]))
	reply, err := in.Ask(ctx, request, c.maxTokens)
	// A reconstruction the output limit cut short is not a context: the
	// full-span strategy takes the run from here.
	if err == nil && (reply.Finish == poo.FinishLength || len(reply.Message.ToolCalls()) > 0) {
		err = errors.New("pointer: the reconstruction did not complete")
	}
	if err != nil {
		return c.fallback.Compact(ctx, in)
	}
	text := strings.TrimSpace(reply.Message.Text())
	if text == "" {
		return c.fallback.Compact(ctx, in)
	}
	replacement := cut.Marker(r, markerVerb) + "\n\n" + text
	if files := cut.FileList(c.files, covered); files != "" {
		replacement += "\n\n" + files
	}
	replacement += "\n\n" + cut.Retention(r.Cut, in.SessionID, in.HistoryTool)
	return poo.Compaction{
		Strategy: Strategy,
		Replace: &poo.RangeReplacement{
			Cut:      r.Cut,
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, replacement)},
		},
		Side: &poo.SideRequests{Fork: fork},
	}, nil
}

// bytePrefix returns the longest prefix of s that fits n bytes, cut
// on a rune boundary: the budget it serves is a byte count, and a cut
// that landed mid-rune would hand the compose ask text that is not
// text.
func bytePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// extract runs the queries over the retained history and returns the
// matching lines within the snippet budget. A history that cannot be
// read returns nothing: the compose ask still has the seed and the
// notes.
func (c *Compactor) extract(ctx context.Context, history poo.History, queries []string) []poo.HistoryMatch {
	if history == nil {
		return nil
	}
	var out []poo.HistoryMatch
	seen := map[string]bool{}
	used := 0
	for _, query := range queries {
		// The query is the pointer's literal string: it is quoted
		// before it reaches the search, so a query that reads as a
		// pattern is still the text it was.
		matches, err := history.Search(ctx, regexp.QuoteMeta(query))
		if err != nil {
			continue
		}
		for _, m := range matches {
			key := m.Text
			if seen[key] {
				continue
			}
			text := m.Text
			if used+len(text) > c.snippetBudget {
				// The budget is the whole digest's: a line that does
				// not fit is cut to what remains - a byte cut that
				// backs up to a rune boundary, so the line is text
				// and the budget stays bytes - and the queries after
				// it have nothing left to add.
				if remaining := c.snippetBudget - used; remaining > 0 {
					text = bytePrefix(text, remaining)
				} else {
					return out
				}
			}
			seen[key] = true
			used += len(text)
			out = append(out, poo.HistoryMatch{Turn: m.Turn, Text: text})
		}
	}
	return out
}

// parsePointers reads the fork's reply: one JSON object of queries and
// notes. A reply that does not parse is the notes, as it was said, and
// parsed reports that it did parse - a reply that parsed but named no
// queries is machine-readable already, and the reformation it does not
// need is not spent on it.
func parsePointers(reply string) (queries []string, notes string, parsed bool) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start >= 0 && end > start {
		var parsedReply struct {
			Queries []string `json:"queries"`
			Notes   string   `json:"notes"`
		}
		if err := json.Unmarshal([]byte(reply[start:end+1]), &parsedReply); err == nil {
			return parsedReply.Queries, strings.TrimSpace(parsedReply.Notes), true
		}
	}
	return nil, strings.TrimSpace(reply), false
}

// reformationWanted reports that a pointer reply has something to
// reform: it names content the reconstruction should keep, rather than
// being empty.
func reformationWanted(reply string) bool {
	return strings.TrimSpace(reply) != ""
}

// reform asks the compaction role to re-form a malformed pointer reply
// into the machine-readable shape. The input is the reply itself — a
// few kilobytes next to the window's prefill the fork already paid —
// and the output is parsed leniently like the fork's own reply.
func (c *Compactor) reform(ctx context.Context, in poo.CompactionEnv, malformed string) ([]string, string) {
	if in.Ask == nil {
		return nil, strings.TrimSpace(malformed)
	}
	reply, err := in.Ask(ctx, reformRequest(malformed), c.pointerMaxTokens)
	if err != nil || len(reply.Message.ToolCalls()) > 0 {
		return nil, strings.TrimSpace(malformed)
	}
	queries, notes, parsed := parsePointers(reply.Message.Text())
	if !parsed {
		// The reform did not parse either: the reply was prose worth
		// keeping, and the notes carry it.
		return nil, strings.TrimSpace(malformed)
	}
	return queries, notes
}

// seed returns the orientation messages: the last three non-empty
// user messages of the view, as the compose ask reads them. A session
// longer than that orients by where it stands, not by where it began.
func seed(view poo.View) []poo.Message {
	var users []poo.Message
	for _, m := range view.Messages() {
		if m.Origin == poo.OriginCompaction || m.Role != poo.RoleUser {
			continue
		}
		if strings.TrimSpace(m.Text()) == "" {
			continue
		}
		users = append(users, m)
	}
	if len(users) > 3 {
		users = users[len(users)-3:]
	}
	return users
}

// queryLimit returns the queries the pass may run, capped.
func (c *Compactor) queryLimit(queries []string) []string {
	if len(queries) > c.queryCount {
		return queries[:c.queryCount]
	}
	return queries
}
