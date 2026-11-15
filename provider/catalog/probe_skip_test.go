package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo/provider/catalog"
)

// TestAnUnprobedEndpointIsNotAsked covers the declared endpoint: an endpoint
// whose models are declared is not probed, so the endpoint sees no request at
// all, and its models keep the settings' own fields.
func TestAnUnprobedEndpointIsNotAsked(t *testing.T) {
	var probes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := catalog.New(catalog.Options{
		Settings: catalog.Settings{
			Endpoints: map[string]catalog.EndpointSettings{
				"declared": {Endpoint: server.URL, SkipProbe: true},
			},
			Models: map[string]catalog.ModelSettings{
				"declared/model": {Endpoint: "declared", ModelID: "model", ContextLimit: 272000},
			},
		},
		StateDir: t.TempDir(),
	})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("the refresh: %v", err)
	}
	if probes.Load() != 0 {
		t.Errorf("the endpoint was probed %d times, want none", probes.Load())
	}
	info, err := c.Resolve("declared/model")
	if err != nil {
		t.Fatalf("the model: %v", err)
	}
	if info.ContextLimit != 272000 || info.Source["context_limit"] != catalog.LayerSettings {
		t.Errorf("the context limit is %d from %q, want the settings' 272000",
			info.ContextLimit, info.Source["context_limit"])
	}
}
