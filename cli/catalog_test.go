package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	entry := func(description string, schema map[string]any) map[string]any {
		return map[string]any{
			"description": description,
			"command":     []string{"true"},
			"inputSchema": schema,
		}
	}
	empty := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	// The read entry takes an argument, so a plan through the filter can be
	// refused by the entry's own schema rather than by the catalog's list.
	needsPath := map[string]any{
		"description": "Read something",
		"command":     []string{"true"},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"path": map[string]any{"type": "string"}},
			"required":             []any{"path"},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "path", "style": "positional"}},
	}
	body, err := json.Marshal(map[string]any{
		"version": 1,
		"tools": map[string]any{
			"read":  needsPath,
			"write": entry("Write something", empty),
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
	if _, err := kept.Plan("read", map[string]any{"path": "x"}); err != nil {
		t.Errorf("planning a kept entry: %v", err)
	}
	// A kept entry keeps the schema it was loaded with, so a plan still
	// refuses the arguments its own manifest forbids.
	if _, err := kept.Plan("read", map[string]any{}); err == nil {
		t.Error("the filtered catalog planned a kept entry without its required argument")
	}
	keptEntry, ok := kept.Lookup("read")
	if !ok {
		t.Fatal("the filtered catalog lost a kept entry's lookup")
	}
	loadedEntry, ok := catalog.Lookup("read")
	if !ok {
		t.Fatal("the loaded catalog lost the read entry")
	}
	if !reflect.DeepEqual(keptEntry.Schema, loadedEntry.Schema) {
		t.Errorf("the filtered catalog changed the entry's schema: %v", keptEntry.Schema)
	}

	// A condition the consumer does not supply is an error, never a default.
	if _, err := catalog.Filter(nil); err == nil {
		t.Error("filtering without the declared condition was allowed")
	}
}

func TestANilCatalogRendersAnEmptySection(t *testing.T) {
	// A session that offers no catalog still knows the section, so a prompt
	// directory that carries one loads.
	var catalog *cli.Catalog
	if entries := catalog.Entries(); len(entries) != 0 {
		t.Errorf("a nil catalog holds %d entries", len(entries))
	}
	// Rendering is the point: a nil catalog must not panic, and its section
	// carries no entry line.
	if text := catalog.Section(false); strings.Contains(text, "name: ") {
		t.Errorf("a nil catalog's section lists an entry: %q", text)
	}
}
