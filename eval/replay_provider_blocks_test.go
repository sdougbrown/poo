package eval

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAReplayedResponseKeepsItsProviderBlocks covers the blocks a storeless
// endpoint produced. A recording replays the provider's answers, so a replayed
// turn has to carry the opaque blocks the recording's message holds: without
// them the replayed session sends a different next request than the recording
// did, and the comparison is against a conversation the model never had.
func TestAReplayedResponseKeepsItsProviderBlocks(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"message","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":2,"origin":"model","role":"assistant","content":[`+
			`{"kind":"thinking","text":"weighing","provider":{"api":"responses","type":"reasoning","data":{"type":"reasoning","encrypted_content":"enc_one"}}},`+
			`{"kind":"provider","provider":{"api":"responses","type":"reasoning","data":{"type":"reasoning","encrypted_content":"enc_two"}}},`+
			`{"kind":"text","text":"the answer"}]}`,
		usageRecord(2, 2, 100, 5, 0, nil),
		settledRecord("end_turn", 2),
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := ScriptFromLog(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Turns) != 1 {
		t.Fatalf("the script holds %d turns, want the one assistant message", len(script.Turns))
	}
	turn := script.ToScript().Turns[0]
	if len(turn.Blocks) != 2 {
		t.Fatalf("the replayed turn carries %d blocks, want the thinking's companion and the lone one", len(turn.Blocks))
	}
	for i, want := range []string{"enc_one", "enc_two"} {
		got := turn.Blocks[i]
		if got.API != poo.APIResponses || got.Type != "reasoning" {
			t.Errorf("block %d is %s %s, want the recorded reasoning block", i, got.API, got.Type)
		}
		if !strings.Contains(string(got.Data), want) {
			t.Errorf("block %d is %s, want the recorded payload %s", i, got.Data, want)
		}
	}
	if turn.Thinking != "weighing" || turn.Text != "the answer" {
		t.Errorf("the turn is %q / %q, want the recorded text", turn.Thinking, turn.Text)
	}
}
