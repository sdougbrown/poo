package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestTheLockProbeDoesNotRefuseAFreeSession covers the read-only lock
// probe: Locked takes the exclusive lock to find out whether another
// process holds it, so a Load or a Prune that runs at the same moment
// is told the session is locked when nobody owns it.
func TestTheLockProbeDoesNotRefuseAFreeSession(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	path := LockPath(dir, "s1")

	var stop atomic.Bool
	var wg sync.WaitGroup
	var probes atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			if _, err := Locked(path); err == nil {
				probes.Add(1)
			}
		}
	}()

	refused := 0
	for i := 0; i < 20000; i++ {
		a, err := Acquire(path)
		if err != nil {
			if errors.Is(err, ErrSessionLocked) {
				refused++
				continue
			}
			t.Fatalf("acquiring: %v", err)
		}
		a.Release()
	}
	stop.Store(true)
	wg.Wait()
	if refused > 0 {
		t.Errorf("the free session was refused %d times out of 20000 acquires (%d probes ran)", refused, probes.Load())
	}
}
