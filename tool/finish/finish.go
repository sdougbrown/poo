// Package finish implements the typed-result tool.
//
// The tool gives a headless consumer a typed result instead of prose:
// the model calls finish with an object, the object is validated against
// the consumer's schema, and the run settles as finished with the object
// in the settle detail.
//
// The tool is opt-in: a session gets it when it names a schema.
package finish

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sdougbrown/poo"
)

// ToolName is the name the model calls the tool by.
const ToolName = "finish"

// Tool is the typed-result tool.
//
// The tool is read-only: it changes nothing in the world, it only ends
// the run with a result.
type Tool struct {
	// schema is the JSON schema the result is validated against.
	schema map[string]any
}

// New returns a finish tool that validates its result against a schema.
//
// An empty schema accepts any object: the result is still typed, and the
// run still settles as finished.
func New(schema json.RawMessage) (*Tool, error) {
	parsed := map[string]any{}
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &parsed); err != nil {
			return nil, fmt.Errorf("finish: the schema is not valid JSON: %w", err)
		}
	}
	return &Tool{schema: parsed}, nil
}

// Spec describes the tool.
//
// The plan's call is finish({result: <object>}), so the advertised
// arguments wrap the consumer's schema in the result property. The
// consumer's schema describes the result, not the call's arguments.
func (t *Tool) Spec() poo.ToolSpec {
	result := t.schema
	if result == nil {
		result = map[string]any{"type": "object"}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"result": result},
		"required":             []string{"result"},
		"additionalProperties": false,
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		raw = json.RawMessage(`{"type":"object","properties":{"result":{"type":"object"}},"required":["result"],"additionalProperties":false}`)
	}
	return poo.ToolSpec{
		Name:        ToolName,
		Description: "Finish the task and report the result. Call it exactly once, when the task is done.",
		Schema:      raw,
		Class:       poo.ClassReadOnly,
	}
}

// Prepare validates the call's arguments.
//
// The result is validated against the schema here, so a failure is a
// recoverable error the model can fix: it sees the message and may call
// again.
func (t *Tool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var args struct {
		Result json.RawMessage `json:"result"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return nil, poo.InvalidArgsf("finish: the arguments are not valid: %v", err)
	}
	if len(args.Result) == 0 {
		return nil, poo.InvalidArgsf("finish: the call has no result")
	}
	if err := t.validate(args.Result); err != nil {
		return nil, poo.InvalidArgsf("finish: %v", err)
	}
	return prepared{result: args.Result}, nil
}

// Call returns the result.
//
// The result is the tool's whole work: the core reads it off the
// prepared call when the batch completes.
func (t *Tool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p, ok := in.(prepared)
	if !ok {
		return poo.ToolResult{}, poo.InvalidArgsf("finish: the call was not prepared")
	}
	return poo.ToolResult{
		Text:       "The result was recorded.",
		Structured: p.result,
	}, nil
}

// prepared is a validated finish call.
type prepared struct {
	// result is the validated result object.
	result json.RawMessage
}

// Summary describes the call.
func (p prepared) Summary() string { return "finish" }

// Result returns the validated result.
//
// The core reads it when the batch completes, so a valid call settles
// the run as finished.
func (p prepared) Result() json.RawMessage { return p.result }

// FinishResult returns the validated result.
//
// The method is what the core recognizes: a prepared call that carries
// one is a finished call.
func (p prepared) FinishResult() json.RawMessage { return p.result }

// PreparedResult returns the validated result of a prepared finish call.
//
// It is nil when the call is not a finish call.
func PreparedResult(p poo.Prepared) json.RawMessage {
	r, ok := p.(poo.FinishResult)
	if !ok {
		return nil
	}
	return r.FinishResult()
}

// validate checks a result against the schema.
//
// The subset is the plan's: type, properties, required, enum, items and
// additionalProperties. A failure is a recoverable error the model can
// fix.
func (t *Tool) validate(result json.RawMessage) error {
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		return fmt.Errorf("the result is not valid JSON: %w", err)
	}
	return validateValue(t.schema, value, "result")
}

// validateValue checks a value against a schema.
func validateValue(schema map[string]any, value any, path string) error {
	if len(schema) == 0 {
		return nil
	}
	if raw, ok := schema["type"]; ok {
		if err := checkType(raw, value, path); err != nil {
			return err
		}
	}
	if raw, ok := schema["enum"]; ok {
		options, ok := raw.([]any)
		if !ok {
			return nil
		}
		found := false
		for _, o := range options {
			if sameJSON(o, value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is not one of the allowed values", path)
		}
	}
	switch v := value.(type) {
	case map[string]any:
		if err := validateObject(schema, v, path); err != nil {
			return err
		}
	case []any:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return nil
		}
		for i, item := range v {
			if err := validateValue(items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateObject checks an object against a schema.
func validateObject(schema map[string]any, value map[string]any, path string) error {
	props, _ := schema["properties"].(map[string]any)
	if required, ok := schema["required"].([]any); ok {
		for _, r := range required {
			name, ok := r.(string)
			if !ok {
				continue
			}
			if _, present := value[name]; !present {
				return fmt.Errorf("%s is missing the required field %q", path, name)
			}
		}
	}
	for name, item := range value {
		if props == nil {
			break
		}
		sub, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		if err := validateValue(sub, item, path+"."+name); err != nil {
			return err
		}
	}
	if additional, ok := schema["additionalProperties"]; ok {
		if allowed, ok := additional.(bool); ok && !allowed && props != nil {
			var extra []string
			for name := range value {
				if _, known := props[name]; !known {
					extra = append(extra, name)
				}
			}
			if len(extra) > 0 {
				sort.Strings(extra)
				return fmt.Errorf("%s has the unknown field(s) %s", path, strings.Join(extra, ", "))
			}
		}
	}
	return nil
}

// checkType checks a value against a schema's type.
func checkType(raw any, value any, path string) error {
	want, ok := raw.(string)
	if !ok {
		return nil
	}
	switch want {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s is not an object", path)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%s is not an array", path)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s is not a string", path)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s is not a number", path)
		}
	case "integer":
		f, ok := value.(float64)
		if !ok || f != float64(int64(f)) {
			return fmt.Errorf("%s is not an integer", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s is not a boolean", path)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s is not null", path)
		}
	}
	return nil
}

// sameJSON reports whether two decoded JSON values are the same.
func sameJSON(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, ok := bv[k]
			if !ok || !sameJSON(v, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i, v := range av {
			if !sameJSON(v, bv[i]) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
