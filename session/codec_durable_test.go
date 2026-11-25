package session

import (
	"reflect"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestCodecRoundTripsTheDurableSessionRecords covers the codec's
// promise for the records a durable session adds: every field of a
// record survives the encode and the decode.
func TestCodecRoundTripsTheDurableSessionRecords(t *testing.T) {
	records := []poo.Record{
		&poo.ToolStartedRecord{
			RecordMeta: poo.NewRecordMeta("tool.started"),
			RunID:      "r1",
			Turn:       2,
			Handle:     "r3",
			CallID:     "call_1",
			Tool:       "bash",
		},
		&poo.RunResumedRecord{
			RecordMeta:        poo.NewRecordMeta("run.resumed"),
			RunID:             "r1",
			ResumptionAttempt: 1,
			FromPhase:         poo.StateExecuting,
			Turn:              2,
		},
		&poo.TurnHookRecord{
			RecordMeta:  poo.NewRecordMeta("turn.hook"),
			RunID:       "r1",
			Turn:        2,
			Phase:       "after_turn",
			Status:      "ended",
			HookIndex:   1,
			Interrupted: true,
		},
		&poo.FeedSubscribedRecord{
			RecordMeta: poo.NewRecordMeta("feed.subscribed"),
			Feed:       "inbox",
			Instance:   "i1",
			Scope:      "all",
			Prompt:     "watch the inbox",
		},
		&poo.FeedUnsubscribedRecord{
			RecordMeta: poo.NewRecordMeta("feed.unsubscribed"),
			Feed:       "inbox",
			Instance:   "i1",
		},
		&poo.PermissionAskedRecord{
			RecordMeta: poo.NewRecordMeta("permission.asked"),
			RunID:      "r1",
			AskID:      "a1",
			Tool:       "bash",
			Summary:    "the command",
			Class:      poo.ClassSideEffecting,
			Reason:     poo.AskReasonPolicy,
			Deadline:   "2026-01-01T00:00:00.000000001Z",
			Fallback:   "deny",
		},
	}
	for _, want := range records {
		line, err := JSONLCodec{}.Encode(want)
		if err != nil {
			t.Fatalf("encoding the %s record: %v", want.Meta().Record, err)
		}
		got, err := JSONLCodec{}.Decode(line)
		if err != nil {
			t.Fatalf("decoding the %s record: %v", want.Meta().Record, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("the %s record did not round-trip:\nwant %+v\ngot  %+v", want.Meta().Record, want, got)
		}
	}
}
