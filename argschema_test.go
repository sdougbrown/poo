package poo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestAValidatorHoldsTheCallToTheSchema covers the enforcement the
// session adds to every call: unknown fields, unknown enum values, and
// missing required arguments are failures the model corrects, and a
// call that fits passes whole.
func TestAValidatorHoldsTheCallToTheSchema(t *testing.T) {
	tool := &schemaTool{}
	compiled, err := compileArgSchemas(map[string]Tool{"probe": tool})
	if err != nil {
		t.Fatalf("compiling the schemas: %v", err)
	}
	for _, arguments := range []string{
		`{"url": "https://example.com/", "deep": true}`,
		`{"url": "https://example.com/", "format": "html"}`,
		`{"format": "raw"}`,
	} {
		if err := compiled.validate("probe", json.RawMessage(arguments)); err == nil {
			t.Errorf("the call %s validated, want the schema's refusal", arguments)
		} else if !strings.Contains(err.Error(), "probe") {
			t.Errorf("the refusal %q does not name the tool", err.Error())
		}
	}
	if err := compiled.validate("probe", json.RawMessage(`{"url": "https://example.com/", "format": "raw"}`)); err != nil {
		t.Errorf("the call that fits is refused: %v", err)
	}
}

// TestAToolWithoutASchemaValidatesNothing covers the opt-out: a tool
// with no schema takes its chances with its own Prepare, as before.
func TestAToolWithoutASchemaValidatesNothing(t *testing.T) {
	compiled, err := compileArgSchemas(map[string]Tool{"bare": &bareTool{}})
	if err != nil {
		t.Fatalf("compiling the schemas: %v", err)
	}
	if err := compiled.validate("bare", json.RawMessage(`{"anything": true}`)); err != nil {
		t.Errorf("the schema-less tool's call is refused: %v", err)
	}
}

// TestASchemaThatDoesNotCompileFailsTheRegistration covers the
// construction failure: a tool whose schema reaches past the closed
// vocabulary fails the registration, and the failure names the tool
// and the keyword.
func TestASchemaThatDoesNotCompileFailsTheRegistration(t *testing.T) {
	_, err := compileArgSchemas(map[string]Tool{"extravagant": &extravagantSchemaTool{}})
	if err == nil {
		t.Fatal("the registration succeeded, want the schema refused")
	}
	if !strings.Contains(err.Error(), `"extravagant"`) {
		t.Errorf("the failure %q does not name the tool", err.Error())
	}
	if !strings.Contains(err.Error(), "anyOf") {
		t.Errorf("the failure %q does not name the unsupported keyword", err.Error())
	}
}

// TestAValidatorOfNilValidatesNothing covers the empty session: a
// cache that was never built refuses nothing, so a consumer that
// drives a tool by hand sees no change.
func TestAValidatorOfNilValidatesNothing(t *testing.T) {
	var compiled *argSchemas
	if err := compiled.validate("anything", json.RawMessage(`{}`)); err != nil {
		t.Errorf("the nil cache refused a call: %v", err)
	}
}

// schemaTool is a tool whose schema is fetch-shaped, for the
// enforcement tests.
type schemaTool struct{}

func (t *schemaTool) Spec() ToolSpec {
	return ToolSpec{
		Name: "probe",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "format": {"type": "string", "enum": ["raw", "markdown"]}
  },
  "required": ["url"],
  "additionalProperties": false
}`),
	}
}

func (t *schemaTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	return preparedStub{}, nil
}

func (t *schemaTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	return ToolResult{}, nil
}

// bareTool has no schema, and so opts out of the enforcement.
type bareTool struct{}

func (t *bareTool) Spec() ToolSpec { return ToolSpec{Name: "bare"} }
func (t *bareTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	return preparedStub{}, nil
}
func (t *bareTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	return ToolResult{}, nil
}

// extravagantSchemaTool reaches past the closed vocabulary, and its
// registration is the failure the compile reports.
type extravagantSchemaTool struct{}

func (t *extravagantSchemaTool) Spec() ToolSpec {
	return ToolSpec{
		Name:   "extravagant",
		Schema: json.RawMessage(`{"type": "object", "anyOf": []}`),
	}
}

func (t *extravagantSchemaTool) Prepare(raw json.RawMessage, env PrepareEnv) (Prepared, error) {
	return preparedStub{}, nil
}
func (t *extravagantSchemaTool) Call(ctx context.Context, in Prepared, env ToolEnv) (ToolResult, error) {
	return ToolResult{}, nil
}

// preparedStub is the prepared value the stub tools return.
type preparedStub struct{}

func (preparedStub) Summary() string { return "a stub call" }
