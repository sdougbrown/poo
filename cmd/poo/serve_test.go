package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestCreateAppliesThePromptOverrides covers session/create: the params
// carry prompt_overrides, and the session's prompt holds the client's
// section from the first run on.
func TestCreateAppliesThePromptOverrides(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, doneAnswer())
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{
		"working_dir": dir,
		"prompt_overrides": map[string]any{
			"sections": map[string]any{"house": "the house style"},
		},
	})
	created := result(t, answer)
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("the session has no id: %v", created)
	}

	answer = client.call(t, "session/get", map[string]any{"session_id": sessionID})
	state := result(t, answer)
	prompt, _ := state["prompt"].(map[string]any)
	text, _ := prompt["text"].(string)
	if !strings.Contains(text, "the house style") {
		t.Errorf("the prompt is %q, want the client's section from session/create", text)
	}
}

// TestCreateAndConfigureReportTheSessionsModels covers the result of
// session/create and session/configure: they name the session's own
// models, not the default runtime's.
func TestCreateAndConfigureReportTheSessionsModels(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, doneAnswer())
	settings := filepath.Join(dir, "settings.json")
	body := `{"version":1,
		"endpoints":{"test":{"endpoint":"` + endpoint.URL + `/v1"}},
		"models":{
			"first-model":{"endpoint":"test","model_id":"first-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768},
			"second-model":{"endpoint":"test","model_id":"second-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768},
			"decide-model":{"endpoint":"test","model_id":"decide-model","api":"chat_completions","tools_mode":"native","ops":["chat","systemone"],"context_limit":32768}},
		"roles":{"chat":"first-model"},
		"profile":"code"}`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{
		"working_dir": dir,
		"models":      map[string]any{"chat": "second-model", "decider": "decide-model"},
	})
	created := result(t, answer)
	sessionID, _ := created["session_id"].(string)
	models, _ := created["models"].(map[string]any)
	if models["chat"] != "second-model" {
		t.Errorf("the create result's chat model is %v, want second-model", models["chat"])
	}
	if models["decider"] != "decide-model" {
		t.Errorf("the create result's decider model is %v, want decide-model", models["decider"])
	}

	answer = client.call(t, "session/configure", map[string]any{"session_id": sessionID})
	configured := result(t, answer)
	models, _ = configured["models"].(map[string]any)
	if models["chat"] != "second-model" || models["decider"] != "decide-model" {
		t.Errorf("the configure result names %v, want the session's own models", models)
	}
}

// TestCreateRejectsAnUnknownDeciderModel covers the models rule: a
// role that names no settings model is refused as unknown_model.
func TestCreateRejectsAnUnknownDeciderModel(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, doneAnswer())
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{
		"working_dir": dir,
		"models":      map[string]any{"decider": "no-such-model"},
	})
	failure, _ := answer["error"].(map[string]any)
	data, _ := failure["data"].(map[string]any)
	if data["code"] != "unknown_model" {
		t.Errorf("the failure is %v, want unknown_model", failure)
	}
}

// TestJobsCapabilityFollowsTheTools covers capabilities.jobs: it says
// whether a tool may run in the background, so the context strategy
// does not change it.
func TestJobsCapabilityFollowsTheTools(t *testing.T) {
	for _, strategy := range []string{"none", "handoff"} {
		t.Run(strategy, func(t *testing.T) {
			dir := t.TempDir()
			endpoint := newFakeEndpoint(t, doneAnswer())
			settings := filepath.Join(dir, "settings.json")
			body := `{"version":1,
				"endpoints":{"test":{"endpoint":"` + endpoint.URL + `/v1"}},
				"models":{"test-model":{"endpoint":"test","model_id":"test-model","api":"chat_completions","tools_mode":"native","ops":["chat"],"context_limit":32768}},
				"roles":{"chat":"test-model"},
				"profile":"code",
				"context":{"strategy":"` + strategy + `"}}`
			if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
				t.Fatalf("writing the settings: %v", err)
			}
			client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
			answer := client.call(t, "poo/initialize", map[string]any{
				"protocol_versions": []int{1},
				"client":            map[string]any{"name": "test", "version": "test"},
			})
			handshake := result(t, answer)
			capabilities, _ := handshake["capabilities"].(map[string]any)
			if jobs, _ := capabilities["jobs"].(bool); !jobs {
				t.Errorf("capabilities.jobs is %v; the code profile's bash may run in the background", capabilities["jobs"])
			}
		})
	}
}

// TestSettingsGetReportsTheEffectiveSettings covers settings/get: it
// returns the settings the command runs with, so a flag is in the
// document.
func TestSettingsGetReportsTheEffectiveSettings(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, doneAnswer())
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t,
		"--settings", settings,
		"--profile", "search",
		"--permission-unmatched", "deny",
		"--session-dir", filepath.Join(dir, "sessions"),
	)
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "settings/get", map[string]any{})
	effective := result(t, answer)
	if effective["profile"] != "search" {
		t.Errorf("settings/get reports the profile %v, want the effective search", effective["profile"])
	}
	permission, _ := effective["permission"].(map[string]any)
	if permission["unmatched"] != "deny" {
		t.Errorf("settings/get reports permission.unmatched %v, want the effective deny", permission["unmatched"])
	}
	session, _ := effective["session"].(map[string]any)
	if session["dir"] != filepath.Join(dir, "sessions") {
		t.Errorf("settings/get reports the session dir %v, want the effective one", session["dir"])
	}
}

// TestConfigureRejectsAnUnknownDeciderModel covers the models rule on
// session/configure: a decider the settings do not name is refused the
// same way a chat model is.
func TestConfigureRejectsAnUnknownDeciderModel(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, doneAnswer())
	settings := theSettings(t, dir, endpoint.URL+"/v1")

	client := startServe(t, "--settings", settings, "--session-dir", filepath.Join(dir, "sessions"))
	client.call(t, "poo/initialize", map[string]any{
		"protocol_versions": []int{1},
		"client":            map[string]any{"name": "test", "version": "test"},
	})
	answer := client.call(t, "session/create", map[string]any{"working_dir": dir})
	created := result(t, answer)
	sessionID, _ := created["session_id"].(string)
	answer = client.call(t, "session/configure", map[string]any{
		"session_id": sessionID,
		"models":     map[string]any{"decider": "no-such-model"},
	})
	failure, _ := answer["error"].(map[string]any)
	data, _ := failure["data"].(map[string]any)
	if data["code"] != "unknown_model" {
		t.Errorf("the failure is %v, want unknown_model", failure)
	}
}
