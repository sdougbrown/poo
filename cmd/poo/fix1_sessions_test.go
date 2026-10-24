package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// TestThePruneOlderThanFlagDeletesTheOldSessions covers the CLI
// mapping: --older-than marks a session by its age, and --dry-run
// deletes nothing.
func TestThePruneOlderThanFlagDeletesTheOldSessions(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")
	sessionDir := filepath.Join(dir, "sessions")
	old := time.Now().Add(-48 * time.Hour)
	store, err := session.NewStore(sessionDir, session.WithClock(func() time.Time { return old }))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	created := &poo.SessionCreated{
		RecordMeta: poo.NewRecordMeta("session.created"),
		Format:     poo.FormatVersion,
		SessionID:  "old-session",
		Mode:       "logged",
		CreatedAt:  old.UTC().Format(time.RFC3339Nano),
		Model:      poo.SessionModel{ModelID: "test-model"},
		Profile:    "code",
	}
	if _, err := store.Create(context.Background(), created); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1", "--dry-run"}); code != 0 {
			t.Errorf("the dry run exited with %d", code)
		}
	})
	if !strings.Contains(out, "would delete old-session") {
		t.Errorf("the dry run says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); err != nil {
		t.Errorf("the dry run deleted the session: %v", err)
	}

	out = captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1"}); code != 0 {
			t.Errorf("the prune exited with %d", code)
		}
	})
	if !strings.Contains(out, "deleted old-session") {
		t.Errorf("the prune says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); !os.IsNotExist(err) {
		t.Errorf("the old session is still there: %v", err)
	}
}
