package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/openai"
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

// bodyServer answers with an answer and keeps the bodies it was sent.
type bodyServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
}

// newBodyServer starts an endpoint that keeps every decoded body.
func newBodyServer(t *testing.T) *bodyServer {
	t.Helper()
	s := &bodyServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		s.mu.Lock()
		s.bodies = append(s.bodies, decoded)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(s.Close)
	return s
}

// last returns the last body the endpoint received.
func (s *bodyServer) last(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		t.Fatal("the endpoint got no request")
	}
	return s.bodies[len(s.bodies)-1]
}

// TestASwitchShapesEverythingTheNextRequestNeeds covers the descriptor
// after a switch: the ceiling, the sampling, the reasoning control, the
// tools mode and the transport all follow the new model.
func TestASwitchShapesEverythingTheNextRequestNeeds(t *testing.T) {
	oldServer := newBodyServer(t)
	nextServer := newBodyServer(t)

	first := poo.ModelInfo{
		Name:         "old-model",
		ModelID:      "old-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 32768,
		MaxOutput:    4096,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
	}
	oldTransport := openai.New(openai.Options{Endpoint: oldServer.URL, Model: first.ModelID, ModelInfo: first})
	sink := &recordingSink{}
	s := newTestSession(t, poo.Options{
		Transport:    oldTransport,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        first,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})

	topP := 0.9
	next := poo.ModelInfo{
		Name:         "next-model",
		ModelID:      "next-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 32768,
		MaxOutput:    512,
		Sampling:     poo.Sampling{TopP: &topP},
		Tools:        poo.ToolCalling{Mode: poo.ToolsText, TextFormat: "auto"},
		Reasoning:    poo.ReasoningControl{Mode: poo.ReasoningToggle, Param: poo.ReasoningPathToggle},
	}
	nextTransport := openai.New(openai.Options{
		Endpoint:  nextServer.URL,
		Model:     next.ModelID,
		ModelInfo: next,
		Reasoning: "low",
	})
	if _, err := s.SetModel(next, nextTransport); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	runSession(t, s, "hi")

	nextServer.mu.Lock()
	n := len(nextServer.bodies)
	nextServer.mu.Unlock()
	if n == 0 {
		t.Fatalf("the new transport got no request; the records are %v", sink.Names())
	}
	body := nextServer.last(t)
	if got := body["max_tokens"]; got != float64(512) {
		t.Errorf("the ceiling is %v, want the new model's 512", got)
	}
	if got := body["top_p"]; got != 0.9 {
		t.Errorf("the top_p is %v, want the new model's 0.9", got)
	}
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != true {
		t.Errorf("the reasoning control did not follow the new model: %v", body["chat_template_kwargs"])
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("the request carries reasoning_effort the new model does not take")
	}
	if tools, ok := body["tools"]; ok && tools != nil {
		t.Errorf("the text-mode request carries native tools: %v", tools)
	}
	oldServer.mu.Lock()
	oldBodies := len(oldServer.bodies)
	oldServer.mu.Unlock()
	if oldBodies != 0 {
		t.Errorf("the old transport got %d requests, want none", oldBodies)
	}
}

// TestTheSwitchReachesTheTransport covers the descriptor the
// request is shaped from after a model switch.
//
// The session's SetModel replaces poo.Options.Model, but the transport
// keeps the poo.ModelInfo it was built with: tools mode, temperature
// control, reasoning control and quirks all still come from the model
// the session started on.
func TestTheSwitchReachesTheTransport(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		mu.Lock()
		bodies = append(bodies, decoded)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	first := poo.ModelInfo{
		Name:         "native-model",
		ModelID:      "native-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 32768,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
		Temperature:  poo.TempFree,
	}
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: first.ModelID, ModelInfo: first})
	s := newTestSession(t, poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": &echoTool{}},
		ToolOrder:    []string{"echo"},
		Sink:         &recordingSink{},
		SessionID:    "s1",
		Model:        first,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	})

	// The new model writes its calls as text, so the request must not
	// carry the native tools array.
	next := poo.ModelInfo{
		Name:         "text-model",
		ModelID:      "text-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		ContextLimit: 32768,
		Tools:        poo.ToolCalling{Mode: poo.ToolsText, TextFormat: "auto"},
		Temperature:  poo.TempFree,
	}
	if _, err := s.SetModel(next); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	runSession(t, s, "hi")

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the endpoint got no request")
	}
	body := bodies[len(bodies)-1]
	if got := body["model"]; got != "text-model" {
		t.Fatalf("the request names the model %v, want text-model", got)
	}
	if tools, ok := body["tools"]; ok && tools != nil {
		t.Errorf("the request still carries the native tools of the old model: %s", fieldOfBody(body, "tools"))
	}
}

// fieldOfBody renders one field of a decoded body.
func fieldOfBody(body map[string]any, field string) string {
	raw, err := json.Marshal(body[field])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
