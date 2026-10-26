package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
)

// newTestStore returns a store over a temporary directory.
//
// The store is closed when the test ends.
func newTestStore(t *testing.T) (string, *FileStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return dir, store
}

// createdRecord returns the header of a session.
func createdRecord(id string) *poo.SessionCreated {
	return &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    id,
		Mode:         string(ModeLogged),
		CreatedAt:    "2026-10-20T00:00:00Z",
		WorkingDir:   "/tmp",
		Model:        poo.SessionModel{ModelID: "m"},
		Tools:        []poo.ToolInfo{{Name: "bash", Class: poo.ClassSideEffecting}, {Name: "read", Class: poo.ClassReadOnly}},
		ContextLimit: 32768,
		ToolsMode:    "native",
		Build:        poo.BuildInfo{Name: "poo", Version: "0.1.0"},
	}
}

// writeSession writes a session's file with the given records.
//
// The records are written as they are, without a sequence number: the
// file is written by hand, the way a session that was cut short is.
func writeSession(t *testing.T, dir, id string, recs ...poo.Record) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	for _, rec := range recs {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("encoding the record: %v", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("writing the record: %v", err)
		}
	}
	return path
}

// appendBytes writes bytes to a session's file, the way a process that
// was killed mid-write leaves it.
func appendBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatalf("writing to %s: %v", path, err)
	}
}

// logRecords reads the records of a session's file.
func logRecords(t *testing.T, path string) []poo.Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	codec := NewCodec()
	var out []poo.Record
	for _, line := range bytesLines(data) {
		if len(line) == 0 {
			continue
		}
		rec, err := codec.Decode(line)
		if err != nil {
			t.Fatalf("decoding %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// recordNames returns the names of some records, in order.
func recordNames(recs []poo.Record) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Meta().Record)
	}
	return out
}

// runStarted returns a run's first record.
func runStarted(runID string) *poo.RunStarted {
	return &poo.RunStarted{
		RecordMeta: poo.NewRecordMeta("run.started"),
		RunID:      runID,
		StartedAt:  "2026-10-20T00:00:00Z",
		ModelID:    "m",
		ToolsMode:  "native",
	}
}

// toolCall returns a tool call's record.
func toolCall(runID string, turn int, handle, tool string) *poo.ToolCallRecord {
	return &poo.ToolCallRecord{
		RecordMeta: poo.NewRecordMeta("tool.call"),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		Tool:       tool,
		Arguments:  json.RawMessage(`{}`),
	}
}

// toolResult returns a tool result's record.
func toolResult(runID string, turn int, handle, tool string, outcome poo.Outcome) *poo.ToolResultRecord {
	return &poo.ToolResultRecord{
		RecordMeta: poo.NewRecordMeta("tool.result"),
		RunID:      runID,
		Turn:       turn,
		Handle:     handle,
		Tool:       tool,
		Outcome:    outcome,
		Text:       "done",
	}
}

// permissionAsked returns a permission ask's record.
func permissionAsked(runID, askID, tool string) *poo.PermissionAskedRecord {
	return &poo.PermissionAskedRecord{
		RecordMeta: poo.NewRecordMeta("permission.asked"),
		RunID:      runID,
		AskID:      askID,
		Tool:       tool,
		Reason:     "the call is not allowed by any rule",
	}
}

// jobStarted returns a job's first record.
func jobStarted(job, handle, tool, runID string) *poo.JobStarted {
	return &poo.JobStarted{
		RecordMeta: poo.NewRecordMeta("job.started"),
		SessionID:  "s1",
		JobID:      job,
		Handle:     handle,
		Tool:       tool,
		RunID:      runID,
		StartedAt:  "2026-10-20T00:00:00Z",
	}
}

// jobEnded returns a job's last record.
func jobEnded(job, handle, tool, runID, inputID, text string) *poo.JobEnded {
	return &poo.JobEnded{
		RecordMeta: poo.NewRecordMeta("job.ended"),
		SessionID:  "s1",
		JobID:      job,
		Handle:     handle,
		Tool:       tool,
		RunID:      runID,
		Outcome:    poo.OutcomeOK,
		InputID:    inputID,
		Text:       text,
		EndedAt:    "2026-10-20T00:00:01Z",
	}
}

// inputQueued returns the record of an accepted input.
func inputQueued(inputID string, mode poo.InputMode) *poo.InputQueued {
	return &poo.InputQueued{
		RecordMeta: poo.NewRecordMeta("input.queued"),
		SessionID:  "s1",
		InputID:    inputID,
		Mode:       mode,
		Text:       "hello",
		QueuedAt:   "2026-10-20T00:00:00Z",
	}
}

// inputDelivered returns the record of an input that reached the model.
func inputDelivered(inputID string, mode poo.InputMode) *poo.InputDelivered {
	return &poo.InputDelivered{
		RecordMeta:  poo.NewRecordMeta("input.delivered"),
		SessionID:   "s1",
		RunID:       "r1",
		InputID:     inputID,
		Mode:        mode,
		DeliveredAt: "2026-10-20T00:00:01Z",
	}
}

// toolCallWithID returns a tool call's record, with the provider's id of
// the call.
func toolCallWithID(runID string, turn int, handle, tool, callID string) *poo.ToolCallRecord {
	rec := toolCall(runID, turn, handle, tool)
	rec.CallID = callID
	return rec
}
