package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// modelFromFlags returns the model the flags describe.
//
// The flags are the settings the command builds when no settings file
// is given, so the model they name is the model the catalog resolves.
func modelFromFlags(t *testing.T, o *runOptions) poo.ModelInfo {
	t.Helper()
	settings := &config.Settings{Version: 1}
	if err := o.apply(settings); err != nil {
		t.Fatalf("applying the flags: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return modelWithFlags(runtime, o)
}

// The model the run talks to.
//
// The flags name the model, and the descriptor they build is what the
// transport and the session read.

// TestTheModelDescriptorComesFromTheFlags covers the descriptor: the
// flags name the model, its window, its ceiling, its tools mode and
// its reasoning control.
func TestTheModelDescriptorComesFromTheFlags(t *testing.T) {
	o := runOptions{
		endpointFlags: endpointFlags{
			endpoint:     "http://localhost:4000/v1",
			modelID:      "gemma4",
			contextLimit: 32768,
			toolsMode:    "text",
		},
		maxOutput:       4096,
		reasoningEffort: "low",
	}
	info := modelFromFlags(t, &o)
	if info.ModelID != "gemma4" {
		t.Errorf("the model is %q, want gemma4", info.ModelID)
	}
	if info.API != poo.APIChatCompletions {
		t.Errorf("the API is %q, want %q", info.API, poo.APIChatCompletions)
	}
	if !info.HasOp(poo.OpChat) {
		t.Errorf("the model does not serve the chat operation: %v", info.Ops)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the window is %d, want 32768", info.ContextLimit)
	}
	if info.MaxOutput != 4096 {
		t.Errorf("the ceiling is %d, want 4096", info.MaxOutput)
	}
	if info.Tools.Mode != poo.ToolsText {
		t.Errorf("the tools mode is %q, want %q", info.Tools.Mode, poo.ToolsText)
	}
	if info.Reasoning.Mode != poo.ReasoningEffort {
		t.Errorf("the reasoning control is %q, want %q", info.Reasoning.Mode, poo.ReasoningEffort)
	}
}

// TestTheModelWithoutAReasoningFlagHasTheFamilyRule covers the model
// that is asked to think by nobody: the flags name no control, and the
// catalog's family rule does. The rule knows how the family thinks,
// and the setting says whether it does.
func TestTheModelWithoutAReasoningFlagHasTheFamilyRule(t *testing.T) {
	o := runOptions{endpointFlags: endpointFlags{endpoint: "http://localhost:4000/v1", modelID: "gemma4", contextLimit: 32768, toolsMode: "native"}}
	info := modelFromFlags(t, &o)
	if info.Reasoning.Mode != poo.ReasoningToggle {
		t.Errorf("the reasoning control is %q, want %q", info.Reasoning.Mode, poo.ReasoningToggle)
	}
}

// TestAModelWithoutAFamilyRuleHasNoReasoningControl covers the model
// the catalog knows nothing about: the descriptor names no control,
// and the request carries no reasoning field.
func TestAModelWithoutAFamilyRuleHasNoReasoningControl(t *testing.T) {
	o := runOptions{endpointFlags: endpointFlags{endpoint: "http://localhost:4000/v1", modelID: "test-model", contextLimit: 32768, toolsMode: "native"}}
	info := modelFromFlags(t, &o)
	if info.Reasoning.Mode != "" {
		t.Errorf("the reasoning control is %q, want none", info.Reasoning.Mode)
	}
}

// TestTheRequestBodyCarriesTheModelAndTheCeiling covers the request
// the run sends: the model, the messages, the tools and the ceiling,
// and nothing the model did not ask for.
func TestTheRequestBodyCarriesTheModelAndTheCeiling(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	code, stderr := runCLI(t, "run",
		"--endpoint", srv.URL,
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--log", filepath.Join(dir, "session.jsonl"),
		"--working-dir", dir,
		"hello",
	)
	if code != 0 {
		t.Fatalf("poo run exited with %d: %s", code, stderr)
	}
	if len(body) == 0 {
		t.Fatal("the endpoint got no request")
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
	keys := make([]string, 0, len(decoded))
	for k := range decoded {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "max_tokens,messages,model,stream,stream_options,tools"
	if strings.Join(keys, ",") != want {
		t.Errorf("the body carries %v, want %s", keys, want)
	}
	var ceiling int
	if err := json.Unmarshal(decoded["max_tokens"], &ceiling); err != nil {
		t.Fatalf("decoding the ceiling: %v", err)
	}
	if ceiling != poo.DefaultMaxOutput {
		t.Errorf("the ceiling is %d, want %d", ceiling, poo.DefaultMaxOutput)
	}
}
