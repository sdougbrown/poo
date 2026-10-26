package eval

import (
	"context"
	"testing"
)

// A replay applies the compactions the recording holds.
//
// The strategy that made them is never run: the recorded compaction is
// applied as it was recorded, at the point the recording compacted.

// TestTheReplayAppliesTheRecordedCompaction covers the plan's rule: a
// replay applies the recorded compaction, and never runs the strategy
// that made it.
func TestTheReplayAppliesTheRecordedCompaction(t *testing.T) {
	// The recording compacted after its first turn, and the compaction
	// replaced that turn with a summary.
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"compaction","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","strategy":"summarize","form":"replace","cut":{"first":1,"last":1},"replacement":[{"role":"user","content":[{"kind":"text","text":"the summary"}]}],"tokens_before":100,"tokens_after":20,"compaction":{"strategy":"summarize","replace":{"cut":{"first":1,"last":1},"replacement":[{"role":"user","content":[{"kind":"text","text":"the summary"}]}]}}}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"the answer"}]}`,
		`{"record":"run.settled","seq":5,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":2}`,
	)

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if result.Compactions != 1 {
		t.Errorf("the replay applied %d compactions, want 1", result.Compactions)
	}
	if len(result.Replayed) != 1 {
		t.Fatalf("the replay produced %d calls, want 1: %+v", len(result.Replayed), result.Replayed)
	}
	if !result.Match {
		t.Errorf("the sequences differ: %v", result.Differences)
	}
}

// TestTheReplayWithoutACompactionRunsUncompacted covers the plan's
// rule: a recording with no compaction replays without one.
func TestTheReplayWithoutACompactionRunsUncompacted(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"run.settled","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	)

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if result.Compactions != 0 {
		t.Errorf("the replay applied %d compactions, want none", result.Compactions)
	}
	if len(result.Replayed) != 1 {
		t.Errorf("the replay produced %d calls, want 1: %+v", len(result.Replayed), result.Replayed)
	}
}
