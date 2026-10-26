package cliregistry_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo/tool/cliregistry"
)

// The fixture registry is two CLIs whose commands are the scripts in
// testdata/bin: a real command-line interface with a flag, an array
// argument, a failing exit code and a record that it ran.
//
// The command is the script's absolute path, so the manifest does not
// depend on the directory the test runs in.

// script returns the absolute path of a fixture CLI.
func script(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "bin", name))
	if err != nil {
		t.Fatalf("resolving the fixture %q: %v", name, err)
	}
	return path
}

// markerEnv is the variable the say fixture appends to when it runs.
const markerEnv = "FIXTURE_MARKER"

// sayTool is the manifest body of the say CLI.
func sayTool(t *testing.T) map[string]any {
	return map[string]any{
		"description": "Say hello to a name",
		"command":     []string{script(t, "say")},
		"env":         map[string]string{markerEnv: "env:" + markerEnv},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "name", "style": "space", "flag": "--name"},
		},
	}
}

// listTool is the manifest body of the list CLI.
func listTool(t *testing.T) map[string]any {
	return map[string]any{
		"description": "Print the words it is given, one per line",
		"command":     []string{script(t, "list")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"words": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "words", "style": "positional"},
		},
	}
}

// writeManifest writes a registry manifest into dir and returns its path.
func writeManifest(t *testing.T, dir, name string, tools map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": 1, "tools": tools})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// load loads the registries and fails the test when they do not load.
func load(t *testing.T, paths ...string) *cliregistry.Catalog {
	t.Helper()
	catalog, err := cliregistry.Load(paths...)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	return catalog
}

// fixtureCatalog loads a catalog with the two fixture CLIs.
//
// The say CLI records that it ran in the file markerPath names.
func fixtureCatalog(t *testing.T) *cliregistry.Catalog {
	t.Helper()
	t.Setenv(markerEnv, filepath.Join(t.TempDir(), "ran"))
	path := writeManifest(t, t.TempDir(), "fixture.json", map[string]any{
		"say":  sayTool(t),
		"list": listTool(t),
	})
	return load(t, path)
}

// wideCatalog loads a catalog with two hundred CLIs, the size a real
// registry of a company's commands reaches.
func wideCatalog(t *testing.T) *cliregistry.Catalog {
	t.Helper()
	tools := map[string]any{}
	for i := 0; i < 200; i++ {
		tools[wideName(i)] = map[string]any{
			"description": fmt.Sprintf("The fixture CLI number %d", i),
			"command":     []string{script(t, "list")},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		}
	}
	path := writeManifest(t, t.TempDir(), "wide.json", tools)
	return load(t, path)
}

// wideName is the name of the entry at an index.
func wideName(i int) string { return fmt.Sprintf("tool-%03d", i) }
