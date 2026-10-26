package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestTheMemoryRefusesASessionWithNoHeader covers the ephemeral store's
// check: a header that is nil is refused, not dereferenced.
func TestTheMemoryRefusesASessionWithNoHeader(t *testing.T) {
	memory := NewMemory()
	if _, err := memory.Create(context.Background(), nil); err == nil {
		t.Fatal("the memory accepted a nil header")
	}
}

// TestTheClockReachesARecordsTimestamp covers the store's clock: a
// record's timestamp is the store's, so last activity is testable.
func TestTheClockReachesARecordsTimestamp(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	store, err := NewStore(dir, WithClock(func() time.Time { return at }))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	header := createdRecord("s1")
	header.CreatedAt = ""
	if _, err := store.Create(context.Background(), header); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(context.Background(), runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	summaries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("the store lists %d sessions, want 1", len(summaries))
	}
	if !summaries[0].LastActivity.Equal(at) {
		t.Errorf("the session's last activity is %s, want %s", summaries[0].LastActivity, at)
	}
}

// TestACorruptMiddleLineDoesNotHideLaterActivity covers the summary
// scan: one record that does not decode does not stop the activity that
// followed it.
func TestACorruptMiddleLineDoesNotHideLaterActivity(t *testing.T) {
	dir, store := newTestStore(t)
	at := pruneNow.Add(-time.Hour)
	path := sessionAt(t, dir, "s1", pruneNow.Add(-3*time.Hour))
	appendBytes(t, path, []byte("this is not a record\n"))
	started := runStarted("r2")
	started.TS = at.Format(time.RFC3339Nano)
	started.StartedAt = at.Format(time.RFC3339Nano)
	b, err := jsonBytes(started)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	appendBytes(t, path, append(b, '\n'))
	summaries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("the store lists %d sessions, want 1", len(summaries))
	}
	if !summaries[0].LastActivity.Equal(at) {
		t.Errorf("the session's last activity is %s, want %s", summaries[0].LastActivity, at)
	}
}

// TestTheCountRuleBreaksTiesTheSameWay covers the count rule's tie: two
// sessions with the same last activity keep the same one on every run.
func TestTheCountRuleBreaksTiesTheSameWay(t *testing.T) {
	dir, store := newTestStore(t)
	at := pruneNow.Add(-time.Hour)
	sessionAt(t, dir, "a", at)
	sessionAt(t, dir, "b", at)
	result, err := store.Prune(context.Background(), Policy{Keep: 1, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].ID != "b" {
		t.Fatalf("the prune deleted %v, want the session b", result.Deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jsonl")); err != nil {
		t.Errorf("the kept session is gone: %v", err)
	}
}

// TestAStaleTemporaryFileIsRemovedUnderItsOwnLock covers a session id
// that ends in ".tmp": the stale file's lock is the session's, not a
// truncated id's.
func TestAStaleTemporaryFileIsRemovedUnderItsOwnLock(t *testing.T) {
	dir, store := newTestStore(t)
	sessionAt(t, dir, "foo", pruneNow.Add(-time.Hour))
	sessionAt(t, dir, "foo.tmp", pruneNow.Add(-time.Hour))
	tmp := filepath.Join(dir, "foo.tmp.jsonl.tmp")
	if err := os.WriteFile(tmp, []byte("a stale construction"), 0o600); err != nil {
		t.Fatalf("writing the stale file: %v", err)
	}
	old := pruneNow.Add(-2 * StaleTmpAge)
	if err := os.Chtimes(tmp, old, old); err != nil {
		t.Fatalf("aging the stale file: %v", err)
	}
	// The other session's lock is held: the removal must not need it.
	lock, err := Acquire(LockPath(dir, "foo"))
	if err != nil {
		t.Fatalf("acquiring foo's lock: %v", err)
	}
	defer lock.Release()
	result, err := store.Prune(context.Background(), Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	found := false
	for _, removed := range result.Removed {
		if removed == tmp {
			found = true
		}
	}
	if !found {
		t.Errorf("the prune removed %v, want the stale file %s", result.Removed, tmp)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("the stale file is still there: %v", err)
	}
}

// TestResumeReportsEachWarningOnce covers the two warning lists: the
// store and the restore say the same thing, and a resumed session
// reports it once.
func TestResumeReportsEachWarningOnce(t *testing.T) {
	dir, store := newTestStore(t)
	tool := newBlockingTool()
	created := testHeader("s1", tool)
	created.Build = poo.BuildInfo{Name: "poo", Version: "old"}
	created.SettingsHash = "old-settings"
	writeSession(t, dir, "s1", created, runStarted("r1"), &poo.RunSettled{
		RecordMeta: poo.NewRecordMeta("run.settled"),
		RunID:      "r1",
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  "2026-10-20T00:00:01Z",
	})
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	other, err := NewStore(dir,
		WithBuild(poo.BuildInfo{Name: "poo", Version: "new"}),
		WithSettingsHash("new-settings"))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	tr := pootest.NewTransport(pootest.Turn{Text: "done"})
	opts := testOptions("s1", tool, tr)
	opts.Build = poo.BuildInfo{Name: "poo", Version: "new"}
	opts.SettingsHash = "new-settings"
	resumed, err := Resume(context.Background(), other, "s1", opts)
	if err != nil {
		t.Fatalf("resuming: %v", err)
	}
	defer resumed.Session.Close()
	if len(resumed.Loaded.Warnings) != 0 {
		t.Errorf("the load holds %v, want the warnings merged into the restore", resumed.Loaded.Warnings)
	}
	seen := map[string]bool{}
	for _, warning := range resumed.Restored.Warnings {
		if seen[warning] {
			t.Errorf("the warning %q is reported twice", warning)
		}
		seen[warning] = true
	}
	if len(resumed.Restored.Warnings) != 2 {
		t.Errorf("the resume reports %v, want two warnings", resumed.Restored.Warnings)
	}
}

// jsonBytes encodes a record.
func jsonBytes(rec poo.Record) ([]byte, error) {
	return json.Marshal(rec)
}
