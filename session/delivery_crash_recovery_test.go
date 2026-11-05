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
	for _, mode := range []poo.InputMode{poo.Steer, poo.InputModeFeed} {
		t.Run(string(mode), func(t *testing.T) {
			dir, store := newTestStore(t)
			ctx := context.Background()
			writeSession(t, dir, "s1",
				createdRecord("s1"),
				runStarted("r1"),
				inputQueued("in1", mode),
				&poo.MessageRecord{RecordMeta: poo.NewRecordMetaFrom("message", poo.Origin("steer")), RunID: "r1", Role: poo.RoleUser, InputID: "in1"},
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
					if r.RunID != "r1" || r.Mode != mode {
						t.Errorf("delivered record = %+v, want run r1 and mode %s", r, mode)
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
