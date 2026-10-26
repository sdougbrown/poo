package eval

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// The descriptor of a catalog entry.
//
// The catalog entry names the model, and the descriptor is what the
// harness and the harness's transport read.

// TestTheModelDescriptorComesFromTheCatalogEntry covers the descriptor:
// the entry names the model, its window, its tools mode and its
// reasoning control.
func TestTheModelDescriptorComesFromTheCatalogEntry(t *testing.T) {
	m := Model{
		Name:            "gemma4",
		BaseURL:         "http://localhost:4000/v1",
		ModelID:         "gemma4",
		ContextLimit:    32768,
		ToolsMode:       "text",
		ReasoningEffort: "low",
	}
	info := m.ModelInfo()
	if info.Name != "gemma4" {
		t.Errorf("the name is %q, want gemma4", info.Name)
	}
	if info.Endpoint != "http://localhost:4000/v1" {
		t.Errorf("the endpoint is %q, want the entry's base URL", info.Endpoint)
	}
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
	if info.Tools.Mode != poo.ToolsText {
		t.Errorf("the tools mode is %q, want %q", info.Tools.Mode, poo.ToolsText)
	}
	if info.Reasoning.Mode != poo.ReasoningEffort {
		t.Errorf("the reasoning control is %q, want %q", info.Reasoning.Mode, poo.ReasoningEffort)
	}
}

// TestTheModelWithoutAReasoningEffortHasNoReasoningControl covers the
// entry that names no effort: the descriptor names no control either.
func TestTheModelWithoutAReasoningEffortHasNoReasoningControl(t *testing.T) {
	m := Model{Name: "m", BaseURL: "http://x", ModelID: "m", ContextLimit: 32768}
	info := m.ModelInfo()
	if info.Reasoning.Mode != "" {
		t.Errorf("the reasoning control is %q, want none", info.Reasoning.Mode)
	}
	if info.Tools.Mode != poo.ToolsNative {
		t.Errorf("the tools mode is %q, want %q", info.Tools.Mode, poo.ToolsNative)
	}
}
