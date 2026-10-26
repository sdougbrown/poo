package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAListRaceDoesNotRefuseARealLoad is the store-level form of the
// lock probe: a List over the same directory makes a Load of a free
// session fail with session_locked.
func TestAListRaceDoesNotRefuseARealLoad(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	lister, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the listing store: %v", err)
	}
	defer lister.Close()
	loader, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the loading store: %v", err)
	}
	defer loader.Close()

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			if _, err := lister.List(ctx); err != nil {
				t.Errorf("listing: %v", err)
				return
			}
		}
	}()
	refused := 0
	for i := 0; i < 2000; i++ {
		loaded, err := loader.Load(ctx, "s1")
		if err != nil {
			if errors.Is(err, ErrSessionLocked) {
				refused++
				continue
			}
			t.Fatalf("loading: %v", err)
		}
		if err := loader.Close(); err != nil {
			t.Fatalf("closing the session: %v", err)
		}
		_ = loaded
	}
	stop.Store(true)
	wg.Wait()
	if refused > 0 {
		t.Errorf("a free session was refused %d times out of 2000 loads while a List ran", refused)
	}
}
