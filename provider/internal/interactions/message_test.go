package interactions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/interactions"
)

// encode turns the steps of a history into the JSON the request carries, so a
// test reads the steps the way the endpoint does.
func encode(t *testing.T, msgs []poo.Message) []map[string]any {
	t.Helper()
	steps, instruction, err := interactions.Messages(msgs)
	if err != nil {
		t.Fatalf("converting the history: %v", err)
	}
	_ = instruction
	raw, err := json.Marshal(steps)
	if err != nil {
		t.Fatalf("encoding the steps: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding the steps: %v", err)
	}
	return out
}

// signature is the provider block a thought step's signature rides as.
func signature(t *testing.T, value string) *poo.ProviderBlock {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding the signature: %v", err)
	}
	return &poo.ProviderBlock{
		API:      poo.APIGoogleInteractions,
		Type:     "thought_signature",
		Data:     raw,
		Required: true,
	}
}

// TestTheHistoryBecomesTheStepsTheAPISends covers the conversion of a whole
// turn: the system prompt travels in its own field, and the tool's result is
// the step that answers its call.
func TestTheHistoryBecomesTheStepsTheAPISends(t *testing.T) {
	msgs := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "be brief"),
		poo.NewTextMessage(poo.RoleUser, "read a.py"),
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "Read the file first.", Provider: signature(t, "sig_abc")},
			{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.py"}`)},
		}},
		{Role: poo.RoleTool, Content: []poo.ContentBlock{
			{Kind: poo.ContentToolResult, ToolCallID: "call_1", Name: "read_file", Text: "x = 1"},
		}},
	}

	steps, instruction, err := interactions.Messages(msgs)
	if err != nil {
		t.Fatalf("converting the history: %v", err)
	}
	if instruction != "be brief" {
		t.Errorf("the system instruction is %q, want the system message's text", instruction)
	}
	if len(steps) != 4 {
		t.Fatalf("the history became %d steps, want four", len(steps))
	}
	// The thought rides with the signature that lets the model read it back,
	// and the call follows it in the API's own order.
	if steps[1].Type != interactions.StepThought || steps[1].Signature != "sig_abc" {
		t.Errorf("the second step is %+v, want the signed thought", steps[1])
	}
	if len(steps[1].Summary) != 1 || steps[1].Summary[0].Text != "Read the file first." {
		t.Errorf("the thought's summary is %+v", steps[1].Summary)
	}
	if steps[2].Type != interactions.StepFunctionCall || steps[2].ID != "call_1" || steps[2].Name != "read_file" {
		t.Errorf("the third step is %+v, want the call", steps[2])
	}
	if string(steps[2].Arguments) != `{"path":"a.py"}` {
		t.Errorf("the call's arguments are %s, want the object the block holds", steps[2].Arguments)
	}
	if steps[3].Type != interactions.StepFunctionResult || steps[3].CallID != "call_1" {
		t.Errorf("the fourth step is %+v, want the result", steps[3])
	}
	if steps[3].Result != "x = 1" {
		t.Errorf("the result is %v, want the text it answers with", steps[3].Result)
	}
}

// TestAnUnsignedCallIsReplayedWithTheSentinel covers the one rule the API
// makes about a call of the current turn: its thought must carry a signature,
// and a call that has none is replayed with Google's own skip value.
func TestAnUnsignedCallIsReplayedWithTheSentinel(t *testing.T) {
	msgs := []poo.Message{
		poo.NewTextMessage(poo.RoleUser, "read a.py"),
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.py"}`)},
		}},
	}

	steps := encode(t, msgs)

	if len(steps) != 3 {
		t.Fatalf("the history became %d steps, want the sentinel and the call", len(steps))
	}
	if steps[1]["type"] != interactions.StepThought || steps[1]["signature"] != interactions.SkipThoughtSignature {
		t.Errorf("the step before the call is %v, want the sentinel", steps[1])
	}
	if steps[2]["type"] != interactions.StepFunctionCall {
		t.Errorf("the last step is %v, want the call", steps[2])
	}
}

// TestASignedCallIsReplayedWithoutTheSentinel is the other half: a thought the
// model signed is replayed as it is, and no sentinel is added beside it.
func TestASignedCallIsReplayedWithoutTheSentinel(t *testing.T) {
	msgs := []poo.Message{
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "thinking", Provider: signature(t, "sig_abc")},
			{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{}`)},
		}},
	}

	steps := encode(t, msgs)

	if len(steps) != 2 {
		t.Fatalf("the history became %d steps, want the thought and the call", len(steps))
	}
	if steps[0]["signature"] != "sig_abc" {
		t.Errorf("the thought's signature is %v, want the model's own", steps[0]["signature"])
	}
}

// TestATextAfterACallKeepsItsPlace covers the output steps: consecutive text is
// one step, and text that follows a call is a step of its own, because the
// order of the conversation is the order the steps are in.
func TestATextAfterACallKeepsItsPlace(t *testing.T) {
	msgs := []poo.Message{
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "before"},
			{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{}`)},
			{Kind: poo.ContentText, Text: "after"},
		}},
	}

	steps := encode(t, msgs)

	var outputs int
	for _, step := range steps {
		if step["type"] == interactions.StepModelOutput {
			outputs++
		}
	}
	if outputs != 2 {
		t.Errorf("the history became %d output steps, want one on each side of the call", outputs)
	}
}

// TestAnAttachmentRidesInline covers the parts: an image is sent as its own
// bytes, and a tool's result carries them as the parts the API takes.
func TestAnAttachmentRidesInline(t *testing.T) {
	msgs := []poo.Message{
		{Role: poo.RoleUser, Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "look"},
			{Kind: poo.ContentImage, MediaType: "image/jpeg", Data: []byte("jpeg")},
		}},
		{Role: poo.RoleTool, Content: []poo.ContentBlock{
			{Kind: poo.ContentToolResult, ToolCallID: "call_1", Name: "screenshot", Text: "the shot"},
			{Kind: poo.ContentImage, MediaType: "image/png", Data: []byte("png")},
		}},
	}

	steps, _, err := interactions.Messages(msgs)
	if err != nil {
		t.Fatalf("converting the history: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("the history became %d steps, want two", len(steps))
	}
	result, ok := steps[1].Result.([]interactions.Content)
	if !ok {
		t.Fatalf("the result is %T, want the parts it answers with", steps[1].Result)
	}
	if len(result) != 2 || result[0].Type != "text" || result[1].Type != "image" {
		t.Fatalf("the result's parts are %+v, want the text and the image", result)
	}
	if result[1].MediaType != "image/png" || result[1].Data == "" {
		t.Errorf("the image part is %+v, want the bytes inline", result[1])
	}
	// The user's image rides in the input step, after the words it answers.
	if len(steps[0].Content) != 2 || steps[0].Content[1].Type != "image" {
		t.Errorf("the input step holds %+v, want the text and the image", steps[0].Content)
	}
	if steps[0].Content[1].MediaType != "image/jpeg" {
		t.Errorf("the input's image is %+v, want the block's media type", steps[0].Content[1])
	}
}

// TestAForeignRequiredBlockStopsTheRequest covers the rule every transport
// shares: a block that belongs to another API cannot be carried, and a
// required one makes the request impossible rather than lossy.
func TestAForeignRequiredBlockStopsTheRequest(t *testing.T) {
	msgs := []poo.Message{
		{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{
			Kind: poo.ContentThinking,
			Text: "thinking",
			Provider: &poo.ProviderBlock{
				API:      poo.APIAnthropicMessages,
				Type:     "thinking_signature",
				Data:     json.RawMessage(`"sig"`),
				Required: true,
			},
		}}},
	}

	_, _, err := interactions.Messages(msgs)
	if err == nil {
		t.Fatal("the history was converted, want the foreign block refused")
	}
	if !strings.Contains(err.Error(), "cannot carry") {
		t.Errorf("the failure is %v, want the block named", err)
	}
}
