package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/decide"
)

// TestTheDecisionSinkWritesStandaloneRecords covers the decision sink:
// a caller that makes no session still keeps the decisions it made.
func TestTheDecisionSinkWritesStandaloneRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	sink, err := NewJSONLDecisionSink(path)
	if err != nil {
		t.Fatalf("opening the sink: %v", err)
	}
	ctx := context.Background()
	rec := decide.Record{
		Origin:    "permission",
		StateHash: "abc",
		Outcome:   decide.OutcomeOK,
		LatencyMS: 12.5,
		Questions: []decide.Question{{ID: "q1", Kind: decide.Choice{Options: []decide.Option{{Name: "yes"}}}}},
	}
	if err := sink.Record(ctx, rec); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if err := sink.Record(ctx, decide.Record{Origin: "other", Outcome: decide.OutcomeDeferred}); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("closing the sink: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the file holds %d lines, want 2", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("the first line is not JSON: %v", err)
	}
	if first["record"] != "decision" {
		t.Errorf("the line names %v, want decision", first["record"])
	}
	if first["origin"] != "permission" {
		t.Errorf("the line's origin is %v, want permission", first["origin"])
	}
	if first["outcome"] != decide.OutcomeOK {
		t.Errorf("the line's outcome is %v, want %q", first["outcome"], decide.OutcomeOK)
	}
	if first["state_hash"] != "abc" {
		t.Errorf("the line's state hash is %v, want abc", first["state_hash"])
	}
}

// TestTheDecisionSinkIsASink covers the sink's shape: it is a
// decide.Sink.
func TestTheDecisionSinkIsASink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	sink, err := NewJSONLDecisionSink(path)
	if err != nil {
		t.Fatalf("opening the sink: %v", err)
	}
	defer sink.Close()
	var asSink decide.Sink = sink
	if err := asSink.Record(context.Background(), decide.Record{Outcome: decide.OutcomeOK}); err != nil {
		t.Fatalf("recording: %v", err)
	}
}
