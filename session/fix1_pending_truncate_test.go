package session

// Review scratch test: Pending reads a session it holds no authority
// over, and the read truncates the file.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A store whose session is held by another store (another process, in
// the plan's terms) still truncates the session's file when Pending
// reads a torn tail. The plan puts the truncation in the recovery, under
// the Authority lock; Pending holds no authority.
func TestPendingDoesNotTruncateAFileItHasNoAuthorityOver(t *testing.T) {
	dir, storeA := newTestStore(t)
	ctx := context.Background()
	if _, err := storeA.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// The lock holder's file: a complete record, and then a write that
	// is on its way (the bytes a kill in the middle of a write leaves).
	path := filepath.Join(dir, "s1.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	appendBytes(t, path, []byte(`{"record":"run.started","run_id`))
	mid, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	// Another store asks about the pending operations. It holds no
	// authority: the session is locked by store A.
	storeB, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the second store: %v", err)
	}
	defer storeB.Close()
	pend, err := storeB.Pending(ctx, "s1")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pend == nil || len(pend.Runs) != 0 {
		t.Logf("pending: %+v", pend)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(after) != len(mid) {
		t.Errorf("the file shrank from %d to %d bytes: Pending truncated a session it holds no authority over (the holder's lock is still held)", len(mid), len(after))
	}
	_ = before
}
