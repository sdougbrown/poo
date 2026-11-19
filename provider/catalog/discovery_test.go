package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
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
