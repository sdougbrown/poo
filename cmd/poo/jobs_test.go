package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
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

// TestTheOneShotPrintsTheLastRunsAnswer covers the one-shot's stdout
// rule: without a finish result, the answer is the last run's final
// assistant message, so the client must wait for jobs and wakes before
// it selects what to print.
func TestTheOneShotPrintsTheLastRunsAnswer(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tr := pootest.NewTransport(
		pootest.Turn{
			Text:  "initial answer",
			Calls: []pootest.Call{{ID: "c", Name: "job", Arguments: []byte(`{"text":"one"}`)}},
		},
		pootest.Turn{Text: "woken answer"},
	)
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"job": tool},
		ToolOrder:    []string{"job"},
		ContextLimit: 32768,
		SessionID:    "s1",
		ModelID:      "m",
		MaxTurns:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.WaitRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	tool.Release()
	stdout, err := runOutput(context.Background(), s, first, false)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != s.LastAssistantMessage() {
		t.Fatalf("stdout is %q, but the last run's answer is %q", stdout, s.LastAssistantMessage())
	}
}
