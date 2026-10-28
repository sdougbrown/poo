package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// runCLI runs the command with the given arguments and returns its exit
// code and its standard error.
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, stderr, _ := runCLIWithStdout(t, args...)
	return code, stderr
}

// runCLIWithStdout runs the command and returns its exit code, its
// stderr and its stdout.
func runCLIWithStdout(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldStderr := os.Stderr
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stderr = w
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe: %v", err)
	}
	os.Stdout = outW

	stderrDone := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		stderrDone <- out
	}()
	stdoutDone := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(outR)
		stdoutDone <- out
	}()

	code := run(args)

	os.Stderr = oldStderr
	os.Stdout = oldStdout
	w.Close()
	outW.Close()
	stderr := <-stderrDone
	stdout := <-stdoutDone
	r.Close()
	outR.Close()
	return code, string(stderr), string(stdout)
}

func TestTheRequiredFlagsAreChecked(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no base url", []string{"run", "--model-id", "m", "--context-limit", "1024", "hi"}, "--endpoint"},
		{"no model id", []string{"run", "--endpoint", "http://x", "--context-limit", "1024", "hi"}, "--model-id"},
		{"no context limit", []string{"run", "--endpoint", "http://x", "--model-id", "m", "hi"}, "--context-limit"},
		{"a zero context limit", []string{"run", "--endpoint", "http://x", "--model-id", "m", "--context-limit", "0", "hi"}, "--context-limit"},
		{"no prompt", []string{"run", "--endpoint", "http://x", "--model-id", "m", "--context-limit", "1024"}, "prompt"},
		{"a bad tools mode", []string{"run", "--endpoint", "http://x", "--model-id", "m", "--context-limit", "1024", "--tools-mode", "weird", "hi"}, "--tools-mode"},
		{"only a context limit", []string{"run", "--context-limit", "1000", "hi"}, "--context-limit needs --endpoint and --model-id"},
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

func TestTheDefaultSessionDirectoryIsUnderTheStateDirectory(t *testing.T) {
	settings := &config.Settings{Version: 1}
	dir, err := config.SessionDirectory(settings, config.Overrides{})
	if err != nil {
		t.Fatalf("resolving the session directory: %v", err)
	}
	if filepath.Base(dir) != "sessions" {
		t.Errorf("the default session directory is %q, want it to end in sessions", dir)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("the default session directory is %q, want it to be absolute", dir)
	}
}

func TestTheSystemPromptStatesTheOperatingContract(t *testing.T) {
	// The bash tool's section is the tool's guidance, and it renders
	// only when the tool is linked.
	prompt := sessionPrompt(t, "gemma4", "native", "")
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
	prompt := sessionPromptWith(t, "gemma4", tools, "text", "")
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
