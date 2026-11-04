package main

import (
	"flag"
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
		"--endpoint", endpoint.URL+"/v1",
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
		"--endpoint", endpoint.URL+"/v1",
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

// TestAnEmptyManifestPathFlagIsSkipped covers the manifest-path flags: an
// empty value, such as an unset variable expanded on the command line,
// adds no path, so it never resolves to the working directory.
func TestAnEmptyManifestPathFlagIsSkipped(t *testing.T) {
	var f sharedFlags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	f.register(fs)
	if err := fs.Parse([]string{"--feeds-path", "", "--feeds-path", "feeds", "--cli-registry-path", ""}); err != nil {
		t.Fatalf("parsing the flags: %v", err)
	}
	overrides, err := f.overrides()
	if err != nil {
		t.Fatalf("reading the overrides: %v", err)
	}
	if got := overrides.FeedsPaths; len(got) != 1 || got[0] != "feeds" {
		t.Errorf("the feed paths are %q, want only %q", got, "feeds")
	}
	if got := overrides.CLIRegistryPaths; len(got) != 0 {
		t.Errorf("the registry paths are %q, want none", got)
	}
}
