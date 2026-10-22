package session

// Review scratch test: CallIDs pairs calls with ids by ordinal, so a
// batch whose records landed only in part pairs the wrong id.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// A parallel batch made three calls; the process was killed after the
// record of the second one (r2) landed. The assistant message holds all
// three call blocks (id1, id2, id3). The k-th call record of the turn is
// the k-th call block, so r2 is paired with id1 — the first call's id —
// and the recovery's interrupted answer names the wrong call.
func TestAPartialBatchPairsEachSurvivingCallWithItsOwnID(t *testing.T) {
	dir, store := newTestStore(t)
	_ = dir
	msg := &poo.MessageRecord{
		RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
		RunID:      "r1",
		Turn:       1,
		Role:       poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentToolCall, ToolCallID: "id1", Name: "blocker", Arguments: json.RawMessage(`{}`)},
			{Kind: poo.ContentToolCall, ToolCallID: "id2", Name: "blocker", Arguments: json.RawMessage(`{}`)},
			{Kind: poo.ContentToolCall, ToolCallID: "id3", Name: "blocker", Arguments: json.RawMessage(`{}`)},
		},
	}
	writeSession(t, dir, "s1",
		testHeader("s1", newBlockingTool()),
		runStarted("r1"),
		msg,
		toolCallWithID("r1", 1, "r2", "blocker", "id2"),
	)
	loaded, err := store.Load(context.Background(), "s1")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	// The surviving call record is r2, and r2 is the second call the
	// model made: its id is id2, not the first call's.
	answered := map[string]bool{}
	for _, rec := range loaded.Records {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Handle != "r2" {
			continue
		}
		if r.CallID != "id2" {
			t.Errorf("the result for r2 carries the call id %q, want id2", r.CallID)
		}
	}
	for _, rec := range loaded.Records {
		m, ok := rec.(*poo.MessageRecord)
		if !ok {
			continue
		}
		for _, b := range m.Content {
			if b.Kind == poo.ContentToolResult {
				answered[b.ToolCallID] = true
			}
		}
	}
	// Every call the model saw is answered, and each answer names its own
	// call.
	for _, id := range []string{"id1", "id2", "id3"} {
		if !answered[id] {
			t.Errorf("the recovery did not answer %q", id)
		}
	}
}
