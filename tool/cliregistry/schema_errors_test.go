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
// was an RFC 6901 pointer and an empty message ("call_tool: deploy:
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

// list names the arguments, it does not embed enum values — the values
// belong to the refusal's own clause when the enum is the miss.
func TestTheRefusalKeepsNamesOnly(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"env":"canary","mode":"canary"}}`)
	if !strings.Contains(err.Error(), "deploy accepts: env*, replicas") {
		t.Errorf("the refusal's accepted list changed shape: %v", err)
	}
}

// TestABadTypeNamesTheType covers the type refusal and the article it
// takes.
func TestABadTypeNamesTheType(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"env":"staging","replicas":"abc"}}`)
	if !strings.Contains(err.Error(), `"replicas" must be an integer`) {
		t.Errorf("the refusal does not name the type: %v", err)
	}
}

// TestABadConstNamesTheValue covers a plain const property, the path
// the oneOf branches never reach.
func TestABadConstNamesTheValue(t *testing.T) {
	tool := deployTool(t)
	schema := tool["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	props["channel"] = map[string]any{"const": "main"}
	tool["bindings"] = append(tool["bindings"].([]any),
		map[string]any{"property": "channel", "style": "equals", "flag": "--channel"})
	path := writeManifest(t, t.TempDir(), "deploy.json", map[string]any{"deploy": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"deploy","args":{"env":"staging","channel":"other"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the off-const value was accepted")
	}
	if !strings.Contains(err.Error(), `"channel" must be "main"`) {
		t.Errorf("the refusal does not name the const: %v", err)
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
	if !strings.Contains(err.Error(), `"env" must be one of: "staging", "production"`) {
		t.Errorf("the refusal does not name the enum's values: %v", err)
	}
}

// TestABoundArgumentNamesTheBound covers every bound code the
// renderer translates, one violated bound each.
func TestABoundArgumentNamesTheBound(t *testing.T) {
	tool := map[string]any{
		"description": "Tune a service",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"env":  map[string]any{"type": "string", "enum": []string{"staging", "production"}},
				"rate": map[string]any{"type": "number", "exclusiveMinimum": 0},
				"load": map[string]any{"type": "number", "exclusiveMaximum": 100},
				"name": map[string]any{"type": "string", "minLength": 2},
				"note": map[string]any{"type": "string", "maxLength": 5},
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
				"ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 3},
			},
			"required":             []string{"env"},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "env", "style": "equals", "flag": "--env"},
			map[string]any{"property": "rate", "style": "equals", "flag": "--rate"},
			map[string]any{"property": "load", "style": "equals", "flag": "--load"},
			map[string]any{"property": "name", "style": "equals", "flag": "--name"},
			map[string]any{"property": "note", "style": "equals", "flag": "--note"},
			map[string]any{"property": "tags", "style": "equals", "flag": "--tags"},
			map[string]any{"property": "ids", "style": "equals", "flag": "--ids"},
		},
	}
	path := writeManifest(t, t.TempDir(), "tune.json", map[string]any{"tune": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	for _, tc := range []struct{ args, want string }{
		{`{"name":"tune","args":{"env":"staging","rate":0}}`, `"rate" must be greater than 0`},
		{`{"name":"tune","args":{"env":"staging","load":100}}`, `"load" must be less than 100`},
		{`{"name":"tune","args":{"env":"staging","name":"a"}}`, `"name" is shorter than the minimum length of 2`},
		{`{"name":"tune","args":{"env":"staging","note":"toolong"}}`, `"note" is longer than the maximum length of 5`},
		{`{"name":"tune","args":{"env":"staging","tags":[]}}`, `"tags" has fewer than 1 items`},
		{`{"name":"tune","args":{"env":"staging","ids":["a","b","c","d"]}}`, `"ids" has more than 3 items`},
	} {
		_, err := tool2.Prepare(json.RawMessage(tc.args), poo.PrepareEnv{})
		if err == nil {
			t.Errorf("the call %s was accepted, want a refusal", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("the refusal for %s does not carry %q: %v", tc.args, tc.want, err)
		}
	}
	// The minimum and maximum refusals keep their own names.
	for _, tc := range []struct{ args, want string }{
		{`{"name":"deploy","args":{"env":"staging","replicas":0}}`, `"replicas" is below the minimum of 1`},
		{`{"name":"deploy","args":{"env":"staging","replicas":11}}`, `"replicas" is above the maximum of 10`},
	} {
		err := prepareDeploy(t, tc.args)
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("the refusal for %s does not carry %q: %v", tc.args, tc.want, err)
		}
	}
}

// TestTheRefusalNeverEndsInAnEmptyMessage pins the old failure shape:
// no refusal may render as a bare pointer with an empty message. The
// check must not trim, because trimming is what hides the trailing
// space the old renderer left behind.
func TestTheRefusalNeverEndsInAnEmptyMessage(t *testing.T) {
	err := prepareDeploy(t, `{"name":"deploy","args":{"mode":"canary"}}`)
	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("the refusal ends in an empty message: %q", err.Error())
	}
}

// TestABadDiscriminatorNamesTheBranchValues covers a tagged oneOf: a
// discriminator value no branch declares is refused with the values
// the branches declare.
func TestABadDiscriminatorNamesTheBranchValues(t *testing.T) {
	tool := map[string]any{
		"description": "Send a payload",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"oneOf": []any{
				map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"kind": map[string]any{"const": "text"}},
					"required":             []string{"kind"},
					"additionalProperties": false,
				},
				map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"kind": map[string]any{"const": "image"}},
					"required":             []string{"kind"},
					"additionalProperties": false,
				},
			},
		},
	}
	path := writeManifest(t, t.TempDir(), "send.json", map[string]any{"send": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"send","args":{"kind":"video"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the unmatched discriminator was accepted")
	}
	if !strings.Contains(err.Error(), `"kind" must be one of: "text", "image"`) {
		t.Errorf("the refusal does not name the branch values: %v", err)
	}
}

// TestANonObjectAgainstATaggedOneOfNamesTheType covers the type miss a
// tagged oneOf reports for a value that is not an object.
func TestANonObjectAgainstATaggedOneOfNamesTheType(t *testing.T) {
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
	_, err := tool2.Prepare(json.RawMessage(`{"name":"ship","args":{"build":7}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the non-object value was accepted")
	}
	if !strings.Contains(err.Error(), `"build" must be an object`) {
		t.Errorf("the refusal does not name the type: %v", err)
	}
}

// TestATypelessOneOfNamesTheType covers the tagged oneOf whose schema
// declares no type: the miss still renders as an object refusal.
func TestATypelessOneOfNamesTheType(t *testing.T) {
	tool := map[string]any{
		"description": "Send a payload",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"build": map[string]any{
					"oneOf": []any{
						map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"kind": map[string]any{"const": "text"}},
							"required":             []string{"kind"},
							"additionalProperties": false,
						},
						map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"kind": map[string]any{"const": "image"}},
							"required":             []string{"kind"},
							"additionalProperties": false,
						},
					},
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "build", "style": "equals", "flag": "--build"},
		},
	}
	path := writeManifest(t, t.TempDir(), "send.json", map[string]any{"send": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"send","args":{"build":7}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the non-object value was accepted")
	}
	if !strings.Contains(err.Error(), `"build" must be an object`) {
		t.Errorf("the refusal does not name the type: %v", err)
	}
}

// TestATypeSiblingBesideAOneOfRendersTheObjectSemantics covers a
// schema the library compiles with a oneOf and a sibling type: the
// refusal follows what the schema enforces, not the ignored sibling.
func TestATypeSiblingBesideAOneOfRendersTheObjectSemantics(t *testing.T) {
	tool := map[string]any{
		"description": "Send a payload",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"build": map[string]any{
					"type": "array",
					"oneOf": []any{
						map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"kind": map[string]any{"const": "text"}},
							"required":             []string{"kind"},
							"additionalProperties": false,
						},
						map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"kind": map[string]any{"const": "image"}},
							"required":             []string{"kind"},
							"additionalProperties": false,
						},
					},
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "build", "style": "equals", "flag": "--build"},
		},
	}
	path := writeManifest(t, t.TempDir(), "send.json", map[string]any{"send": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"send","args":{"build":7}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the non-object value was accepted")
	}
	if strings.Contains(err.Error(), "must be an array") {
		t.Errorf("the refusal follows the ignored sibling type: %v", err)
	}
	if !strings.Contains(err.Error(), `"build" must be an object`) {
		t.Errorf("the refusal does not name the object semantics: %v", err)
	}
}

// TestARefPropertyRendersThroughTheDefinition covers a property the
// schema declares through $ref: the refusal reads the definition the
// pointer resolves to, not the reference's bare map.
func TestARefPropertyRendersThroughTheDefinition(t *testing.T) {
	tool := map[string]any{
		"description": "Label a thing",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{"$ref": "#/$defs/color"},
			},
			"required":             []string{"color"},
			"additionalProperties": false,
			"$defs": map[string]any{
				"color": map[string]any{"type": "string", "enum": []string{"red", "green"}},
			},
		},
		"bindings": []any{
			map[string]any{"property": "color", "style": "equals", "flag": "--color"},
		},
	}
	path := writeManifest(t, t.TempDir(), "label.json", map[string]any{"label": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"label","args":{"color":"blue"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the off-enum value was accepted")
	}
	if !strings.Contains(err.Error(), `"color" must be one of: "red", "green"`) {
		t.Errorf("the refusal does not read through the reference: %v", err)
	}
}

// TestATwoHopReferenceDegradesToThePath pins the documented limit: a
// reference chain deeper than one hop falls back to the pointer and
// the issue code, never to an empty message.
func TestATwoHopReferenceDegradesToThePath(t *testing.T) {
	tool := map[string]any{
		"description": "Label a thing",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{"$ref": "#/$defs/color"},
			},
			"required":             []string{"color"},
			"additionalProperties": false,
			"$defs": map[string]any{
				"color": map[string]any{"$ref": "#/$defs/hue"},
				"hue":   map[string]any{"type": "string", "enum": []string{"red", "green"}},
			},
		},
		"bindings": []any{
			map[string]any{"property": "color", "style": "equals", "flag": "--color"},
		},
	}
	path := writeManifest(t, t.TempDir(), "label.json", map[string]any{"label": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"label","args":{"color":"blue"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the off-enum value was accepted")
	}
	if !strings.Contains(err.Error(), "/color: enum") {
		t.Errorf("the two-hop refusal does not degrade to the pointer and code: %v", err)
	}
	if strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("the degraded refusal ends in an empty message: %v", err)
	}
}

// TestANestedUnknownArgumentScopesTheParent covers a nested unknown
// argument: the accepted list names the parent the argument belongs
// under, not the tool's top level.
func TestANestedUnknownArgumentScopesTheParent(t *testing.T) {
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
	for _, want := range []string{`unknown argument "targit"`, "ship.build accepts: target*"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}
}

// TestAnArrayItemConstraintNamesTheItem covers the per-item walk: a
// constraint the schema declares on an array's items renders with the
// item's path, not the raw pointer.
func TestAnArrayItemConstraintNamesTheItem(t *testing.T) {
	tool := map[string]any{
		"description": "Tag a thing",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string", "enum": []string{"red", "green"}},
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "tags", "style": "equals", "flag": "--tags"},
		},
	}
	path := writeManifest(t, t.TempDir(), "tag.json", map[string]any{"tag": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"tag","args":{"tags":["red","blue"]}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the off-enum item was accepted")
	}
	if !strings.Contains(err.Error(), `"tags/1" must be one of: "red", "green"`) {
		t.Errorf("the refusal does not name the item and its values: %v", err)
	}
}

// TestARefPropertyHintsItsValues covers the reference in the hint: a
// property the schema declares through $ref shows the definition's
// values in the catalog line, the way a refusal reads them.
func TestARefPropertyHintsItsValues(t *testing.T) {
	tool := map[string]any{
		"description": "Label a thing",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{"$ref": "#/$defs/color"},
			},
			"required":             []string{"color"},
			"additionalProperties": false,
			"$defs": map[string]any{
				"color": map[string]any{"type": "string", "enum": []string{"red", "green"}},
			},
		},
		"bindings": []any{
			map[string]any{"property": "color", "style": "equals", "flag": "--color"},
		},
	}
	path := writeManifest(t, t.TempDir(), "label.json", map[string]any{"label": tool})
	catalog := load(t, path)
	text := catalog.Section(false)
	if !strings.Contains(text, "label (args: color*[red|green]:") && !strings.Contains(text, "label (args: color*[red|green])") {
		t.Errorf("the catalog line does not carry the reference's values: %q", text)
	}
}

// TestAnEnumValueWithAPipeIsQuoted covers the bracket list's own
// separator: an enum value with a pipe in it quotes the list's string
// values, so the list stays one value per member.
func TestAnEnumValueWithAPipeIsQuoted(t *testing.T) {
	tool := map[string]any{
		"description": "Match a mode",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{
					"type": "string",
					"enum": []string{"a|b", "c"},
				},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "mode", "style": "equals", "flag": "--mode"},
		},
	}
	path := writeManifest(t, t.TempDir(), "match.json", map[string]any{"match": tool})
	catalog := load(t, path)
	text := catalog.Section(false)
	if !strings.Contains(text, `mode["a|b"|"c"]`) {
		t.Errorf("the catalog line does not quote the piped value: %q", text)
	}
}

// TestTheAcceptedArgumentsListKeepsNameOrder pins the ordering
// convention: names sort by their base name, and a required mark is
// appended after the sort, so the call-path renderer and the catalog
// hint list the same arguments in the same order.
func TestTheAcceptedArgumentsListKeepsNameOrder(t *testing.T) {
	tool := map[string]any{
		"description": "Order check",
		"command":     []string{script(t, "say")},
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a":  map[string]any{"type": "string"},
				"a!": map[string]any{"type": "string"},
			},
			"required":             []string{"a"},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "a", "style": "equals", "flag": "--a"},
			map[string]any{"property": "a!", "style": "equals", "flag": "--a-bang"},
		},
	}
	path := writeManifest(t, t.TempDir(), "order.json", map[string]any{"order": tool})
	tool2 := cliregistry.NewCall(load(t, path))
	_, err := tool2.Prepare(json.RawMessage(`{"name":"order","args":{"zzz":1}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the unknown argument was accepted")
	}
	if !strings.Contains(err.Error(), "order accepts: a*, a!") {
		t.Errorf("the accepted list does not keep name order: %v", err)
	}
}

// TestTheCatalogCarriesEachToolSArguments pins the up-front half: the
// prompt's catalog line names the tool's arguments, required ones
// marked, with an enum's values in brackets, so the first call can be
// right.
func TestTheCatalogCarriesEachToolSArguments(t *testing.T) {
	path := writeManifest(t, t.TempDir(), "deploy.json", map[string]any{"deploy": deployTool(t)})
	catalog := load(t, path)
	text := catalog.Section(false)
	if !strings.Contains(text, "deploy (args: env*[staging|production], replicas): Deploy a service") {
		t.Errorf("the catalog line does not carry the arguments with the enum's values: %q", text)
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
