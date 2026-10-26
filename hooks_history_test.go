package poo_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
)

// The view hands out copies: a hook that changes what it got back
// changes nothing, neither the model's history nor another caller's
// copy. The nested content and the tool payloads are copied too.

// mutatingHook is a hook that changes everything the view hands it.
type mutatingHook struct {
	// mutated reports how many blocks the hook changed.
	mutated int
}

// AfterTurn changes every copy the view hands out.
func (h *mutatingHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	for _, m := range ctx.HistoryView.Messages() {
		for i := range m.Content {
			m.Content[i].Text = "changed"
			if len(m.Content[i].Arguments) > 0 {
				m.Content[i].Arguments = json.RawMessage(`{"changed":true}`)
			}
			if m.Content[i].Provider != nil {
				m.Content[i].Provider.Data = json.RawMessage(`"changed"`)
			}
			h.mutated++
		}
	}
	for _, turn := range ctx.HistoryView.Turns() {
		for i := range turn.Calls {
			turn.Calls[i].Summary = "changed"
			turn.Calls[i].Outcome = poo.OutcomeError
		}
	}
	for handle := range map[string]bool{"r1": true} {
		if r, ok := ctx.HistoryView.Result(handle); ok {
			r.Text = "changed"
			r.Structured = json.RawMessage(`"changed"`)
		}
	}
	return poo.TurnDecision{}, nil
}

// TestMutatingTheViewDoesNotChangeTheHistory covers the plan's rule: the
// view hands out copies, so a hook that changes what it got back
// changes nothing.
func TestMutatingTheViewDoesNotChangeTheHistory(t *testing.T) {
	hook := &mutatingHook{}
	tool := &echoTool{result: "the tool's output"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	runSession(t, s, "the prompt")
	if hook.mutated == 0 {
		t.Fatal("the hook changed nothing, want it to change the view it was given")
	}
	// The model sees the history the hook tried to change.
	requests := tr.Requests()
	if len(requests) < 2 {
		t.Fatalf("the model was asked %d time(s), want 2", len(requests))
	}
	messages := requests[1].Messages
	if historyHas(messages, "changed") {
		t.Errorf("the model's view is %+v, want the hook's changes left out", messages)
	}
	if !historyHas(messages, "the tool's output") {
		t.Errorf("the model's view is %+v, want the tool's own result", messages)
	}
	if !historyHas(messages, "the prompt") {
		t.Errorf("the model's view is %+v, want the prompt", messages)
	}
}
