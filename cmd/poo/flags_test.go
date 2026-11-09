package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/session"
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
	controlEndpoint := newFakeEndpointSequence(t, []string{bashCall(), doneAnswer()})
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
	if outcome := toolOutcome(t, controlDir); outcome != "ok" {
		t.Fatalf("the control outcome = %q, want ok", outcome)
	}

	// The flag: the same run with --permission-fallback allow.
	dir := t.TempDir()
	endpoint := newFakeEndpointSequence(t, []string{bashCall(), doneAnswer()})
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
	if outcome := toolOutcome(t, dir); outcome != "ok" {
		t.Errorf("the tool's outcome = %q, want ok: the flag allows the call", outcome)
	}
	if !strings.Contains(stderr, `falls back to "allow"`) {
		t.Errorf("stderr is %q, want the warning to name the allow fallback", stderr)
	}
}

func bashCall() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo allowed\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func doneAnswer() string {
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

func toolOutcome(t *testing.T, dir string) string {
	t.Helper()
	outcome := ""
	for _, rec := range readLog(t, filepath.Join(dir, "run.jsonl")) {
		if rec["record"] == "tool.result" {
			outcome, _ = rec["outcome"].(string)
		}
	}
	return outcome
}

// TestThePruneOlderThanFlagDeletesTheOldSessions covers the CLI
// mapping: --older-than marks a session by its age, and --dry-run
// deletes nothing.
func TestThePruneOlderThanFlagDeletesTheOldSessions(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")
	sessionDir := filepath.Join(dir, "sessions")
	old := time.Now().Add(-48 * time.Hour)
	store, err := session.NewStore(sessionDir, session.WithClock(func() time.Time { return old }))
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID: "old-session",
			Mode:      "logged",
			CreatedAt: old.UTC().Format(time.RFC3339Nano),

			Profile: "code", Model: poo.SessionModel{ModelID: "test-model"}},
	}
	if _, err := store.Create(context.Background(), created); err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1", "--dry-run"}); code != 0 {
			t.Errorf("the dry run exited with %d", code)
		}
	})
	if !strings.Contains(out, "would delete old-session") {
		t.Errorf("the dry run says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); err != nil {
		t.Errorf("the dry run deleted the session: %v", err)
	}

	out = captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--older-than", "1"}); code != 0 {
			t.Errorf("the prune exited with %d", code)
		}
	})
	if !strings.Contains(out, "deleted old-session") {
		t.Errorf("the prune says %q, want it to name old-session", out)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "old-session.jsonl")); !os.IsNotExist(err) {
		t.Errorf("the old session is still there: %v", err)
	}
}
