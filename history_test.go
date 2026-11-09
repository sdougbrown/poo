package poo_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The history projection: a display summary computed from a session's
// records.

// denyingPolicy refuses the calls it is asked about.
type denyingPolicy struct {
	// denied names the tool the policy refuses.
	denied string
}

// BeforeTool refuses the calls of the denied tool.
func (p denyingPolicy) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	if info.Spec.Name == p.denied {
		return poo.Deny("the policy refuses this call"), nil
	}
	return poo.Allow(), nil
}

// projectionRecords builds the records of a session with tool calls, a
// denied call, a compaction and an interrupted run.
func projectionRecords(t *testing.T) []poo.Record {
	t.Helper()
	compactor := pootest.NewCompactor(true, poo.Compaction{
		Strategy: "the scripted one",
		Replace: &poo.RangeReplacement{
			Cut:      poo.TurnRange{First: 1, Last: 1},
			Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the summary of the first turn")},
		},
	})
	// The compactor waits for a turn to exist, so the cut it names is
	// one the session holds.
	compactor.SetShouldAfter(1)

	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
			{CallID: "c2", Name: "denied", Arguments: json.RawMessage(`{"text":"two"}`)},
		}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.Tools["denied"] = &namedTool{echoTool: echoTool{result: "never"}, name: "denied"}
	opts.ToolOrder = []string{"echo", "denied"}
	opts.Policy = denyingPolicy{denied: "denied"}
	opts.Compactor = compactor

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	records := sink.allRecords()
	// The recovery of a run that was interrupted: the call has no
	// outcome of its own, and the run settles as interrupted.
	seq := int64(len(records))
	stamp := func(rec poo.Record) poo.Record {
		seq++
		poo.Stamp(rec, seq, timeNow())
		return rec
	}
	records = append(records,
		stamp(&poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started"), RunID: "run-2", PromptHash: "hash", StartedAt: "2026-01-01T00:00:00Z", ModelID: "test-model"}),
		stamp(&poo.PermissionAskedRecord{RecordMeta: poo.NewRecordMeta("permission.asked"), RunID: "run-2", AskID: "ask-1", Tool: "echo", Summary: "read a file", Class: poo.ClassReadOnly, Reason: "policy"}),
		stamp(&poo.PermissionAnsweredRecord{RecordMeta: poo.NewRecordMeta("permission.answered"), RunID: "run-2", AskID: "ask-1", Decision: "deny", AnsweredBy: "the client", Provenance: "answered"}),
		stamp(&poo.ToolCallRecord{RecordMeta: poo.NewRecordMetaFrom("tool.call", poo.OriginModel), RunID: "run-2", Turn: 1, Handle: "r9", Tool: "echo", Arguments: json.RawMessage(`{"text":"three"}`)}),
		stamp(&poo.ToolResultRecord{RecordMeta: poo.NewRecordMetaFrom("tool.result", poo.OriginTool), RunID: "run-2", Turn: 1, Handle: "r9", Tool: "echo", Outcome: poo.OutcomeInterrupted, Text: "[r9 · echo]\noutcome unknown; the process stopped during this call"}),
		stamp(&poo.RunSettledRecord{RecordMeta: poo.NewRecordMeta("run.settled"), RunID: "run-2", Reason: poo.ReasonInterrupted, Detail: poo.SettleDetail{Source: "recovery"}, SettledAt: "2026-01-01T00:00:00Z"}),
	)
	return records
}

// TestTheHistoryProjectionHoldsTheSession covers the projection: the
// messages, the calls, the results, the permission outcomes, the
// compaction and the run boundaries.
func TestTheHistoryProjectionHoldsTheSession(t *testing.T) {
	records := projectionRecords(t)
	page := poo.ProjectHistory(records, 0, 0)

	counts := map[string]int{}
	for _, item := range page.Items {
		counts[item.Type]++
	}
	for _, kind := range []string{
		poo.HistoryMessage,
		poo.HistoryToolCall,
		poo.HistoryToolResult,
		poo.HistoryPermission,
		poo.HistoryCompaction,
		poo.HistoryRunStarted,
		poo.HistoryRunSettled,
	} {
		if counts[kind] == 0 {
			t.Errorf("the projection holds no %q item: %v", kind, counts)
		}
	}

	// The denied call is a result with the denied outcome.
	denied := false
	interrupted := false
	for _, item := range page.Items {
		if item.Type == poo.HistoryToolResult && item.Outcome == poo.OutcomeDenied {
			denied = true
		}
		if item.Type == poo.HistoryRunSettled && item.Reason == poo.ReasonInterrupted {
			interrupted = true
		}
	}
	if !denied {
		t.Error("the projection holds no denied result")
	}
	if !interrupted {
		t.Error("the projection holds no interrupted run")
	}

	// The items come back oldest first.
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i].Seq <= page.Items[i-1].Seq {
			t.Fatalf("the item %d has the seq %d, and the one before it has %d", i, page.Items[i].Seq, page.Items[i-1].Seq)
		}
	}
}

// TestTheHistoryProjectionPagesBackwards covers the paging: a page ends
// before a sequence number, holds at most a limit, and says whether
// older entries exist.
func TestTheHistoryProjectionPagesBackwards(t *testing.T) {
	records := projectionRecords(t)
	all := poo.ProjectHistory(records, 0, 0)
	if len(all.Items) < 6 {
		t.Fatalf("the projection holds %d items, want at least 6", len(all.Items))
	}
	if all.HasMore {
		t.Error("the whole projection says it has more")
	}

	// A limited page holds the newest entries and says that older ones
	// exist.
	page := poo.ProjectHistory(records, 0, 3)
	if len(page.Items) != 3 {
		t.Fatalf("the page holds %d items, want 3", len(page.Items))
	}
	if !page.HasMore {
		t.Error("the page says it has no more")
	}
	if page.Items[2].Seq != all.Items[len(all.Items)-1].Seq {
		t.Errorf("the page ends at the seq %d, want %d", page.Items[2].Seq, all.Items[len(all.Items)-1].Seq)
	}

	// A page that ends before a sequence number holds the entries
	// before it.
	before := all.Items[len(all.Items)-4].Seq
	page = poo.ProjectHistory(records, before, 2)
	if len(page.Items) != 2 {
		t.Fatalf("the page holds %d items, want 2", len(page.Items))
	}
	if page.Items[1].Seq >= before {
		t.Errorf("the page ends at the seq %d, and it must end before %d", page.Items[1].Seq, before)
	}
	if !page.HasMore {
		t.Error("the page says it has no more")
	}

	// The last page says there is nothing older.
	first := all.Items[0].Seq
	page = poo.ProjectHistory(records, first, 10)
	if page.HasMore {
		t.Error("the first page says it has more")
	}
	if len(page.Items) != 0 {
		t.Errorf("the first page holds %d items, want none", len(page.Items))
	}
}

// TestTheHistoryProjectionTakesTheDefaultLimit covers the limits: a
// page holds a hundred entries by default, and never more than five
// hundred.
func TestTheHistoryProjectionTakesTheDefaultLimit(t *testing.T) {
	var records []poo.Record
	for i := 0; i < 600; i++ {
		rec := &poo.MessageRecord{RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginUser), Role: poo.RoleUser, Content: []poo.ContentBlock{{Kind: poo.ContentText, Text: "a message"}}}
		poo.Stamp(rec, int64(i+1), timeNow())
		records = append(records, rec)
	}
	page := poo.ProjectHistory(records, 0, 0)
	if len(page.Items) != poo.DefaultHistoryLimit {
		t.Errorf("the page holds %d items, want %d", len(page.Items), poo.DefaultHistoryLimit)
	}
	if !page.HasMore {
		t.Error("the page says it has no more")
	}
	page = poo.ProjectHistory(records, 0, 1000)
	if len(page.Items) != poo.MaxHistoryLimit {
		t.Errorf("the page holds %d items, want %d", len(page.Items), poo.MaxHistoryLimit)
	}
}

// TestTheProjectionIsNotTheModelView covers the difference: the
// projection is a display summary, and the model's view is the history
// the model reads.
func TestTheProjectionIsNotTheModelView(t *testing.T) {
	records := projectionRecords(t)
	page := poo.ProjectHistory(records, 0, 0)
	// A tool result is one entry, not two: the record that holds the
	// result and the message that carries it say the same thing.
	results := 0
	for _, item := range page.Items {
		if item.Type == poo.HistoryToolResult {
			results++
		}
	}
	if results != 3 {
		t.Errorf("the projection holds %d tool results, want 3", results)
	}
}

// timeNow returns the current time, for the records a test builds.
func timeNow() time.Time { return time.Now() }

// TestTheProjectionCapsTheCallArguments covers the cap on a call item's
// text: the item holds the raw arguments, and the arguments can be as
// long as the script that wrote them, so the projection holds them to
// the summary cap.
func TestTheProjectionCapsTheCallArguments(t *testing.T) {
	args := `{"text":"` + strings.Repeat("a", poo.SummaryCapChars+100) + `"}`
	rec := &poo.ToolCallRecord{RecordMeta: poo.NewRecordMetaFrom("tool.call", poo.OriginModel), RunID: "run-1", Turn: 1, Handle: "r1", Tool: "echo", Arguments: json.RawMessage(args)}
	poo.Stamp(rec, 1, timeNow())

	page := poo.ProjectHistory([]poo.Record{rec}, 0, 0)
	if len(page.Items) != 1 {
		t.Fatalf("the page holds %d items, want 1", len(page.Items))
	}
	item := page.Items[0]
	// The arguments are ASCII, so the head cuts at the byte.
	total := utf8.RuneCountInString(args)
	want := args[:poo.SummaryCapChars] + fmt.Sprintf(poo.SummaryElisionNote, total-poo.SummaryCapChars)
	if item.Text != want {
		t.Errorf("the call item's text is %d characters, want the capped arguments", utf8.RuneCountInString(item.Text))
	}
}

// TestTheHistoryPageHoldsItsByteBudget covers the page's byte budget:
// the page keeps the newest entries that fit and says that older ones
// exist, and a page of one oversized entry still comes back, so a
// client paging backwards always moves.
func TestTheHistoryPageHoldsItsByteBudget(t *testing.T) {
	seq := int64(0)
	stamp := func(rec poo.Record) poo.Record {
		seq++
		poo.Stamp(rec, seq, timeNow())
		return rec
	}
	// The items are big: a dozen of them are over the budget.
	var records []poo.Record
	for i := 0; i < 20; i++ {
		records = append(records, stamp(&poo.ToolResultRecord{
			RecordMeta: poo.NewRecordMetaFrom("tool.result", poo.OriginTool),
			RunID:      "run-1", Turn: 1, Handle: fmt.Sprintf("r%d", i), Tool: "echo",
			Text: strings.Repeat("a", poo.PageByteBudget/12),
		}))
	}

	page := poo.ProjectHistory(records, 0, 0)
	bytes := 0
	for _, item := range page.Items {
		encoded, _ := json.Marshal(item)
		bytes += len(encoded) + 1
	}
	if bytes > poo.PageByteBudget {
		t.Errorf("the page holds %d bytes, over the budget", bytes)
	}
	if !page.HasMore {
		t.Error("the page says no older entries exist, want has_more")
	}
	if last := page.Items[len(page.Items)-1]; last.Seq != seq {
		t.Errorf("the page's last item has the seq %d, want the newest record's %d", last.Seq, seq)
	}

	// The page before the first item holds the entries it dropped.
	older := poo.ProjectHistory(records, page.Items[0].Seq, 0)
	if len(older.Items) == 0 {
		t.Fatal("the older page holds no items")
	}
	if last := older.Items[len(older.Items)-1]; last.Seq >= page.Items[0].Seq {
		t.Errorf("the older page ends at the seq %d, want one before %d", last.Seq, page.Items[0].Seq)
	}

	// One oversized item still comes back.
	rec := stamp(&poo.ToolResultRecord{
		RecordMeta: poo.NewRecordMetaFrom("tool.result", poo.OriginTool),
		RunID:      "run-1", Turn: 1, Handle: "big", Tool: "echo",
		Text: strings.Repeat("a", poo.PageByteBudget*2),
	})
	page = poo.ProjectHistory([]poo.Record{rec}, 0, 0)
	if len(page.Items) != 1 {
		t.Errorf("the page holds %d items, want the oversized one", len(page.Items))
	}
}
