package poo_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

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
	tr := openai.New(openai.Options{BaseURL: srv.URL, Model: first.ModelID, ModelInfo: first})
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
