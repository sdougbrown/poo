package eval

import (
	"context"
	"strings"
	"testing"
)

// TestTheReplayCarriesTheRecordingsInputCap covers an endpoint that takes a
// smaller prompt than its window: the replay model holds the cap the
// recorded chat role names, so the replay bounds the prompt by the prompt
// limit and not by the window.
func TestTheReplayCarriesTheRecordingsInputCap(t *testing.T) {
	// The recorded prompt is far over the 2000-token input cap
	// and well under it for the 32768-token window, so a replay that keeps
	// the cap settles context_exhausted before the first request, and one
	// that loses it makes the recorded call.
	prompt := strings.Repeat("x", 14000)
	recording := func(roles string) string {
		return writeLog(t,
			`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768,"roles":`+roles+`}`,
			`{"record":"system_prompt","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":0,"text":"`+prompt+`"}`,
			`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
			`{"record":"tool.result","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"handle":"r1","tool":"bash","outcome":"ok","text":"a.go"}`,
			settledRecord("end_turn", 1),
		)
	}

	capped := recording(`{"chat":{"name":"m","model_id":"m","context_limit":32768,"input_limit":2000}}`)
	result, err := Replay(capped, ReplayOptions{Context: context.Background()})
	if err != nil {
		t.Fatalf("replaying the capped recording: %v", err)
	}
	if len(result.Replayed) != 0 {
		t.Errorf("the replay made %d calls, want none: the prompt is over the 2000-token input cap",
			len(result.Replayed))
	}

	// The same prompt under a model with no cap makes the call, so the
	// difference is the cap and not the prompt.
	uncapped := recording(`{"chat":{"name":"m","model_id":"m","context_limit":32768}}`)
	result, err = Replay(uncapped, ReplayOptions{Context: context.Background()})
	if err != nil {
		t.Fatalf("replaying the uncapped recording: %v", err)
	}
	if len(result.Replayed) != 1 {
		t.Errorf("the replay made %d calls, want the recorded one: the 32768-token window holds the prompt",
			len(result.Replayed))
	}
}
