package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodemodeSettingsSelectScriptLanguage(t *testing.T) {
	for _, test := range []struct {
		engine, script, language, badField string
	}{
		{"", `exit(42)`, "Starlark", ""},
		{"starlark", `exit(42)`, "Starlark", ""},
		{"quickjs", `const xs = [20, 22]; return xs.reduce((a, b) => a + b);`, "QuickJS", ""},
		{"unknown", "", "", "tools.codemode.engine"},
		{"quickjs", "", "", "tools.codemode.exec_steps"},
	} {
		t.Run(test.engine, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"script": test.script})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(string(args))
			call := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"codemode","arguments":` + string(encoded) + `}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
			answer := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
			endpoint := newFakeEndpointSequence(t, []string{call, answer})
			dir := t.TempDir()
			settings := "version: 1\npermission:\n  unmatched: allow\n"
			if test.engine != "" {
				settings += "tools:\n  codemode:\n    engine: " + test.engine + "\n"
			}
			if test.badField == "tools.codemode.exec_steps" {
				settings += "    exec_steps: 100\n"
			}
			path := filepath.Join(dir, "settings.yaml")
			if err := os.WriteFile(path, []byte(settings), 0600); err != nil {
				t.Fatal(err)
			}
			code, stderr := runCLI(t, "run", "--settings", path,
				"--endpoint", endpoint.URL+"/v1", "--model-id", "test-model", "--context-limit", "32768",
				"--working-dir", dir, "--ephemeral", "run the script")
			if test.badField != "" {
				if code == 0 || !strings.Contains(stderr, test.badField) || len(endpoint.requests) != 0 {
					t.Fatalf("invalid engine: exit %d, stderr %q, requests %d", code, stderr, len(endpoint.requests))
				}
				return
			}
			if code != 0 || len(endpoint.requests) != 2 {
				t.Fatalf("run: exit %d, stderr %q, requests %d", code, stderr, len(endpoint.requests))
			}
			for _, field := range []string{"tools", "messages"} {
				data, _ := json.Marshal(endpoint.requests[0][field])
				if !strings.Contains(string(data), test.language) {
					t.Errorf("first request's %s did not name %s", field, test.language)
				}
			}
			messages, _ := endpoint.requests[1]["messages"].([]any)
			for _, item := range messages {
				message, _ := item.(map[string]any)
				content, _ := message["content"].(string)
				if message["role"] == "tool" && strings.Contains(content, "42") {
					return
				}
			}
			t.Errorf("second request did not carry the script result: %v", messages)
		})
	}
}
