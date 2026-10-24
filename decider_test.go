package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
)

// The tests cover the decision model's plumbing: the session wraps it,
// every call is recorded, and the evidence is built from the history.

// deciderModel is a model that serves the systemone operation.
func deciderModel() poo.ModelInfo {
	return poo.ModelInfo{
		Name:    "gemma4",
		ModelID: "gemma4",
		Ops:     []poo.Op{poo.OpSystemOne},
	}
}

// oneQuestion is a question a decider is asked.
func oneQuestion() []decide.Question {
	return []decide.Question{{
		ID:           "q1",
		Instructions: "is it a bug?",
		Kind:         decide.Bool{True: "it is a bug", False: "it is not a bug"},
	}}
}

// decisionRecord returns the decision record the sink kept.
func decisionRecord(t *testing.T, sink *recordingSink) *poo.DecisionRecord {
	t.Helper()
	var found *poo.DecisionRecord
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.DecisionRecord); ok {
			found = d
		}
	}
	if found == nil {
		t.Fatal("the sink kept no decision record")
	}
	return found
}

// TestTheSessionRecordsTheDecisionsItMakes covers the wiring: the
// session's decider writes a record and emits the event.
func TestTheSessionRecordsTheDecisionsItMakes(t *testing.T) {
	inner := pootest.NewDecider(0.7)
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, sink),
		poo.WithDecider(deciderModel(), inner))

	dec := s.Decider()
	if dec == nil {
		t.Fatal("the session has no decision model")
	}
	answers, err := dec.Decide(context.Background(), "the state", "", oneQuestion())
	if err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("the call returned %d answers, want 1", len(answers))
	}

	rec := decisionRecord(t, sink)
	if rec.Outcome != decide.ResultOK {
		t.Errorf("the record's outcome is %q, want %q", rec.Outcome, decide.ResultOK)
	}
	if len(rec.Questions) != 1 || rec.Questions[0].ID != "q1" {
		t.Errorf("the record holds the questions %v, want the one that was asked", rec.Questions)
	}
	if rec.StateHash == "" {
		t.Error("the record holds no state hash")
	}
	if len(rec.Answers) != 1 {
		t.Errorf("the record holds %d answers, want 1", len(rec.Answers))
	}
	if rec.Origin == "" {
		t.Error("the record holds no origin")
	}

	var found bool
	for _, ev := range s.Events() {
		if ev.Name != poo.EventDecision {
			continue
		}
		found = true
		if ev.Payload["outcome"] != decide.ResultOK {
			t.Errorf("the event carries the outcome %v, want %q", ev.Payload["outcome"], decide.ResultOK)
		}
	}
	if !found {
		t.Error("the session emitted no decision event")
	}
}

// TestTheSessionRecordsADeferredDecision covers the failure: a decision
// that could not be made is recorded too.
func TestTheSessionRecordsADeferredDecision(t *testing.T) {
	inner := pootest.NewDecider(0.7)
	inner.SetError(errors.New("the model is down"))
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, sink),
		poo.WithDecider(deciderModel(), inner))

	dec := s.Decider()
	if _, err := dec.Decide(context.Background(), "the state", "", oneQuestion()); err == nil {
		t.Fatal("the call succeeded, want the decider's failure")
	}
	rec := decisionRecord(t, sink)
	if rec.Outcome != decide.ResultDeferred {
		t.Errorf("the record's outcome is %q, want %q", rec.Outcome, decide.ResultDeferred)
	}
	if rec.Error == "" {
		t.Error("the record holds no error")
	}
}

// TestTheSessionRefusesADeciderModelWithoutTheOperation covers the
// construction: a model that does not serve the decider's operation
// cannot be the session's decision model.
func TestTheSessionRefusesADeciderModelWithoutTheOperation(t *testing.T) {
	model := poo.ModelInfo{
		Name:    "gemma4",
		ModelID: "gemma4",
		Ops:     []poo.Op{poo.OpChat},
	}
	_, err := poo.New(testOptions(t, pootest.NewTransport(), &echoTool{}, &recordingSink{}),
		poo.WithDecider(model, pootest.NewDecider(0.7)))
	if err == nil {
		t.Fatal("the session was built with a decider model without the operation")
	}
	var init *poo.ErrSessionInit
	if !errors.As(err, &init) {
		t.Fatalf("the failure is %v, want a construction failure", err)
	}
}

// TestTheSessionGivesTheDeciderToTheHooksAndTheTools covers the
// availability: the decider sits in the hook context and in the tool
// environment.
func TestTheSessionGivesTheDeciderToTheHooksAndTheTools(t *testing.T) {
	hook := &deciderHook{}
	tool := &deciderTool{}
	inner := pootest.NewDecider(0.7)
	opts := poo.Options{
		Transport:  pootest.NewTransport(pootest.Turn{Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`"x"`), ID: "c1"}}}),
		Tools:      map[string]poo.Tool{"echo": tool},
		ToolOrder:  []string{"echo"},
		Sink:       &recordingSink{},
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
	s := newTestSession(t, opts,
		poo.WithDecider(deciderModel(), inner),
		poo.WithBeforeRun(hook))

	runSession(t, s, "hello")
	if !hook.sawDecider {
		t.Error("the before-run hook was given no decision model")
	}
	if !tool.sawDecider {
		t.Error("the tool was given no decision model")
	}
}

// deciderHook is a before-run hook that notes whether it was given a
// decision model.
type deciderHook struct {
	// sawDecider reports that the hook was given a decision model.
	sawDecider bool
}

// BeforeRun notes the decision model.
func (h *deciderHook) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	h.sawDecider = ctx.Decider != nil
	return poo.StartDecision{}, nil
}

// deciderTool is a tool that notes whether it was given a decision
// model.
type deciderTool struct {
	// sawDecider reports that the tool was given a decision model.
	sawDecider bool
}

// Spec describes the tool.
func (t *deciderTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts anything.
func (t *deciderTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{text: string(raw)}, nil
}

// Call notes the decision model.
func (t *deciderTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.sawDecider = env.Decider != nil
	return poo.ToolResult{Text: "done"}, nil
}

// TestTheEvidenceTurnsHoldTheRecentConversation covers the evidence: the
// user's requests, the tool calls and the results, in a bounded window.
func TestTheEvidenceTurnsHoldTheRecentConversation(t *testing.T) {
	messages := []poo.Message{
		poo.NewTextMessage(poo.RoleUser, "fix the bug"),
		{
			Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentText, Text: "looking"},
				{Kind: poo.ContentToolCall, ToolCallID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)},
			},
		},
		poo.ToolResultMessage("c1", "read", "the file's contents", false),
		poo.NewTextMessage(poo.RoleUser, "thanks"),
	}
	view := poo.NewHistoryView(messages, nil, nil)

	turns := poo.EvidenceTurns(view, 0)
	if len(turns) != 4 {
		t.Fatalf("the evidence holds %d turns, want 4", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Text != "fix the bug" {
		t.Errorf("the first turn is %+v, want the user's request", turns[0])
	}
	if turns[1].ToolName != "read" {
		t.Errorf("the second turn is %+v, want the tool call", turns[1])
	}
	if turns[1].ToolInput != `{"path":"a.go"}` {
		t.Errorf("the second turn's input is %q", turns[1].ToolInput)
	}
	if turns[2].Role != "tool" || turns[2].ToolOutput != "the file's contents" {
		t.Errorf("the third turn is %+v, want the tool's result", turns[2])
	}
	if turns[3].Text != "thanks" {
		t.Errorf("the last turn is %+v, want the user's thanks", turns[3])
	}

	// The window is bounded.
	short := poo.EvidenceTurns(view, 2)
	if len(short) != 2 {
		t.Fatalf("the evidence holds %d turns, want 2", len(short))
	}
	if short[0].Role != "tool" {
		t.Errorf("the first turn of the window is %+v, want the tool's result", short[0])
	}
}
