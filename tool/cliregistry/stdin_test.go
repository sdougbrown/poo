package cliregistry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

func TestCallCLIBindsStdin(t *testing.T) {
	manifest := writeManifest(t, t.TempDir(), "stdin.json", map[string]any{
		"stdin": map[string]any{
			"description": "Read input",
			"command":     []string{"/bin/sh", "-c", `printf '%s|' "$#"; printf '%s' "$*"; printf '|'; cat`, "stdin-probe"},
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "additionalProperties": false,
			},
			"bindings": []map[string]any{{"property": "message", "style": "stdin", "flag": "--read-stdin"}},
		},
	})
	tool := cliregistry.NewCall(load(t, manifest))
	for _, tc := range []struct{ name, args, want string }{
		{"absent", `{"name":"stdin","args":{}}`, "0||"},
		{"empty", `{"name":"stdin","args":{"message":""}}`, "1|--read-stdin|"},
		{"text", `{"name":"stdin","args":{"message":"secret\n雪\n"}}`, "1|--read-stdin|secret\n雪\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, tool, tc.args, poo.ToolEnv{})
			if res.IsError {
				t.Fatalf("call failed: %s", res.Text)
			}
			if got := observation(t, res)[bash.EnvelopeOutput]; got != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCallCLIReportsIncompleteStdin(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		code          int
	}{
		{"zero", "exit 0", 0},
		{"configured success", "exit 3", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := writeManifest(t, t.TempDir(), "stdin.json", map[string]any{
				"stdin": map[string]any{
					"description": "Exit without reading",
					"command":     []string{"/bin/sh", "-c", tc.command},
					"output":      map[string]any{"mode": "raw", "success_exit_codes": []int{tc.code}},
					"inputSchema": map[string]any{
						"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "additionalProperties": false,
					},
					"bindings": []map[string]any{{"property": "message", "style": "stdin"}},
				},
			})
			tool := cliregistry.NewCall(load(t, manifest))
			args, err := json.Marshal(map[string]any{"name": "stdin", "args": map[string]any{"message": strings.Repeat("secret", 1<<18)}})
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := tool.Prepare(args, poo.PrepareEnv{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = tool.Call(context.Background(), prepared, poo.ToolEnv{})
			if err == nil {
				t.Fatal("successful exit with undelivered input was accepted")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error contains input")
			}
		})
	}
}
