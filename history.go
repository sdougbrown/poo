package poo

import (
	"strings"
)

// The history projection: a display summary computed from a session's
// records.
//
// It is not the model's view and not the raw records: a projection says
// what happened, in the order it happened, in the shape a client shows.

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
	// Text is the item's text.
	Text string `json:"text,omitempty"`
	// Handle is the tool call's handle, for a tool item.
	Handle string `json:"handle,omitempty"`
	// Tool is the tool the item is about.
	Tool string `json:"tool,omitempty"`
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
	page.Items = older
	return page
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
		item := HistoryItem{Seq: meta.Seq, At: meta.TS}
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
			item.Text = string(r.Arguments)
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
		case *RunStarted:
			item.Type = HistoryRunStarted
			item.RunID = r.RunID
		case *RunSettled:
			item.Type = HistoryRunSettled
			item.RunID = r.RunID
			item.Reason = r.Reason
			item.Text = r.Detail.Source
		case *JobStarted:
			item.Type = HistoryJobStarted
			item.RunID = r.RunID
			item.Handle = r.Handle
			item.Tool = r.Tool
		case *JobEnded:
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
