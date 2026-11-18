package cliregistry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/cli"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// The schema-issue tests cover the error text a malformed call gets back.
//
// A refused call is the model's to correct, so the text has to name the
// argument it got wrong and what would have been accepted. The old text
// was an RFC 6901 pointer and an empty message ("call_cli: deploy:
// /mode: "), which told the model nothing.

// deployTool is a manifest body with a schema whose constraints the
// tests trip one at a time. The command never runs: every call here is
// refused before a process exists.
func deployTool(t *testing.T) map[string]any {
	return map[string]any{
		"description": "Deploy a service",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"env":      map[string]any{"type": "string", "enum": []string{"staging", "production"}},
				"replicas": map[string]any{"type": "integer", "minimum": 1, "maximum": 10},
			},
			"required":             []string{"env"},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "env", "style": "equals", "flag": "--env"},
			map[string]any{"property": "replicas", "style": "equals", "flag": "--replicas"},
		},
	}
}

// prepareDeploy loads a one-CLI catalog with deployTool and prepares a
// call against it.
func prepareDeploy(t *testing.T, args string) error {
	t.Helper()
	path := writeManifest(t, t.TempDir(), "deploy.json", map[string]any{"deploy": deployTool(t)})
	tool := cliregistry.NewCall(load(t, path))
	_, err := tool.Prepare(json.RawMessage(args), poo.PrepareEnv{})
	if err == nil {
		t.Fatalf("the call %s was accepted, want a refusal", args)
	}
	return err
}

// TestAnUnknownArgumentNamesItAndTheAcceptedArguments covers the
// connectivity-test failure: a property the schema does not declare is
// refused with the names the schema does.
func TestAnUnknownArgumentNamesItAndTheAcceptedArguments(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"env":"staging","mode":"canary"}}`)
	for _, want := range []string{`unknown argument "mode"`, "deploy accepts: env*, replicas"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// TestAMissingRequiredArgumentIsNamed covers the required refusal.
func TestAMissingRequiredArgumentIsNamed(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"replicas":2}}`)
	if !strings.Contains(err.Error(), `"env" is required`) {
		t.Errorf("the refusal does not name the required argument: %v", err)
	}
}

// TestABadEnumNamesTheValues covers the enum refusal the connectivity
// test's second probe hit: an action the CLI does not offer.
func TestABadEnumNamesTheValues(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"env":"canary"}}`)
	if !strings.Contains(err.Error(), `must be one of: "staging", "production"`) {
		t.Errorf("the refusal does not name the enum's values: %v", err)
	}
}

// TestABoundArgumentNamesTheBound covers the numeric refusals.
func TestABoundArgumentNamesTheBound(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"name":"deploy","args":{"env":"staging","replicas":0}}`, "is below the minimum of 1"},
		{`{"name":"deploy","args":{"env":"staging","replicas":11}}`, "is above the maximum of 10"},
	} {
		err := prepareDeploy(t, tc.args)
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("the refusal for %s does not carry %q: %v", tc.args, tc.want, err)
		}
	}
}

// TestTheRefusalNeverEndsInAnEmptyMessage pins the old failure shape:
// no refusal may render as a bare pointer with an empty message.
func TestTheRefusalNeverEndsInAnEmptyMessage(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"mode":"canary"}}`)
	if strings.Contains(err.Error(), ": ") && strings.HasSuffix(strings.TrimSpace(err.Error()), ": ") {
		t.Errorf("the refusal ends in an empty message: %v", err)
	}
}

// TestANestedUnknownArgumentNamesTheParentAccepted covers an unknown
// argument inside a nested object.
func TestANestedUnknownArgumentNamesTheParentAccepted(t *testing.T) {
	tool := map[string]any{
		"description": "Ship a build",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"build": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"target": map[string]any{"type": "string"},
					},
					"required":             []string{"target"},
					"additionalProperties": false,
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "build", "style": "equals", "flag": "--build"},
		},
	}
	path := writeManifest(t, t.TempDir(), "ship.json", map[string]any{"ship": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"ship","args":{"build":{"targit":"x"}}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the nested unknown argument was accepted")
	}
	for _, want := range []string{`unknown argument "targit"`, "ship accepts: target*"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// TestTheCatalogCarriesEachToolSArguments pins the up-front half: the
// prompt's catalog line names the tool's arguments, required ones
// marked, so the first call can be right.
func TestTheCatalogCarriesEachToolSArguments(t *testing.T) {
	path := writeManifest(t, t.TempDir(), "deploy.json", map[string]any{"deploy": deployTool(t)})
	catalog := load(t, path)
	text := catalog.Section(false)
	if !strings.Contains(text, "deploy (args: env*, replicas): Deploy a service") {
		t.Errorf("the catalog line does not carry the arguments with required marked: %q", text)
	}
}

// TestTheCatalogOmitsEmptyArguments covers a CLI whose schema declares
// no properties: its line stays bare.
func TestTheCatalogOmitsEmptyArguments(t *testing.T) {
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{"version": 1, "tools": map[string]any{
		"bare": map[string]any{
			"description": "Run bare",
			"command":     []string{"true"},
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{}, "additionalProperties": false,
			},
		},
	}})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	catalog, err := cli.Load(dir)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	text := catalog.Section(false)
	if strings.Contains(text, "args:") {
		t.Errorf("a bare CLI's line carries an argument list: %q", text)
	}
}
