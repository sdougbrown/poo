package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// testClock is a clock a test moves.
//
// The clock is shared between the test and the lease's heartbeat, so it
// is guarded: the heartbeat reads it while the test advances it.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the clock's time.
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward.
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// eventually polls cond until it holds, on a deadline, so a heartbeat
// that never lands fails the test instead of hanging it.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: it did not happen within 5s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// newTestStore returns a store over a temporary database, and the
// database's path.
//
// The store is closed when the test ends.
func newTestStore(t *testing.T) (string, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := New(path)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return path, store
}

// newTestStoreAt returns a store over the database at path.
//
// The store is closed when the test ends.
func newTestStoreAt(t *testing.T, path string) *Store {
	t.Helper()
	store, err := New(path)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// createdRecord returns the header of a session.
func createdRecord(id string) *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:       poo.FormatVersion,
			SessionID:    id,
			Mode:         string(session.ModeLogged),
			CreatedAt:    "2026-10-20T00:00:00Z",
			WorkingDir:   "/tmp",
			Tools:        []poo.ToolInfo{{Name: "bash", Class: poo.ClassSideEffecting}, {Name: "read", Class: poo.ClassReadOnly}},
			Build:        poo.BuildInfo{Name: "poo", Version: "0.1.0"},
			Model:        poo.SessionModel{ModelID: "m"},
			ContextLimit: 32768,
			ToolsMode:    "native",
		},
	}
}

// runStarted returns a run's first record.
func runStarted(runID string) *poo.RunStartedRecord {
	return &poo.RunStartedRecord{
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

// runSettled returns a run's settle record.
func runSettled(runID string) *poo.RunSettledRecord {
	return &poo.RunSettledRecord{
		RecordMeta: poo.NewRecordMeta("run.settled"),
		RunID:      runID,
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  "2026-10-20T00:00:02Z",
	}
}

// recordNames returns the names of some records, in order.
func recordNames(recs []poo.Record) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Meta().Record)
	}
	return out
}

// TestASessionRoundTripsThroughTheStore covers the contract: a session
// is created, its records are appended, and a new store over the same
// database reads it back.
func TestASessionRoundTripsThroughTheStore(t *testing.T) {
	path, store := newTestStore(t)
	ctx := context.Background()

	id, err := store.Create(ctx, createdRecord("s1"))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if id != "s1" {
		t.Errorf("the session's id is %q, want s1", id)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolCall("r1", 1, "r1", "bash")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolResult("r1", 1, "r1", "bash", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, runSettled("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// A new store over the same database reads the session back.
	other := newTestStoreAt(t, path)
	loaded, err := other.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	names := recordNames(loaded.Records)
	want := []string{"session.created", "run.started", "tool.call", "tool.result", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the session holds %v, want %v", names, want)
	}
	if loaded.Metadata.SessionID != "s1" {
		t.Errorf("the session's id is %q, want s1", loaded.Metadata.SessionID)
	}
	if loaded.Metadata.Mode != session.ModeLogged {
		t.Errorf("the session's mode is %q, want logged", loaded.Metadata.Mode)
	}
	if loaded.Metadata.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want 32768", loaded.Metadata.ContextLimit)
	}
	if loaded.RecoveredTail {
		t.Error("the load reports a recovered tail, want none")
	}
}

// TestASessionsMetadataIsReadWithoutOpeningIt covers the header read: a
// store returns the header a session was created with while the session's
// owner holds its lease. The read takes no authority, so a second store
// over the same database answers it without taking, releasing, or fencing
// the owner's lease.
func TestASessionsMetadataIsReadWithoutOpeningIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	clk := &testClock{t: time.Now()}
	// The owner's lease outlives the test, so only the probe can end it.
	store, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 3 * time.Hour, OwnerID: "owner", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}

	reader, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 3 * time.Hour, OwnerID: "probe", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the reading store: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	meta, err := reader.Metadata(ctx, "s1")
	if err != nil {
		t.Fatalf("reading the session's metadata: %v", err)
	}
	if meta.SessionID != "s1" {
		t.Errorf("the session's id is %q, want s1", meta.SessionID)
	}
	if meta.Mode != session.ModeLogged {
		t.Errorf("the session's mode is %q, want logged", meta.Mode)
	}
	if meta.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want 32768", meta.ContextLimit)
	}
	if len(meta.Tools) != 2 || meta.Tools[0].Name != "bash" || meta.Tools[1].Name != "read" {
		t.Errorf("the session's tools are %v, want bash and read", meta.Tools)
	}

	// The probe took no authority: the live lease still refuses a second
	// load, and the owner can still write.
	if _, err := reader.Load(ctx, "s1"); !errors.Is(err, session.ErrSessionLocked) {
		t.Errorf("the load after the probe returned %v, want the session locked", err)
	}
	if err := store.open.owner.CheckHeld(); err != nil {
		t.Errorf("the owner's lease after the probe is %v, want it held", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Errorf("the owner's append after the probe returned %v, want it to hold", err)
	}
}

// TestMetadataReportsAMissingOrInvalidSession covers the header read's
// failures: a session the store does not hold is the store's not-found
// failure, and an id that would name outside the store is refused.
func TestMetadataReportsAMissingOrInvalidSession(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Metadata(ctx, "missing"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("the missing session's metadata read is %v, want %v", err, session.ErrSessionNotFound)
	}
	if _, err := store.Metadata(ctx, "../outside"); !errors.Is(err, session.ErrInvalidSessionID) {
		t.Errorf("the invalid session's metadata read is %v, want %v", err, session.ErrInvalidSessionID)
	}
}

// TestARunBoundaryAndASideEffectingResultAreSynced covers the
// durability rule: a run's boundaries and a side-effecting tool's
// result are on disk before the write returns.
func TestARunBoundaryAndASideEffectingResultAreSynced(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	before := store.open.syncs

	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if store.open.syncs != before+1 {
		t.Errorf("the run's start was synced %d times, want one", store.open.syncs-before)
	}

	// A read-only tool's result is not synced on its own.
	if err := store.Append(ctx, toolResult("r1", 1, "r1", "read", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if store.open.syncs != before+1 {
		t.Errorf("the read-only result was synced, want no sync")
	}

	// A side-effecting tool's result is.
	if err := store.Append(ctx, toolResult("r1", 1, "r2", "bash", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if store.open.syncs != before+2 {
		t.Errorf("the side-effecting result was synced %d times, want one more", store.open.syncs-before)
	}
}

// TestAToolStartAndAResumptionAreSynced covers the durability rule's
// additions: a side-effecting call's start and a run's resumption are on
// disk before the write returns.
func TestAToolStartAndAResumptionAreSynced(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	before := store.open.syncs

	if err := store.Append(ctx, &poo.ToolStartedRecord{
		RecordMeta: poo.NewRecordMeta("tool.started"),
		RunID:      "r1",
		Turn:       1,
		Handle:     "r1",
		CallID:     "c1",
		Tool:       "bash",
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if store.open.syncs != before+1 {
		t.Errorf("the call's start was synced %d times, want one", store.open.syncs-before)
	}

	if err := store.Append(ctx, &poo.RunResumedRecord{
		RecordMeta:        poo.NewRecordMeta("run.resumed"),
		RunID:             "r1",
		ResumptionAttempt: 1,
		FromPhase:         poo.StateExecuting,
		Turn:              1,
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if store.open.syncs != before+2 {
		t.Errorf("the resumption was synced %d times, want two", store.open.syncs-before)
	}
}

// TestAPendingRunIsReported covers the pending read: a run that started
// and never settled is reported as pending.
func TestAPendingRunIsReported(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolCall("r1", 1, "r1", "bash")); err != nil {
		t.Fatalf("appending: %v", err)
	}

	pend, err := store.Pending(ctx, "s1")
	if err != nil {
		t.Fatalf("reading the pending operations: %v", err)
	}
	if len(pend.Runs) != 1 {
		t.Fatalf("the session holds %d pending runs, want 1", len(pend.Runs))
	}
	if pend.Runs[0].RunID != "r1" {
		t.Errorf("the pending run is %q, want r1", pend.Runs[0].RunID)
	}
	if len(pend.Runs[0].Calls) != 1 {
		t.Errorf("the pending run holds %d calls, want 1", len(pend.Runs[0].Calls))
	}
}

// TestAListingNamesTheSession covers the listing: a session's summary
// carries its id, its working directory, and its last activity.
func TestAListingNamesTheSession(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("the store lists %d sessions, want 1", len(summaries))
	}
	if summaries[0].ID != "s1" {
		t.Errorf("the listing's id is %q, want s1", summaries[0].ID)
	}
	if summaries[0].WorkingDir != "/tmp" {
		t.Errorf("the listing's working directory is %q, want /tmp", summaries[0].WorkingDir)
	}
	if summaries[0].Runs != 1 {
		t.Errorf("the listing holds %d runs, want 1", summaries[0].Runs)
	}
}

// TestAPruneDeletesAMarkedSession covers the prune: a session that is
// old enough is deleted, and a prune with no policy fails.
func TestAPruneDeletesAMarkedSession(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// A prune with no policy fails.
	if _, err := store.Prune(ctx, session.Policy{}); !errors.Is(err, session.ErrNoPolicy) {
		t.Errorf("the prune returned %v, want no policy", err)
	}
	// A session that is old enough is deleted: the prune's now is far
	// past the session's last record, and the age rule marks it.
	result, err := store.Prune(ctx, session.Policy{MaxAge: time.Hour, Now: time.Now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].ID != "s1" {
		t.Errorf("the prune deleted %+v, want the one session s1", result.Deleted)
	}
}

// TestReadRecordsReturnsThemInOrder covers the read side: the records
// are read back in the order they were written.
func TestReadRecordsReturnsThemInOrder(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolCall("r1", 1, "r1", "bash")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	recs, err := store.ReadRecords(ctx)
	if err != nil {
		t.Fatalf("reading the records: %v", err)
	}
	names := recordNames(recs)
	want := []string{"session.created", "run.started", "tool.call"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the session holds %v, want %v", names, want)
	}
}

// TestAnAppendAfterTheStoreCloseIsRefused covers the store's backstop:
// a write after the close is refused.
func TestAnAppendAfterTheStoreCloseIsRefused(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	if err := store.Append(ctx, runStarted("r2")); err == nil {
		t.Fatal("the append after the close was accepted")
	}
}

// TestADurableSessionCrashContinuesViaThePlanner covers the durable
// recovery: a session that was left unsettled is planned for a
// continuation, which the load returns as the session's plan.
func TestADurableSessionCrashContinuesViaThePlanner(t *testing.T) {
	path, store := newTestStore(t)
	ctx := context.Background()

	header := createdRecord("s1")
	header.Mode = string(session.ModeDurable)
	if _, err := store.Create(ctx, header); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The run starts and is left unsettled: the process was killed
	// mid-run.
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// A new store over the same database loads the session and plans
	// the continuation.
	other := newTestStoreAt(t, path)
	loaded, err := other.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if loaded.Metadata.Mode != session.ModeDurable {
		t.Errorf("the loaded mode is %q, want %q", loaded.Metadata.Mode, session.ModeDurable)
	}
	if loaded.Continuation == nil {
		t.Fatal("the load planned no continuation, want the run to continue")
	}
	if loaded.Continuation.RunID != "r1" {
		t.Errorf("the continuation is for %q, want r1", loaded.Continuation.RunID)
	}
}

// TestALiveLeaseBlocksASecondOpen covers the acquire's refusal: a
// session another process holds is refused with ErrSessionLocked.
func TestALiveLeaseBlocksASecondOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	clk := &testClock{t: time.Now()}
	store, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}

	other, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the other store: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	_, err = other.Load(context.Background(), "s1")
	if !errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("the second load returned %v, want the session locked", err)
	}
	code, ok := session.CodeOf(err)
	if !ok || code != session.CodeSessionLocked {
		t.Errorf("the failure's code is %q, want %q", code, session.CodeSessionLocked)
	}
}

// TestAnExpiredLeaseIsTakenOverWithAWarning covers the takeover: a
// lease that has expired is taken over, and the takeover carries a
// warning naming the previous owner and its expiry.
func TestAnExpiredLeaseIsTakenOverWithAWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	clk := &testClock{t: time.Now()}
	store, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The first owner dies: the heartbeat stops, but the lease
	// remains.
	store.open.owner.markLost()
	// Let the lease expire.
	clk.Advance(60 * time.Millisecond)

	// A second owner takes over.
	other, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the other store: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	if _, err := other.Load(context.Background(), "s1"); err != nil {
		t.Fatalf("taking over the session: %v", err)
	}
	if other.open.owner.Warning() == "" {
		t.Error("the takeover carries no warning, want one naming the previous owner")
	}
	if !strings.Contains(other.open.owner.Warning(), "owner1") {
		t.Errorf("the warning is %q, want it to name owner1", other.open.owner.Warning())
	}
}

// TestAFencedOwnersAppendFailsWithLeaseLost covers the fencing: a write
// through a store whose lease was taken over fails with ErrLeaseLost,
// and it writes nothing.
func TestAFencedOwnersAppendFailsWithLeaseLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	clk := &testClock{t: time.Now()}
	store, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: 500 * time.Millisecond, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The first owner is slow: the lease expires, but its heartbeat is
	// still running.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over.
	other, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the other store: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	if _, err := other.Load(context.Background(), "s1"); err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	// Wait for the first owner's heartbeat to observe the takeover.
	eventually(t, "the first owner's lease lost", store.open.owner.isLost)
	// The fenced former owner's writes fail, and they write nothing.
	if err := store.Append(context.Background(), runStarted("r1")); !errors.Is(err, session.ErrLeaseLost) {
		t.Errorf("the fenced append returned %v, want the lease lost", err)
	}
	if err := store.Flush(context.Background()); !errors.Is(err, session.ErrLeaseLost) {
		t.Errorf("the fenced flush returned %v, want the lease lost", err)
	}
	// The fenced write wrote nothing: the session holds only the header.
	recs, err := store.ReadRecords(context.Background())
	if err != nil {
		t.Fatalf("reading the records: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("the session holds %d records, want only the header", len(recs))
	}
}

// TestAForeignTokenReleaseNeverDeletesAnotherAcquisitionsLease covers
// the release's promise: a release of a fenced former owner never
// deletes a new owner's lease.
func TestAForeignTokenReleaseNeverDeletesAnotherAcquisitionsLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	clk := &testClock{t: time.Now()}
	store, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// Let the lease expire.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over.
	other, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the other store: %v", err)
	}
	t.Cleanup(func() { other.Close() })
	if _, err := other.Load(context.Background(), "s1"); err != nil {
		t.Fatalf("taking over the session: %v", err)
	}
	// The fenced former owner releases late.
	if err := store.Close(); err != nil {
		t.Fatalf("the late release returned %v, want nil", err)
	}
	// The new owner's lease is intact: it can still write.
	if err := other.Append(context.Background(), runStarted("r1")); err != nil {
		t.Errorf("the new owner's append returned %v, want it to hold", err)
	}
}

// TestAPruneSkipsALiveLeasedSessionAndTakesAnExpiredOne covers the
// prune's lease liveness: a live-leased session is skipped, and an
// expired-leased session is deleted.
func TestAPruneSkipsALiveLeasedSessionAndTakesAnExpiredOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	pruneNow := time.Now()
	// The sessions are old through the store's own clock: the store
	// stamps a session's records itself, so an old session is one the
	// store's clock says was created two hours ago.
	aged := pruneNow.Add(-2 * time.Hour)
	clk := &testClock{t: aged}

	// The live-leased session: a lease whose TTL still has an hour to
	// run, so it stays live at the prune's now. The store's own clock
	// ages it too, because the store stamps a session's records itself.
	live, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 3 * time.Hour, OwnerID: "live", Now: clk.Now}), WithClock(clk.Now))
	if err != nil {
		t.Fatalf("opening the live store: %v", err)
	}
	t.Cleanup(func() { live.Close() })
	if _, err := live.Create(context.Background(), createdRecord("live")); err != nil {
		t.Fatalf("creating the live session: %v", err)
	}

	// The expired-leased session: a short TTL on the same aged clock,
	// so the lease expired long before the prune's now.
	expClk := &testClock{t: aged}
	exp, err := New(path, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "expired", Now: expClk.Now}), WithClock(expClk.Now))
	if err != nil {
		t.Fatalf("opening the expired store: %v", err)
	}
	t.Cleanup(func() { exp.Close() })
	if _, err := exp.Create(context.Background(), createdRecord("expired")); err != nil {
		t.Fatalf("creating the expired session: %v", err)
	}
	// The expired owner dies: the heartbeat stops, but the lease
	// remains, and it is expired (pruneNow is past its expiry).
	exp.open.owner.markLost()

	// Prune: the live-leased session is skipped, and the
	// expired-leased session is deleted.
	pruneStore, err := New(path, WithClock(func() time.Time { return pruneNow }))
	if err != nil {
		t.Fatalf("opening the prune store: %v", err)
	}
	t.Cleanup(func() { pruneStore.Close() })
	result, err := pruneStore.Prune(context.Background(), session.Policy{MaxAge: time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	deleted := false
	for _, sum := range result.Deleted {
		if sum.ID == "expired" {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("the expired session was not deleted, want it deleted")
	}
	skipped := false
	for _, sum := range result.Skipped {
		if sum.ID == "live" {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("the live session was not skipped, want it skipped")
	}
}
