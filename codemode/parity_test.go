package codemode_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/codemode/quickjs"
	"github.com/sdougbrown/poo/codemode/starlark"
)

// scriptNames maps every field of poo.NestedResult to the name the
// engines surface it under. A field with no entry fails the parity test,
// so a new NestedResult field cannot ship unseen by the engines' scripts.
var scriptNames = map[string]string{
	"Handle":     "handle",
	"Tool":       "tool",
	"Outcome":    "outcome",
	"Text":       "text",
	"Structured": "structured",
	"IsError":    "is_error",
}

// TestEveryNestedResultFieldReachesBothEngines pins the wire contract in
// one place: each engine shapes a nested result for the script, and both
// prompt files promise the same names. An engine that drops a field, or a
// prompt that promises one, fails here.
func TestEveryNestedResultFieldReachesBothEngines(t *testing.T) {
	typ := reflect.TypeOf(poo.NestedResult{})
	for i := 0; i < typ.NumField(); i++ {
		if _, ok := scriptNames[typ.Field(i).Name]; !ok {
			t.Errorf("poo.NestedResult has a field %q with no script name for the engines", typ.Field(i).Name)
		}
	}
	canned := poo.NestedResult{
		Handle:     "rX",
		Tool:       "read",
		Outcome:    poo.OutcomeOK,
		Text:       "the result",
		Structured: json.RawMessage(`{"k":1}`),
	}
	for _, test := range []struct {
		name   string
		engine codemode.Engine
		script string
	}{
		{"starlark", starlark.New(), `exit(call("read", {}))`},
		{"quickjs", quickjs.New(), `return call("read", {});`},
	} {
		t.Run(test.name, func(t *testing.T) {
			res, err := test.engine.Run(t.Context(), codemode.RunOptions{
				Script: test.script,
				Nested: cannedNested{canned},
			})
			if err != nil {
				t.Fatalf("the run failed: %v", err)
			}
			var seen map[string]any
			if err := json.Unmarshal(res.Value, &seen); err != nil {
				t.Fatalf("the script returned no object: %v", err)
			}
			for _, name := range scriptNames {
				if _, ok := seen[name]; !ok {
					t.Errorf("the script cannot see the result's %q", name)
				}
			}
		})
	}
	for _, doc := range []string{"../tool/codemode/prompt.md", "../tool/codemode/quickjs.md"} {
		text, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading the prompt: %v", err)
		}
		for _, name := range scriptNames {
			if !strings.Contains(string(text), name) {
				t.Errorf("%s does not promise the result's %q", doc, name)
			}
		}
	}
}

// cannedNested answers every call with the one result it holds.
type cannedNested struct {
	result poo.NestedResult
}

func (c cannedNested) Call(context.Context, []poo.NestedCall) ([]poo.NestedResult, error) {
	return []poo.NestedResult{c.result}, nil
}
