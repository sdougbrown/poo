package cliregistry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// jsonTool writes a registry with one CLI that takes its json-bound
// object on stdin and decodes its stdout as json. The command is a
// script through /bin/sh -c; a script that does not read its input
// drains it through /dev/null last, the way the stdin fixture drains
// it, so the write of the bound JSON never meets a closed pipe.
func jsonTool(t *testing.T, script string) string {
	t.Helper()
	return writeManifest(t, t.TempDir(), "json.json", map[string]any{
		"json": map[string]any{
			"description": "Take a json object on stdin and print json",
			"command":     []string{"/bin/sh", "-c", script},
			"output":      map[string]any{"mode": "json"},
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"data": map[string]any{
						"type":                 "object",
						"properties":           map[string]any{"op": map[string]any{"type": "string"}},
						"required":             []any{"op"},
						"additionalProperties": true,
					},
				},
				"required":             []any{"data"},
				"additionalProperties": false,
			},
			"bindings": []map[string]any{{"property": "data", "style": "json", "flag": "--data-stdin"}},
		},
	})
}

// TestACallCLIToolReturnsItsDecodedJSON covers the success path: a tool
// whose manifest decodes its stdout returns the decoded JSON as the
// result, and the bash exit-code envelope is bypassed.
func TestACallCLIToolReturnsItsDecodedJSON(t *testing.T) {
	tool := cliregistry.NewCall(load(t, jsonTool(t, `printf '%s' '{"sum":3,"op":"add"}'; cat > /dev/null`)))

	res := call(t, tool, `{"name":"json","args":{"data":{"op":"add","a":1,"b":2}}}`, poo.ToolEnv{})
	if res.IsError {
		t.Fatalf("the call is an error: %q", res.Text)
	}
	want := `{"op":"add","sum":3}`
	if got := string(res.Structured); got != want {
		t.Errorf("the structured result is %s, want %s", got, want)
	}
	if res.Text != want {
		t.Errorf("the text is %q, want %q", res.Text, want)
	}
}

// TestACallCLIToolReportsADecodeFailure covers a successful exit whose
// stdout does not decode: the call is a recoverable error that names the
// decode failure.
func TestACallCLIToolReportsADecodeFailure(t *testing.T) {
	tool := cliregistry.NewCall(load(t, jsonTool(t, `echo not-json; cat > /dev/null`)))

	prepared, err := prepare(t, tool, `{"name":"json","args":{"data":{"op":"add"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Call(context.Background(), prepared, poo.ToolEnv{})
	if err == nil {
		t.Fatal("a stdout that does not decode was accepted")
	}
	if !strings.Contains(err.Error(), "decode output:") {
		t.Errorf("the error %q does not name the decode failure", err)
	}
}

// TestACallCLIToolNullGoesThroughTheEnvelope pins the fall-through
// edge: a decoded null is not a result, so a json tool that prints
// null comes back through the exit-status envelope, which carries the
// exit code and the output.
func TestACallCLIToolNullGoesThroughTheEnvelope(t *testing.T) {
	tool := cliregistry.NewCall(load(t, jsonTool(t, `printf 'null'; cat > /dev/null`)))

	res := call(t, tool, `{"name":"json","args":{"data":{"op":"add"}}}`, poo.ToolEnv{})
	if res.IsError {
		t.Fatalf("the call is an error: %q", res.Text)
	}
	obs := observation(t, res)
	if code, ok := obs[bash.EnvelopeReturncode].(float64); !ok || code != 0 {
		t.Errorf("the observation has no exit code 0: %v", obs[bash.EnvelopeReturncode])
	}
	if out, _ := obs[bash.EnvelopeOutput].(string); out != "null" {
		t.Errorf("the output is %q, want the null the tool printed", out)
	}
	want := `{"returncode": 0, "timed_out": false, "output": "null"}`
	if res.Text != want {
		t.Errorf("the text is %q, want the envelope %q", res.Text, want)
	}
}

// TestACallCLIToolCarriesJSONBoundArgsToTheChild covers the round trip:
// the json-bound object is written to the child's stdin intact, and what
// the child prints comes back as the tool result.
func TestACallCLIToolCarriesJSONBoundArgsToTheChild(t *testing.T) {
	tool := cliregistry.NewCall(load(t, jsonTool(t, `cat`)))

	res := call(t, tool, `{"name":"json","args":{"data":{"op":"echo","msg":"雪","zeta":1,"alpha":2}}}`, poo.ToolEnv{})
	if res.IsError {
		t.Fatalf("the call is an error: %q", res.Text)
	}
	want := `{"alpha":2,"msg":"雪","op":"echo","zeta":1}`
	if got := string(res.Structured); got != want {
		t.Errorf("the structured result is %s, want %s", got, want)
	}
	if res.Text != want {
		t.Errorf("the text is %q, want %q", res.Text, want)
	}
}
