package poo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

func TestDeliveryCrashRestoresOnce(t *testing.T) {
	for _, kind := range []string{"job", "feed"} {
		t.Run(kind, func(t *testing.T) {
			records := []poo.Record{header("s1")}
			if kind == "job" {
				records = append(records, &poo.JobEndedRecord{RecordMeta: poo.NewRecordMeta("job.ended"), SessionID: "s1", JobID: "j1", Handle: "r1", Tool: "echo", RunID: "run-1", Outcome: poo.OutcomeOK, InputID: "s1-ji1", Text: "unique delivery payload"})
			} else {
				records = append(records, &poo.InputQueuedRecord{RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")), SessionID: "s1", InputID: "s1-ji1", Mode: poo.InputModeFeed, Text: "unique delivery payload"})
			}
			sink := &recordingSink{}
			opts := restoreOptions(t, scripted([]fakeResponse{{text: "ok"}}), sink)
			opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
			s, _, err := poo.Restore(opts, records)
			if err != nil {
				t.Fatal(err)
			}
			runSession(t, s, "first")
			s.Close()
			found := false
			for _, rec := range sink.allRecords() {
				if _, ok := rec.(*poo.InputDeliveredRecord); ok {
					break
				}
				records = append(records, rec)
				if m, ok := rec.(*poo.MessageRecord); ok && len(m.Content) > 0 && strings.Contains(m.Content[0].Text, "unique delivery payload") {
					found = true
				}
			}
			if !found {
				t.Fatal("delivery message missing before input.delivered")
			}
			tr := scripted([]fakeResponse{{text: "ok"}})
			opts = restoreOptions(t, tr, &recordingSink{})
			opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
			restored, _, err := poo.Restore(opts, records)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			runSession(t, restored, "next")
			count := 0
			for _, m := range tr.Requests()[0].Messages {
				count += strings.Count(m.Text(), "unique delivery payload")
			}
			if count != 1 {
				t.Fatalf("model saw input %d times, want once", count)
			}
		})
	}
}

func TestCompactedDeliveryDoesNotRequeue(t *testing.T) {
	var message poo.MessageRecord
	if err := json.Unmarshal([]byte(`{"record":"message","run_id":"run-1","turn":1,"role":"user","input_id":"s1-ji1","content":[{"kind":"text","text":"payload"}]}`), &message); err != nil {
		t.Fatal(err)
	}
	records := []poo.Record{header("s1"), &poo.InputQueuedRecord{RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")), InputID: "s1-ji1", Mode: poo.InputModeFeed, Text: "payload"}, &poo.RunStartedRecord{RecordMeta: poo.NewRecordMeta("run.started"), RunID: "run-1"}, &message, &poo.CompactionRecord{RecordMeta: poo.NewRecordMeta("compaction"), RunID: "run-1", Compaction: poo.Compaction{Strategy: "test", Replace: &poo.RangeReplacement{Cut: poo.TurnRange{First: 1, Last: 1}, Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "summary")}}}}}
	opts := restoreOptions(t, scripted(nil), &recordingSink{})
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s, info, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if info.Compactions != 1 {
		t.Fatalf("compactions = %d", info.Compactions)
	}
	if len(info.Jobs) != 0 {
		t.Fatalf("compacted delivery requeued: %+v", info.Jobs)
	}
	var text string
	for _, m := range poo.Messages(s) {
		text += m.Text()
	}
	if strings.Contains(text, "payload") || !strings.Contains(text, "summary") {
		t.Fatalf("compacted history = %q", text)
	}
}

func TestLegacyMessageWithoutInputIDStillRequeues(t *testing.T) {
	records := []poo.Record{header("s1"), &poo.JobEndedRecord{RecordMeta: poo.NewRecordMeta("job.ended"), InputID: "s1-ji1", Text: "old"}, &poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), Role: poo.RoleUser, Content: poo.NewTextMessage(poo.RoleUser, "old").Content}}
	tr := scripted([]fakeResponse{{text: "ok"}})
	opts := restoreOptions(t, tr, &recordingSink{})
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s, info, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(info.Jobs) != 1 {
		t.Fatalf("legacy input pending count = %d, want 1", len(info.Jobs))
	}
	runSession(t, s, "next")
	count := 0
	for _, m := range tr.Requests()[0].Messages {
		count += strings.Count(m.Text(), "old")
	}
	if count != 2 {
		t.Fatalf("legacy input appeared %d times, want 2", count)
	}
}
