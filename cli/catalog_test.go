package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/cli"
)

// loadCatalog writes a one-entry registry whose description ends in the
// given newlines and loads it.
func loadCatalog(t *testing.T, description string) *cli.Catalog {
	t.Helper()
	tools := map[string]any{
		"last": map[string]any{
			"description": description,
			"command":     []string{"true"},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
	body, err := json.Marshal(map[string]any{"version": 1, "tools": tools})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	catalog, err := cli.Load(dir)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return catalog
}

// TestTheCatalogTrimsTrailingNewlines covers the dynamic end of the
// section: a description that ends in a newline does not leave one
// behind, and neither does one that ends in a blank line.
func TestTheCatalogTrimsTrailingNewlines(t *testing.T) {
	want := loadCatalog(t, "the last command").Section(false)
	if strings.HasSuffix(want, "\n") {
		t.Fatalf("the catalog ends in a newline: %q", want)
	}
	for _, description := range []string{"the last command\n", "the last command\n\n"} {
		if got := loadCatalog(t, description).Section(false); got != want {
			t.Errorf("the catalog for %q is %q, want %q", description, got, want)
		}
	}
}

// TestTheSearchCatalogTrimsTrailingNewlines covers the search form too:
// its last line is prose, so the file's sentinel newline is the only
// one it has.
func TestTheSearchCatalogTrimsTrailingNewlines(t *testing.T) {
	got := loadCatalog(t, "the last command").Section(true)
	if strings.HasSuffix(got, "\n") {
		t.Errorf("the search catalog ends in a newline: %q", got)
	}
	if !strings.Contains(got, "find_tool") {
		t.Errorf("the search catalog does not name the search: %q", got)
	}
}

// loadGatedCatalog writes a two-entry registry whose second entry the rules
// gate on a boolean condition, and loads it.
func loadGatedCatalog(t *testing.T) *cli.Catalog {
	t.Helper()
	entry := func(description string) map[string]any {
		return map[string]any{
			"description": description,
			"command":     []string{"true"},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		}
	}
	body, err := json.Marshal(map[string]any{
		"version": 1,
		"tools": map[string]any{
			"read":  entry("Read something"),
			"write": entry("Write something"),
		},
		"availability": map[string]any{
			"conditions": map[string]any{"writable": map[string]any{"type": "boolean"}},
			"rules": []any{map[string]any{
				"type": "enabledWhen", "field": "write",
				"when": map[string]any{"op": "cond", "condition": "writable"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	catalog, err := cli.Load(dir)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return catalog
}

func TestFilterKeepsTheEntriesTheRulesLeave(t *testing.T) {
	catalog := loadGatedCatalog(t)
	if inputs := catalog.AvailabilityInputs(); inputs["writable"] != "boolean" {
		t.Fatalf("the catalog declares %v, want a boolean writable", inputs)
	}

	kept, err := catalog.Filter(map[string]any{"writable": false})
	if err != nil {
		t.Fatalf("filtering the catalog: %v", err)
	}
	if got := strings.Join(kept.Names(), ","); got != "read" {
		t.Errorf("the filtered catalog holds %q, want read", got)
	}
	// The filter is a view: the catalog it came from still offers both.
	if got := strings.Join(catalog.Names(), ","); got != "read,write" {
		t.Errorf("filtering changed the catalog: %q", got)
	}
	// A kept entry is still callable through the filter, so the plan shares
	// the registry rather than a copy of it.
	if _, err := kept.Plan("write", map[string]any{}); err == nil {
		t.Error("the filtered catalog planned an entry it dropped")
	}
	if _, err := kept.Plan("read", map[string]any{}); err != nil {
		t.Errorf("planning a kept entry: %v", err)
	}

	// A condition the consumer does not supply is an error, never a default.
	if _, err := catalog.Filter(nil); err == nil {
		t.Error("filtering without the declared condition was allowed")
	}
}
