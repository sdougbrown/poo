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

// markerLines returns how many times the say tool recorded that it ran.
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

	res := call(t, tool, `{"command":"say","args":{"name":"ada"}}`, poo.ToolEnv{
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
	res = call(t, tool, `{"command":"list","args":{"words":["one","two"]}}`, poo.ToolEnv{})
	obs = observation(t, res)
	output, _ = obs[bash.EnvelopeOutput].(string)
	if output != "one\ntwo\n" {
		t.Errorf("the output is %q, want the two words", output)
	}

	// A tool that takes no arguments is called without the args
	// property: an omitted args is an empty object.
	res = call(t, tool, `{"command":"list"}`, poo.ToolEnv{})
	if res.IsError {
		t.Errorf("a call with no args is an error: %q", res.Text)
	}
}

// TestAKilledCLIReportsNoReturncode covers the envelope's null: a
// process that died by a signal has no exit status, and bash reports
// null for it.
func TestAKilledCLIReportsNoReturncode(t *testing.T) {
	tool := cliregistry.NewCall(load(t, suicideManifest(t)))

	res := call(t, tool, `{"command":"suicide","args":{}}`, poo.ToolEnv{})
	if !res.IsError {
		t.Errorf("a tool that died by a signal is not an error: %q", res.Text)
	}
	obs := observation(t, res)
	code, ok := obs[bash.EnvelopeReturncode]
	if !ok {
		t.Fatalf("the observation has no return code: %v", obs)
	}
	if code != nil {
		t.Errorf("the return code is %v, want null: a killed process has no exit status", code)
	}
}

// suicideManifest writes a registry with one tool that dies by a signal.
func suicideManifest(t *testing.T) string {
	t.Helper()
	return writeManifest(t, t.TempDir(), "suicide.json", map[string]any{
		"suicide": map[string]any{
			"description": "Print, then die by a signal",
			"command":     []string{script(t, "suicide")},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	})
}

// TestCallReportsAFailedExit covers a command that fails: the model sees
// the exit status and what the command said, as a recoverable error.
func TestCallReportsAFailedExit(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := cliregistry.NewCall(catalog)

	res := call(t, tool, `{"command":"say","args":{"name":"nobody"}}`, poo.ToolEnv{})
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
// the tool's schema are refused in Prepare, and the fixture's record shows
// that nothing ran.
func TestPrepareRefusesBadArgumentsBeforeAnyProcess(t *testing.T) {
	catalog := fixtureCatalog(t)
	tool := cliregistry.NewCall(catalog)

	marker := t.TempDir() + "/ran"
	t.Setenv(markerEnv, marker)

	// The record works: a call that prepares and runs wrote to it.
	call(t, tool, `{"command":"say","args":{}}`, poo.ToolEnv{})
	if got := markerLines(t, marker); got != 1 {
		t.Fatalf("the fixture ran %d times, want 1", got)
	}

	bad := []struct {
		name string
		args string
	}{
		{"an unknown name", `{"command":"nope","args":{}}`},
		{"a missing name", `{"args":{}}`},
		{"an argument of the wrong type", `{"command":"say","args":{"command":5}}`},
		{"an argument the schema does not have", `{"command":"say","args":{"colour":"red"}}`},
		{"args that are not an object", `{"command":"say","args":["ada"]}`},
		{"an explicit null", `{"command":"say","args":null}`},
		{"a malformed call", `{"command":"say"} {}`},
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

	res := call(t, tool, `{"command":"flood","args":{}}`, poo.ToolEnv{
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

// floodManifest writes a registry with one tool that prints a long output.
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

// TestAnUnknownCommandIsSelfCorrecting covers the two misreadings the tool's
// name invited: nesting it inside itself, and handing it a tool the session
// already offers. Each is answered with the correction, not the catalog's
// size, and neither starts a process.
func TestAnUnknownCommandIsSelfCorrecting(t *testing.T) {
	tool := cliregistry.NewCall(fixtureCatalog(t))

	for _, c := range []struct {
		name string
		args string
		want []string
	}{
		{
			name: "the tool nested inside itself",
			args: `{"command":"call_cli","args":{"command":"say"}}`,
			want: []string{"call_cli", `"command"`},
		},
		{
			name: "the retired tool name nested inside itself",
			args: `{"command":"call_tool","args":{"command":"say"}}`,
			want: []string{"call_cli", `"command"`},
		},
		{
			name: "a built-in tool",
			args: `{"command":"bash","args":{"command":"ls"}}`,
			want: []string{`"bash" is a built-in tool`, "call it directly"},
		},
		{
			name: "a tool the core adds",
			args: `{"command":"jobs"}`,
			want: []string{`"jobs" is a built-in tool`, "call it directly"},
		},
		{
			name: "a name the catalog does not hold",
			args: `{"command":"nope"}`,
			want: []string{`no tool named "nope"`, "list, say"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := prepare(t, tool, c.args)
			if err == nil {
				t.Fatalf("%s prepared a call", c.args)
			}
			text := err.Error()
			for _, want := range c.want {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal of %s does not carry %q: %q", c.args, want, text)
				}
			}
		})
	}
}

// TestABuiltinTheSessionDoesNotOfferPointsAtTheCatalog covers the
// refusal's session scope: a built-in name the session does not offer
// cannot be called directly, so the refusal points at the catalog
// instead of at a call that cannot succeed.
func TestABuiltinTheSessionDoesNotOfferPointsAtTheCatalog(t *testing.T) {
	tool := cliregistry.NewCall(fixtureCatalog(t)).WithSessionTools([]string{"bash"})

	_, err := prepare(t, tool, `{"command":"edit","args":{"path":"a","content":"b"}}`)
	if err == nil {
		t.Fatal("the call prepared")
	}
	text := err.Error()
	if strings.Contains(text, "is a built-in tool") {
		t.Errorf("the refusal names a tool the session does not offer a built-in: %q", text)
	}
	for _, want := range []string{`no tool named "edit"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not carry %q: %q", want, text)
		}
	}
}
