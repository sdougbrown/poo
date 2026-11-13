package poo_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// The message model is the thing every transport converts to and from, so
// every block type the wire mapping names has to be representable, and the
// opaque provider data has to survive a round trip through JSON untouched.

// TestEveryBlockTypeIsRepresentable covers the block types the wire
// mapping lists: text, thinking, a tool call, a tool result and an opaque
// provider block, plus the provider companion of a visible block.
func TestEveryBlockTypeIsRepresentable(t *testing.T) {
	blocks := []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "hello"},
		{Kind: poo.ContentThinking, Text: "thinking about it"},
		{
			Kind:       poo.ContentToolCall,
			ToolCallID: "call_1",
			Name:       "bash",
			Arguments:  json.RawMessage(`{"command":"ls"}`),
		},
		{
			Kind:       poo.ContentToolResult,
			ToolCallID: "call_1",
			Name:       "bash",
			Text:       "out",
			IsError:    true,
		},
		{
			Kind:      poo.ContentImage,
			MediaType: "image/png",
			Data:      []byte{0x89, 0x50},
		},
		{
			Kind: poo.ContentProvider,
			Provider: &poo.ProviderBlock{
				API:      poo.APIResponses,
				Type:     "compaction",
				Data:     json.RawMessage(`{"encrypted_content":"abc"}`),
				Required: true,
			},
		},
		{
			Kind: poo.ContentThinking,
			Text: "signed thinking",
			Provider: &poo.ProviderBlock{
				API:  poo.APIAnthropicMessages,
				Type: "thinking_signature",
				Data: json.RawMessage(`"sig-123"`),
			},
		},
	}

	for _, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encoding a %s block: %v", b.Kind, err)
		}
		var back poo.ContentBlock
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("decoding a %s block: %v", b.Kind, err)
		}
		if back.Kind != b.Kind {
			t.Errorf("the block's kind is %q, want %q", back.Kind, b.Kind)
		}
		if back.Text != b.Text {
			t.Errorf("the %s block's text is %q, want %q", b.Kind, back.Text, b.Text)
		}
		if back.ToolCallID != b.ToolCallID {
			t.Errorf("the %s block's call id is %q, want %q", b.Kind, back.ToolCallID, b.ToolCallID)
		}
		if back.Name != b.Name {
			t.Errorf("the %s block's name is %q, want %q", b.Kind, back.Name, b.Name)
		}
		if b.IsError != back.IsError {
			t.Errorf("the %s block's error flag is %v, want %v", b.Kind, back.IsError, b.IsError)
		}
		if string(b.Arguments) != string(back.Arguments) {
			t.Errorf("the %s block's arguments are %s, want %s", b.Kind, back.Arguments, b.Arguments)
		}
		if b.Provider == nil {
			if back.Provider != nil {
				t.Errorf("the %s block gained a provider block", b.Kind)
			}
			continue
		}
		if back.Provider == nil {
			t.Fatalf("the %s block lost its provider block", b.Kind)
		}
		if back.Provider.API != b.Provider.API {
			t.Errorf("the provider block's api is %q, want %q", back.Provider.API, b.Provider.API)
		}
		if back.Provider.Type != b.Provider.Type {
			t.Errorf("the provider block's type is %q, want %q", back.Provider.Type, b.Provider.Type)
		}
		if back.Provider.Required != b.Provider.Required {
			t.Errorf("the provider block's required flag is %v, want %v", back.Provider.Required, b.Provider.Required)
		}
		// The payload is opaque: it comes back byte for byte.
		if string(back.Provider.Data) != string(b.Provider.Data) {
			t.Errorf("the provider block's data is %s, want %s", back.Provider.Data, b.Provider.Data)
		}
	}
}

// TestAMessageCarriesItsIdOriginAndDeliveryNote covers the three fields
// the record adds to a message: the session-monotonic id, the origin and
// the delivery note.
func TestAToolResultCarriesTheImagesBesideItsText(t *testing.T) {
	image := poo.Image{MediaType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}}
	msg := poo.ToolResultMessage("call_1", "read", "read: shot.png (image, 4 bytes)", false, []poo.Image{image})

	if msg.Role != poo.RoleTool {
		t.Errorf("the message's role is %q, want tool", msg.Role)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("the message holds %d blocks, want the result and one image", len(msg.Content))
	}
	if msg.Content[0].Kind != poo.ContentToolResult || msg.Content[0].ToolCallID != "call_1" {
		t.Errorf("the first block is %+v, want the tool result for call_1", msg.Content[0])
	}
	got := msg.Content[1]
	if got.Kind != poo.ContentImage {
		t.Fatalf("the second block is %q, want an image", got.Kind)
	}
	if got.MediaType != image.MediaType {
		t.Errorf("the image's media type is %q, want %q", got.MediaType, image.MediaType)
	}
	if string(got.Data) != string(image.Data) {
		t.Errorf("the image's bytes are %v, want %v", got.Data, image.Data)
	}

	// The block round-trips through the record's own encoding.
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("encoding the message: %v", err)
	}
	var decoded poo.Message
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding the message: %v", err)
	}
	if len(decoded.Content) != 2 || decoded.Content[1].Kind != poo.ContentImage {
		t.Fatalf("the decoded message lost its image: %+v", decoded.Content)
	}
	if decoded.Content[1].MediaType != image.MediaType {
		t.Errorf("the decoded media type is %q, want %q", decoded.Content[1].MediaType, image.MediaType)
	}
	if string(decoded.Content[1].Data) != string(image.Data) {
		t.Errorf("the decoded bytes are %v, want %v", decoded.Content[1].Data, image.Data)
	}

	// A result without images is the one block it always was.
	plain := poo.ToolResultMessage("call_2", "bash", "out", true, nil)
	if len(plain.Content) != 1 {
		t.Errorf("a plain result holds %d blocks, want one", len(plain.Content))
	}
}

func TestAMessageCarriesItsIdOriginAndDeliveryNote(t *testing.T) {
	msg := poo.Message{
		Role:   poo.RoleUser,
		Origin: poo.OriginUser,
		ID:     "m7",
		Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "the user typed this"},
		},
		DeliveryNote: &poo.DeliveryNote{
			MessageID: "m6",
			UpToChars: 0,
			Text:      "[The user did not see or hear any of your reply m6.]",
		},
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("encoding the message: %v", err)
	}
	var back poo.Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("decoding the message: %v", err)
	}
	if back.ID != "m7" {
		t.Errorf("the message's id is %q, want m7", back.ID)
	}
	if back.Origin != poo.OriginUser {
		t.Errorf("the message's origin is %q, want user", back.Origin)
	}
	if back.DeliveryNote == nil {
		t.Fatal("the message lost its delivery note")
	}
	if back.DeliveryNote.MessageID != "m6" {
		t.Errorf("the note names %q, want m6", back.DeliveryNote.MessageID)
	}
	if back.DeliveryNote.UpToChars != 0 {
		t.Errorf("the note's character count is %d, want 0", back.DeliveryNote.UpToChars)
	}
	if back.DeliveryNote.Text == "" {
		t.Error("the note's text is empty")
	}
}

// TestTheHookOriginNamesTheHook covers the origin a hook's appended
// messages carry.
func TestTheHookOriginNamesTheHook(t *testing.T) {
	if got := poo.HookOrigin("AfterTurn"); got != "hook:AfterTurn" {
		t.Errorf("the hook origin is %q, want hook:AfterTurn", got)
	}
}
