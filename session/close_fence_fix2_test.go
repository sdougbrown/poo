package session

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// TestAnAppendAfterTheStoreCloseIsRefused covers the store's backstop for
// the session's write fence: the log closes under the same lock as its
// appends, before the authority is released, so a write that loses the
// race with the close is refused and the file is left exactly as it was.
func TestAnAppendAfterTheStoreCloseIsRefused(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	id, err := store.Create(ctx, createdRecord("s1"))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	path := store.path(id)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	if err := store.Append(ctx, runStarted("r2")); err == nil {
		t.Fatal("the append after the close was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log after the refused append: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the log changed after the refused append: %q, want %q", after, before)
	}
	if names := recordNames(logRecords(t, path)); len(names) != 2 {
		t.Errorf("the log holds %v, want the session header and one run", names)
	}
}
