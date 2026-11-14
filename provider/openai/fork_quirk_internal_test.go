package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// TestAForkShapesTheMessagesItAppends covers the fork's appended messages: a
// message the fork adds is shaped by the profiles the snapshot was shaped with.
//
// The request names the profile and the transport's own model info does not, so
// a fork that fell back to the transport's info would send the appended part in
// the spec's shape.
func TestAForkShapesTheMessagesItAppends(t *testing.T) {
	// A profile of the shape a model on another server needs: the part that
	// carries a picture is named differently, and a parameter goes with it.
	registerQuirk(quirkProfile{
		name: "test_part_rename",
		shapeRequest: func(s *requestShape) {
			s.setParam("test_param", true)
			for i := range s.request.Messages {
				parts, ok := s.request.Messages[i].Content.([]chatContentPart)
				if !ok {
					continue
				}
				for j := range parts {
					if parts[j].ImageURL != nil {
						parts[j].Type = "test_image"
					}
				}
			}
		},
	})

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"+"data: [DONE]\n\n")
	}))
	defer srv.Close()

	tr := New(Options{Endpoint: srv.URL, Model: "m"})
	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "m",
		// The request's own info, which the transport's configuration does
		// not carry.
		ModelInfo: poo.ModelInfo{Name: "m", Quirks: []string{"test_part_rename"}},
		MaxTokens: 1024,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "the prompt")},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if len(rendered.Body) == 0 {
		t.Fatal("the snapshot holds no body")
	}

	picture := poo.ToolResultMessage("call_1", "read", "read: shot.png", false,
		[]poo.Image{{MediaType: "image/png", Data: []byte{1, 2, 3}}})
	reader, err := tr.Fork(context.Background(), rendered, []poo.Message{picture}, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("the server received %d bodies, want the request and the fork", len(bodies))
	}
	var decoded struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &decoded); err != nil {
		t.Fatalf("decoding the fork's body: %v", err)
	}
	if len(decoded.Messages) != 3 {
		t.Fatalf("the fork holds %d messages, want the prompt, the result, and the picture", len(decoded.Messages))
	}
	parts, _ := decoded.Messages[2].Content.([]any)
	if len(parts) != 1 {
		t.Fatalf("the appended picture produced %d parts, want one", len(parts))
	}
	part, _ := parts[0].(map[string]any)
	if part["type"] != "test_image" {
		t.Errorf("the appended part is %v, want the shape the model's quirks name", part["type"])
	}

	// The snapshot is re-sent as it was built, apart from the messages the
	// fork adds and the ceiling it asks for.
	if first, _ := decoded.Messages[0].Content.(string); first != "the prompt" {
		t.Errorf("the snapshot's first message is %q, want it unchanged", first)
	}
	if !strings.Contains(bodies[1], `"max_tokens":2048`) {
		t.Errorf("the fork does not carry its own ceiling: %s", bodies[1])
	}
}
