package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// fix1BodyServer collects the bodies it is sent.
type fix1BodyServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

// newFix1BodyServer starts a server that answers every request with a
// plain answer and keeps the body.
func newFix1BodyServer(t *testing.T) *fix1BodyServer {
	t.Helper()
	s := &fix1BodyServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, b.String())
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(s.Close)
	return s
}

// stripField removes one top-level field's value from a body, leaving
// the rest of the bytes alone.
func stripField(t *testing.T, body, field string) string {
	t.Helper()
	key := `"` + field + `":`
	i := strings.Index(body, key)
	if i < 0 {
		t.Fatalf("the body has no %s: %s", field, body)
	}
	rest := body[i+len(key):]
	depth := 0
	inString := false
	escaped := false
	end := -1
	if rest[0] == '[' || rest[0] == '{' {
		for j := 0; j < len(rest); j++ {
			c := rest[j]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == '"' {
					inString = false
				}
				continue
			}
			switch c {
			case '"':
				inString = true
			case '[', '{':
				depth++
			case ']', '}':
				depth--
				if depth == 0 {
					end = j + 1
				}
			}
			if end >= 0 {
				break
			}
		}
	} else {
		for j := 0; j < len(rest); j++ {
			if rest[j] == ',' || rest[j] == '}' {
				end = j
				break
			}
		}
	}
	if end < 0 {
		t.Fatalf("the field %s does not end: %s", field, body)
	}
	return body[:i] + rest[end:]
}

// TestTheForkDiffersOnlyInTheMessagesAndTheCeiling covers the
// plan's fork item exactly: "The bodies are byte-identical except for
// the appended messages and max_tokens."
//
// The shipped test compares the prefix up to the messages array, which
// says nothing about the rest of the body. This one removes the two
// fields the fork is allowed to change and compares what is left.
func TestTheForkDiffersOnlyInTheMessagesAndTheCeiling(t *testing.T) {
	srv := newFix1BodyServer(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "native"})

	req := poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "the prompt"),
			poo.NewTextMessage(poo.RoleAssistant, "an answer"),
		},
		Tools: []poo.ToolSpec{{
			Name:        "echo",
			Description: "echo",
			Schema:      []byte(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		}},
	}
	_, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	reader, err := tr.Fork(context.Background(), rendered, []poo.Message{
		poo.NewTextMessage(poo.RoleUser, "the appended message"),
	}, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()

	if len(srv.bodies) != 2 {
		t.Fatalf("the server got %d bodies, want 2", len(srv.bodies))
	}
	first := stripField(t, stripField(t, srv.bodies[0], "messages"), "max_tokens")
	second := stripField(t, stripField(t, srv.bodies[1], "messages"), "max_tokens")
	if first != second {
		t.Errorf("the bodies differ outside messages and max_tokens:\n%s\n%s", first, second)
	}
}
