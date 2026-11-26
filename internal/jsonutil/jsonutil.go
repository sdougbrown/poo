// Package jsonutil decodes and encodes the arguments a tool's call carries.
//
// Three dispatchers — bash, call_cli and call_mcp — take the model's JSON
// arguments, and each holds them to the same strictness: the arguments are
// one JSON value and nothing else, and a value that cannot encode comes
// back as an empty object rather than a failure the model cannot fix. One
// package keeps the three call paths to the same rules, so a strictness fix
// lands everywhere at once.
package jsonutil

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// DecodeStrict decodes one JSON value and nothing else.
//
// A second value, or anything after the first, is a malformed call: the
// model sent more than it meant to, and taking the first object would
// silently run something else.
func DecodeStrict(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("the arguments hold more than one JSON value")
		}
		return err
	}
	return nil
}

// Marshal encodes a value, falling back to an empty object.
//
// The encoded value is the call's record: a value that cannot encode is a
// bug in the tool, not a failure to hand the model, and the empty object
// says so without stopping the run.
func Marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
