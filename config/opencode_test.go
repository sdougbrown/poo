package config_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// The OpenCode Console's provider ids, as the built-in record names its
// endpoints.
const (
	opencodePAYG = "opencode"
	opencodeGo   = "opencode-go"
)

// writeOpencodeAuth writes poo's own auth file with the entries it is given,
// keyed by provider id.
func writeOpencodeAuth(t *testing.T, dir string, entries map[string]string) string {
	t.Helper()
	credentials := "version: 1\ncredentials:\n"
	for provider, key := range entries {
		credentials += "  " + provider + ":\n    type: api_key\n    key: " + key + "\n"
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(credentials), 0o600); err != nil {
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
	// agent is the User-Agent header the last request carried.
	agent string
	// paths are the request paths the requests reached.
	paths []string
	// bodies are the request bodies the requests carried.
	bodies []string
}

// RoundTrip answers the routes the catalog and the transports reach. A
// messages request gets a stream; a chat request gets one too; anything
// else gets the model list.
func (g *opencodeGateway) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.session = req.Header.Get("x-opencode-session")
	g.authorization = req.Header.Get("Authorization")
	g.agent = req.Header.Get("User-Agent")
	g.paths = append(g.paths, req.URL.Path)
	raw, _ := io.ReadAll(req.Body)
	g.bodies = append(g.bodies, string(raw))
	body := `{"data":[{"id":"glm-5.3"}]}`
	if strings.HasSuffix(req.URL.Path, "/chat/completions") {
		body = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
	} else if strings.HasSuffix(req.URL.Path, "/messages") {
		body = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// opencodeRuntime builds a runtime whose settings hold the key entries the
// test names, and whose HTTP client is the fake gateway.
func opencodeRuntime(t *testing.T, dir string, entries map[string]string, roles string, gateway *opencodeGateway) (*config.Runtime, *config.Settings) {
	t.Helper()
	writeOpencodeAuth(t, dir, entries)
	path := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: `+roles+`
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: gateway})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return runtime, settings
}

// TestTheBuiltinOpencodeEndpointsAreOfferedOnTheirKey covers the offering:
// the built-in endpoints wake when their key is there, both read the one
// credential — a key belongs to a workspace whose plan says which endpoint
// bills it — and the gateway lists its models, so the endpoints are probed
// rather than pinned to the record's snapshot.
func TestTheBuiltinOpencodeEndpointsAreOfferedOnTheirKey(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	dir := t.TempDir()
	runtime, settings := opencodeRuntime(t, dir, map[string]string{"opencode": "oc_sk_test"}, "opencode-go/glm-5.3", &opencodeGateway{})

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
	if catalogEndpoint(t, settings, opencodeGo).SkipProbe {
		t.Error("the opencode-go endpoint is declared, want it probed")
	}
	if got := runtime.EndpointKey(model); got != "oc_sk_test" {
		t.Errorf("the key is %q, want the stored credential", got)
	}
	// The pay-as-you-go endpoint is offered beside the Go one, and the same
	// credential serves it.
	pay, err := runtime.Catalog.Resolve(opencodePAYG + "/glm-5.3")
	if err != nil {
		t.Fatalf("resolving the pay-as-you-go model: %v", err)
	}
	if got := runtime.EndpointKey(pay); got != "oc_sk_test" {
		t.Errorf("the pay-as-you-go key is %q, want the same stored credential", got)
	}
}

// TestTheBuiltinOpencodeEndpointsSleepOnAnotherProvidersCredential covers
// the sharper gate: a credential file holding another provider's credential
// does not wake an endpoint this key cannot reach, and a chat role naming a
// sleeping model is a settings failure that names the model.
func TestTheBuiltinOpencodeEndpointsSleepOnAnotherProvidersCredential(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"ollama-cloud": "an-ollama-key"})
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
	_, err = buildCodex(t, settings, &http.Client{Transport: &opencodeGateway{}})
	if err == nil {
		t.Fatal("the build offered the opencode-go model on an Ollama key, want it refused")
	}
	if !strings.Contains(err.Error(), "opencode-go/glm-5.3") {
		t.Errorf("the refusal is %q, want it to name the model", err)
	}
}

// TestTheBuiltinOpencodeEndpointsWakeOnTheVariable covers the variable's own
// rung: with no OpenCode entry in the credential file, the variable the
// endpoints read is what offers them.
func TestTheBuiltinOpencodeEndpointsWakeOnTheVariable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCODE_API_KEY", "oc_sk_from_env")
	runtime, _ := opencodeRuntime(t, dir, map[string]string{"ollama-cloud": "an-ollama-key"}, "opencode-go/glm-5.3", &opencodeGateway{})

	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if got := runtime.EndpointKey(model); got != "oc_sk_from_env" {
		t.Errorf("the key is %q, want the variable's", got)
	}
}

// TestTheBuiltinOpencodeEndpointsWakeOnTheSettingsLiteral covers the literal's
// own rung: a settings endpoint of the layer's own name carrying api_key is a
// key the transport resolves, so the gate keeps the models it names.
func TestTheBuiltinOpencodeEndpointsWakeOnTheSettingsLiteral(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"ollama-cloud": "an-ollama-key"})
	path := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
endpoints:
  opencode-go:
    endpoint: https://opencode.ai/inference/go/openai/v1
    api_key: oc_sk_from_settings
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
	if got := runtime.EndpointKey(model); got != "oc_sk_from_settings" {
		t.Errorf("the key is %q, want the settings' literal", got)
	}
}

// TestTheOpencodeTransportNamesTheSession covers the header the gateway
// requires: the request carries the session's id, so the gateway routes one
// conversation together, and the stored key is the bearer token. Both
// endpoints declare the header, so each one's transport sends it.
func TestTheOpencodeTransportNamesTheSession(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	for _, name := range []string{opencodeGo, opencodePAYG} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			gateway := &opencodeGateway{}
			runtime, _ := opencodeRuntime(t, dir, map[string]string{"opencode": "oc_sk_test"}, name+"/glm-5.3", gateway)
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
		})
	}
}

// TestTheBuiltinAnthropicEndpointsServeOnTheirKey covers the Messages pair:
// offered on the same key as the Chat Completions one, and the transport
// names the session through the endpoint's own declared header.
func TestTheBuiltinAnthropicEndpointsServeOnTheirKey(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	dir := t.TempDir()
	gateway := &opencodeGateway{}
	runtime, _ := opencodeRuntime(t, dir, map[string]string{"opencode": "oc_sk_test"}, "opencode-go-anthropic/claude-haiku-5-5", gateway)

	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.Name != "opencode-go-anthropic/claude-haiku-5-5" {
		t.Fatalf("the chat model is %q, want the built-in anthropic model", model.Name)
	}
	if model.Endpoint != "https://opencode.ai/inference/go/anthropic/v1" {
		t.Errorf("the endpoint is %q, want the Messages gateway", model.Endpoint)
	}
	if model.API != poo.APIAnthropicMessages {
		t.Errorf("the API is %q, want the Messages API", model.API)
	}
	if got := runtime.EndpointKey(model); got != "oc_sk_test" {
		t.Errorf("the key is %q, want the stored credential", got)
	}
	transport, err := runtime.Transport(model)
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	reader, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Model:     model.ModelID,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 32,
		SessionID: "ses_anthropic_test",
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}
	if gateway.session != "ses_anthropic_test" {
		t.Errorf("the request carried session %q, want the session's id", gateway.session)
	}
	if gateway.authorization != "Bearer oc_sk_test" {
		t.Errorf("the request carried %q, want the stored key", gateway.authorization)
	}
	// The binary names itself, so the gateway that watches its traffic
	// reads a client and not the HTTP library that carried it.
	if gateway.agent != "poo/test" {
		t.Errorf("the request named itself %q, want the build's own name", gateway.agent)
	}
	// The Messages transport is the one that served: the request went to
	// the messages path, in the Messages API's own shape.
	if !strings.HasSuffix(gateway.paths[len(gateway.paths)-1], "/v1/messages") {
		t.Errorf("the request went to %q, want the messages path", gateway.paths[len(gateway.paths)-1])
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(gateway.bodies[len(gateway.bodies)-1]), &sent); err != nil {
		t.Fatalf("the request body does not decode: %v", err)
	}
	if sent["max_tokens"] != float64(32) || sent["stream"] != true {
		t.Errorf("the request is %v, want a streaming Messages request with the output limit", sent)
	}
	// The pay-as-you-go sibling is offered beside the Go one.
	if _, err := runtime.Catalog.Resolve("opencode-anthropic/claude-haiku-5-5"); err != nil {
		t.Fatalf("resolving the pay-as-you-go model: %v", err)
	}
}

// TestEachEndpointReadsItsOwnStoredKey covers the join: two providers' keys in
// one file, and each endpoint takes its own, so one provider's key never
// reaches the other's gateway.
func TestEachEndpointReadsItsOwnStoredKey(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoints are offered or which key answers.
	t.Setenv("OPENCODE_API_KEY", "")

	dir := t.TempDir()
	runtime, _ := opencodeRuntime(t, dir, map[string]string{
		"ollama-cloud": "an-ollama-key",
		"opencode":     "oc_sk_test",
	}, "opencode-go/glm-5.3", &opencodeGateway{})

	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if got := runtime.EndpointKey(model); got != "oc_sk_test" {
		t.Errorf("the opencode-go key is %q, want the OpenCode credential", got)
	}
	pay, err := runtime.Catalog.Resolve(opencodePAYG + "/glm-5.3")
	if err != nil {
		t.Fatalf("resolving the pay-as-you-go model: %v", err)
	}
	if got := runtime.EndpointKey(pay); got != "oc_sk_test" {
		t.Errorf("the pay-as-you-go key is %q, want the OpenCode credential", got)
	}
}
