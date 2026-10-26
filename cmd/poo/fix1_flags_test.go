package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestThePermissionFallbackFlagIsApplied covers the one-shot's
// --permission-fallback flag: a side-effecting call no rule covers is
// decided by the fallback, and the flag overrides the settings' or the
// default fallback for the run.
func TestThePermissionFallbackFlagIsApplied(t *testing.T) {
	// Control: the same configuration named in the settings file
	// reaches the policy.
	controlDir := t.TempDir()
	settingsPath := filepath.Join(controlDir, "settings.yaml")
	settings := "version: 1\npermission:\n  unmatched: ask\n  fallback: allow\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	controlEndpoint := newFakeEndpointSequence(t, []string{fix1BashCall(), fix1DoneAnswer()})
	controlCode, controlStderr := runCLI(t,
		"run",
		"--settings", settingsPath,
		"--endpoint", controlEndpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", controlDir,
		"--log", filepath.Join(controlDir, "run.jsonl"),
		"run the command",
	)
	if controlCode != 0 {
		t.Fatalf("the control run exited with %d: %s", controlCode, controlStderr)
	}
	if outcome := fix1ToolOutcome(t, controlDir); outcome != "ok" {
		t.Fatalf("the control outcome = %q, want ok", outcome)
	}

	// The flag: the same run with --permission-fallback allow.
	dir := t.TempDir()
	endpoint := newFakeEndpointSequence(t, []string{fix1BashCall(), fix1DoneAnswer()})
	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--permission-unmatched", "ask",
		"--permission-fallback", "allow",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"run the command",
	)
	if code != 0 {
		t.Fatalf("the run exited with %d: %s", code, stderr)
	}
	if outcome := fix1ToolOutcome(t, dir); outcome != "ok" {
		t.Errorf("the tool's outcome = %q, want ok: the flag allows the call", outcome)
	}
	if !strings.Contains(stderr, `falls back to "allow"`) {
		t.Errorf("stderr is %q, want the warning to name the allow fallback", stderr)
	}
}

func fix1BashCall() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo allowed\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func fix1DoneAnswer() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func fix1ToolOutcome(t *testing.T, dir string) string {
	t.Helper()
	outcome := ""
	for _, rec := range readLog(t, filepath.Join(dir, "run.jsonl")) {
		if rec["record"] == "tool.result" {
			outcome, _ = rec["outcome"].(string)
		}
	}
	return outcome
}
