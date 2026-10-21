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
// the plan adds to a message: the session-monotonic id, the origin and
// the delivery note.
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

// TestTheToolResultOutcomeDefaults covers the outcome a tool result
// carries when the tool names none: an ordinary success, and an ordinary
// recoverable failure.
func TestTheToolResultOutcomeDefaults(t *testing.T) {
	ok := poo.ToolResult{Text: "fine"}
	if got := ok.EffectiveOutcome(); got != poo.OutcomeOK {
		t.Errorf("the outcome is %q, want ok", got)
	}
	bad := poo.ToolResult{Text: "broken", IsError: true}
	if got := bad.EffectiveOutcome(); got != poo.OutcomeError {
		t.Errorf("the outcome is %q, want error", got)
	}
	started := poo.ToolResult{Text: "started", Outcome: poo.OutcomeStarted}
	if got := started.EffectiveOutcome(); got != poo.OutcomeStarted {
		t.Errorf("the outcome is %q, want started", got)
	}
}

// TestTheBackgroundModeDefaultsToNever covers the tool spec's background
// mode: a tool that says nothing is an ordinary synchronous tool.
func TestTheBackgroundModeDefaultsToNever(t *testing.T) {
	spec := poo.ToolSpec{Name: "bash"}
	if spec.Background != poo.BackgroundNever {
		t.Errorf("the background mode is %q, want never", spec.Background)
	}
	if poo.BackgroundNever != "" {
		t.Errorf("the never mode is %q, want the empty string", poo.BackgroundNever)
	}
}
