package cliregistry_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// directTools returns the catalog's entries as tools of their own, by name.
func directTools(t *testing.T, tool *cliregistry.CallTool) map[string]poo.Tool {
	t.Helper()
	byName := map[string]poo.Tool{}
	for _, entry := range tool.Tools() {
		byName[entry.Spec().Name] = entry
	}
	return byName
}

// TestTheCallToolOffersOneToolPerEntry covers the seam: the catalog's entries
// become tools of their own, in name order, each with the entry's own name and
// description.
func TestTheCallToolOffersOneToolPerEntry(t *testing.T) {
	tool := cliregistry.NewCall(fixtureCatalog(t))
	tools := tool.Tools()
	if len(tools) != 2 {
		t.Fatalf("the call tool offers %d direct tools, want the catalog's two entries", len(tools))
	}
	names := make([]string, 0, len(tools))
	for _, entry := range tools {
		names = append(names, entry.Spec().Name)
	}
	if got, want := strings.Join(names, ","), "list,say"; got != want {
		t.Errorf("the direct tools are %q, want %q", got, want)
	}
	if spec := directTools(t, tool)["say"].Spec(); spec.Description != "Say hello to a name" {
		t.Errorf("the say tool describes itself as %q", spec.Description)
	}
	// An empty catalog offers nothing: there is no entry to name.
	empty, err := cliregistry.Load()
	if err != nil {
		t.Fatalf("loading an empty catalog: %v", err)
	}
	if got := cliregistry.NewCall(empty).Tools(); len(got) != 0 {
		t.Errorf("an empty catalog offers %d direct tools", len(got))
	}
}

// TestADirectToolCarriesTheManifestsSchema covers the spec: the model reads the
// entry's own arguments as the tool's schema, and the tool is side-effecting
// and background-capable the way the call is.
func TestADirectToolCarriesTheManifestsSchema(t *testing.T) {
	catalog := fixtureCatalog(t)
	entry, ok := catalog.Lookup("say")
	if !ok {
		t.Fatal("the fixture catalog holds no say entry")
	}
	spec := directTools(t, cliregistry.NewCall(catalog))["say"].Spec()
	if spec.Class != poo.ClassSideEffecting {
		t.Errorf("the say tool is %q, want side-effecting", spec.Class)
	}
	if spec.Background != poo.BackgroundAllowed {
		t.Errorf("the say tool's background mode is %q, want %q", spec.Background, poo.BackgroundAllowed)
	}
	want, err := json.Marshal(entry.Schema)
	if err != nil {
		t.Fatalf("encoding the entry's schema: %v", err)
	}
	var gotSchema, wantSchema map[string]any
	if err := json.Unmarshal(spec.Schema, &gotSchema); err != nil {
		t.Fatalf("decoding the tool's schema %q: %v", spec.Schema, err)
	}
	if err := json.Unmarshal(want, &wantSchema); err != nil {
		t.Fatalf("decoding the entry's schema %q: %v", want, err)
	}
	if got, want := string(mustMarshal(t, gotSchema)), string(mustMarshal(t, wantSchema)); got != want {
		t.Errorf("the tool's schema is %s, want the manifest's %s", got, want)
	}
}

// mustMarshal encodes a value for a comparison, sorted the way both sides are.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding the value: %v", err)
	}
	return raw
}

// TestADirectToolKeepsItsOwnBackgroundArgument covers the reserved name: a
// manifest whose schema already defines "background" keeps it, and the entry
// runs in the turn instead of the session failing to register the tool.
func TestADirectToolKeepsItsOwnBackgroundArgument(t *testing.T) {
	path := writeManifest(t, t.TempDir(), "reserved.json", map[string]any{
		"reserved": map[string]any{
			"description": "A tool with its own background argument",
			"command":     []string{script(t, "list")},
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"background": map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
			"bindings": []any{
				map[string]any{"property": "background", "style": "equals", "flag": "--background"},
			},
		},
	})
	spec := directTools(t, cliregistry.NewCall(load(t, path)))["reserved"].Spec()
	if spec.Background != poo.BackgroundNever {
		t.Errorf("the tool's background mode is %q, want %q: poo would add a second background argument",
			spec.Background, poo.BackgroundNever)
	}
}

// TestADirectToolTakesTheArgumentsThemselves covers the argument path: the
// model passes the command's own arguments, with no command property wrapping
// them, and the call runs the entry.
func TestADirectToolTakesTheArgumentsThemselves(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := directTools(t, cliregistry.NewCall(catalog))["say"]

	prepared, err := tool.Prepare(json.RawMessage(`{"name":"ada"}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing a direct call: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: bash.ObservationCapMax},
	})
	if err != nil {
		t.Fatalf("running the direct call: %v", err)
	}
	if res.IsError {
		t.Errorf("the direct call is an error: %q", res.Text)
	}
	output, _ := observation(t, res)["output"].(string)
	if !strings.Contains(output, "hello ada") {
		t.Errorf("the output is %q, want a greeting", output)
	}
	if got := markerLines(t, os.Getenv(markerEnv)); got != 1 {
		t.Errorf("the entry recorded %d runs, want one", got)
	}
}

// TestADirectToolRefusesTheWrappedArguments covers the misreading the call
// invites: a model that sends the call's own shape is refused by the entry's
// schema, with the schema's own words, before anything runs.
func TestADirectToolRefusesTheWrappedArguments(t *testing.T) {
	tool := directTools(t, cliregistry.NewCall(fixtureCatalog(t)))["say"]
	_, err := tool.Prepare(json.RawMessage(`{"command":"say","args":{"name":"ada"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("a wrapped call prepared a direct tool")
	}
	for _, want := range []string{"command", "say accepts: name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not carry %q", err, want)
		}
	}
	if got := markerLines(t, os.Getenv(markerEnv)); got != 0 {
		t.Errorf("the refused call recorded %d runs, want none", got)
	}
}

// TestADirectToolRefusesANullArgumentObject covers the edge the call refuses
// too: an explicit null is not an omitted object, because the schema's type is
// an object.
func TestADirectToolRefusesANullArgumentObject(t *testing.T) {
	tool := directTools(t, cliregistry.NewCall(fixtureCatalog(t)))["say"]
	_, err := tool.Prepare(json.RawMessage(`null`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("a null argument object prepared a direct tool")
	}
	if !strings.Contains(err.Error(), "not null") {
		t.Errorf("the refusal %q does not say what a null is", err)
	}
}

// TestADirectToolTakesNoArgumentsWhenNoneAreGiven covers the omitted object: a
// command that takes no arguments is called with nothing said.
func TestADirectToolTakesNoArgumentsWhenNoneAreGiven(t *testing.T) {
	tool := directTools(t, cliregistry.NewCall(fixtureCatalog(t)))["list"]
	for _, raw := range []string{``, `{}`} {
		prepared, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{})
		if err != nil {
			t.Fatalf("preparing %q: %v", raw, err)
		}
		if got := prepared.Summary(); got != "list {}" {
			t.Errorf("the call %q summarizes itself as %q", raw, got)
		}
	}
}

// TestADirectToolKeepsTheManifestsBindings covers the shared execution: a
// direct tool builds its argv from the entry's bindings the way the call over
// the catalog does, because both run the same plan through the same code.
func TestADirectToolKeepsTheManifestsBindings(t *testing.T) {
	path := writeManifest(t, t.TempDir(), "stdin.json", map[string]any{
		"echo": map[string]any{
			"description": "Echo what it reads",
			"command":     []string{script(t, "list")},
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"words": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"additionalProperties": false,
			},
			"bindings": []any{
				map[string]any{"property": "words", "style": "positional"},
			},
		},
	})
	tool := directTools(t, cliregistry.NewCall(load(t, path)))["echo"]
	prepared, err := tool.Prepare(json.RawMessage(`{"words":["one","two"]}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing the direct call: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("running the direct call: %v", err)
	}
	output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
	if output != "one\ntwo\n" {
		t.Errorf("the tool printed %q, want the two words", output)
	}
}

// TestADirectToolsCatalogIsTheCalls covers the shared catalog: the tools are
// built from the catalog the call answers to, so a narrowed catalog offers the
// entries it leaves and nothing else.
func TestADirectToolsCatalogIsTheCalls(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"version": 1,
		"tools": map[string]any{
			"read-it":  registryEntryBody("Read it", script(t, "list")),
			"write-it": registryEntryBody("Write it", script(t, "list")),
		},
		"availability": map[string]any{
			"conditions": map[string]any{"profile_side_effecting": map[string]any{"type": "boolean"}},
			"rules": []any{map[string]any{
				"type": "enabledWhen", "field": "write-it",
				"when":   map[string]any{"op": "cond", "condition": "profile_side_effecting"},
				"reason": "the profile is read-only",
			}},
		},
	})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "gated.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	catalog := load(t, path)
	narrowed, err := catalog.Filter(map[string]any{"profile_side_effecting": false})
	if err != nil {
		t.Fatalf("narrowing the catalog: %v", err)
	}
	tools := cliregistry.NewCall(narrowed).Tools()
	if len(tools) != 1 || tools[0].Spec().Name != "read-it" {
		t.Fatalf("the narrowed catalog offers %d tools, want read-it alone", len(tools))
	}
}

// registryEntryBody is a manifest body whose command is the one named.
func registryEntryBody(description, command string) map[string]any {
	return map[string]any{
		"description": description,
		"command":     []string{command},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	}
}
