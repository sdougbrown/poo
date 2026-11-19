package transport

import (
	"encoding/json"
	"strings"
)

// SetNested sets a named parameter in a request's parameter map.
//
// A name may be a path, e.g. "thinking.budget_tokens", and the value is then
// nested under the parts before it. A parameter that is already there keeps
// the parts another call set: the API takes one object per top-level name,
// and two fields of one object are one parameter's parts.
func SetNested(params map[string]json.RawMessage, name string, value any) {
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		SetFlat(params, name, value)
		return
	}
	var leaf any = value
	for i := len(parts) - 1; i > 0; i-- {
		leaf = map[string]any{parts[i]: leaf}
	}
	SetFlat(params, parts[0], leaf)
}

// SetFlat sets a parameter, merging it into an object that is there.
func SetFlat(params map[string]json.RawMessage, name string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	existing, ok := params[name]
	if !ok {
		params[name] = raw
		return
	}
	merged, err := MergeJSON(existing, raw)
	if err != nil {
		params[name] = raw
		return
	}
	params[name] = merged
}

// MergeJSON merges two JSON objects, one field at a time.
//
// A field the second object does not have keeps the first object's value.
func MergeJSON(first, second []byte) ([]byte, error) {
	var a, b map[string]json.RawMessage
	if err := json.Unmarshal(first, &a); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(second, &b); err != nil {
		return nil, err
	}
	for k, v := range b {
		a[k] = v
	}
	return json.Marshal(a)
}
