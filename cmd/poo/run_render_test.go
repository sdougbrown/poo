package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
)

// The command adds the finish tool and its own prompt sections after
// the build rendered. The session's render reads that environment, so a
// template the build accepted can still fail when the session is
// constructed. The failure is the prompt's, so it takes the settings'
// exit code and message.

// TestAPromptThatFailsWithTheAddedToolTakesTheConfigExit covers a
// template that renders with the profile's tools and fails once the
// finish tool joins them.
func TestAPromptThatFailsWithTheAddedToolTakesTheConfigExit(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "result.schema.json")
	schema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatalf("writing the schema: %v", err)
	}
	settings, err := config.Parse([]byte(promptRenderSettings(schemaPath)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}

	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     dir,
		DefaultProfile: defaultProfile,
		Tools:          linkedTools(),
		Profiles:       linkedProfiles(),
		Executors:      executorRegistry(),
		Askers:         []string{"decide"},
		PromptFS: fstest.MapFS{
			"base.md": {Data: []byte("{{if gt (len .Tools) 4}}{{index .Tools 99}}{{end}}the base\n")},
		},
		Build: poo.BuildInfo{Name: "poo", Version: version},
	})
	if err != nil {
		t.Fatalf("the build refused a prompt that renders on the build's tools: %v", err)
	}
	transport := pootest.NewTransport(pootest.Turn{Text: "done"})
	runtime.Options.Transport = transport

	// The command's own changes, in the order the run makes them.
	if err := addFinishTool(runtime); err != nil {
		t.Fatalf("adding the finish tool: %v", err)
	}
	runtime.Options.Prompt = append(runtime.Options.Prompt, promptMutation(runtime, runtime.Options.Model, ""))

	sess, code, err := newSession(runtime)
	if sess != nil {
		sess.Close()
	}
	if err == nil {
		t.Fatal("the session accepted a prompt that does not render")
	}
	if code != 2 {
		t.Errorf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "base.md") {
		t.Errorf("the failure %v does not name the file", err)
	}
	if !strings.Contains(err.Error(), `the field "system_prompt"`) {
		t.Errorf("the failure %v is not the settings' own", err)
	}
	if got := len(transport.Requests()); got != 0 {
		t.Errorf("the endpoint got %d requests, want none", got)
	}
}

// promptRenderSettings returns a settings document with a chat model
// and the finish schema.
func promptRenderSettings(schemaPath string) string {
	return "version: 1\n" +
		"endpoints:\n" +
		"  test:\n" +
		"    endpoint: http://127.0.0.1:1/v1\n" +
		"models:\n" +
		"  test-model:\n" +
		"    endpoint: test\n" +
		"    model_id: test-model\n" +
		"    api: chat_completions\n" +
		"    tools_mode: native\n" +
		"    ops: [chat]\n" +
		"    context_limit: 32768\n" +
		"roles:\n" +
		"  chat: test-model\n" +
		"finish:\n" +
		"  schema_file: " + schemaPath + "\n"
}
