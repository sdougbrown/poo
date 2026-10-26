package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The tests drive a model switch through the public API: the session
// switches between the runs, and the record and the event say what
// happened.

// chatModel is a model that serves the chat operation.
func chatModel(name string, limit int) poo.ModelInfo {
	return poo.ModelInfo{
		Name:         name,
		ModelID:      name,
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: limit,
	}
}

// providerBlockHook is a session-start hook that appends a message with
// a provider block.
//
// The block is the way a history holds a payload the harness must give
// back unchanged.
type providerBlockHook struct {
	// block is the block the appended message carries.
	block poo.ProviderBlock
}

// SessionStart appends the message.
func (h *providerBlockHook) SessionStart(poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{
		Append: []poo.Message{{
			Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentText, Text: "the model's own thinking"},
				{Kind: poo.ContentProvider, Provider: &h.block},
			},
		}},
	}, nil
}

// modelsChanged returns the model-switch record the sink kept.
func modelsChanged(t *testing.T, sink *recordingSink) *poo.ModelsChangedRecord {
	t.Helper()
	var found *poo.ModelsChangedRecord
	for _, rec := range sink.allRecords() {
		if m, ok := rec.(*poo.ModelsChangedRecord); ok {
			found = m
		}
	}
	if found == nil {
		t.Fatal("the sink kept no model-switch record")
	}
	return found
}

// TestTheSessionRefusesAModelSwitchWhileARunIsActive covers the timing:
// the switch happens between the runs.
func TestTheSessionRefusesAModelSwitchWhileARunIsActive(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	tool := &echoTool{call: func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		return poo.ToolResult{Text: "done"}, nil
	}}
	tr := pootest.NewTransport(pootest.Turn{
		Text:  "working",
		Calls: []pootest.Call{{Name: "echo", Arguments: json.RawMessage(`"x"`), ID: "c1"}},
	})
	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	_, err := s.SetModel(chatModel("other", 32768))
	if !errors.Is(err, poo.ErrBusy) {
		t.Errorf("the switch failed with %v, want it to be refused", err)
	}
	close(release)
	waitRun(t, s)
}

// TestTheSessionRefusesAModelWithoutTheChatOperation covers the first
// check: the model must serve the operation its role needs.
func TestTheSessionRefusesAModelWithoutTheChatOperation(t *testing.T) {
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, &recordingSink{}))

	model := poo.ModelInfo{
		Name:    "gemma4",
		ModelID: "gemma4",
		Ops:     []poo.Op{poo.OpSystemOne},
	}
	_, err := s.SetModel(model)
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the switch failed with %v, want a provider failure", err)
	}
	if perr.Kind != poo.ProviderKindSessionIncompatible {
		t.Errorf("the failure is a %q, want %q", perr.Kind, poo.ProviderKindSessionIncompatible)
	}
}

// TestTheSessionRefusesAModelSwitchWithARequiredForeignBlock covers the
// second check: a required block from another API cannot be carried, and
// the session is incompatible with the new model.
func TestTheSessionRefusesAModelSwitchWithARequiredForeignBlock(t *testing.T) {
	block := poo.ProviderBlock{
		API:      poo.APIAnthropicMessages,
		Type:     "thinking_signature",
		Data:     json.RawMessage(`"payload"`),
		Required: true,
	}
	sink := &recordingSink{}
	opts := testOptions(t, pootest.NewTransport(), &echoTool{}, sink)
	opts.Model.API = poo.APIAnthropicMessages
	s := newTestSession(t, opts, poo.WithSessionStart(&providerBlockHook{block: block}))

	_, err := s.SetModel(chatModel("other", 32768))
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the switch failed with %v, want a provider failure", err)
	}
	if perr.Kind != poo.ProviderKindSessionIncompatible {
		t.Errorf("the failure is a %q, want %q", perr.Kind, poo.ProviderKindSessionIncompatible)
	}
	if sink.count("session.models_changed") != 0 {
		t.Errorf("the sink kept %d switch records, want none", sink.count("session.models_changed"))
	}
}

// TestTheSessionChecksTheForeignBlocksWithoutBeingToldTheAPI covers
// the default: the session speaks the Chat Completions API, so a
// required block from another API stops the switch even when the
// consumer named no API at all.
func TestTheSessionChecksTheForeignBlocksWithoutBeingToldTheAPI(t *testing.T) {
	block := poo.ProviderBlock{
		API:      poo.APIAnthropicMessages,
		Type:     "thinking_signature",
		Data:     json.RawMessage(`"payload"`),
		Required: true,
	}
	sink := &recordingSink{}
	opts := testOptions(t, pootest.NewTransport(), &echoTool{}, sink)
	s := newTestSession(t, opts, poo.WithSessionStart(&providerBlockHook{block: block}))

	_, err := s.SetModel(chatModel("other", 32768))
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the switch failed with %v, want a provider failure", err)
	}
	if perr.Kind != poo.ProviderKindSessionIncompatible {
		t.Errorf("the failure is a %q, want %q", perr.Kind, poo.ProviderKindSessionIncompatible)
	}
}

// TestTheSessionOmitsForeignBlocksAndRecordsTheOmission covers the
// second check's other half: a block that is not required is left out
// of the requests, and the omission is recorded.
func TestTheSessionOmitsForeignBlocksAndRecordsTheOmission(t *testing.T) {
	block := poo.ProviderBlock{
		API:  poo.APIAnthropicMessages,
		Type: "thinking_signature",
		Data: json.RawMessage(`"payload"`),
	}
	sink := &recordingSink{}
	opts := testOptions(t, pootest.NewTransport(), &echoTool{}, sink)
	opts.Model.API = poo.APIAnthropicMessages
	tr := pootest.NewTransport(pootest.Turn{Text: "hello"})
	opts.Transport = tr
	s := newTestSession(t, opts, poo.WithSessionStart(&providerBlockHook{block: block}))

	if _, err := s.SetModel(chatModel("other", 32768)); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	rec := modelsChanged(t, sink)
	if len(rec.Omitted) != 1 {
		t.Fatalf("the record holds %d omitted blocks, want 1", len(rec.Omitted))
	}
	if rec.Omitted[0].API != poo.APIAnthropicMessages {
		t.Errorf("the omitted block belongs to %q, want %q", rec.Omitted[0].API, poo.APIAnthropicMessages)
	}
	if rec.Omitted[0].Type != "thinking_signature" {
		t.Errorf("the omitted block is a %q, want thinking_signature", rec.Omitted[0].Type)
	}
	if rec.Omitted[0].Count != 1 {
		t.Errorf("the record counts %d omitted blocks, want 1", rec.Omitted[0].Count)
	}

	// The request leaves the block out.
	runSession(t, s, "hello")
	requests := tr.Requests()
	if len(requests) == 0 {
		t.Fatal("the transport received no request")
	}
	for _, msg := range requests[0].Messages {
		for _, c := range msg.Content {
			if c.Provider != nil {
				t.Errorf("the request carries the provider block %q", c.Provider.Type)
			}
			if c.Kind == poo.ContentProvider {
				t.Errorf("the request carries a provider block")
			}
		}
	}
}

// TestTheSessionWarnsWhenTheViewDoesNotFitTheNewWindow covers the third
// check: the history is never truncated, and the next run's gate settles
// the run as context_exhausted.
func TestTheSessionWarnsWhenTheViewDoesNotFitTheNewWindow(t *testing.T) {
	sink := &recordingSink{}
	opts := testOptions(t, pootest.NewTransport(), &echoTool{}, sink)
	opts.Model.ContextLimit = 8000
	opts.SystemPrompt = ""
	s := newTestSession(t, opts, poo.WithSessionStart(&bigHistoryHook{}))

	outcome, err := s.SetModel(chatModel("small", 4000))
	if err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	if len(outcome.Warnings) == 0 {
		t.Fatal("the switch carried no warning")
	}
	if !strings.Contains(outcome.Warnings[0], "context_exhausted") {
		t.Errorf("the warning is %q, want it to name the settle reason", outcome.Warnings[0])
	}

	result := runSession(t, s, "hello")
	if result.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled as %q, want %q", result.Reason, poo.ReasonContextExhausted)
	}
}

// bigHistoryHook is a session-start hook that appends a long message.
type bigHistoryHook struct{}

// SessionStart appends the message.
func (h *bigHistoryHook) SessionStart(poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{
		Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, strings.Repeat("x", 20000))},
	}, nil
}

// TestTheSessionRecordsTheModelSwitch covers the record and the event.
func TestTheSessionRecordsTheModelSwitch(t *testing.T) {
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, sink))

	outcome, err := s.SetModel(chatModel("gemma4", 32768))
	if err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	if outcome.From != "test-model" {
		t.Errorf("the switch came from %q, want test-model", outcome.From)
	}
	if outcome.To != "gemma4" {
		t.Errorf("the switch went to %q, want gemma4", outcome.To)
	}
	rec := modelsChanged(t, sink)
	if rec.From != "test-model" || rec.To != "gemma4" {
		t.Errorf("the record says %q -> %q, want test-model -> gemma4", rec.From, rec.To)
	}
	if rec.Meta().Record != "session.models_changed" {
		t.Errorf("the record is a %q, want session.models_changed", rec.Meta().Record)
	}
	var found bool
	for _, ev := range s.Events() {
		if ev.Name != poo.EventModelsChanged {
			continue
		}
		found = true
		if ev.Payload["to"] != "gemma4" {
			t.Errorf("the event carries the model %v, want gemma4", ev.Payload["to"])
		}
	}
	if !found {
		t.Error("the session emitted no model-switch event")
	}
}

// TestTheSessionRendersThePromptWithTheNewModel covers the fourth
// check: the environment section names the model, so the prompt the next
// run renders names it too.
func TestTheSessionRendersThePromptWithTheNewModel(t *testing.T) {
	sink := &recordingSink{}
	opts := testOptions(t, pootest.NewTransport(), &echoTool{}, sink)
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)

	if _, err := s.SetModel(chatModel("gemma4", 32768)); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	runSession(t, s, "hello")
	var prompt *poo.SystemPromptRecord
	for _, rec := range sink.allRecords() {
		if p, ok := rec.(*poo.SystemPromptRecord); ok {
			prompt = p
		}
	}
	if prompt == nil {
		t.Fatal("the session wrote no prompt record")
	}
	if !strings.Contains(prompt.Text, "Model: gemma4") {
		t.Errorf("the prompt does not name the model:\n%s", prompt.Text)
	}
}

// TestTheSessionSwitchesTheTransport covers the transport: a model on
// another endpoint needs its own transport.
func TestTheSessionSwitchesTheTransport(t *testing.T) {
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, sink))

	tr := pootest.NewTransport(pootest.Turn{Text: "hello"})
	if _, err := s.SetModel(chatModel("gemma4", 32768), tr); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	runSession(t, s, "hello")
	if len(tr.Requests()) != 1 {
		t.Errorf("the new transport received %d requests, want 1", len(tr.Requests()))
	}
}

// waitRun waits for the run to settle.
func waitRun(t *testing.T, s *poo.Session) poo.Result {
	t.Helper()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return res
}

// The switch is refused while a run is active, and the reason is the
// busy one: the caller waits for the run and tries again.
func TestTheSessionSwitchErrorNamesTheReason(t *testing.T) {
	s := newTestSession(t, testOptions(t, pootest.NewTransport(), &echoTool{}, &recordingSink{}))
	_, err := s.SetModel(poo.ModelInfo{Name: "gemma4"})
	if err == nil {
		t.Fatal("the switch was made for a model with no operation")
	}
	if !strings.Contains(err.Error(), "gemma4") {
		t.Errorf("the failure is %v, want it to name the model", err)
	}
	if !strings.Contains(fmt.Sprint(err), "chat") {
		t.Errorf("the failure is %v, want it to name the operation", err)
	}
}
