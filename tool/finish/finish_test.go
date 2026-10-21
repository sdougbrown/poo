package finish_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/finish"
)

// The finish tool gives a headless consumer a typed result: the model
// calls it with an object, the object is validated against the
// consumer's schema, and the run settles as finished with the object.

// TestTheToolDescribesItself covers the tool's description: the model
// is told what the tool is for, and the schema is the consumer's.
func TestTheToolDescribesItself(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	spec := tool.Spec()
	if spec.Name != finish.ToolName {
		t.Errorf("the tool's name is %q, want %q", spec.Name, finish.ToolName)
	}
	if spec.Class != poo.ClassReadOnly {
		t.Errorf("the tool's class is %q, want read_only", spec.Class)
	}
	if !json.Valid(spec.Schema) {
		t.Fatalf("the tool's schema is not valid JSON: %s", spec.Schema)
	}
	var decoded map[string]any
	if err := json.Unmarshal(spec.Schema, &decoded); err != nil {
		t.Fatalf("decoding the schema: %v", err)
	}
	if decoded["type"] != "object" {
		t.Errorf("the schema's type is %v, want object", decoded["type"])
	}
}

// TestAValidResultIsAccepted covers the happy path: the result passes
// the schema, and the prepared call carries it.
func TestAValidResultIsAccepted(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	prepared, err := tool.Prepare(json.RawMessage(`{"result":{"answer":"42"}}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}
	result := finish.PreparedResult(prepared)
	if result == nil {
		t.Fatal("the prepared call carries no result")
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("the result is not valid JSON: %v", err)
	}
	if payload["answer"] != "42" {
		t.Errorf("the result is %v, want the answer", payload)
	}
}

// TestAMissingRequiredFieldIsARecoverableError covers the schema's
// required list.
func TestAMissingRequiredFieldIsARecoverableError(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Prepare(json.RawMessage(`{"result":{"wrong":"yes"}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the tool accepted a result that is missing a required field")
	}
	var invalid *poo.ErrInvalidArgs
	if !asInvalidArgs(err, &invalid) {
		t.Fatalf("the error is %T, want an ErrInvalidArgs", err)
	}
}

// TestAWrongTypeIsARecoverableError covers the schema's type.
func TestAWrongTypeIsARecoverableError(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Prepare(json.RawMessage(`{"result":{"answer":42}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the tool accepted a result of the wrong type")
	}
}

// TestAnUnknownFieldIsARecoverableError covers the schema's
// additionalProperties.
func TestAnUnknownFieldIsARecoverableError(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"additionalProperties":false}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Prepare(json.RawMessage(`{"result":{"answer":"42","extra":true}}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the tool accepted a result with an unknown field")
	}
}

// TestAnEnumIsChecked covers the schema's enum.
func TestAnEnumIsChecked(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["ok","failed"]}}}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	if _, err := tool.Prepare(json.RawMessage(`{"result":{"status":"ok"}}`), poo.PrepareEnv{}); err != nil {
		t.Fatalf("the tool refused an allowed value: %v", err)
	}
	if _, err := tool.Prepare(json.RawMessage(`{"result":{"status":"maybe"}}`), poo.PrepareEnv{}); err == nil {
		t.Fatal("the tool accepted a value outside the enum")
	}
}

// TestAnItemsSchemaIsChecked covers the schema's items.
func TestAnItemsSchemaIsChecked(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"files":{"type":"array","items":{"type":"string"}}}}`)
	tool, err := finish.New(schema)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	if _, err := tool.Prepare(json.RawMessage(`{"result":{"files":["a","b"]}}`), poo.PrepareEnv{}); err != nil {
		t.Fatalf("the tool refused a valid array: %v", err)
	}
	if _, err := tool.Prepare(json.RawMessage(`{"result":{"files":["a",1]}}`), poo.PrepareEnv{}); err == nil {
		t.Fatal("the tool accepted an array with a wrong item type")
	}
}

// TestAnUnknownArgumentIsRefused covers the tool's strict decoding: the
// model may not send arguments the tool does not name.
func TestAnUnknownArgumentIsRefused(t *testing.T) {
	tool, err := finish.New(nil)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Prepare(json.RawMessage(`{"result":{},"extra":1}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the tool accepted an unknown argument")
	}
}

// TestACallWithoutAResultIsRefused covers the tool's required argument.
func TestACallWithoutAResultIsRefused(t *testing.T) {
	tool, err := finish.New(nil)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Prepare(json.RawMessage(`{}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("the tool accepted a call without a result")
	}
}

// TestTheToolReturnsTheResult covers the call: the result rides in the
// structured payload.
func TestTheToolReturnsTheResult(t *testing.T) {
	tool, err := finish.New(nil)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	prepared, err := tool.Prepare(json.RawMessage(`{"result":{"answer":"42"}}`), poo.PrepareEnv{})
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}
	result, err := tool.Call(context.Background(), prepared, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Structured, &payload); err != nil {
		t.Fatalf("the payload is not valid JSON: %v", err)
	}
	if payload["answer"] != "42" {
		t.Errorf("the payload is %v, want the answer", payload)
	}
}

// TestASchemaThatIsNotJSONIsRefused covers the construction: a schema
// that does not parse is a build error.
func TestASchemaThatIsNotJSONIsRefused(t *testing.T) {
	if _, err := finish.New(json.RawMessage(`{not json`)); err == nil {
		t.Fatal("the tool was built with a schema that is not JSON")
	}
}

// asInvalidArgs reports that the error is an ErrInvalidArgs.
func asInvalidArgs(err error, target **poo.ErrInvalidArgs) bool {
	for err != nil {
		if e, ok := err.(*poo.ErrInvalidArgs); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
