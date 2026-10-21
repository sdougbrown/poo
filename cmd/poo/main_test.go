package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// runCLI runs the command with the given arguments and returns its exit
// code and its standard error.
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stderr = w

	// The pipe is drained while the command runs, so output larger than
	// the pipe's buffer cannot block it.
	done := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- out
	}()

	code := run(args)

	os.Stderr = oldStderr
	w.Close()
	out := <-done
	r.Close()
	return code, string(out)
}

func TestTheRequiredFlagsAreChecked(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no base url", []string{"run", "--model-id", "m", "--context-limit", "1024", "hi"}, "--base-url"},
		{"no model id", []string{"run", "--base-url", "http://x", "--context-limit", "1024", "hi"}, "--model-id"},
		{"no context limit", []string{"run", "--base-url", "http://x", "--model-id", "m", "hi"}, "--context-limit"},
		{"a zero context limit", []string{"run", "--base-url", "http://x", "--model-id", "m", "--context-limit", "0", "hi"}, "--context-limit"},
		{"no prompt", []string{"run", "--base-url", "http://x", "--model-id", "m", "--context-limit", "1024"}, "prompt"},
		{"a bad tools mode", []string{"run", "--base-url", "http://x", "--model-id", "m", "--context-limit", "1024", "--tools-mode", "weird", "hi"}, "--tools-mode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, stderr := runCLI(t, c.args...)
			if code != 2 {
				t.Errorf("the exit code is %d, want 2", code)
			}
			if !strings.Contains(stderr, c.want) {
				t.Errorf("the error is %q, want it to name %s", stderr, c.want)
			}
		})
	}
}

func TestAnUnknownCommandFails(t *testing.T) {
	code, stderr := runCLI(t, "fly")
	if code != 2 {
		t.Errorf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("the error is %q, want it to name the unknown command", stderr)
	}
}

func TestTheHelpIsPrinted(t *testing.T) {
	code, _ := runCLI(t, "--help")
	if code != 0 {
		t.Errorf("the exit code is %d, want 0", code)
	}
}

func TestTheDefaultLogPathIsInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("changing the working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	path := defaultLogPath(dir)
	if filepath.Dir(path) != dir {
		t.Errorf("the default log path is %q, want it under %q", path, dir)
	}
	if !strings.HasPrefix(filepath.Base(path), "poo-session-") {
		t.Errorf("the default name is %q, want it to start with poo-session-", filepath.Base(path))
	}
}

func TestTheSystemPromptStatesTheOperatingContract(t *testing.T) {
	prompt := systemPrompt("gemma4", nil, "native")
	for _, needle := range []string{
		"fresh shell",
		"narrow reads",
		"Done means",
		"Background processes are killed",
	} {
		if !strings.Contains(prompt, needle) {
			t.Errorf("the prompt does not mention %q", needle)
		}
	}
}

func TestTheTextModePromptNamesTheToolsAndTheCallFormat(t *testing.T) {
	// In the text mode the model cannot call a tool through the wire
	// format, so the prompt has to say how to write one.
	tools := []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
	prompt := systemPrompt("gemma4", tools, "text")
	for _, needle := range []string{
		"bash: Run a shell command.",
		"read: Read a range of lines from a file.",
		"<tool_call>",
		`{"name": "bash"`,
	} {
		if !strings.Contains(prompt, needle) {
			t.Errorf("the prompt does not mention %q", needle)
		}
	}
}

// The system prompt must be usable as JSON content, so the model's own
// quoting of it does not break the request.
func TestTheSystemPromptSurvivesJSONEncoding(t *testing.T) {
	b, err := json.Marshal(systemPrompt("gemma4", nil, "native"))
	if err != nil {
		t.Fatalf("encoding the prompt: %v", err)
	}
	if !json.Valid(b) {
		t.Error("the encoded prompt is not valid JSON")
	}
}
