package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/catalog"
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
	settings := theSettings(t, dir, "http://127.0.0.1:1/v1")

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

// theProbingServer is a live endpoint that remembers the names its
// probes carried. The models tests share it, so the probe route and
// its answer have one owner.
func theProbingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var agents []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"test-model"}]}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), agents...)
	}
}

// assertProbesNamed fails when a probe arrived under a name the binary
// does not carry.
func assertProbesNamed(t *testing.T, agents []string) {
	t.Helper()
	for _, agent := range agents {
		if !strings.HasPrefix(agent, "poo/") {
			t.Errorf("a probe named itself %q, want the binary's own name", agent)
		}
	}
}

// TestTheModelsCommandNamesTheClient covers the name the command's probes
// carry: the same binary asks the endpoints what they serve, so the same
// name answers for them.
func TestTheModelsCommandNamesTheClient(t *testing.T) {
	server, probed := theProbingServer(t)
	dir := t.TempDir()
	settings := theSettings(t, dir, server.URL+"/v1")
	captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings}); code != 0 {
			t.Errorf("poo models exited with %d", code)
		}
	})
	agents := probed()
	if len(agents) == 0 {
		t.Fatal("the command never probed the endpoint")
	}
	assertProbesNamed(t, agents)
}

// TestTheModelsCommandAsksEvenWhenTheCacheIsFresh covers the command's
// own ask: the endpoints are asked again even when the discovery cache
// holds a fresh result, the way the wire's models/list reprobe does.
// The names test caught this only in full-package runs and -count=2,
// where an earlier probe had freshened the cache the whole process
// shares.
func TestTheModelsCommandAsksEvenWhenTheCacheIsFresh(t *testing.T) {
	server, probed := theProbingServer(t)
	dir := t.TempDir()
	settings := theSettings(t, dir, server.URL+"/v1")
	// Two runs over one endpoint: the first freshens the shared cache,
	// and the second must ask anyway.
	for round := 0; round < 2; round++ {
		captureStdout(t, func() {
			if code := run([]string{"models", "--settings", settings}); code != 0 {
				t.Errorf("poo models exited with %d", code)
			}
		})
	}
	if got := len(probed()); got != 2 {
		t.Fatalf("the command probed %d times in two runs, want the ask made again each time", got)
	}
	assertProbesNamed(t, probed())
}

// TestTheModelsCommandPrintsJSON covers the flag: the models are
// printed as JSON, and a consumer reads them.
func TestTheModelsCommandPrintsJSON(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://127.0.0.1:1/v1")

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
	settings := theSettings(t, dir, "http://127.0.0.1:1/v1")

	out := captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings, "--explain", "test-model"}); code != 0 {
			t.Errorf("poo models exited with %d", code)
		}
	})
	// The layer a field carries is its row's last cell, so the
	// assertion is per field: one row misattributed to another layer
	// fails, however many rows still say settings.
	for _, field := range []string{"model_id", "context_limit"} {
		if layer, ok := layerFor(out, field); !ok || layer != "settings" {
			t.Errorf("the explanation is %q, want %q set by the settings layer", out, field)
		}
	}
}

// layerFor returns the layer the explanation's row for the field
// names.
func layerFor(out, field string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		cells := strings.Fields(line)
		if len(cells) == 3 && cells[0] == field {
			return cells[2], true
		}
	}
	return "", false
}

// TestThePruneWithoutAPolicyFails covers the opt-in rule: retention is
// opt-in, and a prune with no policy says so and does nothing.
func TestThePruneWithoutAPolicyFails(t *testing.T) {
	dir := t.TempDir()
	settings := theSettings(t, dir, "http://127.0.0.1:1/v1")

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
	schema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`
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
	schema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`
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

// TestTheModelsCommandExplainsTheModelsFile covers the layer the catalog reads
// below the settings: a model the models file declares is listed and explained,
// and its fields are attributed to the file.
func TestTheModelsCommandExplainsTheModelsFile(t *testing.T) {
	// The user's config directory is the settings' last resort, and the
	// credential file's default: a directory of the test's own keeps both out
	// of the developer's own config.
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	t.Setenv("APPDATA", configDir)

	dir := t.TempDir()
	models := filepath.Join(dir, config.ModelsFileName)
	if err := os.WriteFile(models, []byte(`endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    models:
      qwen3:
        context_limit: 32768
`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte(`version: 1
models_file: models.yaml
session:
  mode: ephemeral
roles:
  chat: home/qwen3
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"models", "--settings", settings, "--explain", "home/qwen3"}); code != 0 {
			t.Errorf("poo models --explain exited with %d", code)
		}
	})
	if !strings.Contains(out, "models_file") {
		t.Errorf("the explanation is %q, want it to name the models file's layer", out)
	}
	if !strings.Contains(out, "32768") {
		t.Errorf("the explanation is %q, want it to show the file's context limit", out)
	}
}

// TestTheRolesCommandResolvesEachRole covers the check: a role is printed with
// the model, the provider and where the key comes from, and no key value
// appears.
func TestTheRolesCommandResolvesEachRole(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	t.Setenv("APPDATA", configDir)
	t.Setenv("POO_ROLES_KEY", "the-key-nobody-should-see")

	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte(`version: 1
endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
    api_key_env: POO_ROLES_KEY
models:
  home/qwen3:
    endpoint: home
    model_id: qwen3
    ops: [chat]
roles:
  chat: home/qwen3
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	out := captureStdout(t, func() {
		if code := run([]string{"roles", "--settings", settings}); code != 0 {
			t.Errorf("poo roles exited with %d", code)
		}
	})
	for _, want := range []string{"ROLE", "chat", "home/qwen3", "home", "env:POO_ROLES_KEY"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is %q, want it to name %q", out, want)
		}
	}
	if strings.Contains(out, "the-key-nobody-should-see") {
		t.Errorf("the report carries the key: %q", out)
	}
	// A role the settings do not assign is listed with no model, no provider and no
	// credential: a session does without it.
	if row := rowOf(out, "decider"); row != "decider - - -" {
		t.Errorf("the unassigned role's row is %q, want it empty", row)
	}
}

// rowOf returns the table row a role is printed on, its cells joined by single spaces.
func rowOf(out, role string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != role {
			continue
		}
		return strings.Join(fields, " ")
	}
	return ""
}

// TestTheRolesCommandFailsOnARoleThatDoesNotResolve covers the exit code a check
// is read by: a role whose model is not in the catalog fails, and the message
// names the names that would have served it.
func TestTheRolesCommandFailsOnARoleThatDoesNotResolve(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	t.Setenv("APPDATA", configDir)

	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte(`version: 1
endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
models:
  home/qwen3:
    endpoint: home
    model_id: qwen3
roles:
  chat: qwen3
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	code, stderr, out := runCLIWithStdout(t, "roles", "--settings", settings)
	if code == 0 {
		t.Errorf("poo roles exited 0 for a model that is not in the catalog")
	}
	if !strings.Contains(stderr, "home/qwen3") {
		t.Errorf("the failure is %q, want it to name the model that would have served the role", stderr)
	}
	// The row says what the settings wrote, and claims no provider or credential for a
	// model that did not resolve.
	if row := rowOf(out, "chat"); row != "chat qwen3 - -" {
		t.Errorf("the failed role's row is %q, want the name as written and no provider", row)
	}
}

// TestTheRolesCommandFailsOnAModelThatDoesNotServeTheRole covers the half of the check
// that a name lookup misses: the model is in the catalog, and the role needs an
// operation it does not serve, so a session would refuse it. The command must refuse
// too, or it is not the check it says it is.
func TestTheRolesCommandFailsOnAModelThatDoesNotServeTheRole(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)
	t.Setenv("APPDATA", configDir)

	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte(`version: 1
endpoints:
  home:
    endpoint: http://127.0.0.1:1/v1
models:
  home/qwen3:
    endpoint: home
    model_id: qwen3
    ops: [systemone]
roles:
  chat: home/qwen3
`), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	code, stderr, out := runCLIWithStdout(t, "roles", "--settings", settings)
	if code == 0 {
		t.Error("poo roles exited 0 for a model that cannot serve the role")
	}
	if !strings.Contains(stderr, "chat") || !strings.Contains(stderr, "does not serve") {
		t.Errorf("the failure is %q, want it to name the operation the role needs", stderr)
	}
	if row := rowOf(out, "chat"); row != "chat home/qwen3 - -" {
		t.Errorf("the failed role's row is %q, want the name as written and no provider", row)
	}
}

// TestTheRoleCredentialSourceIsRendered covers the three sources a key has. Only one of
// them was exercised by the command's own test, so a regression in the others would show
// the wrong source with nothing to catch it.
func TestTheRoleCredentialSourceIsRendered(t *testing.T) {
	for _, c := range []struct {
		name       string
		credential catalog.Credential
		want       string
	}{
		{name: "the environment", credential: catalog.Credential{Source: catalog.CredentialEnv, Env: "HOME_KEY"}, want: "env:HOME_KEY"},
		{name: "the settings", credential: catalog.Credential{Source: catalog.CredentialStatic}, want: "static"},
		{name: "none", credential: catalog.Credential{Source: catalog.CredentialNone}, want: "none"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := credential(c.credential); got != c.want {
				t.Errorf("the source renders as %q, want %q", got, c.want)
			}
		})
	}
}
