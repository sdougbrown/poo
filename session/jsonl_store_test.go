package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

func TestASessionRoundTripsThroughItsFile(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	id, err := store.Create(ctx, createdRecord("s1"))
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if id != "s1" {
		t.Errorf("the session's id is %q, want s1", id)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolCall("r1", 1, "r1", "bash")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, toolResult("r1", 1, "r1", "bash", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, &poo.RunSettled{
		RecordMeta: poo.NewRecordMeta("run.settled"),
		RunID:      "r1",
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  "2026-10-20T00:00:02Z",
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	// A new store over the same directory reads the session back.
	other, err := NewStore(dir)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer other.Close()
	loaded, err := other.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	names := recordNames(loaded.Records)
	want := []string{"session.created", "run.started", "tool.call", "tool.result", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the session holds %v, want %v", names, want)
	}
	if loaded.Metadata.SessionID != "s1" {
		t.Errorf("the session's id is %q, want s1", loaded.Metadata.SessionID)
	}
	if loaded.Metadata.Mode != ModeLogged {
		t.Errorf("the session's mode is %q, want logged", loaded.Metadata.Mode)
	}
	if loaded.Metadata.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want 32768", loaded.Metadata.ContextLimit)
	}
	if loaded.Metadata.Build.Name != "poo" {
		t.Errorf("the build is %q, want poo", loaded.Metadata.Build.Name)
	}
	if loaded.RecoveredTail {
		t.Error("the load reports a recovered tail, want none")
	}
	if len(loaded.Requeue) != 0 {
		t.Errorf("the load queues %d job results, want none", len(loaded.Requeue))
	}
}

// TestTheFileHoldsOneRecordPerLine covers the shape of the log: a
// reader sees one whole record per line.
func TestTheFileHoldsOneRecordPerLine(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "s1.jsonl"))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("the log does not end with a newline")
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the log holds %d lines, want 2", len(lines))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Errorf("line %d is %q, want a JSON object", i, line)
		}
	}
}

// TestARunBoundaryAndASideEffectingResultAreSynced covers the
// durability rule: a run's boundaries and a side-effecting tool's
// result are on disk before the write returns.
func TestARunBoundaryAndASideEffectingResultAreSynced(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	log := store.open.log
	before := log.syncs

	if err := store.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if log.syncs != before+1 {
		t.Errorf("the run's start was synced %d times, want one", log.syncs-before)
	}

	// A read-only tool's result is not synced on its own.
	if err := store.Append(ctx, toolResult("r1", 1, "r1", "read", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if log.syncs != before+1 {
		t.Errorf("the read-only result was synced, want no sync")
	}

	// A side-effecting tool's result is.
	if err := store.Append(ctx, toolResult("r1", 1, "r2", "bash", poo.OutcomeOK)); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if log.syncs != before+2 {
		t.Errorf("the side-effecting result was synced %d times, want one more", log.syncs-before)
	}
}

// TestACodecMismatchIsATypedError covers a log written in a format the
// codec does not know.
func TestACodecMismatchIsATypedError(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	header := createdRecord("s1")
	header.Format = "poo-session-v-1"
	writeSession(t, dir, "s1", header)

	_, err := store.Load(ctx, "s1")
	if err == nil {
		t.Fatal("the load succeeded, want a codec mismatch")
	}
	if !errors.Is(err, ErrCodecMismatch) {
		t.Errorf("the failure is %v, want a codec mismatch", err)
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeCodecMismatch {
		t.Errorf("the failure's code is %q, want %q", code, CodeCodecMismatch)
	}
}

// TestTheMigrationHookMigratesAnOlderLog covers the codec's migration
// hook: a log in an older format is migrated before it is read.
func TestTheMigrationHookMigratesAnOlderLog(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	header := createdRecord("s1")
	header.Format = "poo-session-v-1"
	writeSession(t, dir, "s1", header, runStarted("r1"), &poo.RunSettled{
		RecordMeta: poo.NewRecordMeta("run.settled"),
		RunID:      "r1",
		Reason:     poo.ReasonEndTurn,
		Detail:     poo.SettleDetail{Source: "model"},
		SettledAt:  "2026-10-20T00:00:02Z",
	})

	migrated := false
	store.codec = migratingCodec{onMigrate: func() { migrated = true }}
	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	if !migrated {
		t.Error("the codec's migration hook was not called")
	}
	if len(loaded.Records) != 3 {
		t.Errorf("the session holds %d records, want 3", len(loaded.Records))
	}
}

// migratingCodec is a codec that migrates an older log.
type migratingCodec struct {
	// onMigrate is called when the migration runs.
	onMigrate func()
}

// Version returns the current format.
func (m migratingCodec) Version() string { return poo.FormatVersion }

// Encode returns a record's line.
func (m migratingCodec) Encode(rec poo.Record) ([]byte, error) {
	return JSONLCodec{}.Encode(rec)
}

// Decode returns the record a line holds.
func (m migratingCodec) Decode(line []byte) (poo.Record, error) {
	return JSONLCodec{}.Decode(line)
}

// Migrate returns the log's lines in the current format.
func (m migratingCodec) Migrate(from string, lines [][]byte) ([][]byte, error) {
	if m.onMigrate != nil {
		m.onMigrate()
	}
	return lines, nil
}

// TestADurableSessionIsRefused covers the reserved mode.
func TestADurableSessionIsRefused(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	header := createdRecord("s1")
	header.Mode = string(ModeDurable)
	_, err := store.Create(ctx, header)
	if !errors.Is(err, ErrUnsupportedMode) {
		t.Fatalf("the creation returned %v, want an unsupported mode", err)
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeUnsupportedMode {
		t.Errorf("the failure's code is %q, want %q", code, CodeUnsupportedMode)
	}
}

// TestAnEphemeralSessionWritesNoFiles covers the ephemeral mode: the
// records are kept in memory, and there is nothing to load.
func TestAnEphemeralSessionWritesNoFiles(t *testing.T) {
	memory := NewMemory()
	ctx := context.Background()

	header := createdRecord("s1")
	header.Mode = string(ModeEphemeral)
	id, err := memory.Create(ctx, header)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if id != "s1" {
		t.Errorf("the session's id is %q, want s1", id)
	}
	if err := memory.Append(ctx, runStarted("r1")); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if len(memory.Records()) != 2 {
		t.Errorf("the memory holds %d records, want 2", len(memory.Records()))
	}
	if _, err := memory.Load(ctx, "s1"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("the load returned %v, want no such session", err)
	}
	// The memory is not a directory: it holds no file.
	dir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("the directory holds %d entries, want none", len(entries))
	}
}

// TestTheConstructionLeavesNothingBehind covers the atomic
// construction: a failure at any step leaves no session and no
// temporary file.
func TestTheConstructionLeavesNothingBehind(t *testing.T) {
	for step := 1; step <= 4; step++ {
		t.Run("", func(t *testing.T) {
			dir, store := newTestStore(t)
			ctx := context.Background()
			store.initFault = step

			if _, err := store.Create(ctx, createdRecord("s1")); err == nil {
				t.Fatal("the creation succeeded, want the injected failure")
			}
			for _, name := range []string{"s1.jsonl", "s1.jsonl.tmp"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s is there, want it gone", name)
				}
			}
		})
	}
}

// TestASessionThatAlreadyExistsIsRefused covers the rule that a log
// holds one session: a second one would take the first one's records
// with it.
func TestASessionThatAlreadyExistsIsRefused(t *testing.T) {
	_, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	_, err := store.Create(ctx, createdRecord("s1"))
	if err == nil {
		t.Fatal("the second creation succeeded, want it refused")
	}
	code, ok := CodeOf(err)
	if !ok || code != CodeSessionExists {
		t.Errorf("the failure's code is %q, want %q", code, CodeSessionExists)
	}
}

// TestLoadingASessionThatDoesNotExist covers the missing session: there
// is nothing to load, and the leftover lock file is harmless.
func TestLoadingASessionThatDoesNotExist(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Load(ctx, "s1")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("the load returned %v, want no such session", err)
	}
	// The lock file is a harmless leftover.
	if _, err := os.Stat(LockPath(dir, "s1")); err != nil {
		t.Errorf("the lock file is missing: %v", err)
	}
}

// TestTheStoreTakesTheAuthorityOnASession covers the lock: a session
// that is open is the store's, and the lock file is there.
func TestTheStoreTakesTheAuthorityOnASession(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, createdRecord("s1")); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	locked, err := Locked(LockPath(dir, "s1"))
	if err != nil {
		t.Fatalf("checking the lock: %v", err)
	}
	if !locked {
		t.Error("the session is not locked, want it locked")
	}
	if _, err := os.Stat(LockPath(dir, "s1")); err != nil {
		t.Errorf("the lock file is missing: %v", err)
	}
}
