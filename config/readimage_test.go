package config_test

import (
	"strings"
	"testing"

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
