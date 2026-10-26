package session

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAFaultAtEveryRecoveryBoundaryIsFinished sweeps the injected append
// failure over every record the recovery writes, and checks the second
// load finishes the job with no duplicate and no missing record.
func TestAFaultAtEveryRecoveryBoundaryIsFinished(t *testing.T) {
	for fault := 1; fault <= 5; fault++ {
		t.Run("", func(t *testing.T) {
			dir, store := newTestStore(t)
			ctx := context.Background()
			writeSession(t, dir, "s1",
				createdRecord("s1"),
				runStarted("r1"),
				&poo.MessageRecord{
					RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginModel),
					RunID:      "r1",
					Turn:       1,
					Role:       poo.RoleAssistant,
					MessageID:  "m1",
					Content: []poo.ContentBlock{{
						Kind:       poo.ContentToolCall,
						ToolCallID: "call_1",
						Name:       "bash",
						Arguments:  []byte(`{}`),
					}},
				},
				toolCallWithID("r1", 1, "r1", "bash", "call_1"),
				jobStarted("j1", "r1", "bash", "r1"),
				inputQueued("in1", poo.FollowUp),
			)
			store.appendFault = fault
			if _, err := store.Load(ctx, "s1"); err == nil {
				t.Fatal("the first load succeeded, want the injected failure")
			}
			store.appendFault = 0
			loaded, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("the second load: %v", err)
			}
			counts := map[string]int{}
			for _, rec := range loaded.Records {
				counts[rec.Meta().Record]++
			}
			if counts["tool.result"] != 1 {
				t.Errorf("the session holds %d tool results, want 1", counts["tool.result"])
			}
			if counts["message"] != 2 { // the assistant call and the result's message
				t.Errorf("the session holds %d messages, want 2", counts["message"])
			}
			if counts["run.settled"] != 1 {
				t.Errorf("the session holds %d settles, want 1", counts["run.settled"])
			}
			if counts["job.ended"] != 1 {
				t.Errorf("the session holds %d job ends, want 1", counts["job.ended"])
			}
			if counts["input.dropped"] != 1 {
				t.Errorf("the session holds %d dropped inputs, want 1", counts["input.dropped"])
			}
			if len(loaded.Jobs) != 1 {
				t.Errorf("the load queues %d job results, want 1", len(loaded.Jobs))
			}
			// A third load adds nothing.
			before := len(loaded.Records)
			third, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("the third load: %v", err)
			}
			if len(third.Records) != before {
				t.Errorf("the third load holds %d records, want %d", len(third.Records), before)
			}
		})
	}
}
