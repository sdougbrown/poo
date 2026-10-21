package poo

import (
	"encoding/json"
	"testing"
)

// TestTheEstimateCountsProviderDataAndTheDeliveryNote covers the two
// parts of a message the provider also reads: an opaque block's payload
// and the delivery note's text.
func TestTheEstimateCountsProviderDataAndTheDeliveryNote(t *testing.T) {
	plain := Message{Role: RoleAssistant, Content: []ContentBlock{{Kind: ContentText, Text: "hi"}}}
	base := estimateMessageChars(plain)

	withData := Message{Role: RoleAssistant, Content: []ContentBlock{{
		Kind:     ContentProvider,
		Provider: &ProviderBlock{API: APIChatCompletions, Type: "reasoning", Data: json.RawMessage(`{"sig":"0123456789"}`)},
	}, {Kind: ContentText, Text: "hi"}}}
	if got := estimateMessageChars(withData); got < base+len(`{"sig":"0123456789"}`) {
		t.Errorf("the estimate with provider data is %d, want at least %d", got, base+len(`{"sig":"0123456789"}`))
	}

	note := plain
	note.DeliveryNote = &DeliveryNote{MessageID: "m1", UpToChars: 3, Text: "the user saw 3 characters"}
	if got := estimateMessageChars(note); got < base+len(note.DeliveryNote.Text) {
		t.Errorf("the estimate with a delivery note is %d, want at least %d", got, base+len(note.DeliveryNote.Text))
	}
}
