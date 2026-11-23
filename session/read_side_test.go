package session

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sdougbrown/poo"
)

// The read side is the history tool's whole supply line: these cover
// the three implementations it reads through.

// TestALogReadRecordsRoundTripsTheRecords covers the log's read side:
// the records the log wrote come back whole, oldest first.
func TestALogReadRecordsRoundTripsTheRecords(t *testing.T) {
	path := t.TempDir() + "/session.jsonl"
	log, err := Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	first := &poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 1,
		Role: poo.RoleUser, Content: poo.NewTextMessage(poo.RoleUser, "first").Content}
	second := &poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 2,
		Role: poo.RoleAssistant, Content: poo.NewTextMessage(poo.RoleAssistant, "second").Content}
	if err := log.Append(context.Background(), first); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Append(context.Background(), second); err != nil {
		t.Fatalf("appending: %v", err)
	}
	records, err := log.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("the read holds %d records, want 2", len(records))
	}
	firstBack, ok := records[0].(*poo.MessageRecord)
	if !ok || firstBack.Turn != 1 || len(firstBack.Content) != 1 || firstBack.Content[0].Text != "first" {
		t.Errorf("the first record read back is %+v, want the first message", records[0])
	}
	secondBack, ok := records[1].(*poo.MessageRecord)
	if !ok || secondBack.Turn != 2 || secondBack.Content[0].Text != "second" {
		t.Errorf("the second record read back is %+v, want the second message", records[1])
	}
}

// TestTheStoreReaderRefusesAnEmptySessionFile covers the degenerate
// chain: a session file of zero length holds no session, and the read
// says so instead of indexing past it.
func TestTheStoreReaderRefusesAnEmptySessionFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	created := &poo.SessionCreatedRecord{
		RecordMeta:      poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{SessionID: "s1", Format: poo.FormatVersion, Mode: string(ModeLogged)},
	}
	if _, err := store.Create(context.Background(), created); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The degenerate chain: a store's file holds the header, so the
	// empty read comes from a store whose file was emptied out from
	// under it — the state the load path already treats as expected.
	store.mu.Lock()
	if err := os.WriteFile(store.open.path, nil, 0o644); err != nil {
		t.Fatalf("emptying the file: %v", err)
	}
	store.mu.Unlock()
	_, err = store.ReadRecords(context.Background())
	if err == nil {
		t.Fatal("the read succeeded on an empty session file")
	}
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Code != CodeCorruptLog {
		t.Errorf("the read failed with %v, want the corrupt-log error", err)
	}
}

// TestTheStoreReaderRefusesAStoreWithNoOpenSession covers the guard: a
// store with no open session has no chain to read, and the read says so
// in the store's own error code.
func TestTheStoreReaderRefusesAStoreWithNoOpenSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	_, err = store.ReadRecords(context.Background())
	if err == nil {
		t.Fatal("the read succeeded on a store with no open session")
	}
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Code != CodeStoreError {
		t.Errorf("the read failed with %v, want the store error", err)
	}
}

// TestTheRecordingKeepsOnlyWhatTheStoreAcceptedAndReadsTheChain covers
// the recording over a real store: a write the store refused is in
// neither the buffer nor the file, and the read goes through the
// store, so the chain is whole however much the buffer holds.
func TestTheRecordingKeepsOnlyWhatTheStoreAcceptedAndReadsTheChain(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	rec := &poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 1,
		Role: poo.RoleUser, Content: poo.NewTextMessage(poo.RoleUser, "kept").Content}
	// The fault counts from the header's own append: the header lands,
	// and the recording's write is the one that fails.
	store.appendFault = 1
	if _, err := store.Create(context.Background(), &poo.SessionCreatedRecord{
		RecordMeta:      poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{SessionID: "s1", Format: poo.FormatVersion, Mode: string(ModeLogged)},
	}); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	recording := &Recording{Store: store}
	if err := recording.Append(context.Background(), rec); err == nil {
		t.Fatal("the append succeeded under the fault")
	}
	store.appendFault = 0
	if err := recording.Append(context.Background(), rec); err != nil {
		t.Fatalf("appending after the fault: %v", err)
	}
	records, err := recording.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading through the store: %v", err)
	}
	// The chain: the header, the refused record absent, the accepted
	// record present.
	if len(records) != 2 {
		t.Fatalf("the chain holds %d records, want the header and the accepted write", len(records))
	}
	if _, ok := records[0].(*poo.SessionCreatedRecord); !ok {
		t.Errorf("the first record is %T, want the header", records[0])
	}
	if msg, ok := records[1].(*poo.MessageRecord); !ok || msg.Turn != 1 {
		t.Errorf("the second record is %v, want the accepted message", records[1])
	}

	// The memory path: a recording over nothing keeps its own.
	bare := &Recording{}
	if err := bare.Append(context.Background(), rec); err != nil {
		t.Fatalf("appending to the memory: %v", err)
	}
	if records, _ := bare.ReadRecords(context.Background()); len(records) != 1 {
		t.Errorf("the memory holds %d records, want 1", len(records))
	}
}

// TestARecordingReadsThroughAReadableStore covers the delegation: a
// recording over a readable store reads the store's chain.
func TestARecordingReadsThroughAReadableStore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	rec := &poo.MessageRecord{RecordMeta: poo.NewRecordMeta("message"), RunID: "r1", Turn: 1,
		Role: poo.RoleUser, Content: poo.NewTextMessage(poo.RoleUser, "kept").Content}
	if _, err := store.Create(context.Background(), &poo.SessionCreatedRecord{
		RecordMeta:      poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{SessionID: "s1", Format: poo.FormatVersion, Mode: string(ModeLogged)},
	}); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(context.Background(), rec); err != nil {
		t.Fatalf("appending: %v", err)
	}
	recording := &Recording{Store: store}
	records, err := recording.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading through the store: %v", err)
	}
	// The chain: the header, then the message the model could recover.
	if len(records) != 2 {
		t.Fatalf("the store's chain holds %d records, want the header and the message", len(records))
	}
	if msg, ok := records[1].(*poo.MessageRecord); !ok || msg.Content[0].Text != "kept" {
		t.Errorf("the second record is %v, want the message the session wrote", records[1])
	}
}
