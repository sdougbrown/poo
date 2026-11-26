package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

// TestLeaseAcquireAndHeartbeatRenew covers the acquire and the
// heartbeat's renewal: a lease that is acquired is on disk, and a
// heartbeat renews it past its expiry.
func TestLeaseAcquireAndHeartbeatRenew(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	defer a.Close()

	state, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease: %v", err)
	}
	if state.OwnerID != "owner1" {
		t.Errorf("the lease's owner is %q, want owner1", state.OwnerID)
	}
	firstExpires := state.ExpiresAt

	// Advance the clock past the lease's expiry, so a renewal is
	// needed, and wait for the heartbeat to renew it.
	clk.Advance(60 * time.Millisecond)
	eventually(t, "the heartbeat's renewal", func() bool {
		st, err := readLease(LeasePath(dir, "s1"))
		return err == nil && st.ExpiresAt.After(firstExpires)
	})

	state, err = readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease after the renewal: %v", err)
	}
	if !state.ExpiresAt.After(firstExpires) {
		t.Errorf("the lease's expiry is %v, want it renewed past %v", state.ExpiresAt, firstExpires)
	}
	if state.OwnerID != "owner1" {
		t.Errorf("the lease's owner is %q, want owner1", state.OwnerID)
	}
}

// TestLeaseACancelledCallerContextKeepsTheHeartbeatBeating covers the
// heartbeat's lifetime: a caller's context that is cancelled after the
// acquire does not stop the heartbeat; the heartbeat keeps renewing
// until the authority is released.
func TestLeaseACancelledCallerContextKeepsTheHeartbeatBeating(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	// The caller's context dies immediately after the acquire, the
	// way a request-scoped context does.
	cancel()
	if ctx.Err() == nil {
		t.Fatal("the caller's context is not cancelled")
	}
	state, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease: %v", err)
	}
	firstExpires := state.ExpiresAt
	// Step the clock well past the lease's expiry, so a renewal is
	// needed, and wait for the heartbeat to renew it.
	clk.Advance(60 * time.Millisecond)
	eventually(t, "the heartbeat's renewal", func() bool {
		st, err := readLease(LeasePath(dir, "s1"))
		return err == nil && st.ExpiresAt.After(firstExpires)
	})
	state, err = readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease after the renewal: %v", err)
	}
	if !state.ExpiresAt.After(firstExpires) {
		t.Errorf("the lease's expiry is %v, want it renewed past %v", state.ExpiresAt, firstExpires)
	}
	// The lease is still live: a second owner is refused.
	_, err = AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now})
	if !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("the second acquire returned %v, want the session locked", err)
	}
	lastHeartbeat := state.HeartbeatAt
	// The release stops the heartbeat.
	if err := a.Release(); err != nil {
		t.Fatalf("releasing the lease: %v", err)
	}
	select {
	case <-a.stop:
	default:
		t.Error("the release did not stop the heartbeat")
	}
	// Step the clock and wait a few intervals: a tick that would
	// follow does not renew.
	clk.Advance(60 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	after, err := readLease(LeasePath(dir, "s1"))
	switch {
	case err == nil:
		if !after.HeartbeatAt.Equal(lastHeartbeat) {
			t.Errorf("the heartbeat advanced after the release: %v, want %v", after.HeartbeatAt, lastHeartbeat)
		}
	case !errors.Is(err, fs.ErrNotExist):
		t.Fatalf("reading the lease after the release: %v", err)
	}
}

// TestLeaseALiveLeaseBlocksASecondAcquire covers the acquire's refusal:
// a session another process holds is refused with ErrSessionLocked.
func TestLeaseALiveLeaseBlocksASecondAcquire(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	defer a.Close()

	_, err = AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now})
	if !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("the second acquire returned %v, want the session locked", err)
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeSessionLocked {
		t.Errorf("the failure's code is %q, want %q", code, CodeSessionLocked)
	}
}

// TestLeaseAnExpiredLeaseAllowsATakeover covers the takeover: a lease
// that has expired is taken over, and the takeover carries a warning
// naming the previous owner and its expiry.
func TestLeaseAnExpiredLeaseAllowsATakeover(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	// The first owner dies: the heartbeat stops, but the lease
	// remains.
	a.markLost()
	// Let the lease expire.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over.
	b, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now})
	if err != nil {
		t.Fatalf("taking over the lease: %v", err)
	}
	defer b.Close()
	if b.Warning() == "" {
		t.Error("the takeover carries no warning, want one naming the previous owner")
	}
	if !strings.Contains(b.Warning(), "owner1") {
		t.Errorf("the warning is %q, want it to name owner1", b.Warning())
	}
	state, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease: %v", err)
	}
	if state.OwnerID != "owner2" {
		t.Errorf("the lease's owner is %q, want owner2", state.OwnerID)
	}
}

// TestLeaseALateReleaseDoesNotDeleteTheNewOwnersLease covers the
// release's promise: a release of a fenced former owner never deletes a
// new owner's lease.
func TestLeaseALateReleaseDoesNotDeleteTheNewOwnersLease(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	// Let the lease expire.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over.
	b, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now})
	if err != nil {
		t.Fatalf("taking over the lease: %v", err)
	}
	defer b.Close()
	// The fenced former owner releases late.
	if err := a.Close(); err != nil {
		t.Fatalf("the late release returned %v, want nil", err)
	}
	// The new owner's lease is intact.
	state, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease: %v", err)
	}
	if state.OwnerID != "owner2" {
		t.Errorf("the lease's owner is %q, want owner2", state.OwnerID)
	}
}

// TestLeaseFencedWritesFailWithLeaseLost covers the fencing: a write
// through a store whose lease was taken over fails with ErrLeaseLost,
// and it writes nothing.
func TestLeaseFencedWritesFailWithLeaseLost(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	store, err := NewStore(dir, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: 500 * time.Millisecond, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	// The first owner is slow: the lease expires, but its heartbeat is
	// still running.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over.
	other, err := NewStore(dir, WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner2", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the other store: %v", err)
	}
	defer other.Close()
	if _, err := other.Load(context.Background(), "s1"); err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	// Wait for the first owner's heartbeat to observe the takeover.
	first, ok := store.open.owner.(*LeaseAuthority)
	if !ok {
		t.Fatalf("the store's owner is %T, want a lease authority", store.open.owner)
	}
	eventually(t, "the first owner's lease lost", first.isLost)
	// The fenced former owner's writes fail, and they write nothing.
	if err := store.Append(context.Background(), runStarted("r1")); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("the fenced append returned %v, want the lease lost", err)
	}
	if err := store.Flush(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("the fenced flush returned %v, want the lease lost", err)
	}
	// The fenced write wrote nothing: the log holds only the header.
	recs := logRecords(t, filepath.Join(dir, "s1.jsonl"))
	if len(recs) != 1 {
		t.Errorf("the log holds %d records, want only the header", len(recs))
	}
}

// TestLeaseSummarizeReportsALiveLeasedSessionAsLocked covers the
// summary: a live-leased session reports Locked: true, the way a
// locked one does.
func TestLeaseSummarizeReportsALiveLeasedSessionAsLocked(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	store, err := NewStore(dir, WithClock(clk.Now), WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("the list holds %d sessions, want 1", len(list))
	}
	if !list[0].Locked {
		t.Error("the live-leased session is not locked, want it locked")
	}
}

// TestLeasePruneSkipsALiveLeasedSessionAndTakesAnExpiredOne covers the
// prune: a live-leased session is skipped, and an expired-leased
// session is deleted.
func TestLeasePruneSkipsALiveLeasedSessionAndTakesAnExpiredOne(t *testing.T) {
	dir := t.TempDir()
	// The sessions' last activity is 2 hours before pruneNow, so the
	// age rule marks them.
	sessionAt(t, dir, "live", pruneNow.Add(-2*time.Hour))
	sessionAt(t, dir, "expired", pruneNow.Add(-2*time.Hour))

	// The live-leased session: a long TTL, so its lease stays live.
	liveClk := &testClock{t: pruneNow}
	if _, err := AcquireLease(dir, "live", LeaseOptions{TTL: 100 * time.Millisecond, OwnerID: "live", Now: liveClk.Now}); err != nil {
		t.Fatalf("acquiring the live lease: %v", err)
	}
	// The expired-leased session: a short TTL, and the owner dies.
	expClk := &testClock{t: pruneNow.Add(-200 * time.Millisecond)}
	exp, err := AcquireLease(dir, "expired", LeaseOptions{TTL: 50 * time.Millisecond, OwnerID: "expired", Now: expClk.Now})
	if err != nil {
		t.Fatalf("acquiring the expired lease: %v", err)
	}
	// The expired owner dies: the heartbeat stops, but the lease
	// remains, and it is expired (pruneNow is past its expiry).
	exp.markLost()

	// Prune: the live-leased session is skipped, and the
	// expired-leased session is deleted.
	pruneStore, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the prune store: %v", err)
	}
	defer pruneStore.Close()
	result, err := pruneStore.Prune(context.Background(), Policy{MaxAge: time.Hour, Now: pruneNow})
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

// TestLeaseATakeoverOfTheSameOwnerNameIsFenced covers the token: a
// takeover under the same owner name is fenced out of the taken lease.
// Two acquisitions in one process share the default name, so the name
// alone cannot tell a stale authority from its takeover.
func TestLeaseATakeoverOfTheSameOwnerNameIsFenced(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	// The stale authority's heartbeat is slow, so its one tick is
	// driven by hand, after the takeover.
	stale, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	defer stale.Close()
	before, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease: %v", err)
	}
	// The first owner stalls: the lease expires.
	clk.Advance(60 * time.Millisecond)
	// A second owner takes over, under the same default name.
	fresh, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, Now: clk.Now})
	if err != nil {
		t.Fatalf("taking over the lease: %v", err)
	}
	defer fresh.Close()
	taken, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the takeover's lease: %v", err)
	}
	if taken.OwnerID != before.OwnerID {
		t.Fatalf("the takeover's owner is %q, want the same name %q: the name is what the token tells apart",
			taken.OwnerID, before.OwnerID)
	}
	if taken.Token == before.Token {
		t.Fatalf("the takeover reused the token %q, want a fresh one", taken.Token)
	}
	// The stale authority's heartbeat beats once, finds the takeover,
	// and is fenced.
	stale.heartbeatTick()
	if !stale.isLost() {
		t.Fatal("the stale authority is not fenced, want it lost")
	}
	if err := stale.CheckHeld(); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("the stale authority's check returned %v, want the lease lost", err)
	}
	// A stale release never deletes the takeover's lease.
	if err := stale.Release(); err != nil {
		t.Fatalf("the stale release returned %v, want nil", err)
	}
	after, err := readLease(LeasePath(dir, "s1"))
	if err != nil {
		t.Fatalf("reading the lease after the stale release: %v", err)
	}
	if after.Token != taken.Token {
		t.Errorf("the lease's token is %q, want the takeover's %q", after.Token, taken.Token)
	}
}

// TestLeaseAReleaseThatFailsStaysRetryable covers the release's
// contract: a release whose cleanup fails returns the failure and
// leaves the lease on disk, and a later release retries the cleanup
// instead of no-opping.
func TestLeaseAReleaseThatFailsStaysRetryable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root process removes the lease from a read-only directory")
	}
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	// A heartbeat that does not tick during the test: a tick that
	// could not renew the lease would lose the authority, and a lost
	// authority has no cleanup left to retry.
	a, err := AcquireLease(dir, "s1", LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, OwnerID: "owner1", Now: clk.Now})
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	defer a.Close()
	lockPath := LockPath(dir, "s1")
	// A failing release must not leave the test's directory unusable.
	t.Cleanup(func() {
		os.Chmod(dir, 0o700)
		os.Chmod(lockPath, lockFileMode)
	})

	// The removal fails: the directory refuses the write.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("making the directory read-only: %v", err)
	}
	err = a.Release()
	if err == nil {
		t.Fatal("the release succeeded, want the removal to fail")
	}
	if code, ok := CodeOf(err); !ok || code != CodeStoreError {
		t.Errorf("the failure's code is %q, want %q", code, CodeStoreError)
	}
	// The heartbeat stopped anyway, so a lease whose cleanup never
	// succeeds expires on its own.
	select {
	case <-a.stop:
	default:
		t.Error("the failed release did not stop the heartbeat")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restoring the directory: %v", err)
	}

	// The flock fails too: a lock file the release cannot open fails
	// it the same way, and leaves it retryable.
	if err := os.Chmod(lockPath, 0o000); err != nil {
		t.Fatalf("making the lock file unopenable: %v", err)
	}
	err = a.Release()
	if err == nil {
		t.Fatal("the release succeeded, want the lock to fail")
	}
	if code, ok := CodeOf(err); !ok || code != CodeStoreError {
		t.Errorf("the failure's code is %q, want %q", code, CodeStoreError)
	}
	if err := os.Chmod(lockPath, lockFileMode); err != nil {
		t.Fatalf("restoring the lock file: %v", err)
	}

	// The retry cleans the lease up.
	if err := a.Release(); err != nil {
		t.Fatalf("the retried release returned %v, want nil", err)
	}
	if _, err := readLease(LeasePath(dir, "s1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the lease still exists after the retried release: %v", err)
	}
}

// TestLeaseSummarizeReportsAnExpiredLeasedSessionAsUnlocked covers the
// summary's negative case: the lease is what makes a leased session
// locked, so a session whose lease expired reports Locked: false even
// while its lease file is still on disk.
func TestLeaseSummarizeReportsAnExpiredLeasedSessionAsUnlocked(t *testing.T) {
	dir := t.TempDir()
	clk := &testClock{t: time.Now()}
	// A heartbeat that does not tick during the test, so the lease
	// does not renew itself behind the assertions.
	store, err := NewStore(dir, WithClock(clk.Now), WithLeaseOptions(LeaseOptions{TTL: 50 * time.Millisecond, HeartbeatInterval: time.Hour, OwnerID: "owner1", Now: clk.Now}))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	if _, err := store.Create(context.Background(), createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("the list holds %d sessions, want 1", len(list))
	}
	if !list[0].Locked {
		t.Fatal("the live-leased session is not locked, want it locked")
	}

	// The owner dies and its lease expires: the lease file remains.
	owner, ok := store.open.owner.(*LeaseAuthority)
	if !ok {
		t.Fatalf("the store's owner is %T, want a lease authority", store.open.owner)
	}
	owner.markLost()
	clk.Advance(60 * time.Millisecond)
	if _, err := readLease(LeasePath(dir, "s1")); err != nil {
		t.Fatalf("reading the expired lease: %v", err)
	}
	list, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("listing after the expiry: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("the list holds %d sessions, want 1", len(list))
	}
	if list[0].Locked {
		t.Error("the expired-leased session is locked, want it unlocked")
	}
}
