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
)

// The model the run talks to.
//
// The flags name the model, and the descriptor they build is what the
// transport and the session read.

// TestTheModelDescriptorComesFromTheFlags covers the descriptor: the
// flags name the model, its window, its ceiling, its tools mode and
// its reasoning control.
func TestTheModelDescriptorComesFromTheFlags(t *testing.T) {
	o := runOptions{
		modelID:         "gemma4",
		contextLimit:    32768,
		maxOutput:       4096,
		toolsMode:       "text",
		reasoningEffort: "low",
	}
	info := o.modelInfo()
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

// TestTheModelWithoutAReasoningFlagHasNoReasoningControl covers the
// model that is asked to think by nobody: the command knows no control
// to send, so the descriptor names none.
func TestTheModelWithoutAReasoningFlagHasNoReasoningControl(t *testing.T) {
	o := runOptions{modelID: "gemma4", contextLimit: 32768, toolsMode: "native"}
	info := o.modelInfo()
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
		"--base-url", srv.URL,
		"--model-id", "gemma4",
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
