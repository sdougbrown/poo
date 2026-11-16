package config_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/provider/catalog"
	"github.com/sdougbrown/poo/provider/codex"
	"github.com/sdougbrown/poo/provider/piauth"
)

// writeCodexSettings writes a settings file and returns its path.
func writeCodexSettings(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	return path
}

// fakeCodexToken renders an access token with a Codex account claim. It is a
// JWT in shape only: nothing in poo verifies a signature.
func fakeCodexToken(t *testing.T, account string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account},
	})
	if err != nil {
		t.Fatalf("encoding the token's claims: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// writeCodexAuth writes pi's auth.json beside the settings, with a Codex
// credential and another provider's entry the refresh must keep.
func writeCodexAuth(t *testing.T, dir, access, refresh string, expires int64) string {
	t.Helper()
	body, err := json.MarshalIndent(map[string]any{
		"other": map[string]any{"type": "api_key", "key": "keep-me"},
		piauth.ProviderCodex: map[string]any{
			"type":      "oauth",
			"access":    access,
			"refresh":   refresh,
			"expires":   expires,
			"accountId": "stored",
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("encoding pi's auth file: %v", err)
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing pi's auth file: %v", err)
	}
	return path
}

// codexEndpoint answers the token endpoint and the Codex endpoint as a fake
// HTTP transport. The test reaches Runtime.Transport's real endpoint and never
// the network.
type codexEndpoint struct {
	// accessToken is the token the refresh answers with.
	accessToken string
	// tokenCalls and responsesCalls count the requests.
	tokenCalls    int
	responsesCall int
	// path, authorization, account and body are what the Codex request
	// carried.
	path          string
	authorization string
	account       string
	body          []byte
}

// stream is the smallest complete answer: a finished response and no text.
const stream = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
	"data: [DONE]\n\n"

// RoundTrip answers the two endpoints the Codex transport reaches.
func (e *codexEndpoint) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Host {
	case "auth.openai.com":
		e.tokenCalls++
		body, err := json.Marshal(map[string]any{
			"access_token":  e.accessToken,
			"refresh_token": "rt_fresh",
			"expires_in":    3600,
		})
		if err != nil {
			return nil, err
		}
		return answer(req, "application/json", string(body)), nil
	case "chatgpt.com":
		e.responsesCall++
		e.path = req.URL.Path
		e.authorization = req.Header.Get("Authorization")
		e.account = req.Header.Get("chatgpt-account-id")
		e.body, _ = io.ReadAll(req.Body)
		return answer(req, "text/event-stream", stream), nil
	}
	return nil, fmt.Errorf("the test endpoint received a request for %s", req.URL)
}

// answer returns a response over a body string.
func answer(req *http.Request, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// buildCodex builds a runtime with the tools, profiles and executors the
// config tests link, and the HTTP client the test hands it.
func buildCodex(t *testing.T, settings *config.Settings, client *http.Client) (*config.Runtime, error) {
	t.Helper()
	return config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
		HTTPClient:     client,
	})
}

// TestACodexModelRunsOnPisCredential covers the whole path. The built-in
// openai-codex model reads the auth file the settings name. The expired
// credential pi left behind is refreshed under the same lock, and the request
// carries the refreshed token to the Codex endpoint.
func TestACodexModelRunsOnPisCredential(t *testing.T) {
	dir := t.TempDir()
	oldToken := fakeCodexToken(t, "acct_stored")
	freshToken := fakeCodexToken(t, "acct_fresh")
	authPath := writeCodexAuth(t, dir, oldToken, "rt_old", 1)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}

	endpoint := &codexEndpoint{accessToken: freshToken}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.Name != "openai-codex/gpt-6.1-sol" {
		t.Errorf("the chat model is %q, want the built-in openai-codex model", model.Name)
	}
	if model.Endpoint != codex.DefaultEndpoint {
		t.Errorf("the endpoint is %q, want %q", model.Endpoint, codex.DefaultEndpoint)
	}
	if model.API != poo.APIResponses {
		t.Errorf("the API is %q, want the Responses API", model.API)
	}
	if model.ContextLimit != 500000 || model.MaxOutput != 128000 {
		t.Errorf("the limits are %d/%d, want the built-in 500000/128000", model.ContextLimit, model.MaxOutput)
	}
	if model.Tools.Mode != poo.ToolsNative {
		t.Errorf("the tools mode is %q, want native", model.Tools.Mode)
	}
	if !model.HasOp(poo.OpChat) {
		t.Errorf("the model's ops are %v, want chat", model.Ops)
	}
	if !model.Reads(poo.ModalityImage) {
		t.Errorf("the model's inputs are %v, want image", model.Inputs)
	}
	levels := strings.Join(model.ReasoningLevels, ",")
	if !strings.Contains(levels, "xhigh") {
		t.Errorf("the levels are %v, want the Codex levels", model.ReasoningLevels)
	}
	if strings.Contains(levels, "off") {
		t.Errorf("the levels are %v, and the model has no level that turns reasoning off", model.ReasoningLevels)
	}
	// The built-in endpoint's models are declared, so discovery must leave it
	// alone rather than ask a chat endpoint for a model list.
	if !catalogEndpoint(t, settings, piauth.ProviderCodex).SkipProbe {
		t.Error("the Codex endpoint is probed, want it declared")
	}

	transport, err := runtime.Transport(model)
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	if _, ok := transport.(*codex.Transport); !ok {
		t.Fatalf("the transport is %T, want the Codex transport", transport)
	}
	reader, _, err := transport.Stream(context.Background(), poo.ChatRequest{
		Model:     model.ModelID,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	if endpoint.tokenCalls != 1 {
		t.Errorf("the token endpoint was called %d times, want the one refresh", endpoint.tokenCalls)
	}
	if endpoint.responsesCall != 1 {
		t.Errorf("the Codex endpoint was called %d times, want 1", endpoint.responsesCall)
	}
	if endpoint.path != "/backend-api/codex/responses" {
		t.Errorf("the request path is %q, want the Codex responses path", endpoint.path)
	}
	if endpoint.authorization != "Bearer "+freshToken {
		t.Errorf("the request carried %q, want the refreshed bearer token", endpoint.authorization)
	}
	if endpoint.account != "acct_fresh" {
		t.Errorf("the request carried account %q, want the refreshed token's account", endpoint.account)
	}
	// The model has no level that turns reasoning off, so a request with no
	// level asks for the lowest one it takes.
	var request struct {
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(endpoint.body, &request); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	if request.Reasoning.Effort != "low" {
		t.Errorf("the request asks for reasoning %q, want low", request.Reasoning.Effort)
	}

	// The refresh wrote pi's file back with the Codex entry replaced and every
	// other provider's entry as it was.
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("reading pi's auth file: %v", err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decoding pi's auth file: %v", err)
	}
	if _, ok := entries["other"]; !ok {
		t.Error("the refresh dropped another provider's entry")
	}
	var stored struct {
		Access  string `json:"access"`
		Refresh string `json:"refresh"`
	}
	if err := json.Unmarshal(entries[piauth.ProviderCodex], &stored); err != nil {
		t.Fatalf("decoding the refreshed credential: %v", err)
	}
	if stored.Access != freshToken || stored.Refresh != "rt_fresh" {
		t.Errorf("the file holds %q/%q, want the refreshed pair", stored.Access, stored.Refresh)
	}
}

// TestPisAuthFileBesideTheModelsFileIsUsed covers the alignment with
// models_file. An auth.json beside the models file is pi's auth file, and the
// built-in provider is offered without the settings naming either.
func TestPisAuthFileBesideTheModelsFileIsUsed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ModelsFileName), []byte(`{
		"providers": {"home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]}}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	catalogModel(t, settings, "openai-codex/gpt-6.1-sol")

	endpoint := &codexEndpoint{accessToken: token}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
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
	if _, ok := transport.(*codex.Transport); !ok {
		t.Fatalf("the transport is %T, want the Codex transport", transport)
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
	if endpoint.tokenCalls != 0 {
		t.Errorf("the token endpoint was called %d times, want none for a live token", endpoint.tokenCalls)
	}
	if endpoint.authorization != "Bearer "+token {
		t.Errorf("the request carried %q, want the live token", endpoint.authorization)
	}
}

// TestAMissingPiAuthFileIsARefusal covers the explicit path's promise. A file
// the settings name is the file they mean. One that is not there fails the
// load rather than falling back to a sibling.
func TestAMissingPiAuthFileIsARefusal(t *testing.T) {
	dir := t.TempDir()
	writeCodexAuth(t, dir, fakeCodexToken(t, "acct_live"), "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: missing.json
session:
  mode: ephemeral
`)
	_, err := config.Load(path)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the load is %v, want a settings failure", err)
	}
	if failure.Field != "pi_auth_file" {
		t.Errorf("the failure names %q, want pi_auth_file", failure.Field)
	}
}

// TestTheSettingsWinOverTheBuiltinProvider covers the precedence: the built-in
// provider is a floor, and a model the settings configure is the one that runs.
func TestTheSettingsWinOverTheBuiltinProvider(t *testing.T) {
	dir := t.TempDir()
	writeCodexAuth(t, dir, fakeCodexToken(t, "acct_live"), "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
endpoints:
  openai-codex:
    endpoint: https://chatgpt.com/backend-api
models:
  openai-codex/gpt-6.1-sol:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    tools_mode: native
    context_limit: 1234
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got := settings.Models["openai-codex/gpt-6.1-sol"].ContextLimit; got != 1234 {
		t.Errorf("the model's context limit is %d, want the settings' 1234", got)
	}
}

// TestABuiltinProviderNeedsTheAuthFileItReads covers the gate. A models file
// on its own is a provider list, not a pi login, so pi's Codex models need
// pi's auth.json beside it.
func TestABuiltinProviderNeedsTheAuthFileItReads(t *testing.T) {
	isolatedConfigDir(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ModelsFileName), []byte(`{
		"providers": {"home": {"baseUrl": "http://127.0.0.1:1/v1", "models": [{"id": "qwen"}]}}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: home/qwen
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	// The built-in provider is the models file's layer, not the settings' own maps, so
	// the check is on what the layer offers without pi's auth file: the file's own
	// provider is there, and the built-in one is not.
	layer := settings.CatalogOptions().File
	if _, ok := layer.Endpoints[piauth.ProviderCodex]; ok {
		t.Fatal("the built-in Codex endpoint was offered without pi's auth file")
	}
	for name := range layer.Models {
		if strings.HasPrefix(name, piauth.ProviderCodex+"/") {
			t.Fatalf("the built-in Codex model %s was offered without pi's auth file", name)
		}
	}
}

// catalogModel resolves a model the settings' catalog holds, for the tests that
// read what a models file contributed.
func catalogModel(t *testing.T, settings *config.Settings, name string) poo.ModelInfo {
	t.Helper()
	model, err := catalog.New(settings.CatalogOptions()).Resolve(name)
	if err != nil {
		t.Fatalf("the catalog resolves no %s: %v", name, err)
	}
	return model
}

// catalogEndpoint returns the endpoint the settings' catalog holds.
func catalogEndpoint(t *testing.T, settings *config.Settings, name string) catalog.Endpoint {
	t.Helper()
	endpoint, ok := catalog.New(settings.CatalogOptions()).Endpoints()[name]
	if !ok {
		t.Fatalf("the catalog holds no %s endpoint", name)
	}
	return endpoint
}

// isolatedConfigDir points the user's config directory where poo resolves its
// own default files at a directory of the test's own, and returns the
// directory the platform answered with.
//
// Each platform reads its own variable, so all of them are pointed at the same
// place. The skip is the guard: a platform that answers elsewhere skips rather
// than reading the developer's own config.
func isolatedConfigDir(t *testing.T) string {
	t.Helper()
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("HOME", config)
	t.Setenv("APPDATA", config)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("the user config directory is not resolvable here: %v", err)
	}
	if !strings.HasPrefix(dir, config) {
		t.Skipf("this platform's user config directory is outside the test's directory: %s", dir)
	}
	return dir
}

// TestACodexModelWithoutANamedCredentialTakesTheDefaultFile covers the file a
// Codex model reads when no setting names one. poo resolves its own credential
// file, and a model built from it is not refused before a run: the credential's
// absence is what a request reports.
func TestACodexModelWithoutANamedCredentialTakesTheDefaultFile(t *testing.T) {
	dir := isolatedConfigDir(t)
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
endpoints:
  openai-codex:
    endpoint: https://chatgpt.com/backend-api
models:
  my-codex:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    tools_mode: native
roles:
  chat: my-codex
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	want := filepath.Join(dir, "poo", config.AuthFileName)
	if got := settings.CredentialFile(); got != want {
		t.Errorf("the credential file is %q, want poo's own at %q", got, want)
	}
	if _, err := buildCodex(t, settings, nil); err != nil {
		t.Fatalf("the model was refused for a credential file a login creates: %v", err)
	}
}

// TestACodexModelRefusesAnotherEndpoint covers the credential's promise. pi's
// OAuth token is for the built-in Codex endpoint, and a provider pointed
// elsewhere is refused rather than sent the token.
func TestACodexModelRefusesAnotherEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeCodexAuth(t, dir, fakeCodexToken(t, "acct_live"), "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
endpoints:
  openai-codex:
    endpoint: http://127.0.0.1:1/v1
models:
  my-codex:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
    api: responses
    tools_mode: native
roles:
  chat: my-codex
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	_, err = buildCodex(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the build is %v, want a settings failure", err)
	}
	if failure.Field != "endpoints.openai-codex.endpoint" {
		t.Errorf("the failure names %q, want the Codex endpoint", failure.Field)
	}
	if !strings.Contains(failure.Message, "sends pi's credential only to") {
		t.Errorf("the failure is %q, want it to say where the credential may go", failure.Message)
	}
}

// loopbackOnly records requests and refuses non-loopback hosts.
type loopbackOnly struct {
	allowed  string
	requests []string
}

func (l *loopbackOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	l.requests = append(l.requests, req.URL.Host)
	if req.URL.Host != l.allowed {
		return nil, fmt.Errorf("the test refused a request to %s", req.URL)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// TestAPiModelsFileCannotPointCodexElsewhere covers the models file's reach.
// pi's models file may name openai-codex itself, so a file that gives the
// provider a foreign baseUrl would offer models whose endpoint is that host.
// Selecting one is refused with the endpoint's config error, before a token
// refresh or any other request goes out.
func TestAPiModelsFileCannotPointCodexElsewhere(t *testing.T) {
	dir := t.TempDir()
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 1)

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, stream)
	}))
	defer foreign.Close()

	if err := os.WriteFile(filepath.Join(dir, config.ModelsFileName), []byte(`{
		"providers": {
			"openai-codex": {
				"baseUrl": "`+foreign.URL+`",
				"api": "openai-codex-responses",
				"models": [{"id": "gpt-6.1-sol", "contextWindow": 111111, "maxTokens": 4096}]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	if got := catalogEndpoint(t, settings, "openai-codex").Endpoint; got != foreign.URL {
		t.Fatalf("the endpoint is %q, want the models file's %q", got, foreign.URL)
	}
	if got := catalogModel(t, settings, "openai-codex/gpt-6.1-sol").ContextLimit; got != 111111 {
		t.Fatalf("the model's context limit is %d, want the models file's 111111", got)
	}

	client := &loopbackOnly{allowed: strings.TrimPrefix(foreign.URL, "http://")}

	// Building the runtime selects the chat model and its transport, so the
	// refusal is what the build reports.
	_, err = buildCodex(t, settings, &http.Client{Transport: client})
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the build is %v, want a settings failure", err)
	}
	if failure.Code != config.CodeUnsupported {
		t.Errorf("the failure's code is %q, want %q", failure.Code, config.CodeUnsupported)
	}
	if failure.Field != "endpoints.openai-codex.endpoint" {
		t.Errorf("the failure names %q, want the Codex endpoint", failure.Field)
	}
	// No request reached either the foreign endpoint or pi's token endpoint.
	if len(client.requests) != 0 {
		t.Errorf("the build made %v, want no request", client.requests)
	}
}

// TestAResponsesModelOnAnotherProviderIsRefused covers the identification:
// the Responses API alone does not make a model Codex, and no transport poo
// ships speaks it for another provider. The model is refused with a message
// naming its provider, rather than sent a Chat Completions request that
// endpoint cannot read.
func TestAResponsesModelOnAnotherProviderIsRefused(t *testing.T) {
	path := writeCodexSettings(t, t.TempDir(), `version: 1
session:
  mode: ephemeral
endpoints:
  local:
    endpoint: http://127.0.0.1:1/v1
models:
  local-responses:
    endpoint: local
    model_id: gpt-6.1-sol
    api: responses
    tools_mode: native
roles:
  chat: local-responses
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	_, err = buildCodex(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) || failure.Code != config.CodeUnsupported {
		t.Fatalf("the build is %v, want the Responses API refused", err)
	}
	if failure.Field != "models.local-responses.api" {
		t.Errorf("the failure names %q, want the model's api field", failure.Field)
	}
}

// TestADeciderRequestForACodexModelIsRefused covers the request path. A
// decider can be named directly, and a Codex model named that way is refused
// before the systemone transport is built.
func TestADeciderRequestForACodexModelIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeCodexAuth(t, dir, fakeCodexToken(t, "acct_live"), "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, nil)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	// DeciderFor is what the wire's configure resolves a decider name
	// through. The name is a settings model, so the role check never sees
	// it, and the transport cannot ask the Codex endpoint a systemone
	// question.
	model, decider, err := runtime.DeciderFor("openai-codex/gpt-6.1-sol")
	if err == nil {
		t.Fatal("the Codex model was accepted as a decider")
	}
	if decider != nil {
		t.Errorf("the Codex model produced a decider: %T", decider)
	}
	if model.Name != "" {
		t.Errorf("the refused decider returned the model %q", model.Name)
	}
	var failure *config.Error
	if !errors.As(err, &failure) || failure.Code != config.CodeUnsupported {
		t.Errorf("the failure is %v, want the unsupported settings failure", err)
	}
}

// writeAuthBeside writes pi's auth.json into dir with an api key for the home
// provider and a live Codex credential, and returns its path.
func writeAuthBeside(t *testing.T, dir, key, token string) string {
	t.Helper()
	body, err := json.MarshalIndent(map[string]any{
		"home": map[string]any{"type": "api_key", "key": key},
		piauth.ProviderCodex: map[string]any{
			"type":      "oauth",
			"access":    token,
			"refresh":   "rt_live",
			"expires":   4102444800000,
			"accountId": "stored",
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("encoding pi's auth file: %v", err)
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing pi's auth file: %v", err)
	}
	return path
}

// TestAModelsFileDefinitionWinsOverTheBuiltinCodexModel covers the collision
// on the built-in provider id. A models file that defines openai-codex is the
// definition that runs, and the built-in models it does not name are still
// offered. The models file names the pinned endpoint, which is the one poo
// sends pi's credential to, so the model it defines is not refused.
func TestAModelsFileDefinitionWinsOverTheBuiltinCodexModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ModelsFileName), []byte(`{
		"providers": {
			"openai-codex": {
				"baseUrl": "`+codex.DefaultEndpoint+`",
				"api": "openai-codex-responses",
				"models": [
					{
						"id": "gpt-6.1-sol",
						"input": ["text", "image"],
						"contextWindow": 111111,
						"maxTokens": 4096,
						"reasoning": true,
						"compat": {"supportsReasoningEffort": true}
					}
				]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}

	if got := catalogEndpoint(t, settings, "openai-codex").Endpoint; got != codex.DefaultEndpoint {
		t.Errorf("the endpoint is %q, want the pinned %q", got, codex.DefaultEndpoint)
	}
	// The models file's definition of the colliding model survives.
	if got := catalogModel(t, settings, "openai-codex/gpt-6.1-sol").ContextLimit; got != 111111 {
		t.Errorf("the colliding model's context limit is %d, want the models file's 111111", got)
	}
	// The built-in models the file does not name are still offered.
	builtin := catalogModel(t, settings, "openai-codex/gpt-5.6-luna")
	if builtin.ContextLimit != 272000 || builtin.MaxOutput != 128000 {
		t.Errorf("the surviving built-in model's limits are %d/%d, want 272000/128000", builtin.ContextLimit, builtin.MaxOutput)
	}
	if builtin.Tools.Mode != poo.ToolsNative {
		t.Errorf("the surviving built-in model's tools mode is %q, want native", builtin.Tools.Mode)
	}

	endpoint := &codexEndpoint{accessToken: token}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.ContextLimit != 111111 || model.MaxOutput != 4096 {
		t.Errorf("the chat model's limits are %d/%d, want the models file's 111111/4096", model.ContextLimit, model.MaxOutput)
	}
	// The pinned endpoint is what makes the transport build: a models file that
	// pointed the provider elsewhere would be refused here.
	transport, err := runtime.Transport(model)
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	if _, ok := transport.(*codex.Transport); !ok {
		t.Errorf("the transport is %T, want the Codex transport", transport)
	}
	surviving, err := runtime.Catalog.Resolve("openai-codex/gpt-5.6-luna")
	if err != nil {
		t.Fatalf("resolving the surviving built-in model: %v", err)
	}
	if surviving.ContextLimit != 272000 || surviving.MaxOutput != 128000 {
		t.Errorf("the surviving built-in model resolves with %d/%d, want 272000/128000", surviving.ContextLimit, surviving.MaxOutput)
	}
}

// TestTheChatReasoningSettingReachesTheCodexRequest covers the configured
// level: reasoning.chat is the operator's, and a Codex request goes out asking
// for it rather than for the model's fallback.
func TestTheChatReasoningSettingReachesTheCodexRequest(t *testing.T) {
	dir := t.TempDir()
	token := fakeCodexToken(t, "acct_live")
	writeCodexAuth(t, dir, token, "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
reasoning:
  chat: high
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}

	endpoint := &codexEndpoint{accessToken: token}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
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
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	var request struct {
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(endpoint.body, &request); err != nil {
		t.Fatalf("decoding the request: %v", err)
	}
	if request.Reasoning.Effort != "high" {
		t.Errorf("the request asks for reasoning %q, want the configured high", request.Reasoning.Effort)
	}
}

// TestAModelsFileInAnotherDirectoryTakesTheAuthBesideIt covers the path the
// credential resolves against. The auth.json beside a models file in another
// directory is the one read, not a same-named file beside the settings. Both
// halves are observable — the imported api key and the token the Codex request
// carries.
func TestAModelsFileInAnotherDirectoryTakesTheAuthBesideIt(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	modelsDir := filepath.Join(root, "pi")
	for _, dir := range []string{settingsDir, modelsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(modelsDir, config.PiModelsFileName), []byte(`{
		"providers": {
			"home": {
				"baseUrl": "http://127.0.0.1:1/v1",
				"models": [{"id": "qwen"}]
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("writing the models file: %v", err)
	}
	adjacentToken := fakeCodexToken(t, "acct_adjacent")
	writeAuthBeside(t, modelsDir, "adjacent-key", adjacentToken)
	settingsToken := fakeCodexToken(t, "acct_settings")
	writeAuthBeside(t, settingsDir, "settings-key", settingsToken)
	path := writeCodexSettings(t, settingsDir, `version: 1
models_file: ../pi/models.json
session:
  mode: ephemeral
roles:
  chat: openai-codex/gpt-6.1-sol
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}

	if key := settings.Endpoints["home"].APIKey; key != "adjacent-key" {
		t.Errorf("the provider imported with the key %q, want the key beside the models file", key)
	}
	catalogModel(t, settings, "openai-codex/gpt-6.1-sol")

	endpoint := &codexEndpoint{accessToken: adjacentToken}
	runtime, err := buildCodex(t, settings, &http.Client{Transport: endpoint})
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
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	if endpoint.authorization != "Bearer "+adjacentToken {
		t.Errorf("the request carried %q, want the token beside the models file", endpoint.authorization)
	}
	if endpoint.account != "acct_adjacent" {
		t.Errorf("the request carried account %q, want the account beside the models file", endpoint.account)
	}
}

// TestACodexModelWithoutTheResponsesAPIIsRefused covers the API the session
// reads. A model declared under the Codex endpoint without the Responses API
// would run on the Codex transport while the session treated it as
// chat-completions, stripping the reasoning blocks the storeless endpoint
// depends on. The setting is refused, and the refusal names what to add.
func TestACodexModelWithoutTheResponsesAPIIsRefused(t *testing.T) {
	dir := t.TempDir()
	writeCodexAuth(t, dir, fakeCodexToken(t, "acct_live"), "rt_live", 4102444800000)
	path := writeCodexSettings(t, dir, `version: 1
pi_auth_file: auth.json
session:
  mode: ephemeral
endpoints:
  openai-codex:
    endpoint: https://chatgpt.com/backend-api
models:
  codex-without-api:
    endpoint: openai-codex
    model_id: gpt-6.1-sol
roles:
  chat: codex-without-api
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	_, err = buildCodex(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) || failure.Code != config.CodeUnsupported {
		t.Fatalf("the build is %v, want the missing API refused", err)
	}
	if failure.Field != "models.codex-without-api.api" {
		t.Errorf("the failure names %q, want the model's api field", failure.Field)
	}
}
