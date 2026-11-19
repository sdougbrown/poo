package config_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/auth"
)

// writeOpencodeAuth writes poo's own auth file with an OpenCode key entry.
func writeOpencodeAuth(t *testing.T, dir, key string) string {
	t.Helper()
	body := `{"version": 1, "credentials": {"opencode": {"type": "api_key", "key": "` + key + `"}}}`
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	return path
}

// opencodeGateway answers the model list and the chat endpoint as a fake of
// the Console gateway. The runtime is built with it as the HTTP client's
// transport, so the catalog's probes and the transport's requests reach the
// fake and never the network.
type opencodeGateway struct {
	mu sync.Mutex
	// session is the x-opencode-session header the last request carried.
	session string
	// authorization is the Authorization header the last request carried.
	authorization string
}

// RoundTrip answers the routes the catalog and the transport reach. A chat
// request gets a stream; anything else gets the model list.
func (g *opencodeGateway) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.session = req.Header.Get("x-opencode-session")
	g.authorization = req.Header.Get("Authorization")
	body := `{"data":[{"id":"glm-5.3"}]}`
	if strings.HasSuffix(req.URL.Path, "/chat/completions") {
		body = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestTheBuiltinOpencodeEndpointsAreOfferedOnACredential covers the offering:
// an auth file with an OpenCode key is what wakes the built-in endpoints. Both
// endpoints read the one credential, because a key belongs to a workspace
// whose plan says which endpoint bills it, and the gateway lists its models,
// so the endpoints are probed rather than pinned to the record's snapshot.
func TestTheBuiltinOpencodeEndpointsAreOfferedOnACredential(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeAuth(t, dir, "oc_sk_test")
	path := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: opencode-go/glm-5.3
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}

	runtime, err := buildCodex(t, settings, &http.Client{Transport: &opencodeGateway{}})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.Name != "opencode-go/glm-5.3" {
		t.Fatalf("the chat model is %q, want the built-in opencode-go model", model.Name)
	}
	if model.Endpoint != "https://opencode.ai/inference/go/openai/v1" {
		t.Errorf("the endpoint is %q, want the Go gateway", model.Endpoint)
	}
	if catalogEndpoint(t, settings, auth.ProviderOpencodeGo).SkipProbe {
		t.Error("the opencode-go endpoint is declared, want it probed")
	}
	if got := runtime.EndpointKey(model); got != "oc_sk_test" {
		t.Errorf("the key is %q, want the stored credential", got)
	}
	// The pay-as-you-go endpoint is offered beside the Go one, and the same
	// credential serves it.
	_, err = runtime.Catalog.Resolve("opencode/glm-5.3")
	if err != nil {
		t.Fatalf("resolving the pay-as-you-go model: %v", err)
	}
}

// TestTheOpencodeTransportNamesTheSession covers the header the gateway
// requires: the request carries the session's id, so the gateway routes one
// conversation together, and the stored key is the bearer token.
func TestTheOpencodeTransportNamesTheSession(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeAuth(t, dir, "oc_sk_test")
	gateway := &opencodeGateway{}
	path := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: opencode-go/glm-5.3
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: gateway})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	transport, err := runtime.Transport(model)
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	reader, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Model:     model.ModelID,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 32,
		SessionID: "ses_opencode_test",
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	if gateway.session != "ses_opencode_test" {
		t.Errorf("the request carried session %q, want the session's id", gateway.session)
	}
	if gateway.authorization != "Bearer oc_sk_test" {
		t.Errorf("the request carried %q, want the stored key", gateway.authorization)
	}
}
