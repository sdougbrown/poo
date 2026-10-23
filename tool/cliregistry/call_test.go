package cliregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// prepare validates a call.
func prepare(t *testing.T, tool *cliregistry.CallTool, args string) (poo.Prepared, error) {
	t.Helper()
	return tool.Prepare(json.RawMessage(args), poo.PrepareEnv{})
}

// call validates and runs a call.
func call(t *testing.T, tool *cliregistry.CallTool, args string, env poo.ToolEnv) poo.ToolResult {
	t.Helper()
	prepared, err := prepare(t, tool, args)
	if err != nil {
		t.Fatalf("preparing %s: %v", args, err)
	}
	res, err := tool.Call(context.Background(), prepared, env)
	if err != nil {
		t.Fatalf("calling %s: %v", args, err)
	}
	return res
}

// observation decodes the structured observation.
func observation(t *testing.T, res poo.ToolResult) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		t.Fatalf("decoding the observation %q: %v", res.Structured, err)
	}
	return out
}

// markerLines returns how many times the say CLI recorded that it ran.
func markerLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("reading the marker: %v", err)
	}
	return strings.Count(string(data), "ran")
}

// TestCallRunsTheCLI covers the happy path: the arguments become the
// command's argv, and the observation carries the output and the exit
// status in bash's envelope.
func TestCallRunsTheCLI(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := cliregistry.NewCall(catalog)

	res := call(t, tool, `{"name":"say","args":{"name":"ada"}}`, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: bash.ObservationCapMax},
	})
	if res.IsError {
		t.Errorf("the call is an error: %q", res.Text)
	}
	obs := observation(t, res)
	if code, ok := obs[bash.EnvelopeReturncode].(float64); !ok || code != 0 {
		t.Errorf("the return code is %v, want 0", obs[bash.EnvelopeReturncode])
	}
	output, _ := obs[bash.EnvelopeOutput].(string)
	if !strings.Contains(output, "hello ada") {
		t.Errorf("the output is %q, want a greeting", output)
	}

	// The array argument becomes the command's positional arguments.
	res = call(t, tool, `{"name":"list","args":{"words":["one","two"]}}`, poo.ToolEnv{})
	obs = observation(t, res)
	output, _ = obs[bash.EnvelopeOutput].(string)
	if output != "one\ntwo\n" {
		t.Errorf("the output is %q, want the two words", output)
	}
}

// TestCallReportsAFailedExit covers a command that fails: the model sees
// the exit status and what the command said, as a recoverable error.
func TestCallReportsAFailedExit(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := cliregistry.NewCall(catalog)

	res := call(t, tool, `{"name":"say","args":{"name":"nobody"}}`, poo.ToolEnv{})
	if !res.IsError {
		t.Error("a call that exited 3 is not an error")
	}
	obs := observation(t, res)
	if code, ok := obs[bash.EnvelopeReturncode].(float64); !ok || code != 3 {
		t.Errorf("the return code is %v, want 3", obs[bash.EnvelopeReturncode])
	}
	stderr, _ := obs[bash.EnvelopeStderr].(string)
	if !strings.Contains(stderr, "nobody") {
		t.Errorf("the observation is %q, want the command's stderr", obs)
	}
}

// TestPrepareRefusesBadArgumentsBeforeAnyProcess covers decode before
// exec: a name that is not in the catalog and arguments that do not fit
// the CLI's schema are refused in Prepare, and the fixture's record shows
// that nothing ran.
func TestPrepareRefusesBadArgumentsBeforeAnyProcess(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := cliregistry.NewCall(catalog)

	marker := t.TempDir() + "/ran"
	t.Setenv(markerEnv, marker)

	// The record works: a call that prepares and runs wrote to it.
	call(t, tool, `{"name":"say","args":{}}`, poo.ToolEnv{})
	if got := markerLines(t, marker); got != 1 {
		t.Fatalf("the fixture ran %d times, want 1", got)
	}

	bad := []struct {
		name string
		args string
	}{
		{"an unknown name", `{"name":"nope","args":{}}`},
		{"a missing name", `{"args":{}}`},
		{"an argument of the wrong type", `{"name":"say","args":{"name":5}}`},
		{"an argument the schema does not have", `{"name":"say","args":{"colour":"red"}}`},
		{"args that are not an object", `{"name":"say","args":["ada"]}`},
		{"a malformed call", `{"name":"say"} {}`},
	}
	for _, c := range bad {
		prepared, err := prepare(t, tool, c.args)
		if err == nil {
			t.Fatalf("%s prepared a call: %v", c.name, prepared)
		}
		var invalid *poo.ErrInvalidArgs
		if !errors.As(err, &invalid) {
			t.Errorf("%s failed with %T, want a recoverable argument error", c.name, err)
		}
		if lines := markerLines(t, marker); lines != 1 {
			t.Errorf("%s started a process: the fixture ran %d times", c.name, lines)
		}
	}
}

// TestTheObservationIsElided covers the envelope's cap: a long output is
// cut to a head and a tail, and the model is told how much went.
func TestTheObservationIsElided(t *testing.T) {
	tool := cliregistry.NewCall(load(t, floodManifest(t)))

	res := call(t, tool, `{"name":"flood","args":{}}`, poo.ToolEnv{
		Settings: poo.ToolSettings{ObservationCapChars: 40},
	})
	obs := observation(t, res)
	if _, ok := obs[bash.EnvelopeOutputHead].(string); !ok {
		t.Errorf("the observation %q has no head", obs)
	}
	if _, ok := obs[bash.EnvelopeOutputTail].(string); !ok {
		t.Errorf("the observation %q has no tail", obs)
	}
	elided, _ := obs[bash.EnvelopeElidedChars].(float64)
	if elided <= 0 {
		t.Errorf("the observation %q has no elided count", obs)
	}
	if _, ok := obs[bash.EnvelopeHint].(string); !ok {
		t.Errorf("the observation %q has no hint", obs)
	}
}

// floodManifest writes a registry with one CLI that prints a long output.
func floodManifest(t *testing.T) string {
	t.Helper()
	return writeManifest(t, t.TempDir(), "flood.json", map[string]any{
		"flood": map[string]any{
			"description": "Print a long output",
			"command":     []string{script(t, "flood")},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	})
}
