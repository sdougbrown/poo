package session_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
)

// records reads the records a log holds.
func records(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	defer f.Close()

	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("the log holds a line that is not JSON: %q", line)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return out
}

func TestTheLogHoldsOneJSONObjectPerLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()

	if err := log.Append(ctx, &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    "s1",
		Mode:         "logged",
		CreatedAt:    "2026-10-20T00:00:00Z",
		WorkingDir:   "/tmp",
		ContextLimit: 32768,
		ToolsMode:    "native",
		Build:        poo.BuildInfo{Name: "poo", Version: "0.1.0"},
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Append(ctx, &poo.RunStarted{
		RecordMeta: poo.NewRecordMeta("run.started"),
		RunID:      "r1",
		PromptHash: "abc",
		StartedAt:  "2026-10-20T00:00:00Z",
		ModelID:    "m",
		ToolsMode:  "native",
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Flush(ctx); err != nil {
		t.Fatalf("flushing: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	if len(recs) != 2 {
		t.Fatalf("the log holds %d records, want 2", len(recs))
	}
	if recs[0]["record"] != "session.created" {
		t.Errorf("the first record is %v, want session.created", recs[0]["record"])
	}
	if recs[0]["format"] != "poo-session-v0" {
		t.Errorf("the format is %v, want poo-session-v0", recs[0]["format"])
	}
	if recs[1]["record"] != "run.started" {
		t.Errorf("the second record is %v, want run.started", recs[1]["record"])
	}
}

func TestTheRecordsCarryTheirSequenceNumberAndTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := log.Append(ctx, &poo.RunStarted{
			RecordMeta: poo.NewRecordMeta("run.started"),
			RunID:      "r1",
		}); err != nil {
			t.Fatalf("appending: %v", err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	for i, rec := range recs {
		seq, ok := rec["seq"].(float64)
		if !ok {
			t.Fatalf("record %d has no sequence number", i)
		}
		if int(seq) != i+1 {
			t.Errorf("record %d has sequence %v, want %d", i, seq, i+1)
		}
		ts, ok := rec["ts"].(string)
		if !ok || ts == "" {
			t.Errorf("record %d has no timestamp", i)
		}
	}
}

// TestAnExistingLogIsRefused covers a log that is opened twice: the
// second open fails, because a session log holds one session's records.
func TestAnExistingLogIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	ctx := context.Background()
	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	if err := log.Append(ctx, &poo.RunStarted{RecordMeta: poo.NewRecordMeta("run.started")}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	// Re-opening the same path is refused rather than extending it.
	log, err = session.Open(path)
	if err == nil {
		log.Close()
		t.Fatal("re-opening the log succeeded, want it refused")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the error is %q, want it to say the log exists", err)
	}

	// The log still holds the one record.
	recs := records(t, path)
	if len(recs) != 1 {
		t.Errorf("the log holds %d records, want 1", len(recs))
	}
	if recs[0]["seq"].(float64) != 1 {
		t.Errorf("the record's sequence number is %v, want 1", recs[0]["seq"])
	}
}

func TestTheUsageRecordCarriesTheCacheReadCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()
	cached := 96
	if err := log.Append(ctx, &poo.Usage{
		RecordMeta:      poo.NewRecordMeta("usage"),
		RunID:           "r1",
		Turn:            1,
		Request:         1,
		InputTokens:     120,
		OutputTokens:    8,
		CacheReadTokens: &cached,
		TTFTMS:          123.5,
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	if len(recs) != 1 {
		t.Fatalf("the log holds %d records, want 1", len(recs))
	}
	if recs[0]["cache_read_tokens"].(float64) != 96 {
		t.Errorf("the cache-read count is %v, want 96", recs[0]["cache_read_tokens"])
	}
	if recs[0]["ttft_ms"].(float64) != 123.5 {
		t.Errorf("the ttft is %v, want 123.5", recs[0]["ttft_ms"])
	}
}

func TestTheCacheReadCountIsAbsentWhenTheEndpointReportsNone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()
	if err := log.Append(ctx, &poo.Usage{
		RecordMeta:   poo.NewRecordMeta("usage"),
		RunID:        "r1",
		InputTokens:  120,
		OutputTokens: 8,
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	if _, ok := recs[0]["cache_read_tokens"]; ok {
		t.Errorf("the record has a cache-read count, want none: %v", recs[0])
	}
}

func TestTheToolCallRecordHoldsTheFullArguments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()
	if err := log.Append(ctx, &poo.ToolCallRecord{
		RecordMeta: poo.NewRecordMeta("tool.call"),
		RunID:      "r1",
		Turn:       1,
		Handle:     "r1",
		Tool:       "bash",
		Arguments:  json.RawMessage(`{"command":"ls -la /tmp"}`),
	}); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	args, ok := recs[0]["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("the arguments are %v, want a JSON object", recs[0]["arguments"])
	}
	if args["command"] != "ls -la /tmp" {
		t.Errorf("the command is %v, want the full arguments", args["command"])
	}
}

func TestALogFileIsReadableOnlyByItsOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	defer log.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stating the log: %v", err)
	}
	// The log carries prompts, responses and tool arguments. Group and
	// other must hold no access; the exact owner bits are the caller's
	// umask's business.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the log's mode is %#o, want no group or other access", perm)
	}
}

func TestAnAppendAfterTheContextIsDoneFailsAndLeavesTheLogEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	defer log.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := &poo.RunStarted{RecordMeta: poo.NewRecordMeta("run.started")}
	if err := log.Append(ctx, rec); !errors.Is(err, context.Canceled) {
		t.Errorf("the append returned %v, want context.Canceled", err)
	}
	if err := log.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("the flush returned %v, want context.Canceled", err)
	}

	// The refused append consumed no sequence number: the next live
	// append writes the first record.
	if err := log.Append(context.Background(), rec); err != nil {
		t.Fatalf("appending after the cancelled one: %v", err)
	}
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("flushing: %v", err)
	}
	recs := records(t, path)
	if len(recs) != 1 {
		t.Fatalf("the log holds %d records, want 1", len(recs))
	}
	if seq, ok := recs[0]["seq"].(float64); !ok || seq != 1 {
		t.Errorf("the record's seq is %v, want 1", recs[0]["seq"])
	}
}

func TestTheDefaultPathIsInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := session.DefaultPath(dir, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	if filepath.Dir(path) != dir {
		t.Errorf("the default path is %q, want it under %q", path, dir)
	}
	if !strings.HasPrefix(filepath.Base(path), "poo-session-") {
		t.Errorf("the default name is %q, want it to start with poo-session-", filepath.Base(path))
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("the default path is %q, want it to end with .jsonl", path)
	}
}

// TestConcurrentAppendsNeverInterleave covers the log's concurrency
// contract: Append may be called from many goroutines at once, and the
// log serializes the writes, so a reader of the log never sees half a
// record.
func TestConcurrentAppendsNeverInterleave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	log, err := session.Open(path)
	if err != nil {
		t.Fatalf("opening the log: %v", err)
	}
	ctx := context.Background()

	// A record big enough that a write that was not serialized would
	// interleave with another one.
	text := strings.Repeat("x", 4096)
	writers := 16
	perWriter := 50

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := log.Append(ctx, &poo.MessageRecord{
					RecordMeta: poo.NewRecordMetaFrom("message", poo.OriginUser),
					RunID:      "r1",
					Turn:       i,
					Role:       poo.RoleUser,
					Content: []poo.ContentBlock{{
						Kind: poo.ContentText,
						Text: fmt.Sprintf("writer %d: %s", w, text),
					}},
				}); err != nil {
					t.Errorf("appending: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	recs := records(t, path)
	if len(recs) != writers*perWriter {
		t.Fatalf("the log holds %d records, want %d", len(recs), writers*perWriter)
	}

	// Every line is a whole record, and the sequence numbers are
	// unique: a write that was not serialized would have broken both.
	seen := make(map[float64]bool, len(recs))
	for i, rec := range recs {
		seq, ok := rec["seq"].(float64)
		if !ok {
			t.Fatalf("record %d has no sequence number: %v", i, rec)
		}
		if seen[seq] {
			t.Fatalf("the sequence number %v is repeated", seq)
		}
		seen[seq] = true

		content, ok := rec["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("record %d's content is %v, want one block", i, rec["content"])
		}
		block, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("record %d's block is %v, want an object", i, content[0])
		}
		got, ok := block["text"].(string)
		if !ok {
			t.Fatalf("record %d's block has no text: %v", i, block)
		}
		if !strings.HasPrefix(got, "writer ") || !strings.HasSuffix(got, text) {
			t.Fatalf("record %d's text is %q, want a whole record", i, got[:min(len(got), 64)])
		}
	}
}
