package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The calls the records hold, and the counters a restored session
// continues.

// TestTheCallIDsPairTheCallsWithTheMessages covers the pairing a
// recovery needs: a call's provider id is on the call's own record, so a
// batch whose records land in any order is paired correctly.
func TestTheCallIDsPairTheCallsWithTheMessages(t *testing.T) {
	records := []poo.Record{
		&poo.MessageRecord{
			RecordMeta: poo.NewRecordMeta("message"),
			RunID:      "r1",
			Turn:       1,
			Role:       poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentToolCall, ToolCallID: "c1", Name: "echo"},
				{Kind: poo.ContentToolCall, ToolCallID: "c2", Name: "echo"},
			},
		},
		// A batch of read-only calls runs at the same time, and its
		// records land in any order.
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r2",
			CallID:     "c2",
			Tool:       "echo",
		},
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r1",
			CallID:     "c1",
			Tool:       "echo",
		},
	}
	ids := poo.CallIDs(records)
	if ids["r1"] != "c1" {
		t.Errorf("the call r1 has the id %q, want c1", ids["r1"])
	}
	if ids["r2"] != "c2" {
		t.Errorf("the call r2 has the id %q, want c2", ids["r2"])
	}
}

// TestTheCallIDsLeaveACallWithNoMessageAlone covers the call the model
// never saw: it has no id, because nothing refers to it.
func TestTheCallIDsLeaveACallWithNoMessageAlone(t *testing.T) {
	records := []poo.Record{
		&poo.ToolCallRecord{
			RecordMeta: poo.NewRecordMeta("tool.call"),
			RunID:      "r1",
			Turn:       1,
			Handle:     "r1",
			Tool:       "echo",
		},
	}
	if ids := poo.CallIDs(records); len(ids) != 0 {
		t.Errorf("the records hold the ids %v, want none", ids)
	}
}

// TestTheRestoredSessionContinuesTheHandles covers the restored
// session's own calls: their handles continue after the ones the
// records used, so a result is never filed under an earlier call's
// handle.
func TestTheRestoredSessionContinuesTheHandles(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)

	// The restored session makes a call of its own.
	restoredSink := &recordingSink{}
	restoredTr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c2", Name: "echo", Arguments: json.RawMessage(`{"text":"again"}`)}}},
		{text: "done"},
	})
	session, _, err := poo.Restore(restoreOptions(t, restoredTr, restoredSink), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	runSession(t, session, "more")

	handles := map[string]bool{}
	for _, rec := range restoredSink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			handles[r.Handle] = true
		}
	}
	if len(handles) != 1 {
		t.Fatalf("the restored session's results hold the handles %v, want one", handles)
	}
	for handle := range handles {
		if handle == "r1" {
			t.Error("the restored session reused the handle r1, want a handle of its own")
		}
	}
}

// TestTheRestoredSessionContinuesTheMessageIDs covers the restored
// session's message ids: they continue after the ones the records
// used, so a delivery note never names an earlier message.
func TestTheRestoredSessionContinuesTheMessageIDs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	s := newTestSession(t, opts)
	runSession(t, s, "hello")
	records := append([]poo.Record{header("s1")}, sink.allRecords()...)
	recorded := map[string]bool{}
	for _, rec := range records {
		if m, ok := rec.(*poo.MessageRecord); ok && m.MessageID != "" {
			recorded[m.MessageID] = true
		}
	}
	if len(recorded) == 0 {
		t.Fatal("the records hold no message id, want the session's own")
	}

	restoredSink := &recordingSink{}
	restoredTr := scripted([]fakeResponse{{text: "done"}})
	session, _, err := poo.Restore(restoreOptions(t, restoredTr, restoredSink), records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	runSession(t, session, "more")

	for _, rec := range restoredSink.allRecords() {
		m, ok := rec.(*poo.MessageRecord)
		if !ok || m.MessageID == "" {
			continue
		}
		if recorded[m.MessageID] {
			t.Errorf("the restored session reused the message id %q, want an id of its own", m.MessageID)
		}
	}
}

// TestTheRestoredSessionContinuesTheJobInputIDs covers the restored
// session's job results: their input ids continue after the ones the
// records used, so a result is never confused with an earlier one.
func TestTheRestoredSessionContinuesTheJobInputIDs(t *testing.T) {
	records := []poo.Record{
		jobHeader("s1"),
		&poo.JobEndedRecord{
			RecordMeta: poo.NewRecordMeta("job.ended"),
			SessionID:  "s1",
			JobID:      "j1",
			Handle:     "r1",
			Tool:       "job",
			RunID:      "r1",
			Outcome:    poo.OutcomeOK,
			InputID:    "s1-ji1",
			Text:       "the earlier result",
			EndedAt:    "2026-01-01T00:00:01Z",
		},
		&poo.InputDeliveredRecord{
			RecordMeta:  poo.NewRecordMeta("input.delivered"),
			SessionID:   "s1",
			RunID:       "r1",
			InputID:     "s1-ji1",
			Mode:        poo.InputModeJob,
			DeliveredAt: "2026-01-01T00:00:02Z",
		},
	}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	opts := jobOptions(t, tr, tool, sink)
	session, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	// The run waits for the job it started, so the job is released
	// while the run waits.
	if _, err := session.Prompt(context.Background(), "more"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	tool.ReleaseAll()
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	waitFor(t, "the job to end", func() bool { return jobEnds(sink) == 1 })

	ended := lastJobEnd(sink)
	if ended == nil {
		t.Fatal("the job never ended")
	}
	if ended.InputID != "s1-ji2" {
		t.Errorf("the job's input id is %q, want s1-ji2", ended.InputID)
	}
}

// jobHeader returns the header of a session that ran a job.
func jobHeader(id string) *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  id,
			Mode:       "logged",
			CreatedAt:  "2026-01-01T00:00:00Z",
			WorkingDir: "/tmp",
			Tools:      []poo.ToolInfo{{Name: "job", Class: poo.ClassSideEffecting, Background: poo.BackgroundAlways}}, ContextLimit: 32768,
			ToolsMode: "native"},
	}
}

// jobEnds returns how many job ends a sink holds.
func jobEnds(sink *recordingSink) int {
	count := 0
	for _, rec := range sink.allRecords() {
		if _, ok := rec.(*poo.JobEndedRecord); ok {
			count++
		}
	}
	return count
}

// lastJobEnd returns the last job end a sink holds.
func lastJobEnd(sink *recordingSink) *poo.JobEndedRecord {
	var last *poo.JobEndedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.JobEndedRecord); ok {
			last = r
		}
	}
	return last
}

// TestARestoredHeldResultKeepsItsImage covers the restart: a held result's
// picture is on the record, so a restored session still delivers a picture
// rather than text that names one.
func TestARestoredHeldResultKeepsItsImage(t *testing.T) {
	image := poo.Attachment{Modality: poo.ModalityImage, MediaType: "image/png", Data: []byte{1, 2, 3, 4}}
	records := []poo.Record{
		jobHeader("s1"),
		&poo.JobEndedRecord{
			RecordMeta:  poo.NewRecordMeta("job.ended"),
			SessionID:   "s1",
			JobID:       "j1",
			Handle:      "r1",
			Tool:        "job",
			RunID:       "r1",
			Outcome:     poo.OutcomeOK,
			InputID:     "s1-ji1",
			Text:        "read: shot.png (image, 4 bytes)",
			Attachments: []poo.Attachment{image},
			EndedAt:     "2026-01-01T00:00:01Z",
		},
	}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := jobOptions(t, tr, tool, &recordingSink{})
	session, restored, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	if len(restored.Jobs) != 1 {
		t.Fatalf("the restored session lists %d results, want one", len(restored.Jobs))
	}
	if len(restored.Jobs[0].Attachments) != 1 {
		t.Fatalf("the restored result carries %d attachments, want one", len(restored.Jobs[0].Attachments))
	}
	if restored.Jobs[0].Attachments[0].MediaType != image.MediaType {
		t.Errorf("the restored image is %q, want %q", restored.Jobs[0].Attachments[0].MediaType, image.MediaType)
	}
	if string(restored.Jobs[0].Attachments[0].Data) != string(image.Data) {
		t.Errorf("the restored bytes are %v, want %v", restored.Jobs[0].Attachments[0].Data, image.Data)
	}
}

// TestAJobResultsImageSurvivesItsRecord covers the record's encoding: a
// picture on a job's end record is written and read back, so a restart that
// loads the log delivers the picture rather than text that names one.
func TestAJobResultsImageSurvivesItsRecord(t *testing.T) {
	image := poo.Attachment{Modality: poo.ModalityImage, MediaType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}}
	record := &poo.JobEndedRecord{
		RecordMeta:  poo.NewRecordMeta("job.ended"),
		SessionID:   "s1",
		JobID:       "j1",
		Handle:      "r1",
		Tool:        "read",
		RunID:       "r1",
		Outcome:     poo.OutcomeOK,
		InputID:     "s1-ji1",
		Text:        "read: shot.png (image, 4 bytes)",
		Attachments: []poo.Attachment{image},
		EndedAt:     "2026-01-01T00:00:01Z",
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encoding the record: %v", err)
	}
	// The keys are the record's own: a renamed tag would drop every picture
	// from every log.
	for _, key := range []string{`"attachments"`, `"modality"`, `"media_type"`, `"data"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("the encoded record carries no %s: %s", key, raw)
		}
	}

	var decoded poo.JobEndedRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding the record: %v", err)
	}
	if len(decoded.Attachments) != 1 {
		t.Fatalf("the decoded record holds %d attachments, want one", len(decoded.Attachments))
	}
	if decoded.Attachments[0].MediaType != image.MediaType {
		t.Errorf("the decoded media type is %q, want %q", decoded.Attachments[0].MediaType, image.MediaType)
	}
	if decoded.Attachments[0].Modality != image.Modality {
		t.Errorf("the decoded modality is %q, want %q", decoded.Attachments[0].Modality, image.Modality)
	}
	if string(decoded.Attachments[0].Data) != string(image.Data) {
		t.Errorf("the decoded bytes are %v, want %v", decoded.Attachments[0].Data, image.Data)
	}
}
