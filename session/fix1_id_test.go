package session

import (
	"context"
	"errors"
	"testing"
)

// TestAnInvalidSessionIDIsRefusedBeforeTheFilesystem covers the id
// rule: an id that could name a file outside the session directory is
// refused by every entry point that would join it to a path.
func TestAnInvalidSessionIDIsRefusedBeforeTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`, "a..b"} {
		if _, err := MetadataOf(dir, id); !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("MetadataOf(%q) failed with %v, want ErrInvalidSessionID", id, err)
		}
		if _, err := store.Load(context.Background(), id); !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("Load(%q) failed with %v, want ErrInvalidSessionID", id, err)
		}
		if _, err := store.Pending(context.Background(), id); !errors.Is(err, ErrInvalidSessionID) {
			t.Errorf("Pending(%q) failed with %v, want ErrInvalidSessionID", id, err)
		}
	}
	if err := ValidateID("sess-1"); err != nil {
		t.Errorf("a plain id was refused: %v", err)
	}
}
