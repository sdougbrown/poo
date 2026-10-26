package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestAnUnknownExecutorIsAnErrorInEveryProfile covers the setting in
// front of every command: a binary that links bash validates the name
// against its executor registry whatever profile runs, so a typo is a
// settings failure and not a command that silently uses no shell.
func TestAnUnknownExecutorIsAnErrorInEveryProfile(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	body := `version: 1
endpoints:
  test:
    endpoint: http://127.0.0.1:1/v1
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat]
    context_limit: 32768
roles:
  chat: test-model
tools:
  bash:
    executor: review-not-an-executor
`
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	for _, profile := range []string{"base", "code", "search"} {
		for _, command := range []string{"prompt", "serve", "run"} {
			t.Run(profile+"/"+command, func(t *testing.T) {
				args := []string{command, "--settings", settings, "--profile", profile}
				if command == "run" {
					args = append(args, "review prompt")
				}
				if command == "serve" {
					// A server that gets past the build reads stdin; an
					// empty one lets the case fail on its output rather
					// than on a read.
					withEmptyStdin(t)
				}
				code, stderr := runCLI(t, args...)
				want := fmt.Sprintf("poo %s: config: unsupported: the field \"tools.bash.executor\": this binary links no executor named \"review-not-an-executor\"\n", command)
				if code != 2 {
					t.Errorf("the exit code is %d, want 2; stderr: %s", code, stderr)
				}
				if stderr != want {
					t.Errorf("stderr is %q, want %q", stderr, want)
				}
			})
		}
	}
}
