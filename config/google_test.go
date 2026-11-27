package config_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// planEndpoint is the provider id the plan's endpoint is declared under, which
// is also the name its credential is stored by.
const planEndpoint = "google-ai-plan"

// googleRuntime builds a runtime whose settings hold the Gemini key's
// credential, and whose HTTP client is the fake endpoint.
func googleRuntime(t *testing.T, gateway *googleGateway) (*config.Runtime, *config.Settings) {
	t.Helper()
	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"google": "gm_test"})
	path := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: google/gemini-4-hydrogen
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

// googleGateway answers the Interactions endpoint as a fake of it: the request
// is recorded, and the answer is one text step and the event that completes the
// interaction.
type googleGateway struct {
	mu sync.Mutex
	// path is the path the last request reached.
	path string
	// key is the key header the last request carried.
	key string
	// authorization is the Authorization header the last request carried.
	authorization string
	// bodies are the request bodies the requests carried.
	bodies []string
}

// RoundTrip answers every request with a stream.
func (g *googleGateway) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.path = req.URL.Path
	g.key = req.Header.Get("x-goog-api-key")
	g.authorization = req.Header.Get("Authorization")
	var raw []byte
	if req.Body != nil {
		raw, _ = io.ReadAll(req.Body)
	}
	g.bodies = append(g.bodies, string(raw))
	body := "event: step.start\ndata: {\"event_type\":\"step.start\",\"index\":0,\"step\":{\"type\":\"model_output\"}}\n\n" +
		"event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":\"hi\"}}\n\n" +
		"event: step.stop\ndata: {\"event_type\":\"step.stop\",\"index\":0}\n\n" +
		"event: interaction.completed\ndata: {\"event_type\":\"interaction.completed\",\"interaction\":{\"id\":\"v1_abc\",\"status\":\"completed\"}}\n\n" +
		"event: done\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// TestTheBuiltinGoogleEndpointServesOnItsKey covers the whole path a Google
// request takes: the built-in endpoint wakes on the credential's key, the model
// resolves to the Interactions API, and the transport reaches the endpoint's
// path with the key in the header the API reads it from.
func TestTheBuiltinGoogleEndpointServesOnItsKey(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoint is offered or which key answers.
	t.Setenv("GEMINI_API_KEY", "")

	g := &googleGateway{}
	runtime, settings := googleRuntime(t, g)

	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.Name != "google/gemini-4-hydrogen" {
		t.Fatalf("the chat model is %q, want the built-in Google model", model.Name)
	}
	if model.API != poo.APIGoogleInteractions {
		t.Errorf("the model speaks %q, want the Interactions API", model.API)
	}
	// The endpoint's model list is not the OpenAI probe's shape, so the record
	// is the word on what it serves.
	if !catalogEndpoint(t, settings, "google").SkipProbe {
		t.Error("the google endpoint is probed, want it declared")
	}
	if got := runtime.EndpointKey(model); got != "gm_test" {
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
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var text string
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		if ev.Kind == "delta" && ev.Delta.Kind == "text" {
			text += ev.Delta.Text
		}
	}
	if text != "hi" {
		t.Errorf("the answer is %q, want the streamed text", text)
	}

	// The build composes the interactions transport for this model, and it
	// reaches the endpoint the record declares with the key the credential
	// holds.
	if g.path != "/v1beta/interactions" {
		t.Errorf("the request reached %q, want the interaction path", g.path)
	}
	if g.key != "gm_test" {
		t.Errorf("the key header is %q, want the stored key", g.key)
	}
	if !strings.Contains(g.bodies[0], `"store":false`) {
		t.Errorf("the request is not stateless: %s", g.bodies[0])
	}
}

// planKey is a P-256 key pair as the credential file holds one: the grant of a
// plan sign-in is bound to it, and the file's entry is refused without it.
func planKey(t *testing.T) map[string]string {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("creating the key: %v", err)
	}
	coordinate := func(value *big.Int) string {
		out := make([]byte, 32)
		value.FillBytes(out)
		return base64.RawURLEncoding.EncodeToString(out)
	}
	return map[string]string{
		"kty": "EC", "crv": "P-256",
		"x": coordinate(private.X), "y": coordinate(private.Y), "d": coordinate(private.D),
	}
}

// TestThePlanEndpointSendsTheSignInsToken covers the second Google provider: the
// built-in endpoint is offered on the sign-in's grant rather than a key, and the
// transport reaches the plan's own quota method with the token in the header the
// API takes an OAuth access token in.
func TestThePlanEndpointSendsTheSignInsToken(t *testing.T) {
	// The endpoint is offered on the file's grant, not a variable, so both key
	// variables are pinned empty: neither decides it.
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("POO_GOOGLE_CLIENT_ID", "app-client")
	t.Setenv("POO_GOOGLE_CLIENT_SECRET", "app-secret")

	dir := t.TempDir()
	key, err := json.Marshal(planKey(t))
	if err != nil {
		t.Fatalf("encoding the key: %v", err)
	}
	entry := `{"type":"oauth","access":"the-access","refresh":"the-refresh",` +
		`"expires":` + fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()) + `,` +
		`"clientId":"app-client","tokenUrl":"https://oauth2.googleapis.com/token",` +
		`"dpopKey":` + string(key) + `}`
	credentials := `{"version":1,"credentials":{"` + planEndpoint + `":` + entry + `}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(credentials), 0o600); err != nil {
		t.Fatalf("writing the auth file: %v", err)
	}
	settingsPath := writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: `+planEndpoint+`/gemini-4-hydrogen
`)
	settings, err := config.Load(settingsPath)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	g := &googleGateway{}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: g})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.Provider != planEndpoint {
		t.Fatalf("the chat model is served by %q, want the plan's endpoint", model.Provider)
	}
	transport, err := runtime.Transport(model)
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	reader, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Model:     model.ModelID,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	for {
		if _, err := reader.Next(); err != nil {
			break
		}
	}

	// The plan's endpoint is its own path, and the token is a bearer token
	// rather than the key header: the two credentials enter the API at two
	// different doors.
	if want := "/v1beta/interactions:createPerUserQuota"; g.path != want {
		t.Errorf("the request reached %q, want %q", g.path, want)
	}
	if g.key != "" {
		t.Errorf("the request carried the key header %q, want a bearer token", g.key)
	}
	if g.authorization != "Bearer the-access" {
		t.Errorf("the request carried %q, want the sign-in's token", g.authorization)
	}
}
