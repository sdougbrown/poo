package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/finish"
)

// The one-shot's job behaviour: it waits for the jobs, it prints the
// run's answer, and its exit code comes from the settle-reason table.

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

// TestTheOneShotWithAStuckJobExitsAtTheFirstSettle covers the flag's
// actual behavior: the model's job never finishes, and the one-shot
// exits at the first settle with the answer instead of waiting for it.
// A regression that waits for jobs never returns, and the test's
// deadline catches it.
func TestTheOneShotWithAStuckJobExitsAtTheFirstSettle(t *testing.T) {
	body := "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","content":"the answer","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"job","arguments":"{\"text\":\"one\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	endpoint := newFakeEndpoint(t, body)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")

	args := []string{
		"run",
		"--base-url", endpoint.URL + "/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--max-turns", "1",
		"--no-wait-jobs",
		"--working-dir", dir,
		"--log", logPath,
		"say hello",
	}

	// The same std-handle juggling as runCLIWithStdout, but the run is
	// awaited under a deadline: waiting for a job that is never
	// released does not return.
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

	type result struct {
		code int
	}
	done := make(chan result, 1)
	go func() {
		done <- result{run(args)}
	}()

	var code int
	select {
	case res := <-done:
		code = res.code
	case <-time.After(10 * time.Second):
		os.Stderr = oldStderr
		os.Stdout = oldStdout
		t.Fatal("the one-shot did not exit at the first settle: --no-wait-jobs was ignored, or the run is stuck waiting for the job")
		return
	}

	os.Stderr = oldStderr
	os.Stdout = oldStdout
	w.Close()
	outW.Close()
	stderr := string(<-stderrDone)
	stdout := string(<-stdoutDone)
	r.Close()
	outR.Close()

	// The turn ceiling settles the run, and the code is the table's.
	if code != poo.ExitCode(poo.ReasonMaxTurns) {
		t.Errorf("the exit code is %d, want %d; stderr: %s", code, poo.ExitCode(poo.ReasonMaxTurns), stderr)
	}
	if !strings.Contains(stdout, "the answer") {
		t.Errorf("stdout is %q, want it to hold the answer printed without waiting for the stuck job", stdout)
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
		// The finish call's own turn is the last one: the candidate
		// it adds outranks nothing at the turn ceiling.
		MaxTurns: 2,
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

// TestTheOneShotPrintsTheFinishResult covers the one-shot's stdout rule
// with a typed result: the finish result the run settled with is what
// stdout gets, ahead of the last assistant message.
func TestTheOneShotPrintsTheFinishResult(t *testing.T) {
	tool, err := finish.New(json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`))
	if err != nil {
		t.Fatal(err)
	}
	tr := pootest.NewTransport(
		pootest.Turn{
			Text:  "the prose answer",
			Calls: []pootest.Call{{ID: "c", Name: "finish", Arguments: []byte(`{"result":{"answer":"the typed answer"}}`)}},
		},
	)
	s, err := poo.New(poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"finish": tool},
		ToolOrder:    []string{"finish"},
		ContextLimit: 32768,
		SessionID:    "s1",
		ModelID:      "m",
		// The finish call's own turn is the last one: the candidate
		// it adds outranks nothing at the turn ceiling.
		MaxTurns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.WaitRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := runOutput(context.Background(), s, res, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "the typed answer") {
		t.Errorf("stdout is %q, want it to hold the finish result", stdout)
	}
	if strings.Contains(stdout, "the prose answer") {
		t.Errorf("stdout is %q, want the finish result ahead of the last assistant message", stdout)
	}
}
