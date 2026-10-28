package eval

import (
	"context"
	"strings"
	"testing"
)

// TestTheReplayCountsTheRecordingsRequests covers a recording whose first
// request carried no usage: no usage record is written for it, and the
// recording's request count is the settle record's. Counting the usage
// records instead would leave the request the script cannot replay
// unreported, and the replay would claim a match.
func TestTheReplayCountsTheRecordingsRequests(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		// The first request recorded no assistant message and no usage:
		// the model sent no turn at all.
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"bash","arguments":{"command":"ls"}}]}`,
		`{"record":"tool.result","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"handle":"r1","tool":"bash","outcome":"ok","text":"a.go"}`,
		usageRecord(2, 2, 100, 5, 0, nil),
		`{"record":"message","seq":5,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":3,"origin":"model","role":"assistant","content":[{"kind":"text","text":"done"}]}`,
		usageRecord(3, 3, 110, 4, 0, nil),
		settledRecord("end_turn", 3),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(log.Usages()); got != 2 {
		t.Fatalf("the recording holds %d usage records, want 2", got)
	}

	result, err := Replay(path, ReplayOptions{Context: context.Background(), ContextLimit: 32768})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(result.Recorded) != 1 || len(result.Replayed) != 1 {
		t.Fatalf("the recording holds %d calls and the replay made %d, want one each",
			len(result.Recorded), len(result.Replayed))
	}
	if result.Match {
		t.Errorf("the replay reports a match although the recording made %d requests and the script holds 2 turns",
			log.Turns())
	}
	var named bool
	for _, difference := range result.Differences {
		if strings.Contains(difference, "no assistant message") {
			named = true
		}
	}
	if !named {
		t.Errorf("the differences do not name the request the script cannot replay: %v", result.Differences)
	}
}
