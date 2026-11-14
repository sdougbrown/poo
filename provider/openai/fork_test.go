package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The fork sends the snapshot's body with the appended messages added and
// the output ceiling replaced. Everything else is byte-identical, so the
// server's prefix cache can hit.

// forkServer records the request bodies the server received.
type forkServer struct {
	*httptest.Server
	// bodies holds the raw request bodies, in order.
	bodies []string
	// headers holds the request headers, in order.
	headers []http.Header
}

// newForkServer starts a server that answers with an SSE body.
func newForkServer(t *testing.T) *forkServer {
	t.Helper()
	s := &forkServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.bodies = append(s.bodies, string(raw))
		s.headers = append(s.headers, r.Header.Clone())

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`)+done)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// TestTheForkSendsTheSamePrefix covers the fork: the body is the
// snapshot's body with the appended message added and the ceiling
// replaced, and the prefix is byte-identical.
func TestTheForkSendsTheSamePrefix(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m"})

	req := poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "the prompt"),
		},
	}
	_, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if len(rendered.Body) == 0 {
		t.Fatal("the snapshot holds no body")
	}

	appended := []poo.Message{poo.NewTextMessage(poo.RoleUser, "the appended message")}
	reader, err := tr.Fork(context.Background(), rendered, appended, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.bodies) != 2 {
		t.Fatalf("the server received %d bodies, want 2", len(srv.bodies))
	}
	first, second := srv.bodies[0], srv.bodies[1]

	// The prefix is the same: everything up to the last message of the
	// original body.
	prefix := first[:strings.LastIndex(first, "]")]
	if !strings.HasPrefix(second, prefix) {
		t.Errorf("the fork's prefix differs from the original:\n%s\n%s", first, second)
	}

	var forked map[string]any
	if err := json.Unmarshal([]byte(second), &forked); err != nil {
		t.Fatalf("decoding the fork's body: %v", err)
	}
	if forked["max_tokens"] != float64(2048) {
		t.Errorf("the fork's ceiling is %v, want 2048", forked["max_tokens"])
	}
	messages, ok := forked["messages"].([]any)
	if !ok {
		t.Fatalf("the fork holds no messages: %s", second)
	}
	if len(messages) != 2 {
		t.Fatalf("the fork holds %d messages, want 2", len(messages))
	}
	last, _ := messages[1].(map[string]any)
	if last["content"] != "the appended message" {
		t.Errorf("the appended message is %v, want the appended message", last["content"])
	}
	if forked["model"] != "m" {
		t.Errorf("the fork's model is %v, want m", forked["model"])
	}
	if forked["stream"] != true {
		t.Errorf("the fork's stream flag is %v, want true", forked["stream"])
	}
}

// TestTheForkWithoutASnapshotIsRefused covers the error: a fork needs a
// body to fork.
func TestTheForkWithoutASnapshotIsRefused(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m"})

	if _, err := tr.Fork(context.Background(), poo.Rendered{}, nil, 0); err == nil {
		t.Error("the fork of an empty snapshot succeeded, want a failure")
	}
}

// TestTheForkKeepsTheBodyKeyOrder covers the key order: the forked body
// has the same keys in the same order, so the prefix is byte-identical.
func TestTheForkKeepsTheBodyKeyOrder(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m"})

	req := poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "the prompt"),
		},
	}
	_, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}

	reader, err := tr.Fork(context.Background(), rendered, nil, 0)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.bodies) != 2 {
		t.Fatalf("the server received %d bodies, want 2", len(srv.bodies))
	}
	// A fork that appends nothing and changes nothing is the same body.
	if srv.bodies[0] != srv.bodies[1] {
		t.Errorf("the fork's body differs from the original:\n%s\n%s", srv.bodies[0], srv.bodies[1])
	}
}

// TestAForkOfAnotherAPIsSnapshotIsRefused covers a snapshot that another
// transport rendered.
func TestAForkOfAnotherAPIsSnapshotIsRefused(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m"})

	_, err := tr.Fork(context.Background(), poo.Rendered{API: poo.APIResponses, Body: []byte(`{"messages":[]}`)}, nil, 16)
	var pe *poo.ProviderError
	if !errors.As(err, &pe) || pe.Kind != poo.ProviderKindInvalidRequest {
		t.Fatalf("the error is %v, want an invalid_request provider error", err)
	}
	if len(srv.bodies) != 0 {
		t.Errorf("the server received %d bodies, want none", len(srv.bodies))
	}
}

// openaiBodyServer collects the bodies it is sent.
type openaiBodyServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

// newFix1BodyServer starts a server that answers every request with a
// plain answer and keeps the body.
func newFix1BodyServer(t *testing.T) *openaiBodyServer {
	t.Helper()
	s := &openaiBodyServer{}
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

// TestTheForkDiffersOnlyInTheMessagesAndTheCeiling covers the fork item exactly: "The bodies are byte-identical except for
// the appended messages and max_tokens."
//
// The shipped test compares the prefix up to the messages array, which
// says nothing about the rest of the body. This one removes the two
// fields the fork is allowed to change and compares what is left.
func TestTheForkDiffersOnlyInTheMessagesAndTheCeiling(t *testing.T) {
	srv := newFix1BodyServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m", ToolsMode: "native"})

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
			Schema:      []byte(`{"type":"object","properties":{"text":{"type":"string"}},"additionalProperties":false}`),
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

// TestAForkCarriesAnAppendedImage covers the fork's appended messages: a tool
// result that carries a picture is sent the way a normal request sends it, so
// the handoff fork does not drop it.
func TestAForkCarriesAnAppendedImage(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Options{Endpoint: srv.URL, Model: "m"})

	req := poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "the prompt"),
		},
	}
	_, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}

	appended := []poo.Message{poo.ToolResultMessage("call_1", "read", "read: shot.png", false,
		[]poo.Attachment{{Modality: poo.ModalityImage, MediaType: "image/png", Data: []byte{1, 2, 3}}})}
	reader, err := tr.Fork(context.Background(), rendered, appended, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.bodies) != 2 {
		t.Fatalf("the server received %d bodies, want 2", len(srv.bodies))
	}
	var forked map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[1]), &forked); err != nil {
		t.Fatalf("decoding the fork's body: %v", err)
	}
	messages, _ := forked["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("the fork holds %d messages, want prompt, tool, and the picture", len(messages))
	}
	last, _ := messages[2].(map[string]any)
	parts, _ := last["content"].([]any)
	if len(parts) != 1 {
		t.Fatalf("the appended picture produced %d parts, want one", len(parts))
	}
	part, _ := parts[0].(map[string]any)
	imageURL, _ := part["image_url"].(map[string]any)
	url, _ := imageURL["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("the forked picture is %q, want a data url", url)
	}
}
