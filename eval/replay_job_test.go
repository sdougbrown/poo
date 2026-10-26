package eval

import (
	"strings"
	"testing"
)

// TestReplayDeliversAJobResultInAWokenRun covers the plan's replay item:
// a job result that reached the model in a woken run is delivered again,
// so the run the recording woke is the run the replay wakes.
func TestReplayDeliversAJobResultInAWokenRun(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768,"tools_mode":"native","model":{"id":"m"}}`,
		`{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r1","trigger":"prompt","tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r1","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"first"}]}`,
		`{"record":"job.started","seq":4,"ts":"2026-10-20T00:00:00Z","session_id":"s","job":"j1","handle":"r1","tool":"echo","run_id":"r1"}`,
		`{"record":"job.ended","seq":5,"ts":"2026-10-20T00:00:01Z","session_id":"s","job":"j1","handle":"r1","tool":"echo","run_id":"r1","outcome":"ok","input_id":"s-ji1","text":"the job's output"}`,
		`{"record":"run.started","seq":6,"ts":"2026-10-20T00:00:02Z","run_id":"r2","trigger":"job","tools_mode":"native","model_id":"m"}`,
		`{"record":"message","seq":7,"ts":"2026-10-20T00:00:02Z","run_id":"r2","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","tool_call_id":"c1","name":"echo","arguments":{}}]}`,
		`{"record":"message","seq":8,"ts":"2026-10-20T00:00:03Z","run_id":"r2","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"done"}]}`,
		`{"record":"run.settled","seq":9,"ts":"2026-10-20T00:00:03Z","run_id":"r2","reason":"end_turn","detail":{"source":"end_turn"},"turns":2}`,
	)
	res, err := Replay(path, ReplayOptions{})
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("the replay delivered %d job results, want 1: %v", len(res.Jobs), res.Jobs)
	}
	if !strings.Contains(res.Jobs[0], "the job's output") {
		t.Errorf("the delivered job result is %q, want the recorded text", res.Jobs[0])
	}
	if !res.Match {
		t.Errorf("the replay's calls differ from the recording: %v", res.Differences)
	}
}
