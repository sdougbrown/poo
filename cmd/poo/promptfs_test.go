package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The read-only conformance binary composes poo the way a custom binary
// does, and it brings a prompt directory of its own. The test proves the
// directory reaches the prompt a session reports, not only the binary's
// build.

// TestTheReadOnlyBinaryPromptIsItsOwn covers the rule that a binary's
// prompt directory replaces the embedded default: the session the
// read-only binary opens reports the base it embeds.
func TestTheReadOnlyBinaryPromptIsItsOwn(t *testing.T) {
	binary := build(t, filepath.Join("..", "..", "wire", "conformance", "testdata", "readonly"), nil)
	settings := writeReadOnlySettings(t)

	cmd := exec.Command(binary, "serve", "--settings", settings)
	cmd.Env = append(os.Environ(), "POO_READONLY_ID=prompt-test")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("opening the binary's stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the binary's stdout: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("the binary's stderr:\n%s", stderr.String())
		}
	})

	lines := bufio.NewScanner(stdout)
	wireCall(t, stdin, lines, "1", "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "prompt-test", "version": "1"},
	})
	created := wireCall(t, stdin, lines, "2", "session/create", map[string]any{
		"working_dir": t.TempDir(),
		"profile":     "search",
		"mode":        "ephemeral",
	})
	var session struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(created, &session); err != nil {
		t.Fatalf("decoding the session: %v", err)
	}
	got := wireCall(t, stdin, lines, "3", "session/get", map[string]any{
		"session_id": session.SessionID,
	})
	var state struct {
		Prompt struct {
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if err := json.Unmarshal(got, &state); err != nil {
		t.Fatalf("decoding the session state: %v", err)
	}
	if !strings.Contains(state.Prompt.Text, "You are poo-readonly") {
		t.Errorf("the session's prompt is not the binary's own:\n%s", state.Prompt.Text)
	}
	if strings.Contains(state.Prompt.Text, "You are a coding agent.") {
		t.Errorf("the session's prompt is still the embedded base:\n%s", state.Prompt.Text)
	}
}

// wireCall sends one request and returns the matching result.
//
// The server sends notifications as well, so the test reads the stream
// until the answer with the request's id arrives.
func wireCall(t *testing.T, stdin interface{ Write([]byte) (int, error) }, lines *bufio.Scanner, id, method string, params any) json.RawMessage {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("encoding %s: %v", method, err)
	}
	if _, err := stdin.Write(append(request, '\n')); err != nil {
		t.Fatalf("writing %s: %v", method, err)
	}
	for lines.Scan() {
		var message struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(lines.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			t.Fatalf("%s failed: %s", method, message.Error.Message)
		}
		return message.Result
	}
	t.Fatalf("the binary closed the stream before answering %s: %v", method, lines.Err())
	return nil
}

// writeReadOnlySettings writes a settings file for the read-only binary.
//
// The endpoint is never called: the test opens a session and reads its
// prompt, and a preview prompt needs no model.
func writeReadOnlySettings(t *testing.T) string {
	t.Helper()
	body := "version: 1\n" +
		"endpoints:\n" +
		"  test:\n" +
		"    endpoint: http://127.0.0.1:1/v1\n" +
		"models:\n" +
		"  test-model:\n" +
		"    endpoint: test\n" +
		"    model_id: test-model\n" +
		"    api: chat_completions\n" +
		"    tools_mode: native\n" +
		"    ops: [chat]\n" +
		"    context_limit: 32768\n" +
		"roles:\n" +
		"  chat: test-model\n"
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}
