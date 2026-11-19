package anthropic_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/anthropic"
)

func TestTheSystemPromptTravelsAlone(t *testing.T) {
	msgs := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "be brief"),
		poo.NewTextMessage(poo.RoleSystem, "stay kind"),
		poo.NewTextMessage(poo.RoleUser, "hi"),
	}
	messages, system, err := anthropic.Messages(msgs)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if system != "be brief\n\nstay kind" {
		t.Errorf("the system prompt is %q", system)
	}
	if len(messages) != 1 {
		t.Fatalf("the messages are %d, want the user's alone", len(messages))
	}
}

func TestAToolResultAnswersItsCall(t *testing.T) {
	toolCall := poo.ContentBlock{Kind: poo.ContentToolCall, ToolCallID: "toolu_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)}
	resultBlock0 := poo.ContentBlock{Kind: poo.ContentToolResult, ToolCallID: "toolu_1", Text: "sunny"}
	msgs := []poo.Message{
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{toolCall}},
		{Role: poo.RoleTool, Content: []poo.ContentBlock{resultBlock0}},
	}
	messages, _, err := anthropic.Messages(msgs)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("the messages are %d, want the call and its answer", len(messages))
	}
	call := messages[0].Content[0]
	if call.Type != "tool_use" || call.ID != "toolu_1" || call.Name != "get_weather" {
		t.Errorf("the call block is %+v", call)
	}
	if string(call.Input) != `{"city":"Paris"}` {
		t.Errorf("the call's arguments are %s", call.Input)
	}
	// The result is the user message that answers the call.
	if messages[1].Role != "user" {
		t.Errorf("the result's message role is %q, want user", messages[1].Role)
	}
	result := messages[1].Content[0]
	if result.Type != "tool_result" || result.ToolUseID != "toolu_1" || result.Content != "sunny" {
		t.Errorf("the result block is %+v", result)
	}
	if result.IsError {
		t.Error("the result is marked a failure, want the answer it is")
	}
}

func TestAnAttachmentRidesAsBase64(t *testing.T) {
	msgs := []poo.Message{{Role: poo.RoleUser, Content: []poo.ContentBlock{{
		Kind:      poo.ContentImage,
		MediaType: "image/png",
		Data:      []byte("by"),
	}}}}
	messages, _, err := anthropic.Messages(msgs)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	image := messages[0].Content[0]
	if image.Type != "image" || image.Source == nil {
		t.Fatalf("the image block is %+v", image)
	}
	if image.Source.MediaType != "image/png" || image.Source.Data != "Ynk=" {
		t.Errorf("the source is %+v", image.Source)
	}
}

func TestSoundIsLeftOutOfTheMessage(t *testing.T) {
	msgs := []poo.Message{{Role: poo.RoleUser, Content: []poo.ContentBlock{{
		Kind:      poo.ContentAudio,
		MediaType: "audio/wav",
		Data:      []byte("by"),
	}}}}
	messages, _, err := anthropic.Messages(msgs)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("the audio became %d messages, want none", len(messages))
	}
}

func TestARequiredForeignBlockIsRefused(t *testing.T) {
	msgs := []poo.Message{{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{
		Kind: poo.ContentProvider,
		Provider: &poo.ProviderBlock{
			API:      poo.APIResponses,
			Type:     "reasoning",
			Data:     json.RawMessage(`{}`),
			Required: true,
		},
	}}}}
	if _, _, err := anthropic.Messages(msgs); err == nil {
		t.Fatal("a required Responses block was carried into a Messages request, want it refused")
	}
}

func TestARequiredForeignCompanionIsRefusedToo(t *testing.T) {
	msgs := []poo.Message{{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{
		Kind: poo.ContentThinking,
		Text: "Let me consider",
		Provider: &poo.ProviderBlock{
			API:      poo.APIResponses,
			Type:     "reasoning",
			Data:     json.RawMessage(`{}`),
			Required: true,
		},
	}}}}
	if _, _, err := anthropic.Messages(msgs); err == nil {
		t.Fatal("a required Responses companion rode a thinking block into a Messages request, want it refused")
	}
}

func TestAPdfRidesAsADocument(t *testing.T) {
	msgs := []poo.Message{{Role: poo.RoleUser, Content: []poo.ContentBlock{{
		Kind:      poo.ContentImage,
		MediaType: "application/pdf",
		Data:      []byte("by"),
	}}}}
	messages, _, err := anthropic.Messages(msgs)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	block := messages[0].Content[0]
	if block.Type != "document" || block.Source == nil {
		t.Fatalf("the document block is %+v", block)
	}
	if block.Source.MediaType != "application/pdf" {
		t.Errorf("the document's media type is %q", block.Source.MediaType)
	}
}
