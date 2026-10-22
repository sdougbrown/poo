package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The command's flags: the profile a run is built under, and the
// action a call no rule covers takes.

// TestTheProfileFlagPicksTheTools covers the flag: the run offers the
// tools the profile names, and the prompt is the profile's.
func TestTheProfileFlagPicksTheTools(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	code, stderr := runCLI(t,
		"run",
		"--profile", "search",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	tools, _ := endpoint.requests[0]["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"read", "grep"} {
		if !names[want] {
			t.Errorf("the request does not offer the %q tool", want)
		}
	}
	for _, unwanted := range []string{"bash", "edit"} {
		if names[unwanted] {
			t.Errorf("the request offers the %q tool, and the search profile does not", unwanted)
		}
	}
}

// TestTheUnmatchedFlagChangesThePolicy covers the flag: the action for
// a call no rule covers is the one the command line names.
//
// The chain has no asker, so a policy that asks falls back, and the
// warning says so.
func TestTheUnmatchedFlagChangesThePolicy(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	code, stderr := runCLI(t,
		"run",
		"--permission-unmatched", "ask",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "falls back") {
		t.Errorf("stderr is %q, want it to warn about the fallback", stderr)
	}
}
