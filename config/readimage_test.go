package config_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// TestTheReadToolFollowsTheModelThatReadsImages covers the wiring: the tool
// a session builds is told whether the model reads attachments, so a text-only
// model is never offered a picture it cannot see.
func TestTheReadToolFollowsTheModelThatReadsImages(t *testing.T) {
	tests := []struct {
		name    string
		inputs  string
		offered bool
	}{
		{name: "a model that reads attachments", inputs: "    inputs: [text, image]\n", offered: true},
		{name: "a model that reads text", inputs: "    inputs: [text]\n", offered: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat]
    context_limit: 32768
` + tt.inputs + `roles:
  chat: test-model
`
			settings, err := config.Parse([]byte(body), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			runtime := build(t, settings, config.Overrides{})

			tool, ok := runtime.Options.Tools["read"]
			if !ok {
				t.Fatal("the session offers no read tool")
			}
			description := tool.Spec().Description
			if got := strings.Contains(description, "image"); got != tt.offered {
				t.Errorf("the read description is %q, and attachments are offered: %v, want %v", description, got, tt.offered)
			}
		})
	}
}

// TestAModelKeepsTheQuirksTheSettingsName covers the quirk path: a model the
// settings give a transport quirk carries it into the resolved model, where the
// transport reads it.
func TestAModelKeepsTheQuirksTheSettingsName(t *testing.T) {
	body := `
version: 1
endpoints:
  rusty:
    endpoint: http://rusty:8091/v1
models:
  gemma:
    endpoint: rusty
    model_id: gemma4-12b
    api: chat_completions
    tools_mode: native
    ops: [chat]
    inputs: [text, audio]
    context_limit: 8192
    quirks: [audio_url]
roles:
  chat: gemma
`
	settings, err := config.Parse([]byte(body), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if len(model.Quirks) != 1 || model.Quirks[0] != "audio_url" {
		t.Errorf("the model's quirks are %v, want [audio_url]", model.Quirks)
	}
	if !model.Reads(poo.ModalityAudio) {
		t.Errorf("the model reads %v, want audio", model.Inputs)
	}
}
