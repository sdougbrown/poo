package cliregistry_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// recorderTool returns a tool whose CLI records that it ran, and the
// path of that record.
//
// A refused call must not start a process, and a record the command
// appends to is how a started process is told from a refused one.
func recorderTool(t *testing.T, extra map[string]any) (*cliregistry.CallTool, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "ran")
	body := map[string]any{
		"description": "Record that it ran",
		"command":     []string{"/bin/sh", "-c", `printf ran >> "$1"`, "probe", marker},
	}
	for key, value := range extra {
		body[key] = value
	}
	path := writeManifest(t, t.TempDir(), "recorder.json", map[string]any{"probe": body})
	return cliregistry.NewCall(load(t, path)), marker
}

// spaceStringManifest binds two string properties as separate values.
func spaceStringManifest() map[string]any {
	return map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a_ignore": map[string]any{"type": "string"},
				"z_color":  map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{
			map[string]any{"property": "a_ignore", "style": "space", "flag": "--ignore"},
			map[string]any{"property": "z_color", "style": "space", "flag": "--color"},
		},
	}
}

// TestASeparateValueBindingRefusesADelimiter covers the "-" delimiter
// read as a value: a separate-value binding puts the value where the
// command parses options, so a call could consume the "--" the library
// would otherwise use to mark an operand.
func TestASeparateValueBindingRefusesADelimiter(t *testing.T) {
	tool, marker := recorderTool(t, spaceStringManifest())

	prepared, err := prepare(t, tool, `{"name":"probe","args":{"a_ignore":"--","z_color":"--recursive"}}`)
	if err == nil {
		t.Fatalf("a call that passes -- as a value prepared: %v", prepared)
	}
	var invalid *poo.ErrInvalidArgs
	if !errors.As(err, &invalid) {
		t.Fatalf("the failure is %T, want a *poo.ErrInvalidArgs: %v", err, err)
	}
	for _, want := range []string{"/a_ignore", "/z_color", "equals"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name %s", err, want)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused call started a process: %v", err)
	}
}

// TestASeparateValueBindingRefusesANegativeNumber covers the value a
// number encodes to: a negative number is a dash-leading string in argv,
// so the same binding refuses it before anything is built.
func TestASeparateValueBindingRefusesANegativeNumber(t *testing.T) {
	tool, marker := recorderTool(t, map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{"type": "number"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "color", "style": "space", "flag": "--color"}},
	})

	for _, args := range []string{
		`{"name":"probe","args":{"color":-1}}`,
		`{"name":"probe","args":{"color":-1.5}}`,
	} {
		prepared, err := prepare(t, tool, args)
		if err == nil {
			t.Fatalf("%s prepared a call: %v", args, prepared)
		}
		var invalid *poo.ErrInvalidArgs
		if !errors.As(err, &invalid) {
			t.Fatalf("%s failed with %T, want a *poo.ErrInvalidArgs: %v", args, err, err)
		}
		if !strings.Contains(err.Error(), "/color") {
			t.Errorf("the failure %q does not name the property", err)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s started a process: %v", args, err)
		}
	}
}

// TestAnEqualsBindingAcceptsAValueThatLooksLikeAFixedOption covers the
// other half of the rule: a value bound with the equals style stays
// inside one argv element, so it cannot become an option even when it
// spells one the command fixes itself.
func TestAnEqualsBindingAcceptsAValueThatLooksLikeAFixedOption(t *testing.T) {
	tool := execTool(t, []string{"/bin/sh", "-c", `printf '<%s>\n' "$@"`, "probe", "--recursive"}, map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ignore": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "ignore", "style": "equals", "flag": "--ignore"}},
	})

	prepared, err := prepare(t, tool, `{"name":"probe","args":{"ignore":"--recursive"}}`)
	if err != nil {
		t.Fatalf("an equals-bound --ignore=--recursive was refused: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("calling the CLI: %v", err)
	}
	output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
	if output != "<--recursive>\n<--ignore=--recursive>\n" {
		t.Errorf("the argv is %q, want the fixed option then one equals-bound element", output)
	}
}
