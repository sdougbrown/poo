package openai

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestTheDescriptorReasoningBeatsTheQuirk covers a descriptor that names
// both the reasoning toggle and the chat_template_thinking quirk, with
// the reasoning setting off.
//
// shapeReasoning writes chat_template_kwargs.enable_thinking=false, and
// the quirk then writes true over it, because the quirks are applied
// after the descriptor's controls and setRaw merges the leaf.
func TestTheDescriptorReasoningBeatsTheQuirk(t *testing.T) {
	tr := New(Config{
		BaseURL: "http://127.0.0.1:1/v1",
		Model:   "gemma4",
		Info: poo.ModelInfo{
			Name:      "gemma4",
			Reasoning: poo.ReasoningControl{Mode: poo.ReasoningToggle, Param: poo.ReasoningPathToggle},
			Quirks:    []string{QuirkChatTemplateThinking},
		},
		Reasoning: "off",
	})
	body, err := tr.buildBody(poo.ChatRequest{Model: "gemma4", MaxTokens: 16})
	if err != nil {
		t.Fatalf("building the body: %v", err)
	}
	var decoded struct {
		ChatTemplateKwargs struct {
			EnableThinking *bool `json:"enable_thinking"`
		} `json:"chat_template_kwargs"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
	if decoded.ChatTemplateKwargs.EnableThinking == nil {
		t.Fatalf("the body carries no enable_thinking: %s", body)
	}
	if *decoded.ChatTemplateKwargs.EnableThinking {
		t.Errorf("the body asks the model to think although the reasoning setting is off: %s", body)
	}
}
