package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// TestADeadlineDuringTheRunSettlesCancelled covers a run whose deadline
// passes while the provider is answering: the run settles cancelled, the
// log holds the settle record, and the exit code says 130.
func TestADeadlineDuringTheRunSettlesCancelled(t *testing.T) {
	// The endpoint sends one chunk and then stalls.
	stop := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + `{"choices":[{"index":0,"delta":{"content":"first"}}]}` + "\n\n"))
		fl.Flush()
		select {
		case <-stop:
		case <-time.After(5 * time.Second):
		}
	}))
	defer endpoint.Close()
	defer close(stop)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--timeout", "200ms",
		"--working-dir", dir,
		"--log", logPath,
		"a prompt",
	)
	if code != 130 {
		t.Fatalf("the exit code is %d, want 130; stderr: %s", code, stderr)
	}

	recs := readLog(t, logPath)
	settled := recs[len(recs)-1]
	if settled["record"] != "run.settled" {
		t.Fatalf("the last record is %v, want run.settled", settled["record"])
	}
	if settled["reason"] != "cancelled" {
		t.Errorf("the settle reason is %v, want cancelled", settled["reason"])
	}
	detail, _ := settled["detail"].(map[string]any)
	if detail["source"] != poo.SourceCancelDeadline {
		t.Errorf("the settle source is %v, want %s", detail["source"], poo.SourceCancelDeadline)
	}
	// The run started, so the run settled: the log never holds a
	// run.started without a run.settled after it.
	started := false
	for _, rec := range recs {
		switch rec["record"] {
		case "run.started":
			started = true
		case "run.settled":
			if !started {
				t.Error("the log holds a run.settled before its run.started")
			}
		}
	}
	if !started {
		t.Error("the log holds no run.started record")
	}
}

// TestAMissingWorkingDirectoryFails covers a typo in the working
// directory: the run must not create it and carry on.
func TestAMissingWorkingDirectoryFails(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	missing := filepath.Join(dir, "not-here")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", missing,
		"say hello",
	)
	if code == 0 {
		t.Fatalf("the run succeeded with a missing working directory; stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "working") {
		t.Errorf("the error is %q, want it to name the working directory", stderr)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the working directory was created: %v", err)
	}
}

// TestTheBaseURLCredentialsAreNotLogged covers a base URL that carries a
// password: the log must not hold it.
func TestTheBaseURLCredentialsAreNotLogged(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", "http://user:s3cret@"+strings.TrimPrefix(endpoint.URL, "http://")+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Errorf("the log holds the credential: %s", raw)
	}
	if strings.Contains(string(raw), "user:") {
		t.Errorf("the log holds the userinfo: %s", raw)
	}

	// The endpoint is still named, so a consumer can tell where the
	// session went.
	recs := readLog(t, logPath)
	created := recs[0]
	model, _ := created["model"].(map[string]any)
	base, _ := model["base_url"].(string)
	if !strings.Contains(base, endpoint.URL) {
		t.Errorf("the logged base URL is %q, want the endpoint", base)
	}
}

// TestAnExistingLogPathIsRefused covers a second run pointed at the same
// log: the run fails before it sends anything.
func TestAnExistingLogPathIsRefused(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	code, stderr = runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello again",
	)
	if code == 0 {
		t.Fatalf("the second run succeeded on the same log; stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "exists") {
		t.Errorf("the error is %q, want it to say the log exists", stderr)
	}
	// The second run sent nothing.
	if len(endpoint.requests) != 1 {
		t.Errorf("the endpoint got %d requests, want 1", len(endpoint.requests))
	}

	// The log still holds one session's records.
	recs := readLog(t, logPath)
	created := 0
	for _, rec := range recs {
		if rec["record"] == "session.created" {
			created++
		}
	}
	if created != 1 {
		t.Errorf("the log holds %d session.created records, want 1", created)
	}
}

// TestTheNativeModePromptDoesNotAskForATextCall covers the prompt's
// contract: the native mode's prompt never tells the model to write a
// text tool call.
func TestTheNativeModePromptDoesNotAskForATextCall(t *testing.T) {
	tools := []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
	prompt := sessionPromptWith(t, "gemma4", tools, "native", "")
	for _, needle := range []string{"tool_call", "Call a tool by writing"} {
		if strings.Contains(prompt, needle) {
			t.Errorf("the native prompt mentions %q", needle)
		}
	}
	// The prompt still says how to work.
	if !strings.Contains(prompt, "fresh shell") {
		t.Error("the native prompt lost the operating contract")
	}
}

// TestTheTextModePromptAsksForATextCall covers the other half: the text
// and auto modes tell the model how to write a call.
func TestTheTextModePromptAsksForATextCall(t *testing.T) {
	tools := []poo.ToolSpec{
		{Name: "bash", Description: "Run a shell command."},
		{Name: "read", Description: "Read a range of lines from a file."},
	}
	for _, mode := range []string{"text", "auto"} {
		prompt := sessionPromptWith(t, "gemma4", tools, mode, "")
		for _, needle := range []string{"tool_call", "bash: Run a shell command."} {
			if !strings.Contains(prompt, needle) {
				t.Errorf("the %s prompt does not mention %q", mode, needle)
			}
		}
	}
}

// TestTheNativeModeRunSendsTheToolsNatively covers a native run: the
// request carries the tools, and the model's history holds no call
// markup.
func TestTheNativeModeRunSendsTheToolsNatively(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}

	tools, _ := endpoint.requests[0]["tools"].([]any)
	// The bash tool may run in the background, so the core registers
	// the jobs tools beside it.
	if len(tools) != 4 {
		t.Errorf("the request held %d tools, want 4", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"bash", "read", "jobs", "cancel_job"} {
		if !names[want] {
			t.Errorf("the request does not offer the %q tool", want)
		}
	}

	// The system prompt never asks for a text call.
	recs := readLog(t, logPath)
	for _, rec := range recs {
		if rec["record"] != "message" {
			continue
		}
		content, _ := rec["content"].([]any)
		for _, block := range content {
			m, _ := block.(map[string]any)
			if m["kind"] != "text" {
				continue
			}
			text, _ := m["text"].(string)
			if strings.Contains(text, "tool_call") {
				t.Errorf("the system prompt asks for a text call: %q", text)
			}
		}
	}
}

// TestAWorkingDirectoryThatIsAFileFails covers a --working-dir that
// exists as a regular file: the run must refuse it and not create
// anything.
func TestAWorkingDirectoryThatIsAFileFails(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	code, stderr := runCLI(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--working-dir", file,
		"say hello",
	)
	if code != 2 {
		t.Fatalf("the exit code is %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "not a directory") {
		t.Errorf("the error is %q, want it to name the directory problem", stderr)
	}
}
