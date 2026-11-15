package piauth

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// takeOver breaks the lock a test holds and creates a new directory in its
// place, the way a writer that judged the lock stale would. The mtime it
// leaves is the caller's: the holder's own, to make the takeover invisible
// to a bare mtime comparison, or a foreign one.
func takeOver(t *testing.T, lockPath string, mtime time.Time) {
	t.Helper()
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("breaking the lock: %v", err)
	}
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	if err := os.Chtimes(lockPath, mtime, mtime); err != nil {
		t.Fatalf("marking the taken lock: %v", err)
	}
}

// TestTheKeepaliveDoesNotRestampATakenLock covers the keepalive of a lock
// another writer took over. The tick must find the directory not its own and
// stop, not stamp the taker's directory and believe it still holds the lock.
func TestTheKeepaliveDoesNotRestampATakenLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	hold, err := acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer hold.release()
	lockPath := path + ".lock"
	held, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stating the held lock: %v", err)
	}

	// The taker's directory carries the holder's own mtime, so only the
	// directory's identity says it is not the same lock.
	takeOver(t, lockPath, held.ModTime())

	if err := hold.touch(); err == nil {
		t.Error("the keepalive stamped the taken lock and still believes it holds it")
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stating the taken lock: %v", err)
	}
	if !after.ModTime().Equal(held.ModTime()) {
		t.Errorf("the taker's mtime is now %v, want its own %v: the keepalive restamped it",
			after.ModTime(), held.ModTime())
	}
	if err := hold.verify(); err == nil {
		t.Error("a verify after the takeover still reports the lock as this holder's")
	}
}

// TestTheKeepaliveKeepsTheHeldLockFresh covers the other half of the tick. A
// lock this holder still owns is stamped again, so a long refresh does not go
// stale and get broken.
func TestTheKeepaliveKeepsTheHeldLockFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	hold, err := acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer hold.release()
	before := hold.mtime

	if err := hold.touch(); err != nil {
		t.Fatalf("the keepalive refused a lock it holds: %v", err)
	}
	if hold.mtime.Before(before) {
		t.Errorf("the keepalive moved the lock's mtime back, to %v from %v", hold.mtime, before)
	}
	if err := hold.verify(); err != nil {
		t.Errorf("the lock is not this holder's after a tick: %v", err)
	}
	if age := time.Since(hold.mtime); age > lockRefresh {
		t.Errorf("the tick left an mtime %v old, want it fresher than %v", age, lockRefresh)
	}
}

// TestAVerifyBetweenTicksStillSeesOneOwner covers the keepalive and the
// pre-write check sharing a lock. A verify that runs while the keepalive stamps
// the directory must not read the new mtime against the old expectation. That
// would report the holder's own lock as another writer's.
func TestAVerifyBetweenTicksStillSeesOneOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	hold, err := acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer hold.release()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := hold.touch(); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		if err := hold.verify(); err != nil {
			t.Fatalf("verify %d reported the holder's own lock: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestTheKeepaliveStampsTheHeldLock covers the tick the keepalive's own
// goroutine makes. It finds the stamp this holder left behind nearly stale, as
// a tick that ran late leaves it, and restamps the directory. A writer judging
// the lock stale then does not break a lock this holder still holds.
//
// The tick comes at the package's own interval, so the test waits seconds for
// it. It waits for the stamp on the filesystem, which outlives the wait,
// rather than for a sleep to end.
func TestTheKeepaliveStampsTheHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	hold, err := acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer hold.release()

	// The mutex makes the stale stamp a critical section of its own: no
	// stamp is in flight, so the goroutine's next one comes after this.
	hold.mu.Lock()
	stale := time.Now().Add(-lockStale + time.Second)
	hold.mtime = stale
	if err := os.Chtimes(hold.path, stale, stale); err != nil {
		hold.mu.Unlock()
		t.Fatalf("aging the held lock: %v", err)
	}
	hold.mu.Unlock()

	deadline := time.Now().Add(2*lockRefresh + time.Second)
	for {
		info, err := os.Stat(hold.path)
		if err != nil {
			t.Fatalf("stating the held lock: %v", err)
		}
		if info.ModTime().After(stale) {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the held lock still carries the %v stamp, so no tick of the keepalive stamped it", stale)
		}
		time.Sleep(lockWaitBase)
	}

	// No tick is in flight while the mutex is held, so the stamp the
	// holder records is the one the directory carries.
	hold.mu.Lock()
	recorded, lost := hold.mtime, hold.lost
	info, err := os.Stat(hold.path)
	hold.mu.Unlock()
	if err != nil {
		t.Fatalf("stating the held lock: %v", err)
	}
	if lost != nil {
		t.Fatalf("the keepalive gave up a lock it holds: %v", lost)
	}
	if !recorded.Equal(info.ModTime()) {
		t.Errorf("the holder records the stamp %v, want the %v it left on the directory", recorded, info.ModTime())
	}
	if age := time.Since(recorded); age >= lockStale {
		t.Errorf("the held lock's stamp is %v old, so another writer would break it", age)
	}
}

// TestReleaseLeavesATakenLockAlone covers the release of a lock another writer
// took. The directory is the taker's, and removing it would hand the file to a
// third writer while the taker holds it.
func TestReleaseLeavesATakenLockAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	hold, err := acquire(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	lockPath := path + ".lock"
	takeOver(t, lockPath, time.Now().Add(-time.Second))
	hold.release()
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("release removed the taker's lock directory: %v", err)
	}
}
