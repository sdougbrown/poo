// Package envelope renders an observation: the fields a tool reports,
// as the text the model reads and as the structured payload beside it.
//
// The tools that capture output share the one shape — the bash
// observation, the registered CLI's result, the fetch's body — so an
// envelope-wide change lands once, and a capped body reads the same
// way whatever tool reported it. The field vocabulary is each tool's
// own; the machinery here is shared.
package envelope

import (
	"encoding/json"
	"strings"
)

// Field is one entry of an observation: a key, and the value both the
// text and the structured payload carry.
type Field struct {
	Key   string
	Value any
}

// Render formats the fields as the observation the model reads.
//
// The fields keep the order the caller names them in, and the result
// is valid JSON, so the model can read it either way.
func Render(fields []Field) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		// The field vocabulary is a string and the values are string,
		// int, bool or nil, so neither marshal can fail.
		key, _ := json.Marshal(f.Key)
		value, _ := json.Marshal(f.Value)
		b.Write(key)
		b.WriteString(": ")
		b.Write(value)
	}
	b.WriteString("}")
	return b.String()
}

// Structured returns the structured payload beside the text: the same
// fields, as a JSON object. The payload is derived from the one list,
// so the two readers never diverge.
func Structured(fields []Field) json.RawMessage {
	structured := map[string]any{}
	for _, f := range fields {
		structured[f.Key] = f.Value
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}
