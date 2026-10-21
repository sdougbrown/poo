package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The one-shot's job behaviour: it waits for the jobs, it prints the
// run's answer, and its exit code comes from the settle-reason table.

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

// TestARunPrintsItsAnswerOnStdout covers the one-shot's output: the
// final assistant message of the run goes to stdout.
func TestARunPrintsItsAnswerOnStdout(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"the answer"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr, stdout := runCLIWithStdout(t,
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
	if !strings.Contains(stdout, "the answer") {
		t.Errorf("stdout is %q, want it to hold the answer", stdout)
	}
}

// TestTheExitCodeComesFromTheSettleReason covers the one-shot's exit
// code: it is the code the settle-reason table gives.
func TestTheExitCodeComesFromTheSettleReason(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"the answer"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr, _ := runCLIWithStdout(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--max-turns", "1",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	// The turn ceiling settles the run, and the code is the table's.
	if code != poo.ExitCode(poo.ReasonMaxTurns) {
		t.Errorf("the exit code is %d, want %d; stderr: %s", code, poo.ExitCode(poo.ReasonMaxTurns), stderr)
	}
}

// TestTheNoWaitJobsFlagIsAccepted covers the flag: the one-shot exits at
// the first settle when it is given.
func TestTheNoWaitJobsFlagIsAccepted(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"the answer"},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	code, stderr, stdout := runCLIWithStdout(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--no-wait-jobs",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	)
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "the answer") {
		t.Errorf("stdout is %q, want it to hold the answer", stdout)
	}
}
