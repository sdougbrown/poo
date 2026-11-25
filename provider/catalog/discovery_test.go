package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/catalog"
)

// vllmServer serves the OpenAI-compatible model list.
//
// The list is the shape a vLLM server answers with, and every other
// path is not there.
func vllmServer(t *testing.T, modelID string, maxModelLen int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": modelID, "object": "model", "max_model_len": maxModelLen},
			},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// llamaServer serves llama.cpp's server props.
func llamaServer(t *testing.T, nCtx int, cachePrompt bool, template string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_generation_settings": map[string]any{
				"n_ctx":        nCtx,
				"cache_prompt": cachePrompt,
			},
			"chat_template": template,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// ollamaServer serves Ollama's model show.
func ollamaServer(t *testing.T, contextLength int, capabilities []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"template":     "{{ if enable_thinking }}<think>{{ end }}",
			"model_info":   map[string]any{"gemma3.context_length": contextLength},
			"capabilities": capabilities,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// settingsFor returns the settings of an endpoint that serves gemma4.
func settingsFor(baseURL string) catalog.Settings {
	return catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"local": {Endpoint: baseURL},
		},
		Models: map[string]catalog.ModelSettings{
			"gemma4": {Endpoint: "local", ModelID: "gemma4"},
		},
	}
}

// TestTheProbeReadsTheLimitsAndCapabilitiesAGatewayReports covers the list
// shape a gateway that knows its models serves: the plain OpenAI shape with
// a limit object, capabilities and modalities added, which is what the
// OpenCode Console answers. A field the list does not carry stays unknown,
// so the plain shape keeps reading as it did.
func TestTheProbeReadsTheLimitsAndCapabilitiesAGatewayReports(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":            "glm-5.3",
					"max_model_len": 8192,
					"limit": map[string]any{
						"context": 1000000,
						"output":  131072,
					},
					"capabilities": map[string]any{
						"tools":     map[string]any{"supported": true},
						"reasoning": map[string]any{"effort": []string{"low", "high", "max"}},
					},
					"modalities": map[string]any{
						"input": []string{"text", "image/jpeg", "audio/wav", "image"},
					},
				},
				{
					"id": "plain-7b",
					"capabilities": map[string]any{
						"tools": map[string]any{"supported": false},
					},
				},
			},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	stateDir := t.TempDir()
	c := catalog.New(catalog.Options{
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"gateway": {Endpoint: server.URL + "/v1"},
			},
		},
		StateDir: stateDir,
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}

	glm, err := c.Resolve("gateway/glm-5.3")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	// The limit object wins when the list carries one; max_model_len is the
	// plain shape's answer.
	if glm.ContextLimit != 1000000 || glm.MaxOutput != 131072 {
		t.Errorf("the limits are %d/%d, want the reported 1000000/131072", glm.ContextLimit, glm.MaxOutput)
	}
	if glm.Tools.Mode != poo.ToolsNative {
		t.Errorf("the tools mode is %q, want native", glm.Tools.Mode)
	}
	if glm.Reasoning.Mode != poo.ReasoningEffort {
		t.Errorf("the reasoning mode is %q, want effort", glm.Reasoning.Mode)
	}
	if strings.Join(glm.ReasoningLevels, ",") != "low,high,max" {
		t.Errorf("the reasoning levels are %v, want the reported ones", glm.ReasoningLevels)
	}
	for _, modality := range []poo.Modality{poo.ModalityText, poo.ModalityImage, poo.ModalityAudio} {
		if !glm.Reads(modality) {
			t.Errorf("the inputs are %v, want %v among them", glm.Inputs, modality)
		}
	}
	for _, layer := range []string{"context_limit", "max_output", "tools.mode", "reasoning.mode", "reasoning.levels", "inputs"} {
		if glm.Source[layer] != catalog.LayerDiscovery {
			t.Errorf("%s comes from %q, want discovery", layer, glm.Source[layer])
		}
	}

	// A model the list says nothing about keeps what it reported: no limits,
	// no reasoning control, and tools the capabilities refuse.
	plain, err := c.Resolve("gateway/plain-7b")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if plain.ContextLimit != 0 || plain.MaxOutput != 0 {
		t.Errorf("the limits are %d/%d, want none reported", plain.ContextLimit, plain.MaxOutput)
	}
	if plain.Tools.Mode != poo.ToolsNone {
		t.Errorf("the tools mode is %q, want none", plain.Tools.Mode)
	}
	if plain.Reasoning.Mode != "" {
		t.Errorf("the reasoning mode is %q, want none reported", plain.Reasoning.Mode)
	}
	if len(plain.Inputs) != 0 {
		t.Errorf("the inputs are %v, want none reported", plain.Inputs)
	}

	// A session's build reads the cache rather than probing, so what the
	// probe learned has to survive the round trip.
	cached := catalog.New(catalog.Options{
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"gateway": {Endpoint: server.URL + "/v1"},
			},
		},
		StateDir: stateDir,
		Cached:   true,
	})
	stored, err := cached.Resolve("gateway/glm-5.3")
	if err != nil {
		t.Fatalf("the cached model: %v", err)
	}
	if stored.MaxOutput != 131072 {
		t.Errorf("the cached max output is %d, want the reported one", stored.MaxOutput)
	}
	if strings.Join(stored.ReasoningLevels, ",") != "low,high,max" {
		t.Errorf("the cached reasoning levels are %v, want the reported ones", stored.ReasoningLevels)
	}
	if !stored.Reads(poo.ModalityImage) {
		t.Errorf("the cached inputs are %v, want the image the list reported", stored.Inputs)
	}
}

// TestTheProbeReadsTheLimitsEachGatewayShapeReports covers the places
// gateways put the token budgets: LiteLLM's max_output_tokens, OpenRouter's
// context_length and top_provider, and the opencode limit object. An input
// cap is not the context window, so it never sets the context limit.
func TestTheProbeReadsTheLimitsEachGatewayShapeReports(t *testing.T) {
	cases := []struct {
		name        string
		item        map[string]any
		wantContext int
		wantOutput  int
	}{
		{
			name:        "litellm",
			item:        map[string]any{"id": "litellm", "object": "model", "owned_by": "openai", "max_input_tokens": 262144, "max_output_tokens": 65536},
			wantContext: 0,
			wantOutput:  65536,
		},
		{
			name: "openrouter",
			item: map[string]any{
				"id":             "openrouter",
				"context_length": 200000,
				"top_provider":   map[string]any{"context_length": 180000, "max_completion_tokens": 32000},
			},
			wantContext: 200000,
			wantOutput:  32000,
		},
		{
			name: "openrouter-unlimited-completion",
			item: map[string]any{
				"id":           "openrouter-unlimited-completion",
				"top_provider": map[string]any{"context_length": 131072, "max_completion_tokens": nil},
			},
			wantContext: 131072,
			wantOutput:  0,
		},
		{
			name:        "opencode-input-cap",
			item:        map[string]any{"id": "opencode-input-cap", "limit": map[string]any{"context": 400000, "input": 272000, "output": 128000}},
			wantContext: 400000,
			wantOutput:  128000,
		},
		{
			name: "two-sources-per-budget",
			item: map[string]any{
				"id":                "two-sources-per-budget",
				"limit":             map[string]any{"context": 300000, "output": 50000},
				"context_length":    900000,
				"max_output_tokens": 70000,
				"top_provider":      map[string]any{"context_length": 800000, "max_completion_tokens": 60000},
			},
			wantContext: 300000,
			wantOutput:  50000,
		},
		{
			name: "two-sources-without-limit-object",
			item: map[string]any{
				"id":                "two-sources-without-limit-object",
				"context_length":    900000,
				"max_output_tokens": 70000,
				"top_provider":      map[string]any{"context_length": 800000, "max_completion_tokens": 60000},
			},
			wantContext: 900000,
			wantOutput:  70000,
		},
	}

	items := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		items = append(items, tc.item)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": items})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"gateway": {Endpoint: server.URL + "/v1"},
			},
		},
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, err := c.Resolve("gateway/" + tc.name)
			if err != nil {
				t.Fatalf("the model: %v", err)
			}
			if model.ContextLimit != tc.wantContext || model.MaxOutput != tc.wantOutput {
				t.Errorf("the limits are %d/%d, want %d/%d", model.ContextLimit, model.MaxOutput, tc.wantContext, tc.wantOutput)
			}
		})
	}
}

// TestTheProbeReadsTheInputCapAGatewayReports covers the largest prompt an
// endpoint takes when it is smaller than the window: the opencode limit
// object's input and LiteLLM's max_input_tokens. The cap is its own limit, and
// the context window stays what the list says it is.
func TestTheProbeReadsTheInputCapAGatewayReports(t *testing.T) {
	cases := []struct {
		name        string
		item        map[string]any
		wantInput   int
		wantContext int
	}{
		{
			name:        "opencode",
			item:        map[string]any{"id": "opencode", "limit": map[string]any{"context": 400000, "input": 272000, "output": 128000}},
			wantInput:   272000,
			wantContext: 400000,
		},
		{
			name:        "litellm",
			item:        map[string]any{"id": "litellm", "max_input_tokens": 262144, "max_output_tokens": 65536},
			wantInput:   262144,
			wantContext: 0,
		},
		{
			name:        "both-shapes",
			item:        map[string]any{"id": "both-shapes", "limit": map[string]any{"input": 200000}, "max_input_tokens": 100000},
			wantInput:   200000,
			wantContext: 0,
		},
		{
			name:        "no-cap",
			item:        map[string]any{"id": "no-cap", "limit": map[string]any{"context": 131072}},
			wantInput:   0,
			wantContext: 131072,
		},
		{
			name:        "malformed-cap",
			item:        map[string]any{"id": "malformed-cap", "limit": map[string]any{"context": 131072, "input": -1}, "max_input_tokens": "lots"},
			wantInput:   0,
			wantContext: 131072,
		},
	}

	items := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		items = append(items, tc.item)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": items})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"gateway": {Endpoint: server.URL + "/v1"},
		},
	}
	stateDir := t.TempDir()
	c := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	cached := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for label, cat := range map[string]*catalog.Catalog{"the probe": c, "the cache": cached} {
				model, err := cat.Resolve("gateway/" + tc.name)
				if err != nil {
					t.Fatalf("%s: the model: %v", label, err)
				}
				if model.InputLimit != tc.wantInput || model.ContextLimit != tc.wantContext {
					t.Errorf("%s: the limits are %d input / %d context, want %d / %d", label, model.InputLimit, model.ContextLimit, tc.wantInput, tc.wantContext)
				}
			}
		})
	}

	explained, fields, err := c.Explain("gateway/opencode")
	if err != nil {
		t.Fatalf("explaining the model: %v", err)
	}
	if explained.InputLimit != 272000 {
		t.Fatalf("the explained input limit is %d, want 272000", explained.InputLimit)
	}
	var found bool
	for _, field := range fields {
		if field.Field == "input_limit" {
			found = true
			if field.Value != "272000" || field.Layer != catalog.LayerDiscovery {
				t.Errorf("the input_limit row is %q from %q, want 272000 from discovery", field.Value, field.Layer)
			}
		}
	}
	if !found {
		t.Error("the explanation has no input_limit row")
	}
}

// TestAMalformedLimitCostsOnlyThatLimit covers a gateway that writes a token
// budget in a form the plain integer does not take. A whole float, an
// exponent and a numeric string read as the number they spell. A negative,
// a fraction, text, a value past what a window can be and a non-numeric
// type read as unknown, and the model beside them still resolves.
func TestAMalformedLimitCostsOnlyThatLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"valid","limit":{"context":131072,"output":16384}},
			{"id":"spelled","limit":{"context":1e5,"output":"8192"}},
			{"id":"whole-float","limit":{"context":65536.0,"output":4096.0}},
			{"id":"malformed","limit":{"context":-1,"output":12.5},"max_output_tokens":99999999999,"context_length":true,"max_model_len":"lots",
			 "top_provider":{"context_length":{"n":1},"max_completion_tokens":[1]}}
		]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"gateway": {Endpoint: server.URL + "/v1"},
			},
		},
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}

	for name, want := range map[string][2]int{
		"valid":       {131072, 16384},
		"spelled":     {100000, 8192},
		"whole-float": {65536, 4096},
		"malformed":   {0, 0},
	} {
		model, err := c.Resolve("gateway/" + name)
		if err != nil {
			t.Fatalf("the model %s: %v", name, err)
		}
		if model.ContextLimit != want[0] || model.MaxOutput != want[1] {
			t.Errorf("%s: the limits are %d/%d, want %d/%d", name, model.ContextLimit, model.MaxOutput, want[0], want[1])
		}
	}
}

// TestAnUnknownListedLimitLeavesTheContextToLlamaCPP covers a list that
// sends -1 for a model's context: the limit reads as unknown, so the
// server's n_ctx still fills it.
func TestAnUnknownListedLimitLeavesTheContextToLlamaCPP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gemma4","max_model_len":-1,"limit":{"context":-1}}]}`))
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_generation_settings": map[string]any{"n_ctx": 32768},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{Settings: settingsFor(server.URL + "/v1"), StateDir: t.TempDir()})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	model, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if model.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want llama.cpp's 32768", model.ContextLimit)
	}
}

// TestADiscoveredModelSpeaksItsEndpointsAPI covers the inheritance: a model
// no layer gave an API to speaks the one its endpoint declares, and a model
// on an endpoint that declares none keeps the chat-completions default.
func TestADiscoveredModelSpeaksItsEndpointsAPI(t *testing.T) {
	server := vllmServer(t, "glm-5.3", 32768)
	settings := func(api poo.API) catalog.Settings {
		return catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"gateway": {Endpoint: server.URL + "/v1", API: api},
			},
		}
	}
	declared := catalog.New(catalog.Options{Settings: settings(poo.APIAnthropicMessages), StateDir: t.TempDir()})
	if err := declared.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	model, err := declared.Resolve("gateway/glm-5.3")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if model.API != poo.APIAnthropicMessages {
		t.Errorf("the API is %q, want the endpoint's", model.API)
	}

	plain := catalog.New(catalog.Options{Settings: settings(""), StateDir: t.TempDir()})
	if err := plain.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	model, err = plain.Resolve("gateway/glm-5.3")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if model.API != poo.APIChatCompletions {
		t.Errorf("the API is %q, want the chat-completions default", model.API)
	}

	// A model's own word wins: the layer that named the model says which
	// API it speaks, and the endpoint's declaration answers only for the
	// models no layer named.
	named := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"gateway": {Endpoint: server.URL + "/v1", API: poo.APIAnthropicMessages},
		},
		Models: map[string]catalog.ModelSettings{
			"gateway/glm-5.3": {Endpoint: "gateway", ModelID: "glm-5.3", API: poo.APIChatCompletions},
		},
	}
	overridden := catalog.New(catalog.Options{Settings: named, StateDir: t.TempDir()})
	if err := overridden.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	named2, err := overridden.Resolve("gateway/glm-5.3")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if named2.API != poo.APIChatCompletions {
		t.Errorf("the API is %q, want the model's own word", named2.API)
	}
}

// TestTheProbeNamesTheClient covers the agent the probes travel under: a
// catalog that names one sends it, and a catalog that names none leaves the
// header to the HTTP library, the way an operator's own client would.
func TestTheProbeNamesTheClient(t *testing.T) {
	var agents []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "gemma4"}},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		Settings:  settingsFor(server.URL + "/v1"),
		StateDir:  t.TempDir(),
		UserAgent: "poo/test",
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("the probe never reached the endpoint")
	}
	for _, agent := range agents {
		if agent != "poo/test" {
			t.Errorf("a probe named itself %q, want poo/test", agent)
			break
		}
	}
	// A catalog that names no client sends no header, so the HTTP library
	// names the request and an operator's own choice stands.
	agents = nil
	bare := catalog.New(catalog.Options{
		Settings: settingsFor(server.URL + "/v1"),
		StateDir: t.TempDir(),
	})
	if err := bare.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for _, agent := range agents {
		if !strings.HasPrefix(agent, "Go-http-client/") {
			t.Errorf("a nameless catalog named itself %q, want the HTTP library's own default", agent)
			break
		}
	}
}

func TestTheVLLMProbeReadsTheModelList(t *testing.T) {
	server := vllmServer(t, "gemma4", 32768)
	c := catalog.New(catalog.Options{
		Settings: settingsFor(server.URL + "/v1"),
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want the probed 32768", info.ContextLimit)
	}
	if info.Source["context_limit"] != catalog.LayerDiscovery {
		t.Errorf("the context limit comes from %q, want discovery", info.Source["context_limit"])
	}
}

func TestTheLlamaCPPProbeReadsTheContext(t *testing.T) {
	server := llamaServer(t, 65536, true, "{{ if enable_thinking }}<think>{{ end }}")
	c := catalog.New(catalog.Options{
		Settings: settingsFor(server.URL + "/v1"),
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 65536 {
		t.Errorf("the context limit is %d, want the probed 65536", info.ContextLimit)
	}
	if info.PrefixCache == nil || !*info.PrefixCache {
		t.Errorf("the prefix cache is %v, want it reported as cached", info.PrefixCache)
	}
	if info.Reasoning.Mode != "toggle" {
		t.Errorf("the reasoning control is %q, want toggle", info.Reasoning.Mode)
	}
}

func TestTheOllamaProbeReadsTheModel(t *testing.T) {
	server := ollamaServer(t, 131072, []string{"completion", "tools"})
	c := catalog.New(catalog.Options{
		Settings: settingsFor(server.URL),
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 131072 {
		t.Errorf("the context limit is %d, want the probed 131072", info.ContextLimit)
	}
	if info.Tools.Mode != "native" {
		t.Errorf("the tools mode is %q, want native", info.Tools.Mode)
	}
}

func TestTheProbeFallsBackToTheCacheWithAWarning(t *testing.T) {
	var probes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gemma4", "max_model_len": 32768}},
		})
	})
	server := httptest.NewServer(mux)
	state := t.TempDir()

	var log bytes.Buffer
	c := catalog.New(catalog.Options{
		Settings: settingsFor(server.URL + "/v1"),
		StateDir: state,
		Logger:   slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if probes.Load() == 0 {
		t.Fatal("the endpoint was not probed")
	}
	if _, err := os.Stat(filepath.Join(state, "poo", "discovery.json")); err != nil {
		t.Fatalf("the cache was not written: %v", err)
	}

	// The endpoint goes away: the probe fails and the cache answers.
	server.Close()
	log.Reset()
	c = catalog.New(catalog.Options{
		Settings: settingsFor(server.URL + "/v1"),
		StateDir: state,
		Logger:   slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err := c.Reprobe(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want the cached 32768", info.ContextLimit)
	}
	if !strings.Contains(log.String(), "could not be probed") {
		t.Errorf("the log is %q, want it to name the failed probe", log.String())
	}
	if !strings.Contains(log.String(), "cached result") {
		t.Errorf("the log is %q, want it to name the cached result", log.String())
	}
}

func TestAFreshCacheIsNotProbedAgain(t *testing.T) {
	var probes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gemma4", "max_model_len": 32768}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	state := t.TempDir()
	settings := settingsFor(server.URL + "/v1")

	c := catalog.New(catalog.Options{Settings: settings, StateDir: state})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	after := probes.Load()

	c = catalog.New(catalog.Options{Settings: settings, StateDir: state})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if probes.Load() != after {
		t.Errorf("the endpoint was probed %d times, want %d", probes.Load(), after)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the context limit is %d, want the cached 32768", info.ContextLimit)
	}
}

func TestAStaleCacheIsProbedAgain(t *testing.T) {
	var probes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gemma4", "max_model_len": 32768}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	state := t.TempDir()
	settings := settingsFor(server.URL + "/v1")

	c := catalog.New(catalog.Options{Settings: settings, StateDir: state})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	after := probes.Load()

	// The cache goes stale, and the endpoint is asked again.
	c = catalog.New(catalog.Options{
		Settings:     settings,
		StateDir:     state,
		DiscoveryTTL: time.Nanosecond,
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if probes.Load() <= after {
		t.Errorf("the endpoint was probed %d times, want more than %d", probes.Load(), after)
	}
}

func TestARefreshProbesEveryEndpoint(t *testing.T) {
	var probes atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gemma4", "max_model_len": 32768}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	state := t.TempDir()
	settings := settingsFor(server.URL + "/v1")

	c := catalog.New(catalog.Options{Settings: settings, StateDir: state})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	after := probes.Load()

	c = catalog.New(catalog.Options{Settings: settings, StateDir: state})
	if err := c.Reprobe(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if probes.Load() <= after {
		t.Errorf("the endpoint was probed %d times, want more than %d", probes.Load(), after)
	}
}

func TestAFailedProbeFallsBackToTheCuratedCatalog(t *testing.T) {
	var log bytes.Buffer
	c := catalog.New(catalog.Options{
		Settings: settingsFor("http://127.0.0.1:1/v1"),
		StateDir: t.TempDir(),
		Logger:   slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	info, err := c.Resolve("gemma4")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 262144 {
		t.Errorf("the context limit is %d, want the curated 262144", info.ContextLimit)
	}
	if info.Source["context_limit"] != catalog.LayerCurated {
		t.Errorf("the context limit comes from %q, want curated", info.Source["context_limit"])
	}
	if !strings.Contains(log.String(), "could not be probed") {
		t.Errorf("the log is %q, want it to name the failed probe", log.String())
	}
}

// TestACachedDiscoveryIsAModelASessionCanName covers the composition this refactor is for: an
// endpoint's own report reaches the catalog without a probe, so a model nobody declared is a
// model a role can name — and the models file still gets the last word on it.
func TestACachedDiscoveryIsAModelASessionCanName(t *testing.T) {
	server := vllmServer(t, "qwen3", 32768)
	stateDir := t.TempDir()
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"local": {Endpoint: server.URL + "/v1"}},
	}

	// One refresh writes the cache: what the endpoint serves, once.
	probed := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir})
	if err := probed.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	server.Close()

	// The session's build reads the cache and never asks the closed endpoint.
	built := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true})
	info, err := built.Resolve("local/qwen3")
	if err != nil {
		t.Fatalf("a model the endpoint reported is not resolvable: %v", err)
	}
	if info.ContextLimit != 32768 {
		t.Errorf("the model's context limit is %d, want the reported 32768", info.ContextLimit)
	}
	if info.Source["context_limit"] != catalog.LayerDiscovery {
		t.Errorf("the context limit comes from %q, want discovery", info.Source["context_limit"])
	}

	// A models file overrules the report, field by field.
	override := catalog.New(catalog.Options{
		Settings: settings,
		File: catalog.FileSettings{
			Models: map[string]catalog.ModelSettings{
				"local/qwen3": {Endpoint: "local", ModelID: "qwen3", ContextLimit: 4096},
			},
		},
		StateDir: stateDir,
		Cached:   true,
	})
	info, err = override.Resolve("local/qwen3")
	if err != nil {
		t.Fatalf("the overridden model: %v", err)
	}
	if info.ContextLimit != 4096 || info.Source["context_limit"] != catalog.LayerModelsFile {
		t.Errorf("the context limit is %d from %q, want the models file's 4096",
			info.ContextLimit, info.Source["context_limit"])
	}
}

// TestACachedEndpointTheCatalogNoLongerDeclaresIsNotAModel covers the operator's removal: a
// cache entry whose endpoint the catalog no longer declares is not a model a session can
// name or list, even though the cache still holds it. The endpoint that stayed keeps its
// cached models.
func TestACachedEndpointTheCatalogNoLongerDeclaresIsNotAModel(t *testing.T) {
	first := vllmServer(t, "qwen3", 32768)
	second := vllmServer(t, "qwen2", 8192)
	stateDir := t.TempDir()
	both := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"aaa": {Endpoint: first.URL + "/v1"},
			"zzz": {Endpoint: second.URL + "/v1"},
		},
	}
	probed := catalog.New(catalog.Options{Settings: both, StateDir: stateDir})
	if err := probed.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}

	// The operator removes the zzz endpoint; the cache still holds what it reported.
	remaining := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"aaa": {Endpoint: first.URL + "/v1"}},
	}
	built := catalog.New(catalog.Options{Settings: remaining, StateDir: stateDir, Cached: true})
	if _, err := built.Resolve("zzz/qwen2"); err == nil {
		t.Error("a cached model whose endpoint the catalog no longer declares resolved")
	}
	for _, info := range built.List() {
		if strings.HasPrefix(info.Name, "zzz/") {
			t.Errorf("the catalog still lists %q, whose endpoint was removed", info.Name)
		}
	}
	if _, err := built.Resolve("aaa/qwen3"); err != nil {
		t.Errorf("the endpoint that stayed lost its cached model: %v", err)
	}
}

// TestACatalogWithoutTheCacheAsksNoEndpoint covers the default: a catalog that did not ask for
// the cached layer holds what its layers declare, and a discovered-only model is not one of them.
func TestACatalogWithoutTheCacheAsksNoEndpoint(t *testing.T) {
	server := vllmServer(t, "qwen3", 32768)
	stateDir := t.TempDir()
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"local": {Endpoint: server.URL + "/v1"}},
	}
	if err := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir}).Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	built := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir})
	if _, err := built.Resolve("local/qwen3"); err == nil {
		t.Fatal("a catalog without the cached layer resolved a model only discovery knows")
	}
}

// TestTheCachedLayerAndARefreshReadTheCacheTheSame pins the one-reader rule: the build's
// cached layer and a refresh-built catalog answer the same set of models from one cache.
// The cache holds an entry for a removed endpoint, an entry with no models, and a stale
// entry with models; the live endpoint answers nothing, so both readers must take
// live/qwen3 from the stale cache — the refresh's fallback is what the agreement is about.
func TestTheCachedLayerAndARefreshReadTheCacheTheSame(t *testing.T) {
	stateDir := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"version":1,"fetched_at":"` + old + `","endpoints":{` +
		`"gone":{"fetched_at":"` + old + `","shape":"vllm","models":{"ghost":{"model_id":"ghost","context_limit":1}}},` +
		`"empty":{"fetched_at":"` + old + `","models":{}},` +
		`"live":{"fetched_at":"` + old + `","shape":"vllm","models":{"qwen3":{"model_id":"qwen3","context_limit":32768}}}}}`
	cachePath := filepath.Join(stateDir, "poo", "discovery.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("making the state directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the discovery cache: %v", err)
	}
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"live":  {Endpoint: "http://127.0.0.1:2/v1"},
			"empty": {Endpoint: "http://127.0.0.1:1/v1"},
		},
	}

	names := func(c *catalog.Catalog) map[string]bool {
		set := map[string]bool{}
		for _, m := range c.List() {
			set[m.Name] = true
		}
		return set
	}
	built := names(catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true}))
	refreshed := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir})
	if err := refreshed.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	got := names(refreshed)

	if len(built) != len(got) {
		t.Fatalf("the build lists %v, the refresh lists %v: the two readers disagree", built, got)
	}
	for name := range got {
		if !built[name] {
			t.Errorf("the build does not list %q, the refresh does: the two readers disagree", name)
		}
	}
	if built["gone/ghost"] || got["gone/ghost"] {
		t.Error("the removed endpoint's cached model reached a catalog")
	}
	if !built["live/qwen3"] {
		t.Error("the stale live/qwen3 entry did not reach either catalog")
	}
}

// TestACorruptCacheIsABuildWithoutTheLayer covers the degradation path: a cache that
// cannot be read is a catalog without the cached layer, not a failed build — the models
// the layers declared still resolve, and the build says why.
func TestACorruptCacheIsABuildWithoutTheLayer(t *testing.T) {
	stateDir := t.TempDir()
	cachePath := filepath.Join(stateDir, "poo", "discovery.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("making the state directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the discovery cache: %v", err)
	}
	var log bytes.Buffer
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"local": {Endpoint: "http://127.0.0.1:1/v1"}},
		Models:    map[string]catalog.ModelSettings{"local/m3": {Endpoint: "local", ModelID: "m3", ContextLimit: 1024}},
		Roles:     map[string]string{"chat": "local/m3"},
	}
	built := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true, Logger: slog.New(slog.NewTextHandler(&log, nil))})
	if _, err := built.Resolve("local/m3"); err != nil {
		t.Errorf("a model the settings declared did not resolve without the cache: %v", err)
	}
	if !strings.Contains(log.String(), "the discovery cache could not be read") {
		t.Errorf("the log is %q, want it to name the unreadable cache", log.String())
	}
}

// TestAnUnprobedEndpointKeepsItsCacheToItself covers the SkipProbe exclusion: an endpoint
// the operator marked unprobed is not read from the cached layer either — its models are
// declared, and a cache entry for it is not a session model.
func TestAnUnprobedEndpointKeepsItsCacheToItself(t *testing.T) {
	stateDir := t.TempDir()
	body := `{"version":1,"fetched_at":"2026-11-17T00:00:00Z","endpoints":{"frozen":` +
		`{"fetched_at":"2026-11-17T00:00:00Z","shape":"vllm","models":{"qwen3":{"model_id":"qwen3","context_limit":32768}}}}}`
	cachePath := filepath.Join(stateDir, "poo", "discovery.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("making the state directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the discovery cache: %v", err)
	}
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"frozen": {Endpoint: "http://127.0.0.1:1/v1", SkipProbe: true}},
	}
	built := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true})
	if _, err := built.Resolve("frozen/qwen3"); err == nil {
		t.Error("a skipped endpoint's cached model resolved")
	}
	for _, info := range built.List() {
		if strings.HasPrefix(info.Name, "frozen/") {
			t.Errorf("the catalog lists %q, whose endpoint is not read from the cache", info.Name)
		}
	}
}

// TestAProbeThatFollowsTheCachedLayerWarnsOnce covers the layer's ownership: a path that
// probes after building gets the discovery layer from Refresh, so an unreadable cache is
// reported once, not once per reader.
func TestAProbeThatFollowsTheCachedLayerWarnsOnce(t *testing.T) {
	stateDir := t.TempDir()
	cachePath := filepath.Join(stateDir, "poo", "discovery.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("making the state directory: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the discovery cache: %v", err)
	}
	var log bytes.Buffer
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{"local": {Endpoint: "http://127.0.0.1:1/v1"}},
		Models:    map[string]catalog.ModelSettings{"local/m3": {Endpoint: "local", ModelID: "m3", ContextLimit: 1024}},
		Roles:     map[string]string{"chat": "local/m3"},
	}
	built, err := catalog.Build(context.Background(), catalog.Options{
		Settings: settings, StateDir: stateDir, Cached: true, Logger: slog.New(slog.NewTextHandler(&log, nil)),
	})
	if err != nil {
		t.Fatalf("the build: %v", err)
	}
	if _, err := built.Resolve("local/m3"); err != nil {
		t.Errorf("the declared model did not resolve: %v", err)
	}
	if got := strings.Count(log.String(), "the discovery cache could not be read"); got != 1 {
		t.Errorf("the unreadable cache is reported %d times, want once:\n%s", got, log.String())
	}
}

// keyedListServer serves an OpenAI-compatible list to a bearer key only.
//
// It is the shape a hosted provider answers with: a key that is not there
// is a refusal, and the probe reads the models only when the request
// carries the key the endpoint's credential resolves to.
func keyedListServer(t *testing.T, key string, ids ...string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		data := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]any{"id": id, "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// TestAProbeCarriesTheEndpointKey covers an endpoint whose list is served
// to a key and to nothing else. The probe asks with the key the endpoint's
// credential resolves to, so the models the list names reach the catalog;
// a probe that asked anonymously would be refused and the endpoint would
// offer nothing.
func TestAProbeCarriesTheEndpointKey(t *testing.T) {
	server := keyedListServer(t, "the-key", "glm-5.3", "kimi-k3")
	asked := map[string]int{}
	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"cloud": {Endpoint: server.URL, APIKeyEnv: "CLOUD_API_KEY"},
			},
		},
		EndpointKey: func(name string, credential catalog.Credential) string {
			asked[name]++
			if credential.Source != catalog.CredentialEnv || credential.Env != "CLOUD_API_KEY" {
				t.Errorf("the probe asks for %q's key with %+v, want the endpoint's variable",
					name, credential)
			}
			return "the-key"
		},
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for _, name := range []string{"cloud/glm-5.3", "cloud/kimi-k3"} {
		if _, err := c.Resolve(name); err != nil {
			t.Errorf("the model %q the keyed list named did not resolve: %v", name, err)
		}
	}
	if asked["cloud"] == 0 {
		t.Errorf("the probe never asked for the endpoint's key")
	}
}

// TestAProbeWhoseKeyIsNotThereAsksAnonymously covers the other half: an
// endpoint with no key in reach is probed with no credential header, so an
// operator's local server sees the plain request it has always seen.
func TestAProbeWhoseKeyIsNotThereAsksAnonymously(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "gemma4"}},
		})
	}))
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"local": {Endpoint: server.URL, APIKeyEnv: "LOCAL_API_KEY"},
			},
		},
		EndpointKey: func(string, catalog.Credential) string { return "" },
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for _, header := range seen {
		if header != "" {
			t.Errorf("a request carried %q, want no credential header", header)
		}
	}
	if _, err := c.Resolve("local/gemma4"); err != nil {
		t.Errorf("the list's model did not resolve: %v", err)
	}
}

// TestTheShowProbeReadsEveryModelTheListServes covers a hosted endpoint
// that serves more models than one probe used to ask about. Every id the
// list named is asked about, so a model past the old request budget
// arrives with the context window and the capabilities its own report
// names rather than as a bare id.
func TestTheShowProbeReadsEveryModelTheListServes(t *testing.T) {
	const count = 25
	ids := make([]string, 0, count)
	for i := range count {
		ids = append(ids, fmt.Sprintf("model-%02d", i))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]any{"id": id, "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		var asked struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&asked)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_info":   map[string]any{"arch.context_length": 65536},
			"capabilities": []string{"completion", "tools"},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"cloud": {Endpoint: server.URL},
			},
		},
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for _, id := range ids {
		info, err := c.Resolve("cloud/" + id)
		if err != nil {
			t.Fatalf("the model %q the list named did not resolve: %v", id, err)
		}
		if info.ContextLimit != 65536 {
			t.Errorf("%s has the context limit %d, want the 65536 its own report names",
				id, info.ContextLimit)
		}
		if info.Tools.Mode != poo.ToolsNative {
			t.Errorf("%s has the tools mode %q, want native", id, info.Tools.Mode)
		}
	}
}

// TestAProbeOfOneEndpointKeepsWhatTheOtherReported covers the cache a single
// endpoint's reprobe writes. The cache is one file for every endpoint, so the file a
// sign-in's probe saves must still describe the endpoints it did not ask about — the
// session that follows reads them from it.
func TestAProbeOfOneEndpointKeepsWhatTheOtherReported(t *testing.T) {
	first := vllmServer(t, "gemma4", 32768)
	second := vllmServer(t, "glm-5.3", 8192)
	stateDir := t.TempDir()
	settings := catalog.Settings{
		Endpoints: map[string]catalog.EndpointSettings{
			"one": {Endpoint: first.URL},
			"two": {Endpoint: second.URL},
		},
	}
	c := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if err := c.ReprobeEndpoint(context.Background(), "two"); err != nil {
		t.Fatalf("the endpoint's reprobe: %v", err)
	}

	// A catalog built from the file the reprobe wrote holds both endpoints' models.
	built := catalog.New(catalog.Options{Settings: settings, StateDir: stateDir, Cached: true})
	for _, name := range []string{"one/gemma4", "two/glm-5.3"} {
		if _, err := built.Resolve(name); err != nil {
			t.Errorf("the model %q is not in the catalog the reprobe wrote: %v", name, err)
		}
	}
}

// TestAShowThatFailsForOneModelKeepsTheRest covers the fan-out's cost: one model's
// show failing, or its answer not reading, takes that model's own fields and not the
// models the list named beside it.
func TestAShowThatFailsForOneModelKeepsTheRest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "good-1"}, {"id": "broken"}, {"id": "good-2"},
			},
		})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		var asked struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&asked)
		if asked.Model == "broken" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_info":   map[string]any{"arch.context_length": 65536},
			"capabilities": []string{"completion", "tools"},
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{"cloud": {Endpoint: server.URL}},
		},
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for _, name := range []string{"cloud/good-1", "cloud/good-2"} {
		info, err := c.Resolve(name)
		if err != nil {
			t.Fatalf("the model %q the list named did not resolve: %v", name, err)
		}
		if info.Tools.Mode != poo.ToolsNative {
			t.Errorf("%s has the tools mode %q, want native", name, info.Tools.Mode)
		}
	}
	// The model whose show failed is still a model the list named; it stands with the
	// fields the list itself gave.
	if _, err := c.Resolve("cloud/broken"); err != nil {
		t.Errorf("the model whose show failed did not resolve: %v", err)
	}
}

// TestAProbeRefusedWithTheKeyAsksAgainWithoutIt covers the endpoint that never took a
// credential: a stored key one endpoint reads must not turn another endpoint's public
// answer into a refusal.
func TestAProbeRefusedWithTheKeyAsksAgainWithoutIt(t *testing.T) {
	var keyed, anonymous atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			keyed.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		anonymous.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "public-model"}},
		})
	}))
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"public": {Endpoint: server.URL, APIKeyEnv: "PUBLIC_API_KEY"},
			},
		},
		EndpointKey: func(string, catalog.Credential) string { return "a-stored-key" },
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if keyed.Load() == 0 || anonymous.Load() == 0 {
		t.Errorf("the probe made %d keyed and %d anonymous requests, want both",
			keyed.Load(), anonymous.Load())
	}
	if _, err := c.Resolve("public/public-model"); err != nil {
		t.Errorf("the list's model did not resolve: %v", err)
	}
}

// TestARefreshDoesNotWakeAnEndpointThatIsNotProbed covers the cache read twice: an
// endpoint whose models are declared rather than listed is not one a build reads from
// the cache, and a refresh of another endpoint must not write its entries back into the
// catalog or the file — a model the session build refuses is not one a refresh offers.
func TestARefreshDoesNotWakeAnEndpointThatIsNotProbed(t *testing.T) {
	fixture := vllmServer(t, "listed", 32768)
	other := vllmServer(t, "other", 8192)
	stateDir := t.TempDir()

	// The endpoint is probeable once, so its report is in the cache.
	probeable := catalog.New(catalog.Options{
		StateDir: stateDir,
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{"declared": {Endpoint: fixture.URL}},
		},
	})
	if err := probeable.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if _, err := probeable.Resolve("declared/listed"); err != nil {
		t.Fatalf("the probed model did not resolve: %v", err)
	}

	// The same endpoint now declares its models, and another endpoint is probed.
	declaring := catalog.New(catalog.Options{
		StateDir: stateDir,
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"declared": {Endpoint: fixture.URL, SkipProbe: true},
				"other":    {Endpoint: other.URL},
			},
			Models: map[string]catalog.ModelSettings{
				"declared/typed": {Endpoint: "declared", ModelID: "typed", ContextLimit: 4096},
			},
		},
	})
	if err := declaring.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if _, err := declaring.Resolve("declared/listed"); err == nil {
		t.Error("a refresh offered a model from an endpoint it does not probe")
	}
	if _, err := declaring.Resolve("declared/typed"); err != nil {
		t.Errorf("the declared model did not resolve: %v", err)
	}
	if _, err := declaring.Resolve("other/other"); err != nil {
		t.Errorf("the probed endpoint's model did not resolve: %v", err)
	}
}

// TestOneEndpointsKeyDoesNotReachAnother covers the fan-out's requests: the key a probe
// carries is the endpoint's own. Two endpoints with keys in reach are two requests that
// must each carry their own, or one provider's credential is spent at another's door.
func TestOneEndpointsKeyDoesNotReachAnother(t *testing.T) {
	seen := map[string]map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Path
		if seen[host] == nil {
			seen[host] = map[string]bool{}
		}
		seen[host][r.Header.Get("Authorization")] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "m"}},
		})
	}))
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"one": {Endpoint: server.URL + "/one/v1", APIKeyEnv: "ONE_KEY"},
				"two": {Endpoint: server.URL + "/two/v1", APIKeyEnv: "TWO_KEY"},
			},
		},
		EndpointKey: func(name string, credential catalog.Credential) string {
			if credential.Env == "ONE_KEY" && name == "one" {
				return "key-one"
			}
			if credential.Env == "TWO_KEY" && name == "two" {
				return "key-two"
			}
			t.Errorf("the probe asks %q for its key with %+v", name, credential)
			return ""
		},
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	for path, want := range map[string]string{
		"/one/v1/models": "Bearer key-one",
		"/two/v1/models": "Bearer key-two",
	} {
		got := seen[path]
		if len(got) != 1 || !got[want] {
			t.Errorf("the requests to %s carried %v, want only %q", path, got, want)
		}
	}
}

// TestAShowTheServerDoesNotServeStopsTheProbeAtOnce covers the endpoint that lists its
// models and answers nothing about them: a 404 on the first model show is the whole
// answer, and the probe must not spend the rest of its budget queueing ids nothing will
// read. Every hosted gateway that is not Ollama's own shape answers this way.
func TestAShowTheServerDoesNotServeStopsTheProbeAtOnce(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data := make([]map[string]any, 0, 12)
		for i := range 12 {
			data = append(data, map[string]any{"id": fmt.Sprintf("model-%02d", i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	c := catalog.New(catalog.Options{
		StateDir: t.TempDir(),
		// The budget is the stall's ceiling: a probe that waits it out is the bug.
		ProbeTimeout: 2 * time.Second,
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{"gateway": {Endpoint: server.URL}},
		},
	})
	start := time.Now()
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the probe took %s, want the shape's absence to end it at once", elapsed)
	}
	// The list's own answer stands: the models are there, described by the list alone.
	for _, id := range []string{"model-00", "model-11"} {
		if _, err := c.Resolve("gateway/" + id); err != nil {
			t.Errorf("the model %q the list named did not resolve: %v", id, err)
		}
	}
}
