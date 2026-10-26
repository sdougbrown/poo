package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commands that read the settings and the store: the catalog the
// models come from, and the sessions the store holds.

// theSettings is a settings file with one model in it.
func theSettings(t *testing.T, dir, baseURL string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	body := `version: 1
endpoints:
  test:
    endpoint: ` + baseURL + `
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
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// TestTheModelsCommandListsTheCatalog covers the command: the models
// the settings offer are listed, with their windows and their
// operations.
func TestTheModelsCommandListsTheCatalog(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")

	out := captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings}); code != 0 {
			t.Errorf("poo models exited with %d", code)
		}
	})
	if !strings.Contains(out, "test-model") {
		t.Errorf("the listing is %q, want it to name the model", out)
	}
	if !strings.Contains(out, "chat") {
		t.Errorf("the listing is %q, want it to name the operations", out)
	}
}

// TestTheModelsCommandPrintsJSON covers the flag: the models are
// printed as JSON, and a consumer reads them.
func TestTheModelsCommandPrintsJSON(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")

	out := captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings, "--json"}); code != 0 {
			t.Errorf("poo models exited with %d", code)
		}
	})
	var models []map[string]any
	if err := json.Unmarshal([]byte(out), &models); err != nil {
		t.Fatalf("decoding the listing: %v\n%s", err, out)
	}
	found := false
	for _, model := range models {
		if model["model_id"] == "test-model" {
			found = true
		}
	}
	if !found {
		t.Errorf("the listing does not hold the model the settings name")
	}
}

// TestTheModelsCommandExplainsAModel covers the flag: the explanation
// says which layer set each field.
func TestTheModelsCommandExplainsAModel(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")

	out := captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings, "--explain", "test-model"}); code != 0 {
			t.Errorf("poo models exited with %d", code)
		}
	})
	if !strings.Contains(out, "model_id") {
		t.Errorf("the explanation is %q, want it to name the fields", out)
	}
	if !strings.Contains(out, "settings") {
		t.Errorf("the explanation is %q, want it to name the layer", out)
	}
}

// TestThePruneWithoutAPolicyFails covers the opt-in rule: retention is
// opt-in, and a prune with no policy says so and does nothing.
func TestThePruneWithoutAPolicyFails(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://localhost:4000/v1")

	code, stderr := runCLI(t, "sessions", "prune", "--settings", settings, "--session-dir", dir)
	if code != 2 {
		t.Errorf("the exit code is %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "retention") {
		t.Errorf("stderr is %q, want it to name the retention policy", stderr)
	}
}

// TestThePruneDeletesTheOldSessions covers the prune: the sessions the
// policy marks are deleted, and a dry run deletes nothing.
func TestThePruneDeletesTheOldSessions(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
	settings := theSettings(t, dir, endpoint.URL+"/v1")
	sessionDir := filepath.Join(dir, "sessions")

	// Two sessions the store holds.
	for i := 0; i < 2; i++ {
		code, stderr := runCLI(t,
			"run",
			"--settings", settings,
			"--working-dir", dir,
			"--session-dir", sessionDir,
			"say hello",
		)
		if code != 0 {
			t.Fatalf("the run exited with %d: %s", code, stderr)
		}
	}

	out := captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--keep", "1", "--dry-run"}); code != 0 {
			t.Errorf("poo sessions prune exited with %d", code)
		}
	})
	if !strings.Contains(out, "would delete") {
		t.Errorf("the dry run says %q, want it to name the sessions", out)
	}
	files, err := filepath.Glob(filepath.Join(sessionDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("looking for the sessions: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("the store holds %d sessions, want 2: the dry run deleted one", len(files))
	}

	out = captureStdout(t, func() {
		if code := run([]string{"sessions", "prune", "--settings", settings, "--session-dir", sessionDir, "--keep", "1"}); code != 0 {
			t.Errorf("poo sessions prune exited with %d", code)
		}
	})
	if !strings.Contains(out, "deleted") {
		t.Errorf("the prune says %q, want it to name the deletions", out)
	}
	files, err = filepath.Glob(filepath.Join(sessionDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("looking for the sessions: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("the store holds %d sessions, want 1", len(files))
	}
}

// TestTheSessionsListNamesTheSessions covers the listing: the store's
// sessions are listed with their ids.
func TestTheSessionsListNamesTheSessions(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
	settings := theSettings(t, dir, endpoint.URL+"/v1")
	sessionDir := filepath.Join(dir, "sessions")

	code, stderr := runCLI(t,
		"run",
		"--settings", settings,
		"--working-dir", dir,
		"--session-dir", sessionDir,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the run exited with %d: %s", code, stderr)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"sessions", "list", "--settings", settings, "--session-dir", sessionDir}); code != 0 {
			t.Errorf("poo sessions list exited with %d", code)
		}
	})
	if !strings.Contains(out, "ID") {
		t.Errorf("the listing is %q, want it to have the columns", out)
	}
}

// captureStdout runs a function and returns what it wrote to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	fn()
	w.Close()
	os.Stdout = old
	out := <-done
	r.Close()
	return out
}

// TestTheFinishResultStaysOnStdout covers the typed result: the
// schema the settings name gives the session the finish tool, and the
// result the model produces is what the client reads on stdout.
func TestTheFinishResultStaysOnStdout(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "result.schema.json")
	schema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatalf("writing the schema: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.yaml")
	settings := "version: 1\nfinish:\n  schema_file: " + schemaPath + "\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	call := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"finish","arguments":"{\"result\":{\"answer\":\"the result\"}}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{call, answer("done")})

	code, stderr, stdout := runCLIWithStdout(t,
		"run",
		"--settings", settingsPath,
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"finish it",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "the result") {
		t.Errorf("stdout is %q, want it to hold the result", stdout)
	}
}

// TestTheFinishResultStaysOnStdoutAfterAWokenReply covers the stdout
// rule: the client asked for a run, and the finish result of that run
// is what it reads. A run that a job woke later does not replace it.
func TestTheFinishResultStaysOnStdoutAfterAWokenReply(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "result.schema.json")
	schema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatalf("writing the schema: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.yaml")
	settings := "version: 1\nfinish:\n  schema_file: " + schemaPath + "\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	job := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo done\",\"background\":true}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	finish := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"finish","arguments":"{\"result\":{\"answer\":\"the result\"}}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpointSequence(t, []string{job, finish, answer("the woken answer")})

	code, stderr, stdout := runCLIWithStdout(t,
		"run",
		"--settings", settingsPath,
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", filepath.Join(dir, "run.jsonl"),
		"start a job and finish",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "the result") {
		t.Errorf("stdout is %q, want it to hold the result", stdout)
	}
	if strings.Contains(stdout, "the woken answer") {
		t.Errorf("stdout is %q, want the woken run's answer left out", stdout)
	}
}

// TestTheEphemeralModeWritesNothing covers the mode: an ephemeral
// session leaves no file behind.
func TestTheEphemeralModeWritesNothing(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, "data: "+`{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")

	code, stderr := runCLI(t,
		"run",
		"--ephemeral",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--session-dir", dir,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v, want nothing", names)
	}
}
