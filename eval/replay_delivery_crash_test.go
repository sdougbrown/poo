package eval

import "testing"

// TestReplaySkipsAnInputItsMessageDelivered covers a recording cut between
// a feed input's message and its delivered record. The message names the
// input, so the replay agrees with a restore and does not queue it again.
func TestReplaySkipsAnInputItsMessageDelivered(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768,"tools_mode":"native","model":{"id":"m"}}`,
		`{"record":"input.queued","seq":2,"ts":"2026-10-20T00:00:00Z","origin":"feed:inbox","session_id":"s","input_id":"s-ji1","mode":"feed","text":"[feed inbox: the message]"}`,
		`{"record":"run.started","seq":3,"ts":"2026-10-20T00:00:01Z","run_id":"r1","trigger":"prompt","tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:01Z","run_id":"r1","origin":"feed:inbox","role":"user","input_id":"s-ji1","content":[{"kind":"text","text":"[feed inbox: the message]"}]}`,
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
