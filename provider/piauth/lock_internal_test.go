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
// another writer took over: the tick must find the directory not its own
// and stop, not put its own time on the taker's directory and then believe
// it still holds the lock.
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

// TestTheKeepaliveKeepsTheHeldLockFresh covers the other half of the tick:
// a lock this holder still owns is stamped again, so a long refresh does
// not go stale and get broken.
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
// pre-write check sharing a lock: a verify that runs while the keepalive
// stamps the directory must not read the new mtime against the old
// expectation and report the holder's own lock as another writer's.
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

// TestReleaseLeavesATakenLockAlone covers the release of a lock another
// writer took: the directory is the taker's, and removing it would hand the
// file to a third writer while the taker believes it holds the lock.
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
