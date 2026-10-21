package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The replay runs a recorded conversation through the current harness.
//
// The recording holds a fixed number of provider responses, and the
// replay must stop when they run out. A transport that repeats the last
// response turns a two-turn recording into an endless loop, and the
// comparison then reports a difference that is only the loop.

func TestTheReplayStopsWhenTheRecordingRunsOut(t *testing.T) {
	// A recording with one tool call. The replay must produce exactly
	// that one call, not a stream of them.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	log := strings.Join([]string{
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls -la"}}]}`,
		`{"record":"tool.result","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"handle":"r1","tool":"bash","outcome":"ok","text":"a.go"}`,
		`{"record":"run.settled","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(result.Recorded) != 1 {
		t.Fatalf("the recording holds %d calls, want 1", len(result.Recorded))
	}
	if len(result.Replayed) != 1 {
		t.Fatalf("the replay produced %d calls, want 1: %+v", len(result.Replayed), result.Replayed)
	}
}

func TestTheReplayUsesTheRecordingsOwnLimits(t *testing.T) {
	// With no limits named, the replay runs on the recording's own: here
	// the recording settled after one turn, and holds two calls, so a
	// replay on the recording's turn count stops after the first call.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	log := strings.Join([]string{
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"pwd"}}]}`,
		`{"record":"run.settled","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"max_turns","detail":{"source":"max_turns"},"turns":1}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	result, err := Replay(path, ReplayOptions{Context: context.Background()})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(result.Recorded) != 2 {
		t.Fatalf("the recording holds %d calls, want 2", len(result.Recorded))
	}
	if len(result.Replayed) != 1 {
		t.Errorf("the replay produced %d calls, want 1 (the recording's turn count): %+v", len(result.Replayed), result.Replayed)
	}
}

func TestTheScriptedTransportStopsAtTheEnd(t *testing.T) {
	// The transport hands out the recorded responses and then says the
	// script is over. It never repeats the last one.
	transport := newScriptedTransport([]ScriptedTurn{
		{Text: "first", Finish: "stop"},
		{Text: "second", Finish: "stop"},
	})
	for i := 0; i < 2; i++ {
		if _, _, err := transport.Stream(context.Background(), poo.ChatRequest{}); err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
	if _, _, err := transport.Stream(context.Background(), poo.ChatRequest{}); err == nil {
		t.Error("the transport served a third response, want the script to be over")
	}
}

func TestTheReplayDifferencesAreExact(t *testing.T) {
	// A recording whose last message is a tool call replays to exactly
	// that one call, and the two sequences are the same. Before the
	// transport stopped at the end of the script, the replay looped
	// until the context ran out and the difference was a count of
	// hundreds.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	log := strings.Join([]string{
		`{"record":"message","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"run.settled","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(result.Replayed) != 1 {
		t.Fatalf("the replay produced %d calls, want 1: %+v", len(result.Replayed), result.Replayed)
	}
	if !result.Match {
		t.Errorf("the sequences differ: %v", result.Differences)
	}
}
