package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The project's instructions are opt-in: the flag asks for them, and
// the loader reads the working directory only.

// promptInLog returns the system prompt a run's log holds.
func promptInLog(t *testing.T, path string) string {
	t.Helper()
	for _, rec := range readLog(t, path) {
		if rec["record"] != "system_prompt" {
			continue
		}
		text, _ := rec["text"].(string)
		return text
	}
	t.Fatalf("the log holds no system_prompt record: %s", path)
	return ""
}

// TestTheProjectPromptFlagAddsTheInstructions covers the flag: the
// working directory's instructions are a section of the prompt.
func TestTheProjectPromptFlagAddsTheInstructions(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("run the tests before you stop"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--project-prompt",
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	prompt := promptInLog(t, logPath)
	if !strings.Contains(prompt, "run the tests before you stop") {
		t.Errorf("the prompt holds no project instructions: %q", prompt)
	}
	// The instructions come after the environment, in the order.
	if strings.Index(prompt, "run the tests before you stop") < strings.Index(prompt, "You are running as the model") {
		t.Errorf("the project section comes before the environment: %q", prompt)
	}
}

// TestTheProjectInstructionsAreNotThereWithoutTheFlag covers the
// opt-in: without the flag the prompt holds no project section.
func TestTheProjectInstructionsAreNotThereWithoutTheFlag(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("run the tests before you stop"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	if prompt := promptInLog(t, logPath); strings.Contains(prompt, "run the tests before you stop") {
		t.Errorf("the prompt holds project instructions without the flag: %q", prompt)
	}
}

// TestTheProjectPromptFlagServesThePromptCommand covers the flag's
// other front door: `poo prompt` prints the sections, and the project's
// instructions are one of them.
func TestTheProjectPromptFlagServesThePromptCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("run the tests before you stop"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	t.Chdir(dir)

	code, stderr, stdout := runCLIWithStdout(t, "prompt", "--project-prompt")
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "run the tests before you stop") {
		t.Errorf("the prompt holds no project instructions:\n%s", stdout)
	}
}

// TestTheUserPromptFlagAddsTheInstructions covers the user's own
// instructions: `poo prompt` prints them, from the file the flag names.
func TestTheUserPromptFlagAddsTheInstructions(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "instructions.md")
	if err := os.WriteFile(userFile, []byte("answer in British English"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	code, stderr, stdout := runCLIWithStdout(t, "prompt", "--user-prompt-file", userFile)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "answer in British English") {
		t.Errorf("the prompt holds no user instructions:\n%s", stdout)
	}
}

// TestTheUserPromptSettingAddsTheInstructions covers the setting: a run
// reads the file the settings name, with no flag at all, and the
// instructions land in the logged system prompt.
func TestTheUserPromptSettingAddsTheInstructions(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	userFile := filepath.Join(dir, "instructions.md")
	if err := os.WriteFile(userFile, []byte("answer in British English"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	body2 := "version: 1\nsession:\n  user_prompt_file: " + userFile + "\n"
	if err := os.WriteFile(settings, []byte(body2), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	logPath := filepath.Join(dir, "run.jsonl")

	t.Setenv("POO_SETTINGS", settings)
	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if prompt := promptInLog(t, logPath); !strings.Contains(prompt, "answer in British English") {
		t.Errorf("the prompt holds no user instructions: %q", prompt)
	}
}

// TestTheProjectPromptFailsOnAFileItCannotRead covers the flag with a
// file the loader cannot read: the run refuses it and says why.
func TestTheProjectPromptFailsOnAFileItCannotRead(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755); err != nil {
		t.Fatalf("making the directory: %v", err)
	}

	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--project-prompt",
		"say hello",
	)
	if code != 2 {
		t.Fatalf("the exit code is %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "AGENTS.md") {
		t.Errorf("the error is %q, want it to name the file", stderr)
	}
}

// TestTheProjectPromptSettingAddsTheInstructions covers the setting:
// session.project_prompt asks for the working directory's instructions
// with no flag at all, the way a session that is not a run — the repl's
// server, mostly — needs to get them. The flag and the setting fill the
// same section, so a run that carries both reads the section once.
func TestTheProjectPromptSettingAddsTheInstructions(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("run the tests before you stop"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	settings := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settings, []byte("version: 1\nsession:\n  project_prompt: true\n"), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	logPath := filepath.Join(dir, "run.jsonl")

	t.Setenv("POO_SETTINGS", settings)
	code, stderr := runCLI(t,
		"run",
		"--endpoint", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	prompt := promptInLog(t, logPath)
	if !strings.Contains(prompt, "run the tests before you stop") {
		t.Errorf("the prompt holds no project instructions: %q", prompt)
	}
	if got := strings.Count(prompt, "run the tests before you stop"); got != 1 {
		t.Errorf("the instructions read %d times, want once: %q", got, prompt)
	}
}
