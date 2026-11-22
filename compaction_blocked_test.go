package poo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/compact/mine"
)

// A session whose view already exceeds its window cannot wait for a
// turn: the ratio trigger needs a request behind it, and the budget
// check would settle the run first. The gate gives the strategy one
// forced compaction there instead.

// TestARestoredSessionOverItsWindowCompactsFirst covers that run: the
// restored view is over the window on the first gate pass, the strategy
// reconstructs, and the run carries on instead of settling.
func TestARestoredSessionOverItsWindowCompactsFirst(t *testing.T) {
	// The original session runs three turns of a view the restored
	// session's window cannot hold.
	big := strings.Repeat("x", 40000)
	original := scripted([]fakeResponse{
		{text: big, calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)}}},
		{text: big, calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	runSession(t, newTestSession(t, testOptions(t, original, &echoTool{}, sink)), "hello")

	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	// The restored session runs under a window the view exceeds: the
	// header's limits are the ones a restore resurrects, so they carry
	// the smaller window.
	for _, rec := range records {
		if created, ok := rec.(*poo.SessionCreatedRecord); ok {
			created.ContextLimit = 20000
			created.Roles.Chat.ContextLimit = 20000
		}
	}

	// The first gate pass has no request to fork, so the pointer pass
	// reports its failure and the mining request reads the transcript
	// alone.
	restoredTransport := scripted([]fakeResponse{
		{text: "## Goal\nfinish the task"},
		{text: "the final answer"},
	})
	restoredSink := &recordingSink{}
	restoredOpts := restoreOptions(t, restoredTransport, restoredSink)
	restoredOpts.Model.ContextLimit = 20000
	restoredOpts.Compactor = mine.New(mine.Options{KeepRecentTokens: 1})

	restored, _, err := poo.Restore(restoredOpts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })

	res := runSession(t, restored, "carry on")
	var view []poo.Message
	origKinds := map[string]int{}
	for _, rec := range records {
		origKinds[rec.Meta().Record]++
		if m, ok := rec.(*poo.MessageRecord); ok {
			view = append(view, poo.Message{Role: m.Role, Content: m.Content})
			var text string
			for _, c := range m.Content {
				text += c.Text
			}
			t.Logf("view message: role %s, chars %d", m.Role, len(text))
		}
	}
	t.Logf("original sink records: %v", origKinds)
	kinds := map[string]int{}
	for _, rec := range restoredSink.allRecords() {
		kinds[rec.Meta().Record]++
	}
	t.Logf("restored sink records: %v, settle %q source %q text %q", kinds, res.Reason, res.Detail.Source, res.Detail.Text)
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn", res.Reason, res.Detail.Text)
	}

	// The compaction replaced the first two turns and kept the last one
	// verbatim; the pointer pass's failure is on the record.
	var compaction *poo.CompactionRecord
	for _, rec := range restoredSink.allRecords() {
		if cr, ok := rec.(*poo.CompactionRecord); ok {
			compaction = cr
		}
	}
	if compaction == nil {
		t.Fatal("the run holds no compaction")
	}
	if compaction.Cut.First != 1 || compaction.Cut.Last != 2 {
		t.Errorf("the compaction's cut is %v, want the turns 1 to 2", compaction.Cut)
	}
	if compaction.Compaction.Side == nil || compaction.Compaction.Side.Fork == nil ||
		!strings.Contains(compaction.Compaction.Side.Fork.Error, "no request to fork") {
		t.Errorf("the compaction's side is %+v, want the fork's failure on it", compaction.Compaction.Side)
	}

	// The run's last request carries the reconstruction and the kept
	// turn, and none of the replaced view.
	last := restoredTransport.Requests()
	if len(last) == 0 {
		t.Fatal("the transport holds no requests")
	}
	var sent strings.Builder
	for _, m := range last[len(last)-1].Messages {
		sent.WriteString(m.Text())
		sent.WriteString("\n")
	}
	if !strings.Contains(sent.String(), "finish the task") {
		t.Errorf("the request is %q, want the reconstruction in it", sent.String())
	}
	if strings.Contains(sent.String(), big) {
		t.Errorf("the request is %q, want the replaced view out of it", sent.String())
	}
}
