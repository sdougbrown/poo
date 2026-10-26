package poo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// The view hands out copies. A hook that mutates what it got back must
// not change the history the model sees, and the next read must see the
// original.

// TestTheViewReturnsDeepCopies covers the plan's rule: the view hands
// out copies, and the nested content and tool payloads are deep-copied on
// access.
func TestTheViewReturnsDeepCopies(t *testing.T) {
	history := poo.NewHistoryView([]poo.Message{
		{
			Role:   poo.RoleAssistant,
			Origin: poo.OriginModel,
			ID:     "m1",
			Content: []poo.ContentBlock{
				{Kind: poo.ContentText, Text: "the answer"},
				{
					Kind:       poo.ContentToolCall,
					ToolCallID: "call_1",
					Name:       "bash",
					Arguments:  json.RawMessage(`{"command":"ls"}`),
				},
				{
					Kind: poo.ContentThinking,
					Text: "the thinking",
					Provider: &poo.ProviderBlock{
						API:  poo.APIAnthropicMessages,
						Type: "thinking_signature",
						Data: json.RawMessage(`"sig"`),
					},
				},
			},
		},
	}, nil, nil)

	// The caller mutates everything it got: the message's fields, the
	// content blocks, the raw arguments and the opaque provider payload.
	got := history.Messages()
	got[0].ID = "mutated"
	got[0].Content[0].Text = "mutated"
	got[0].Content[1].Arguments[2] = 'X'
	got[0].Content[2].Provider.Data[1] = 'X'

	// The next read sees the original.
	again := history.Messages()
	if again[0].Text() != "the answer" {
		t.Errorf("the message's text is %q, want %q", again[0].Text(), "the answer")
	}
	if again[0].ID != "m1" {
		t.Errorf("the message's id is %q, want m1", again[0].ID)
	}
	if again[0].Content[0].Text != "the answer" {
		t.Errorf("the first block's text is %q, want %q", again[0].Content[0].Text, "the answer")
	}
	if string(again[0].Content[1].Arguments) != `{"command":"ls"}` {
		t.Errorf("the call's arguments are %s, want the original", again[0].Content[1].Arguments)
	}
	if string(again[0].Content[2].Provider.Data) != `"sig"` {
		t.Errorf("the provider payload is %s, want the original", again[0].Content[2].Provider.Data)
	}
}

// TestTheViewCopiesTheDeliveryNote covers the note: it is part of the
// message, so it is copied with it.
func TestTheViewCopiesTheDeliveryNote(t *testing.T) {
	history := poo.NewHistoryView([]poo.Message{{
		Role: poo.RoleUser,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "the user typed this"},
		},
		DeliveryNote: &poo.DeliveryNote{MessageID: "m6", UpToChars: 12, Text: "the note"},
	}}, nil, nil)

	got := history.Messages()
	got[0].DeliveryNote.Text = "mutated"
	got[0].DeliveryNote.UpToChars = 999

	again := history.Messages()
	if again[0].DeliveryNote.Text != "the note" {
		t.Errorf("the note's text is %q, want %q", again[0].DeliveryNote.Text, "the note")
	}
	if again[0].DeliveryNote.UpToChars != 12 {
		t.Errorf("the note's character count is %d, want 12", again[0].DeliveryNote.UpToChars)
	}
}

// TestTheViewCopiesTheToolResult covers the tool result the view hands
// out: its structured payload is copied too.
func TestTheViewCopiesTheToolResult(t *testing.T) {
	history := poo.NewHistoryView(nil, map[string]poo.ToolResult{
		"r1": {Text: "the result", Structured: json.RawMessage(`{"count":1}`)},
	}, nil)

	got, ok := history.Result("r1")
	if !ok {
		t.Fatal("the view has no result for r1")
	}
	got.Text = "mutated"
	got.Structured[10] = 'X'

	again, ok := history.Result("r1")
	if !ok {
		t.Fatal("the view has no result for r1")
	}
	if again.Text != "the result" {
		t.Errorf("the result's text is %q, want %q", again.Text, "the result")
	}
	if string(again.Structured) != `{"count":1}` {
		t.Errorf("the result's payload is %s, want the original", again.Structured)
	}
}

// TestTheHistoryCopiesTheMessagesItIsBuiltFrom covers the constructor: a
// caller that changes the slice it passed in does not change the view.
func TestTheHistoryCopiesTheMessagesItIsBuiltFrom(t *testing.T) {
	messages := []poo.Message{{
		Role:    poo.RoleUser,
		Content: []poo.ContentBlock{{Kind: poo.ContentText, Text: "the prompt"}},
	}}
	history := poo.NewHistoryView(messages, nil, nil)
	messages[0].Content[0].Text = "mutated"

	if got := history.Messages(); got[0].Text() != "the prompt" {
		t.Errorf("the message's text is %q, want %q", got[0].Text(), "the prompt")
	}
}

// resultPeekHook reads one tool result through the view.
type resultPeekHook struct {
	text string
	ok   bool
	seen bool
}

// AfterTurn reads r1 the first time it runs.
func (h *resultPeekHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	if !h.seen {
		h.seen = true
		r, ok := ctx.HistoryView.Result("r1")
		h.text, h.ok = r.Text, ok
	}
	return poo.TurnDecision{}, nil
}

// TestTheViewCarriesTheToolResult covers the plan's rule: a hook that
// asks the view for a tool result gets the result the call produced.
func TestTheViewCarriesTheToolResult(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}}},
		{text: "done"},
	})
	tool := &echoTool{result: "echoed"}
	hook := &resultPeekHook{}
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !hook.ok {
		t.Fatal("the view has no result for r1")
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + "echoed"; hook.text != want {
		t.Errorf("the view's result text is %q, want %q", hook.text, want)
	}
}

// TestTheViewReturnsTurnSummariesAsCopies covers the turn summaries: a
// caller that mutates the returned slice, its entries or the calls does
// not change the history's turns.
func TestTheViewReturnsTurnSummariesAsCopies(t *testing.T) {
	history := poo.NewHistoryView(nil, nil, []poo.TurnSummary{{
		Index: 1,
		Calls: []poo.ToolCallSummary{{Handle: "r1", Tool: "bash"}},
	}})

	got := history.Turns()
	got[0].Index = 99
	got[0].Calls[0].Handle = "mutated"
	got[0].Calls = append(got[0].Calls, poo.ToolCallSummary{Handle: "extra"})

	again := history.Turns()
	if again[0].Index != 1 {
		t.Errorf("the turn's index is %d, want 1", again[0].Index)
	}
	if len(again[0].Calls) != 1 || again[0].Calls[0].Handle != "r1" {
		t.Errorf("the turn's calls are %+v, want one call r1", again[0].Calls)
	}
}
