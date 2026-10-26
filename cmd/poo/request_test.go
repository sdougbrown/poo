package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestTheSettingsPathSendsTheSameRequest covers the move from the
// flags to the settings: the same configuration, named two ways, sends
// the same request.
//
// The flags are the settings the command builds when no settings file
// is given, so a run that names its model on the command line and a
// run that names it in a settings file have to reach the provider with
// the same request.
func TestTheSettingsPathSendsTheSameRequest(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	fromFlags := newFakeEndpoint(t, body)
	flagsDir := t.TempDir()
	flagsCode, flagsStderr := runCLI(t,
		"run",
		"--endpoint", fromFlags.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", flagsDir,
		"--log", filepath.Join(flagsDir, "run.jsonl"),
		"a prompt",
	)
	if flagsCode != 0 {
		t.Fatalf("the flags run exited with %d: %s", flagsCode, flagsStderr)
	}

	fromSettings := newFakeEndpoint(t, body)
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.yaml")
	settings := fmt.Sprintf(`version: 1
endpoints:
  test:
    endpoint: %s/v1
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat]
    context_limit: 32768
roles:
  chat: test-model
`, fromSettings.URL)
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	settingsCode, settingsStderr := runCLI(t,
		"run",
		"--settings", settingsPath,
		"--working-dir", settingsDir,
		"--log", filepath.Join(settingsDir, "run.jsonl"),
		"a prompt",
	)
	if settingsCode != 0 {
		t.Fatalf("the settings run exited with %d: %s", settingsCode, settingsStderr)
	}

	if len(fromFlags.requests) != 1 || len(fromSettings.requests) != 1 {
		t.Fatalf("the endpoints saw %d and %d requests, want 1 each",
			len(fromFlags.requests), len(fromSettings.requests))
	}
	flagsRequest := fromFlags.requests[0]
	settingsRequest := fromSettings.requests[0]
	for _, key := range []string{"model", "messages", "tools", "max_tokens", "stream"} {
		flagsValue := flagsRequest[key]
		settingsValue := settingsRequest[key]
		if fmt.Sprint(flagsValue) != fmt.Sprint(settingsValue) {
			t.Errorf("the %q of the request differs:\nflags:    %v\nsettings: %v",
				key, flagsValue, settingsValue)
		}
	}
}
