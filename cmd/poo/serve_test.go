package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The serve command speaks the protocol on stdin and stdout.
func TestServeSpeaksTheProtocol(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()

	stdin, stdout := os.Stdin, os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("the input pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("the output pipe: %v", err)
	}
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = stdin, stdout
	})

	codes := make(chan int, 1)
	go func() {
		codes <- runServe([]string{
			"--endpoint", endpoint.URL,
			"--model-id", "test-model",
			"--context-limit", "8192",
			"--session-dir", filepath.Join(dir, "sessions"),
		})
	}()

	reader := bufio.NewReader(outR)
	write := func(msg any) {
		t.Helper()
		line, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("encoding the message: %v", err)
		}
		if _, err := inW.Write(append(line, '\n')); err != nil {
			t.Fatalf("writing to the server: %v", err)
		}
	}
	// The server sends notifications as it goes, so the answers are
	// the messages that carry an id.
	read := func() map[string]any {
		t.Helper()
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatalf("reading the answer: %v", err)
			}
			var msg map[string]any
			if err := json.Unmarshal(line, &msg); err != nil {
				t.Fatalf("decoding the answer: %v", err)
			}
			if _, ok := msg["id"]; !ok {
				continue
			}
			return msg
		}
	}

	write(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "poo/initialize",
		"params": map[string]any{
			"protocol_versions": []int{1},
			"client":            map[string]any{"name": "test", "version": "test"},
		},
	})
	answer := read()
	result, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("the handshake was refused: %+v", answer)
	}
	if result["protocol_version"] != float64(1) {
		t.Errorf("the protocol version = %v, want 1", result["protocol_version"])
	}

	write(map[string]any{
		"jsonrpc": "2.0",
		"id":      "2",
		"method":  "session/create",
		"params":  map[string]any{"working_dir": dir},
	})
	answer = read()
	created, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("the session was refused: %+v", answer)
	}
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("the session has no id: %+v", created)
	}

	write(map[string]any{
		"jsonrpc": "2.0",
		"id":      "3",
		"method":  "run/prompt",
		"params":  map[string]any{"session_id": sessionID, "text": "hello"},
	})
	answer = read()
	prompted, ok := answer["result"].(map[string]any)
	if !ok {
		t.Fatalf("the prompt was refused: %+v", answer)
	}
	if prompted["run_id"] == "" {
		t.Fatalf("the prompt returned no run id: %+v", prompted)
	}

	// The run settles, and the client hangs up.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("reading the events: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("decoding the event: %v", err)
		}
		params, ok := msg["params"].(map[string]any)
		if !ok {
			continue
		}
		if params["type"] == "run.settled" {
			break
		}
	}
	_ = inW.Close()

	select {
	case code := <-codes:
		if code != 0 {
			t.Errorf("the serve command exited %d, want 0", code)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the serve command did not stop")
	}

	// The session's records are on disk.
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("reading the session directory: %v", err)
	}
	found := false
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".jsonl" {
			found = true
		}
	}
	if !found {
		t.Errorf("the session directory holds no log: %v", entries)
	}
}
