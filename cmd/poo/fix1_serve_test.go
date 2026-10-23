package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateAppliesThePromptOverrides covers session/create: the params
// carry prompt_overrides, and the session's prompt holds the client's
// section from the first run on.
func TestCreateAppliesThePromptOverrides(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, fix1DoneAnswer())
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
	endpoint := newFakeEndpoint(t, fix1DoneAnswer())
	settings := filepath.Join(dir, "settings.json")
	body := `{"version":1,
		"endpoints":{"test":{"base_url":"` + endpoint.URL + `/v1"}},
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
	endpoint := newFakeEndpoint(t, fix1DoneAnswer())
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
			endpoint := newFakeEndpoint(t, fix1DoneAnswer())
			settings := filepath.Join(dir, "settings.json")
			body := `{"version":1,
				"endpoints":{"test":{"base_url":"` + endpoint.URL + `/v1"}},
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

// TestConfigGetReportsTheEffectiveSettings covers config/get: it
// returns the settings the command runs with, so a flag is in the
// document.
func TestConfigGetReportsTheEffectiveSettings(t *testing.T) {
	dir := t.TempDir()
	endpoint := newFakeEndpoint(t, fix1DoneAnswer())
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
	answer := client.call(t, "config/get", map[string]any{})
	effective := result(t, answer)
	if effective["profile"] != "search" {
		t.Errorf("config/get reports the profile %v, want the effective search", effective["profile"])
	}
	permission, _ := effective["permission"].(map[string]any)
	if permission["unmatched"] != "deny" {
		t.Errorf("config/get reports permission.unmatched %v, want the effective deny", permission["unmatched"])
	}
	session, _ := effective["session"].(map[string]any)
	if session["dir"] != filepath.Join(dir, "sessions") {
		t.Errorf("config/get reports the session dir %v, want the effective one", session["dir"])
	}
}
