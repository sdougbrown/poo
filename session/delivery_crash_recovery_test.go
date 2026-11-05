package session

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestRecoveryCompletesADeliveryItsMessageRecorded covers a crash between
// an input's message and its delivered record. The message names the
// input, so the recovery appends the missing delivered record instead of
// dropping the input, and a second load appends nothing.
func TestRecoveryCompletesADeliveryItsMessageRecorded(t *testing.T) {
	for _, tc := range []struct {
		mode   poo.InputMode
		origin poo.Origin
		// recorded is the origin the delivered record carries: a live
		// session stamps one only on a feed's delivery.
		recorded poo.Origin
	}{
		{poo.Steer, poo.Origin("steer"), ""},
		{poo.InputModeFeed, poo.FeedOrigin("inbox"), poo.FeedOrigin("inbox")},
		{poo.InputModeJob, poo.OriginTool, ""},
	} {
		mode := tc.mode
		t.Run(string(mode), func(t *testing.T) {
			dir, store := newTestStore(t)
			ctx := context.Background()
			// A job result has no queued record: its job's end carries
			// the input id.
			accepted := poo.Record(inputQueued("in1", mode))
			if mode == poo.InputModeJob {
				accepted = jobEnded("j1", "r1", "bash", "r1", "in1", "done")
			}
			writeSession(t, dir, "s1",
				createdRecord("s1"),
				runStarted("r1"),
				accepted,
				&poo.MessageRecord{RecordMeta: poo.NewRecordMetaFrom("message", tc.origin), RunID: "r1", Role: poo.RoleUser, InputID: "in1"},
				inputQueued("in2", poo.FollowUp),
			)
			loaded, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("loading the session: %v", err)
			}
			var delivered, dropped []string
			for _, rec := range loaded.Records {
				switch r := rec.(type) {
				case *poo.InputDeliveredRecord:
					delivered = append(delivered, r.InputID)
					if r.RunID != "r1" || r.Mode != mode || r.Origin != tc.recorded {
						t.Errorf("delivered record = %+v, want run r1, mode %s and origin %q", r, mode, tc.recorded)
					}
				case *poo.InputDroppedRecord:
					dropped = append(dropped, r.InputID)
				}
			}
			if len(delivered) != 1 || delivered[0] != "in1" {
				t.Errorf("delivered = %v, want [in1]", delivered)
			}
			if len(dropped) != 1 || dropped[0] != "in2" {
				t.Errorf("dropped = %v, want [in2]", dropped)
			}
			again, err := store.Load(ctx, "s1")
			if err != nil {
				t.Fatalf("loading the session again: %v", err)
			}
			if len(again.Records) != len(loaded.Records) {
				t.Errorf("a second load wrote %d more records", len(again.Records)-len(loaded.Records))
			}
		})
	}
}
