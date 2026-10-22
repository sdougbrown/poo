package eval

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestReplayUsesARecordedResponsesWindow covers the rule that a replay
// applies a recorded provider window as recorded, whatever API the
// recording's transport spoke.
func TestReplayUsesARecordedResponsesWindow(t *testing.T) {
	c := poo.Compaction{Strategy: "native", Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind: poo.ContentProvider,
		Provider: &poo.ProviderBlock{
			API:      poo.APIResponses,
			Type:     "compaction",
			Required: true,
			Data:     []byte(`{"encrypted_content":"abc"}`),
		},
	}}}}
	raw, err := json.Marshal(&poo.CompactionRecord{
		RecordMeta: poo.NewRecordMeta("compaction"),
		RunID:      "r",
		Strategy:   "native",
		Form:       "window",
		Compaction: c,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"tool_call","name":"echo","arguments":{}}]}`,
		string(raw),
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[{"kind":"text","text":"done"}]}`,
		`{"record":"run.settled","seq":5,"ts":"2026-10-20T00:00:00Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":2}`,
	)
	res, err := Replay(path, ReplayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Compactions != 1 {
		t.Fatalf("recorded Responses window silently rejected: compactions=%d match=%v", res.Compactions, res.Match)
	}
}
