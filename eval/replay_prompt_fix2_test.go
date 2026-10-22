package eval

import "testing"

// TestRecordedSystemPromptsUseTheRecord covers the rule that a replay
// uses the recorded system_prompt text and never the system message.
func TestRecordedSystemPromptsUseTheRecord(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}`,
		`{"record":"system_prompt","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","variant":"small-local","text":"the recorded prompt","hash":"abc"}`,
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	got := recordedSystemPrompts(log)
	if len(got) != 1 || got[0] != "the recorded prompt" {
		t.Fatalf("the recorded prompts are %q, want the system_prompt record's text", got)
	}
}
