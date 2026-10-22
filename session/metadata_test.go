package session

import (
	"context"
	"errors"
	"testing"
)

// TestTheMetadataOfASessionIsItsHeader covers the header: the
// metadata is the session's own, and it is read without the lock, so a
// caller may learn what a session was before it opens it.
func TestTheMetadataOfASessionIsItsHeader(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	created := createdRecord("s1")
	created.Profile = "code"
	if _, err := store.Create(ctx, created); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	meta, err := MetadataOf(dir, "s1")
	if err != nil {
		t.Fatalf("reading the metadata: %v", err)
	}
	if meta.SessionID != "s1" {
		t.Errorf("the session is %q, want s1", meta.SessionID)
	}
	if meta.Profile != "code" {
		t.Errorf("the profile is %q, want code", meta.Profile)
	}
	if meta.Mode != ModeLogged {
		t.Errorf("the mode is %q, want %q", meta.Mode, ModeLogged)
	}
	if len(meta.Tools) != 2 {
		t.Errorf("the metadata holds %d tools, want 2", len(meta.Tools))
	}
}

// TestTheMetadataOfASessionThatIsNotThere covers the failure: a
// session the directory holds no log for is the store's not-found
// failure.
func TestTheMetadataOfASessionThatIsNotThere(t *testing.T) {
	dir, _ := newTestStore(t)
	_, err := MetadataOf(dir, "missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("the failure is %v, want the not-found failure", err)
	}
}
