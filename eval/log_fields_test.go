package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// The harness reads the logs of every harness, including the ones the
// earlier versions wrote. The fields the newer core adds are all
// optional, so an old log decodes into the same shape.

// TestTheParserReadsAnOldLog covers a log written before the message ids,
// the origins and the trigger existed: it decodes, and the fields it
// does not carry are empty.
func TestTheParserReadsAnOldLog(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","prompt_hash":"abc","started_at":"2026-10-20T00:00:00Z","model_id":"m","max_turns":8,"tools_mode":"native"}`,
		`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":0,"origin":"user","role":"user","content":[{"kind":"text","text":"the prompt"}]}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"text","text":"the answer"}]}`,
		`{"record":"run.settled","seq":5,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1,"settled_at":"2026-10-20T00:00:01Z"}`,
	)

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if len(log.Records) != 5 {
		t.Fatalf("the log holds %d records, want 5", len(log.Records))
	}

	var started *LogRecord
	for i := range log.Records {
		if log.Records[i].Record == "run.started" {
			started = &log.Records[i]
		}
	}
	if started == nil {
		t.Fatal("the log has no run.started record")
	}
	if started.Trigger != "" {
		t.Errorf("the old log's trigger is %q, want the empty string", started.Trigger)
	}

	for _, r := range log.Records {
		if r.Record != "message" {
			continue
		}
		if r.MessageID != "" {
			t.Errorf("the old log's message id is %q, want the empty string", r.MessageID)
		}
		if r.DeliveryNote != nil {
			t.Errorf("the old log carries a delivery note: %+v", r.DeliveryNote)
		}
	}

	// The metrics still work on an old log: the transcript is what the
	// judge reads.
	if got := log.FinalMessage(); got != "the answer" {
		t.Errorf("the final message is %q, want the answer", got)
	}
	if got := log.ContextLimit(); got != 32768 {
		t.Errorf("the context limit is %d, want 32768", got)
	}
	if got := log.Turns(); got != 1 {
		t.Errorf("the run made %d turns, want 1", got)
	}
}

// TestTheParserReadsANewLog covers a log written by the current core: the
// new fields are read, and the transcript is unchanged.
func TestTheParserReadsANewLog(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s","context_limit":32768}`,
		`{"record":"run.started","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","prompt_hash":"abc","started_at":"2026-10-20T00:00:00Z","model_id":"m","max_turns":8,"tools_mode":"native","trigger":"prompt"}`,
		`{"record":"message","seq":3,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":0,"origin":"user","role":"user","content":[{"kind":"text","text":"the prompt"}]}`,
		`{"record":"message","seq":4,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","message_id":"m1","content":[{"kind":"text","text":"the answer"}]}`,
		`{"record":"run.settled","seq":5,"ts":"2026-10-20T00:00:01Z","run_id":"r","reason":"end_turn","detail":{"source":"end_turn"},"turns":1,"settled_at":"2026-10-20T00:00:01Z"}`,
	)

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}

	var started, model *LogRecord
	for i := range log.Records {
		switch log.Records[i].Record {
		case "run.started":
			started = &log.Records[i]
		case "message":
			if log.Records[i].Origin == "model" {
				model = &log.Records[i]
			}
		}
	}
	if started == nil {
		t.Fatal("the log has no run.started record")
	}
	if started.Trigger != "prompt" {
		t.Errorf("the trigger is %q, want prompt", started.Trigger)
	}
	if model == nil {
		t.Fatal("the log has no model message")
	}
	if model.MessageID != "m1" {
		t.Errorf("the message id is %q, want m1", model.MessageID)
	}
	if got := log.FinalMessage(); got != "the answer" {
		t.Errorf("the final message is %q, want the answer", got)
	}
}

// TestTheParserReadsADeliveryNote covers the note: it decodes, and the
// note's own fields are there.
func TestTheParserReadsADeliveryNote(t *testing.T) {
	path := writeLog(t,
		`{"record":"message","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":0,"origin":"user","role":"user","content":[{"kind":"text","text":"the prompt"}],"delivery_note":{"message_id":"m1","upto_chars":42,"text":"[The user saw or heard your reply m1 only up to: \"...\"]"}}`,
	)

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	note := log.Records[0].DeliveryNote
	if note == nil {
		t.Fatal("the message carries no delivery note")
	}
	if note.MessageID != "m1" {
		t.Errorf("the note names %q, want m1", note.MessageID)
	}
	if note.UpToChars != 42 {
		t.Errorf("the note's character count is %d, want 42", note.UpToChars)
	}
	if note.Text == "" {
		t.Error("the note's text is empty")
	}
}

// TestTheParserReadsAProviderBlock covers the opaque block: it decodes
// without the core interpreting it.
func TestTheParserReadsAProviderBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	line := `{"record":"message","seq":1,"ts":"2026-10-20T00:00:00Z","run_id":"r","turn":1,"origin":"model","role":"assistant","content":[{"kind":"provider","provider":{"api":"responses","type":"compaction","data":{"encrypted_content":"abc"},"required":true}}]}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("writing the log: %v", err)
	}

	log, err := ParseLog(path)
	if err != nil {
		t.Fatalf("parsing the log: %v", err)
	}
	if len(log.Records) != 1 {
		t.Fatalf("the log holds %d records, want 1", len(log.Records))
	}
	if len(log.Records[0].Content) != 1 {
		t.Fatalf("the message holds %d blocks, want 1", len(log.Records[0].Content))
	}
	if log.Records[0].Content[0].Kind != "provider" {
		t.Errorf("the block's kind is %q, want provider", log.Records[0].Content[0].Kind)
	}
}
