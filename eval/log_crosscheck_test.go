package eval

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// The eval package decodes the session log into its own loose shape. A
// field renamed in the log's record types would decode to a zero value
// there without an error, so this test writes a log with the real record
// types and the real writer, and checks that every field the metrics use
// comes back non-zero.
func TestEveryFieldTheMetricsUseSurvivesTheRealWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	w, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	cache := 40
	readArgs := json.RawMessage(`{"path":"a.go","offset":3,"limit":10}`)
	bashArgs := json.RawMessage(`{"command":"ls -la"}`)
	records := []poo.Record{
		&poo.SessionCreated{
			RecordMeta: poo.NewRecordMeta("session.created"), Format: poo.FormatVersion,
			SessionID: "s", WorkingDir: "/w", ContextLimit: 32768, ToolsMode: "native",
		},
		&poo.RunStarted{RecordMeta: poo.NewRecordMeta("run.started"), RunID: "r"},
		&poo.MessageRecord{
			RecordMeta: poo.NewRecordMeta("message"), RunID: "r", Turn: 1, Origin: "model", Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentText, Text: "looking"},
				{Kind: poo.ContentToolCall, ToolCallID: "c1", Name: "read", Arguments: readArgs},
			},
		},
		&poo.Usage{
			RecordMeta: poo.NewRecordMeta("usage"), RunID: "r", Turn: 1, Request: 1,
			InputTokens: 100, OutputTokens: 20, CacheReadTokens: &cache, TTFTMS: 150,
		},
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"), RunID: "r", Turn: 1, Handle: "r1", Tool: "read", Arguments: readArgs,
		},
		&poo.ToolResultRecord{
			RecordMeta: poo.NewRecordMeta("tool.result"), RunID: "r", Turn: 1, Handle: "r1", Tool: "read",
			Outcome: poo.OutcomeError, Text: "no such file",
		},
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"), RunID: "r", Turn: 2, Handle: "r2", Tool: "bash", Arguments: bashArgs,
		},
		&poo.RunSettled{
			RecordMeta: poo.NewRecordMeta("run.settled"), RunID: "r", Reason: poo.ReasonEndTurn,
			Detail: poo.SettleDetail{Source: "end_turn"}, Turns: 2,
		},
	}
	for _, rec := range records {
		if err := w.Append(t.Context(), rec); err != nil {
			t.Fatalf("appending %s: %v", rec.Meta().Record, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the log: %v", err)
	}

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}

	if log.ContextLimit() == 0 {
		t.Error("the session's context_limit did not decode")
	}
	settle := log.Settle()
	if settle == nil || settle.Reason == "" || settle.Turns == 0 || settle.Details.Source == "" {
		t.Errorf("the settle record did not decode: %+v", settle)
	}
	usages := log.Usages()
	if len(usages) != 1 {
		t.Fatalf("the log holds %d usage records, want 1", len(usages))
	}
	u := usages[0]
	if u.Turn == 0 || u.InputTokens == 0 || u.OutputTokens == 0 || u.CacheReadTokens == nil || *u.CacheReadTokens == 0 || u.TTFTMS == 0 {
		t.Errorf("the usage record did not decode: %+v", u)
	}
	calls := log.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("the log holds %d tool calls, want 2", len(calls))
	}
	for _, c := range calls {
		if c.Tool == "" || len(c.Arguments) == 0 {
			t.Errorf("the tool call did not decode: %+v", c)
		}
	}
	if results := log.ToolResults(); len(results) != 1 || results[0].Outcome != "error" {
		t.Errorf("the tool result did not decode: %+v", results)
	}
	if log.FinalMessage() == "" {
		t.Error("the model's message text did not decode")
	}

	m := ComputeMetrics(log, Diff{})
	if m.Turns == 0 || m.InputTokens == 0 || m.OutputTokens == 0 || m.TTFTMS == 0 ||
		m.CacheReadTokens == nil || *m.CacheReadTokens == 0 ||
		m.SettleReason == "" || m.SettleReason == "unknown" || m.SettleSource == "" ||
		m.ToolCalls == 0 || m.ToolErrors == 0 || m.Reads == 0 {
		t.Errorf("a metric came back zero from a real log: %+v", m)
	}
}
