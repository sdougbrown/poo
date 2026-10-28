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

// TestThePrintFlagsAreAnAliasForRun covers the alias: the flag takes the
// prompt, and the flags that follow it are still the run's own.
func TestThePrintFlagsAreAnAliasForRun(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}` + "\n\n" +
		"data: [DONE]\n\n"

	for _, alias := range []string{"-p", "--print"} {
		t.Run(alias, func(t *testing.T) {
			endpoint := newFakeEndpoint(t, body)
			dir := t.TempDir()
			logPath := filepath.Join(dir, "run.jsonl")

			code, stderr, stdout := runCLIWithStdout(t,
				alias, "say hello",
				"--endpoint", endpoint.URL+"/v1",
				"--model-id", "test-model",
				"--context-limit", "32768",
				"--working-dir", dir,
				"--log", logPath,
			)
			if code != 0 {
				t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
			}
			if stdout != "done\n" {
				t.Errorf("the answer is %q, want done", stdout)
			}

			// The log confirms the run: the flags after the prompt were
			// read, not taken for more of the prompt.
			if got := names(readLog(t, logPath)); got[0] != "session.created" {
				t.Errorf("the first record is %q, want session.created", got[0])
			}

			if len(endpoint.requests) != 1 {
				t.Fatalf("the endpoint got %d requests, want 1", len(endpoint.requests))
			}
			messages, ok := endpoint.requests[0]["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Fatalf("the request's messages are %v, want a non-empty list", endpoint.requests[0]["messages"])
			}
			last, ok := messages[len(messages)-1].(map[string]any)
			if !ok {
				t.Fatalf("the last message is %v, want an object", messages[len(messages)-1])
			}
			if last["role"] != "user" {
				t.Fatalf("the last message's role is %v, want user", last["role"])
			}
			if last["content"] != "say hello" {
				t.Errorf("the prompt is %v, want say hello", last["content"])
			}
		})
	}
}

// TestAPrintFlagWithoutAPromptIsTheRunsOwnError covers the missing
// prompt: the alias adds nothing of its own.
func TestAPrintFlagWithoutAPromptIsTheRunsOwnError(t *testing.T) {
	code, stderr := runCLI(t, "-p")
	if code != 2 {
		t.Errorf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(stderr, "a prompt is required") {
		t.Errorf("the error is %q, want it to name the prompt", stderr)
	}
}

// TestAPrintFlagTakesTheRunsFlagsFirst covers flags written before the
// prompt: they are passed through in the run's own order.
func TestAPrintFlagTakesTheRunsFlagsFirst(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()

	code, stderr, stdout := runCLIWithStdout(t,
		"-p",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--ephemeral",
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if stdout != "done\n" {
		t.Errorf("the answer is %q, want done", stdout)
	}
}

// TestAPrintFlagLeavesAPromptOfSeveralWordsInOrder covers an unquoted
// prompt: the words keep their order, as they do after run.
func TestAPrintFlagLeavesAPromptOfSeveralWordsInOrder(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()

	code, stderr, _ := runCLIWithStdout(t,
		"-p",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--ephemeral",
		"say", "hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if len(endpoint.requests) != 1 {
		t.Fatalf("the endpoint got %d requests, want 1", len(endpoint.requests))
	}
	messages, ok := endpoint.requests[0]["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("the request's messages are %v, want a non-empty list", endpoint.requests[0]["messages"])
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("the last message is %v, want an object", messages[len(messages)-1])
	}
	if last["content"] != "say hello" {
		t.Errorf("the prompt is %v, want say hello", last["content"])
	}
}

// TestAPrintPromptKeepsAWordThatLooksLikeAFlag covers a prompt word
// that starts with a dash and is not one of the run's flags: it stays
// in the prompt, as it does after run.
func TestAPrintPromptKeepsAWordThatLooksLikeAFlag(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()

	code, stderr, _ := runCLIWithStdout(t,
		"-p",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--ephemeral",
		"say", "-hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if len(endpoint.requests) != 1 {
		t.Fatalf("the endpoint got %d requests, want 1", len(endpoint.requests))
	}
	messages, ok := endpoint.requests[0]["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("the request's messages are %v, want a non-empty list", endpoint.requests[0]["messages"])
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("the last message is %v, want an object", messages[len(messages)-1])
	}
	if last["content"] != "say -hello" {
		t.Errorf("the prompt is %v, want say -hello", last["content"])
	}
}

// TestAPrintFlagWithABadValueFails covers a run flag with a value it
// refuses: the run reports it, and the flag is not taken for the prompt.
func TestAPrintFlagWithABadValueFails(t *testing.T) {
	code, stderr := runCLI(t, "-p", "fix it", "--max-turns", "many")
	if code != 2 {
		t.Errorf("the exit code is %d, want 2", code)
	}
	if !strings.Contains(stderr, "max-turns") {
		t.Errorf("the error is %q, want it to name --max-turns", stderr)
	}
}

// TestAPrintFlagBetweenPromptWordsIsAFlag covers a run flag placed
// between two words of the prompt: the flag applies, and the words keep
// their order.
func TestAPrintFlagBetweenPromptWordsIsAFlag(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr, _ := runCLIWithStdout(t,
		"-p", "say",
		"--log", logPath,
		"hello",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("the run wrote no log at %s, want --log applied: %v", logPath, err)
	}
	if got := printedPrompt(t, endpoint); got != "say hello" {
		t.Errorf("the prompt is %q, want say hello", got)
	}
}

// TestAPrintPromptAfterTheTerminatorIsAllWords covers "--": every
// argument after it is a word of the prompt, flag-shaped or not.
func TestAPrintPromptAfterTheTerminatorIsAllWords(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)
	dir := t.TempDir()

	code, stderr, _ := runCLIWithStdout(t,
		"-p",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--ephemeral",
		"--", "explain", "--max-turns",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if got := printedPrompt(t, endpoint); got != "explain --max-turns" {
		t.Errorf("the prompt is %q, want explain --max-turns", got)
	}
}

// printedPrompt returns the text of the last message the endpoint got.
func printedPrompt(t *testing.T, endpoint *fakeEndpoint) string {
	t.Helper()
	if len(endpoint.requests) != 1 {
		t.Fatalf("the endpoint got %d requests, want 1", len(endpoint.requests))
	}
	messages, ok := endpoint.requests[0]["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("the request's messages are %v, want a non-empty list", endpoint.requests[0]["messages"])
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("the last message is %v, want an object", messages[len(messages)-1])
	}
	text, _ := last["content"].(string)
	return text
}

func TestTheHelpNamesThePrintAlias(t *testing.T) {
	_, _, stdout := runCLIWithStdout(t, "--help")
	if !strings.Contains(stdout, "poo -p") {
		t.Errorf("the help is %q, want it to name the alias", stdout)
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
