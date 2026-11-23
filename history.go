package poo

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The history projection: a display summary computed from a session's
// records.
//
// It is not the model's view and not the raw records: a projection says
// what happened, in the order it happened, in the shape a client shows.

// RecordReader reads a session's records back, oldest first.
//
// It is the read side of RecordSink, and it is optional: a sink that
// implements it lets the session serve its own history to the tools,
// which is how the history tool reads what a compaction retained. A
// reader sees the records the store has written; a sink that buffers
// makes no promise about the tail.
type RecordReader interface {
	ReadRecords(ctx context.Context) ([]Record, error)
}

// The kinds of item a projection holds.
const (
	// HistoryMessage is a message the model read.
	HistoryMessage = "message"
	// HistoryToolCall is a call the model made.
	HistoryToolCall = "tool_call"
	// HistoryToolResult is the outcome of a call.
	HistoryToolResult = "tool_result"
	// HistoryPermission is a permission ask or its answer.
	HistoryPermission = "permission"
	// HistoryCompaction is a compaction the view went through.
	HistoryCompaction = "compaction"
	// HistoryRunStarted is the beginning of a run.
	HistoryRunStarted = "run_started"
	// HistoryRunSettled is the end of a run.
	HistoryRunSettled = "run_settled"
	// HistoryJobStarted is the beginning of a background job.
	HistoryJobStarted = "job_started"
	// HistoryJobEnded is the end of a background job.
	HistoryJobEnded = "job_ended"
)

// HistoryItem is one entry in a session's history projection.
//
// The item is a summary: it says what happened and where, and it holds
// no more than a client shows.
type HistoryItem struct {
	// Seq is the record's sequence number. It is the item's place in
	// the session's log, and a page asks for the items before it.
	Seq int64 `json:"seq"`
	// Type names the item: "message", "tool_call", "tool_result",
	// "permission", "compaction", "run_started", "run_settled",
	// "job_started" or "job_ended".
	Type string `json:"type"`
	// RunID identifies the run the item belongs to.
	RunID string `json:"run_id,omitempty"`
	// Turn is the turn the item belongs to.
	Turn int `json:"turn,omitempty"`
	// Role is the message's role, for a message item.
	Role Role `json:"role,omitempty"`
	// Origin is the record's own, from the record vocabulary the
	// Origin constants name: "user" for a line the person typed, the
	// delivery origins ("steer", "interrupt" or "follow_up"), "job",
	// "hook:<name>", "feed:<name>" and "notice" on a user-role item,
	// and "model", "tool" or "system" on a model's, a tool's or the
	// system prompt's item. It is empty on a record that had none.
	Origin Origin `json:"origin,omitempty"`
	// Text is the item's text.
	Text string `json:"text,omitempty"`
	// Handle is the tool call's handle, for a tool item.
	Handle string `json:"handle,omitempty"`
	// Tool is the tool the item is about.
	Tool string `json:"tool,omitempty"`
	// Summary is the call's human-facing summary, for a tool call.
	Summary string `json:"summary,omitempty"`
	// Outcome is the tool result's outcome.
	Outcome Outcome `json:"outcome,omitempty"`
	// Decision is the permission answer's decision.
	Decision string `json:"decision,omitempty"`
	// Reason is the run's settle reason, for a settled run.
	Reason Reason `json:"reason,omitempty"`
	// At is the record's timestamp.
	At string `json:"at,omitempty"`
}

// HistoryPage is one page of a session's history.
type HistoryPage struct {
	// Items are the entries, oldest first.
	Items []HistoryItem `json:"items"`
	// HasMore reports that older entries exist before the first one.
	HasMore bool `json:"has_more"`
}

// The limits a history page takes.
const (
	// DefaultHistoryLimit is how many entries a page holds when the
	// caller names no limit.
	DefaultHistoryLimit = 100
	// MaxHistoryLimit is the most entries a page may hold.
	MaxHistoryLimit = 500
)

// ProjectHistory computes the history projection of a session's
// records.
//
// The page is taken backwards: beforeSeq is the sequence number the
// page ends before, and the entries come back oldest first. A limit of
// zero takes the default, and a limit over the maximum takes the
// maximum.
func ProjectHistory(records []Record, beforeSeq int64, limit int) HistoryPage {
	return ProjectHistoryBudget(records, beforeSeq, limit, PageByteBudget)
}

// ProjectHistoryBudget computes the projection with a page byte budget.
//
// The paging and the limit are ProjectHistory's. A budget of zero or
// less takes PageByteBudget.
func ProjectHistoryBudget(records []Record, beforeSeq int64, limit, budget int) HistoryPage {
	if budget <= 0 {
		budget = PageByteBudget
	}
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	if limit > MaxHistoryLimit {
		limit = MaxHistoryLimit
	}
	items := historyItems(records)
	older := make([]HistoryItem, 0, len(items))
	for _, item := range items {
		if beforeSeq > 0 && item.Seq >= beforeSeq {
			continue
		}
		older = append(older, item)
	}
	page := HistoryPage{}
	if len(older) > limit {
		page.HasMore = true
		older = older[len(older)-limit:]
	}
	// The byte budget keeps the newest entries that fit, and says that
	// older ones exist, so a client pages back for what it dropped.
	var byteCut bool
	older, byteCut = PageWithinByteBudget(older, sizeOfItem, budget)
	if byteCut {
		page.HasMore = true
	}
	page.Items = older
	return page
}

// sizeOfItem returns the item's encoded size, as the page's JSON
// carries it.
func sizeOfItem(item HistoryItem) int {
	encoded, _ := json.Marshal(item)
	return len(encoded)
}

// historyItems computes the projection's entries from the records.
//
// A tool result is one entry, not two: the record that holds the result
// and the message that carries it say the same thing, and the result
// says it with the handle.
func historyItems(records []Record) []HistoryItem {
	items := make([]HistoryItem, 0, len(records))
	for _, rec := range records {
		meta := rec.Meta()
		item := HistoryItem{Seq: meta.Seq, At: meta.TS, Origin: meta.Origin}
		switch r := rec.(type) {
		case *MessageRecord:
			if r.Role == RoleTool {
				// The result has its own entry, with the handle.
				continue
			}
			item.Type = HistoryMessage
			item.RunID = r.RunID
			item.Turn = r.Turn
			item.Role = r.Role
			item.Text = messageText(r.Content)
		case *ToolCallRecord:
			item.Type = HistoryToolCall
			item.RunID = r.RunID
			item.Turn = r.Turn
			item.Handle = r.Handle
			item.Tool = r.Tool
			item.Summary = r.Summary
			item.Text = CapSummary(string(r.Arguments))
		case *ToolResultRecord:
			item.Type = HistoryToolResult
			item.RunID = r.RunID
			item.Turn = r.Turn
			item.Handle = r.Handle
			item.Tool = r.Tool
			item.Outcome = r.Outcome
			item.Text = r.Text
		case *PermissionAskedRecord:
			item.Type = HistoryPermission
			item.RunID = r.RunID
			item.Tool = r.Tool
			item.Text = r.Summary
		case *PermissionAnsweredRecord:
			item.Type = HistoryPermission
			item.RunID = r.RunID
			item.Decision = r.Decision
		case *CompactionRecord:
			item.Type = HistoryCompaction
			item.RunID = r.RunID
			item.Text = r.Strategy
		case *RunStartedRecord:
			item.Type = HistoryRunStarted
			item.RunID = r.RunID
		case *RunSettledRecord:
			item.Type = HistoryRunSettled
			item.RunID = r.RunID
			item.Reason = r.Reason
			item.Text = r.Detail.Source
		case *JobStartedRecord:
			item.Type = HistoryJobStarted
			item.RunID = r.RunID
			item.Handle = r.Handle
			item.Tool = r.Tool
		case *JobEndedRecord:
			item.Type = HistoryJobEnded
			item.RunID = r.RunID
			item.Handle = r.Handle
			item.Tool = r.Tool
			item.Text = r.Text
		default:
			continue
		}
		items = append(items, item)
	}
	return items
}

// messageText returns the visible text a message's blocks hold.
//
// A tool call has its own entry, with the handle and the arguments, so
// the message says only what the model read as text.
func messageText(content []ContentBlock) string {
	var b strings.Builder
	for _, c := range content {
		switch c.Kind {
		case ContentText, ContentThinking:
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// History is the retained read side of the history projection.
//
// A compaction changes the model's view, not the store: the turns a
// strategy replaced are still in the records, and the history is how a
// tool reads them back. The turn numbers are the session's own,
// counting from one, so a replacement's marker and the history agree
// about what "turn 3" means.
type History interface {
	// Turns returns the session's turn numbers, oldest first, including
	// the turns a compaction removed. A read runs under the context
	// given: a caller that is cancelled stops paying for it.
	Turns(ctx context.Context) ([]int, error)
	// Turn returns one turn's text, as it was said. A turn the session
	// never had, or one that holds no message, returns empty text.
	Turn(ctx context.Context, turn int) (string, error)
	// Search returns the retained lines that match the regular
	// expression, oldest first. A line that matches names the turn it
	// belongs to.
	Search(ctx context.Context, pattern string) ([]HistoryMatch, error)
}

// HistoryMatch is one line a history search matched.
type HistoryMatch struct {
	// Turn is the turn the line belongs to.
	Turn int
	// Text is the line, as it was said.
	Text string
}

// NewHistory rebuilds the retained history a record chain holds.
//
// The chain is read as the restore reads it: the runs in order, and
// each run's turns in order, numbered continuously across the runs, so
// the turn numbers are the session's own. A compaction record is left
// out rather than applied, so the turns the compaction removed are
// still in what comes back.
// runScan is the runs a record chain holds: the order they were first
// seen in, each run's turn count, and which runs a started record
// names.
type runScan struct {
	order   []string
	turns   map[string]int
	started map[string]bool
}

func NewHistory(records []Record) History {
	scan := scanRunsOf(records)
	offset := 0
	sessionTurn := map[string]int{}
	for _, id := range scan.order {
		for t := 1; t <= scan.turns[id]; t++ {
			sessionTurn[fmt.Sprintf("%s/%d", id, t)] = offset + t
		}
		offset += scan.turns[id]
	}
	// The items are the projection's, with the turns renamed to the
	// session's numbering.
	items := historyItems(records)
	for i, item := range items {
		if turn, ok := sessionTurn[fmt.Sprintf("%s/%d", item.RunID, item.Turn)]; ok {
			items[i].Turn = turn
		}
	}
	return retainedHistory{items: items}
}

// retainedHistory is the retained history one record chain holds.
type retainedHistory struct {
	items []HistoryItem
}

// retainedLine renders one history item as the line the retained
// history holds. The kinds that name what was said are the ones a
// recovery reads; the run and job boundaries are the client
// projection's business and are left out.
func retainedLine(item HistoryItem) (string, bool) {
	switch item.Type {
	case HistoryMessage:
		if strings.TrimSpace(item.Text) == "" {
			return "", false
		}
		return "[" + string(item.Role) + "] " + item.Text, true
	case HistoryToolCall:
		return fmt.Sprintf("[call %s %s] %s", item.Handle, item.Tool, item.Summary), true
	case HistoryToolResult:
		if strings.TrimSpace(item.Text) == "" {
			return "", false
		}
		return fmt.Sprintf("[result %s] %s", item.Handle, item.Text), true
	}
	return "", false
}

// turns collects the turn numbers that hold lines, oldest first.
func (h retainedHistory) turns() []int {
	var out []int
	seen := map[int]bool{}
	for _, item := range h.items {
		if item.Turn < 1 || seen[item.Turn] {
			continue
		}
		if _, ok := retainedLine(item); ok {
			seen[item.Turn] = true
			out = append(out, item.Turn)
		}
	}
	return out
}

// Turns returns the turn numbers that hold lines, oldest first.
func (h retainedHistory) Turns(ctx context.Context) ([]int, error) {
	return h.turns(), nil
}

// Turn returns one turn's lines, as they were said.
func (h retainedHistory) Turn(ctx context.Context, turn int) (string, error) {
	var lines []string
	for _, item := range h.items {
		if item.Turn != turn {
			continue
		}
		if l, ok := retainedLine(item); ok {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// Search returns the retained lines that match the regular expression.
func (h retainedHistory) Search(ctx context.Context, pattern string) ([]HistoryMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	var out []HistoryMatch
	for _, item := range h.items {
		// The lines a search names have to be turns a read can open:
		// the prompt's own message carries no turn, and the tool's read
		// refuses one below one.
		if item.Turn < 1 {
			continue
		}
		l, ok := retainedLine(item)
		if !ok || !re.MatchString(l) {
			continue
		}
		out = append(out, HistoryMatch{Turn: item.Turn, Text: l})
	}
	return out, nil
}

// lazyHistory is the history a sink reads back per call.
//
// The records are read when the tool asks, so a long session does not
// pay for a rebuild it never reads, and a read that fails is the tool
// call's failure rather than the session's.
type lazyHistory struct {
	reader RecordReader
}

// Turns returns the turn numbers the reader's records retain.
func (l lazyHistory) Turns(ctx context.Context) ([]int, error) {
	records, err := l.reader.ReadRecords(ctx)
	if err != nil {
		return nil, err
	}
	return NewHistory(records).Turns(ctx)
}

// Turn returns one turn's text from the reader's records.
func (l lazyHistory) Turn(ctx context.Context, turn int) (string, error) {
	records, err := l.reader.ReadRecords(ctx)
	if err != nil {
		return "", err
	}
	return NewHistory(records).Turn(ctx, turn)
}

// Search returns the retained lines that match, from the reader's
// records.
func (l lazyHistory) Search(ctx context.Context, pattern string) ([]HistoryMatch, error) {
	records, err := l.reader.ReadRecords(ctx)
	if err != nil {
		return nil, err
	}
	return NewHistory(records).Search(ctx, pattern)
}
