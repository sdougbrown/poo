package main

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/session/sqlite"
)

// theSqliteSettings is a settings document with a model and the durable
// mode's sqlite store.
const theSqliteSettings = `
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
    api_key_env: POO_TEST_KEY
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat]
    context_limit: 32768
roles:
  chat: test-model
session:
  mode: durable
  durable:
    store: sqlite
`

// TestTheSqliteStoreBackendIsRegistered covers the binary's wiring: the
// binary links the sqlite store, so the settings' session.durable.store
// name dispatches to it, and a durable session is created and loaded
// through it.
func TestTheSqliteStoreBackendIsRegistered(t *testing.T) {
	settings, err := config.Parse([]byte(theSqliteSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	dir := t.TempDir()
	store, err := runtime.OpenStore("durable", dir)
	if err != nil {
		t.Fatalf("opening the durable store: %v", err)
	}
	defer store.Close()
	// The open dispatched on the settings' store name: the binary
	// registered the sqlite backend, so the store is the sqlite one.
	// A binary that did not link it would fail the open, and a store
	// that is not the sqlite one means the dispatch took another
	// backend.
	if _, ok := store.(*sqlite.Store); !ok {
		t.Fatalf("the store is %T, want a sqlite store: the binary did not register the backend", store)
	}
	// The store creates a session, and loads it back.
	id, err := store.Create(context.Background(), theCreatedRecord("s1", "durable", dir))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	loaded, err := store.Load(context.Background(), id)
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if loaded.ID != id {
		t.Errorf("the loaded session is %q, want %q", loaded.ID, id)
	}
}

// theCreatedRecord is the header a test's store creates a session with.
func theCreatedRecord(id, mode, dir string) *poo.SessionCreatedRecord {
	return &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"),
		SessionMetadata: poo.SessionMetadata{
			Format:       poo.FormatVersion,
			SessionID:    id,
			Mode:         mode,
			CreatedAt:    "2026-10-22T12:00:00Z",
			WorkingDir:   dir,
			Profile:      "code",
			Model:        poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768,
		},
	}
}
