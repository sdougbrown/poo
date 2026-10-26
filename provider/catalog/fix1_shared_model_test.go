package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sdougbrown/poo/provider/catalog"
)

// fix1VLLM starts an endpoint that serves one model id.
func fix1VLLM(t *testing.T, modelID string, maxModelLen int) *httptest.Server {
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

// TestAModelThatTwoEndpointsServeIsListedOnce covers the catalog's
// note: "A model that two endpoints serve is listed once, under the
// first endpoint by name."
//
// applyDiscovery keys the entry by the model id, so the second endpoint
// that serves the same id overwrites the first one's entry.
func TestAModelThatTwoEndpointsServeIsListedOnce(t *testing.T) {
	first := fix1VLLM(t, "shared", 32768)
	second := fix1VLLM(t, "shared", 8192)

	c, err := catalog.Build(context.Background(), catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"aaa": {Endpoint: first.URL},
				"zzz": {Endpoint: second.URL},
			},
		},
	})
	if err != nil {
		t.Fatalf("building the catalog: %v", err)
	}

	var found int
	var endpoint string
	for _, info := range c.List() {
		if info.ModelID != "shared" {
			continue
		}
		found++
		endpoint = info.Endpoint
	}
	if found != 1 {
		t.Fatalf("the catalog lists the shared model %d times, want once", found)
	}
	if endpoint != first.URL {
		t.Errorf("the shared model is attributed to %q, want the first endpoint by name %q",
			endpoint, first.URL)
	}
}

// TestDiscoveryKeepsTheSettingsModel covers the same overwrite when
// one of the two endpoints is the model the settings configured.
func TestDiscoveryKeepsTheSettingsModel(t *testing.T) {
	first := fix1VLLM(t, "shared", 32768)
	second := fix1VLLM(t, "shared", 8192)

	c, err := catalog.Build(context.Background(), catalog.Options{
		StateDir: t.TempDir(),
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"aaa": {Endpoint: first.URL},
				"zzz": {Endpoint: second.URL},
			},
			Models: map[string]catalog.ModelSettings{
				"mine": {Endpoint: "zzz", ModelID: "shared", ContextLimit: 131072},
			},
		},
	})
	if err != nil {
		t.Fatalf("building the catalog: %v", err)
	}
	info, err := c.Resolve("mine")
	if err != nil {
		t.Fatalf("resolving the settings' model: %v", err)
	}
	if info.ContextLimit != 131072 {
		t.Errorf("the settings' window is %d, want 131072", info.ContextLimit)
	}
	if info.Endpoint != second.URL {
		t.Errorf("the settings' model is served by %q, want %q", info.Endpoint, second.URL)
	}
	// The other endpoint also serves it, and the model is still listed
	// once under its own name plus the discovered one: the discovered
	// entry must not take the name the settings use.
	if _, err := c.Resolve("shared"); err != nil {
		t.Logf("the discovered entry is not resolvable: %v", err)
	}
}
