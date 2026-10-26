package poo_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

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
	oldTransport := openai.New(openai.Config{BaseURL: oldServer.URL, Model: first.ModelID, Info: first})
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
	nextTransport := openai.New(openai.Config{
		BaseURL:   nextServer.URL,
		Model:     next.ModelID,
		Info:      next,
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
