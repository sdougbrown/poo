package eval

import (
	"testing"
)

// TestReplayDeliversAFeedInputInAWokenRun covers the replay's treatment
// of a feed's input: the recording's accepted input that never reached
// the model is queued again, so the replay wakes the run the recording
// woke.
func TestReplayDeliversAFeedInputInAWokenRun(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768,"tools_mode":"native","model":{"id":"m"}}`,
		`{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r1","trigger":"prompt","tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r1","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"the answer"}]}`,
		`{"record":"run.settled","seq":4,"ts":"2026-10-20T00:00:01Z","run_id":"r1","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
		`{"record":"input.queued","seq":5,"ts":"2026-10-20T00:00:01Z","origin":"feed:inbox","session_id":"s","input_id":"s-ji1","mode":"feed","text":"[feed inbox: the message]"}`,
		`{"record":"run.started","seq":6,"ts":"2026-10-20T00:00:02Z","run_id":"r2","trigger":"feed","feeds":["inbox"],"tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":7,"ts":"2026-10-20T00:00:03Z","run_id":"r2","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"the feed answer"}]}`,
		`{"record":"run.settled","seq":8,"ts":"2026-10-20T00:00:03Z","run_id":"r2","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	)
	res, err := Replay(path, ReplayOptions{})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("the replay delivered %d results, want 1: %v", len(res.Jobs), res.Jobs)
	}
	if res.Jobs[0] != "[feed inbox: the message]" {
		t.Errorf("the delivered result is %q, want the recorded text", res.Jobs[0])
	}
	if !res.Match {
		t.Errorf("the replay's calls differ from the recording: %v", res.Differences)
	}
}

// TestReplaySkipsADeliveredFeedInput covers the replay's other side: an
// input the recording delivered is not queued again.
func TestReplaySkipsADeliveredFeedInput(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768,"tools_mode":"native","model":{"id":"m"}}`,
		`{"record":"input.queued","seq":2,"ts":"2026-10-20T00:00:00Z","origin":"feed:inbox","session_id":"s","input_id":"s-ji1","mode":"feed","text":"[feed inbox: the message]"}`,
		`{"record":"input.delivered","seq":3,"ts":"2026-10-20T00:00:00Z","origin":"feed:inbox","session_id":"s","run_id":"r1","input_id":"s-ji1","mode":"feed"}`,
		`{"record":"run.started","seq":4,"ts":"2026-10-20T00:00:01Z","run_id":"r1","trigger":"prompt","tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":5,"ts":"2026-10-20T00:00:02Z","run_id":"r1","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"the answer"}]}`,
		`{"record":"run.settled","seq":6,"ts":"2026-10-20T00:00:02Z","run_id":"r1","reason":"end_turn","detail":{"source":"end_turn"},"turns":1}`,
	)
	res, err := Replay(path, ReplayOptions{})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(res.Jobs) != 0 {
		t.Errorf("the replay delivered %d results, want none: %v", len(res.Jobs), res.Jobs)
	}
}
