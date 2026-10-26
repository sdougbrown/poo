package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// pruneNow is the clock the retention tests use.
var pruneNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// sessionAt returns a session whose last record was written at the given
// time.
func sessionAt(t *testing.T, dir, id string, at time.Time) string {
	t.Helper()
	header := createdRecord(id)
	header.CreatedAt = at.Format(time.RFC3339Nano)
	started := runStarted("r1")
	started.Seq = 2
	started.TS = at.Format(time.RFC3339Nano)
	started.StartedAt = at.Format(time.RFC3339Nano)
	// The run settled, so a load of the session recovers nothing and
	// the session's last activity stays where it was.
	settled := &poo.RunSettled{
		RecordMeta: poo.RecordMeta{Record: "run.settled", Seq: 3, TS: at.Format(time.RFC3339Nano)},
		RunID:      "r1",
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  at.Format(time.RFC3339Nano),
	}
	return writeSession(t, dir, id, header, started, settled)
}

// TestPruneWithNoPolicyFails covers the opt-in rule: a prune with no
// policy deletes nothing and fails.
func TestPruneWithNoPolicyFails(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))
	_, err := store.Prune(ctx, Policy{})
	if !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("the prune returned %v, want no policy", err)
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeNoPolicy {
		t.Errorf("the failure's code is %q, want %q", code, CodeNoPolicy)
	}
	if _, err := os.Stat(filepath.Join(dir, "s1.jsonl")); err != nil {
		t.Errorf("the session is gone: %v", err)
	}
}

// TestPruneByAge covers the age rule: a session whose last activity is
// older than the limit is deleted.
func TestPruneByAge(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	old := sessionAt(t, dir, "old", pruneNow.Add(-48*time.Hour))
	fresh := sessionAt(t, dir, "fresh", pruneNow.Add(-time.Hour))

	result, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].ID != "old" {
		t.Errorf("the prune deleted %v, want the old session", result.Deleted)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old session is still there: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the fresh session is gone: %v", err)
	}
}

// TestTheAgeClockIsTheLastActivity covers the age rule's clock: a
// session that was created long ago and used yesterday is not old.
func TestTheAgeClockIsTheLastActivity(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	header := createdRecord("s1")
	header.CreatedAt = "2020-01-01T00:00:00Z"
	started := runStarted("r1")
	started.Seq = 2
	started.TS = pruneNow.Add(-time.Hour).Format(time.RFC3339Nano)
	path := writeSession(t, dir, "s1", header, started)

	result, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("the prune deleted %v, want nothing", result.Deleted)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the session is gone: %v", err)
	}
}

// TestPruneByCount covers the count rule: the newest sessions are kept.
func TestPruneByCount(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	oldest := sessionAt(t, dir, "oldest", pruneNow.Add(-3*time.Hour))
	middle := sessionAt(t, dir, "middle", pruneNow.Add(-2*time.Hour))
	newest := sessionAt(t, dir, "newest", pruneNow.Add(-time.Hour))

	result, err := store.Prune(ctx, Policy{Keep: 1, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 2 {
		t.Errorf("the prune deleted %v, want two sessions", result.Deleted)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Errorf("the newest session is gone: %v", err)
	}
	if _, err := os.Stat(middle); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the middle session is still there: %v", err)
	}
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the oldest session is still there: %v", err)
	}
}

// TestALockedSessionIsSkipped covers the lock-safe deletion: a session
// another process holds is not deleted.
func TestALockedSessionIsSkipped(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))
	authority, err := Acquire(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("taking the authority: %v", err)
	}
	defer authority.Release()

	result, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Errorf("the prune deleted %v, want nothing", result.Deleted)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].ID != "s1" {
		t.Errorf("the prune skipped %v, want the locked session", result.Skipped)
	}
	if _, err := os.Stat(filepath.Join(dir, "s1.jsonl")); err != nil {
		t.Errorf("the locked session is gone: %v", err)
	}
}

// TestTheKeepRuleCountsLockedSessions covers the count rule: a locked
// session is one of the sessions that are kept, so it is counted.
func TestTheKeepRuleCountsLockedSessions(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	// The locked session is the newest one, so the count of two keeps
	// it and the next one, and deletes the third.
	sessionAt(t, dir, "locked", pruneNow.Add(-time.Hour))
	sessionAt(t, dir, "middle", pruneNow.Add(-2*time.Hour))
	oldest := sessionAt(t, dir, "oldest", pruneNow.Add(-3*time.Hour))

	authority, err := Acquire(LockPath(dir, "locked"))
	if err != nil {
		t.Fatalf("taking the authority: %v", err)
	}
	defer authority.Release()

	result, err := store.Prune(ctx, Policy{Keep: 2, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].ID != "oldest" {
		t.Errorf("the prune deleted %v, want the oldest session", result.Deleted)
	}
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the oldest session is still there: %v", err)
	}
}

// TestTheLockFilesSurviveAPrune covers the rule that a lock file is
// never deleted: deleting it would break the mutual exclusion.
func TestTheLockFilesSurviveAPrune(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))
	lock := LockPath(dir, "s1")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("creating the lock file: %v", err)
	}

	if _, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow}); err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the lock file is gone: %v", err)
	}
}

// TestADryRunDeletesNothing covers the dry run: it lists the same set
// and deletes nothing.
func TestADryRunDeletesNothing(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	path := sessionAt(t, dir, "s1", pruneNow.Add(-48*time.Hour))

	dry, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, DryRun: true, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(dry.Deleted) != 1 || dry.Deleted[0].ID != "s1" {
		t.Errorf("the dry run lists %v, want the old session", dry.Deleted)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the dry run deleted the session: %v", err)
	}

	// The real prune deletes the same set.
	real, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(real.Deleted) != len(dry.Deleted) {
		t.Errorf("the prune deleted %v, want the dry run's set %v", real.Deleted, dry.Deleted)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the session is still there: %v", err)
	}
}

// TestAStaleTemporaryFileIsRemoved covers the cleanup of a construction
// that was cut short.
func TestAStaleTemporaryFileIsRemoved(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	stale := filepath.Join(dir, "s1.jsonl.tmp")
	if err := os.WriteFile(stale, []byte("half a record"), 0o600); err != nil {
		t.Fatalf("creating the temporary file: %v", err)
	}
	old := pruneNow.Add(-2 * StaleTmpAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("ageing the temporary file: %v", err)
	}
	// A fresh temporary file is left alone: its construction may be
	// running.
	fresh := filepath.Join(dir, "s2.jsonl.tmp")
	if err := os.WriteFile(fresh, []byte("half a record"), 0o600); err != nil {
		t.Fatalf("creating the temporary file: %v", err)
	}

	result, err := store.Prune(ctx, Policy{MaxAge: 24 * time.Hour, Now: pruneNow})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != stale {
		t.Errorf("the prune removed %v, want the stale temporary file", result.Removed)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stale temporary file is still there: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the fresh temporary file is gone: %v", err)
	}
}

// TestTheListShowsTheSessions covers the listing: a summary says what a
// reader of the session directory wants to know.
func TestTheListShowsTheSessions(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	sessionAt(t, dir, "s1", pruneNow.Add(-time.Hour))
	sessionAt(t, dir, "s2", pruneNow.Add(-2*time.Hour))

	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("the store lists %d sessions, want 2", len(summaries))
	}
	first := summaries[0]
	if first.ID != "s1" {
		t.Errorf("the first session is %q, want s1", first.ID)
	}
	if first.Mode != ModeLogged {
		t.Errorf("the session's mode is %q, want logged", first.Mode)
	}
	if first.Runs != 1 {
		t.Errorf("the session had %d runs, want 1", first.Runs)
	}
	if first.Size == 0 {
		t.Error("the session's size is zero, want the file's size")
	}
	if first.Locked {
		t.Error("the session is locked, want it free")
	}
	if !first.LastActivity.Equal(pruneNow.Add(-time.Hour)) {
		t.Errorf("the session's last activity is %s, want %s", first.LastActivity, pruneNow.Add(-time.Hour))
	}
}
