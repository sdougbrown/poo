package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The one-shot's job behaviour at the command line: the flag that
// stops the wait, the exit codes the wakes add up to, and the held
// results a run leaves behind.

// backgroundCall is a tool call that runs a command in the background.
func backgroundCall(command string) string {
	arguments, err := json.Marshal(map[string]any{"command": command, "background": true})
	if err != nil {
		panic(err)
	}
	// The arguments are a JSON string inside the response, so their
	// own quotes are escaped.
	escaped := strings.ReplaceAll(string(arguments), `"`, `\"`)
	return "data: " + `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"` + escaped + `"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
}

// answer is a run's final answer.
func answer(text string) string {
	return "data: " + fmt.Sprintf(`{"choices":[{"index":0,"delta":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, text) + "\n\n" +
		"data: [DONE]\n\n"
}

// TestNoWaitJobsStopsTheRunningJob covers the flag: the one-shot exits
// at the first settle, the job that is still running is stopped
// through the session's close, and nothing is left behind.
//
// The run is interrupted while the job is running: the run settles
// cancelled, and the job is still going when the one-shot goes.
func TestNoWaitJobsStopsTheRunningJob(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	logPath := filepath.Join(dir, "run.jsonl")
	endpoint := newFakeEndpointSequence(t, []string{
		backgroundCall(fmt.Sprintf("echo $$ > %s; sleep 30", pidFile)),
		answer("never"),
	})

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	type result struct {
		code   int
		stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stderr := runCLI(t,
			"run",
			"--base-url", endpoint.URL+"/v1",
			"--model-id", "test-model",
			"--context-limit", "32768",
			"--no-wait-jobs",
			"--working-dir", dir,
			"--log", logPath,
			"start a job",
		)
		done <- result{code: code, stderr: stderr}
	}()

	// The job is running when the run is interrupted: the run waits for
	// the job's result, and the interrupt ends the wait.
	waitForFile(t, pidFile)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}

	var got result
	select {
	case got = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not settle after the interrupt")
	}
	if got.code != poo.ExitCode(poo.ReasonCancelled) {
		t.Errorf("the exit code is %d, want %d; stderr: %s",
			got.code, poo.ExitCode(poo.ReasonCancelled), got.stderr)
	}

	recs := readLog(t, logPath)
	ended := false
	for _, rec := range recs {
		if rec["record"] != "job.ended" {
			continue
		}
		ended = true
		if rec["outcome"] != "cancelled" {
			t.Errorf("the job ended %v, want cancelled", rec["outcome"])
		}
		if rec["cause"] != "session_closed" {
			t.Errorf("the job ended because of %v, want session_closed", rec["cause"])
		}
	}
	if !ended {
		t.Error("the log holds no job.ended record")
	}

	// The job's process must be gone: the session's close stops it.
	pid := readPID(t, pidFile)
	if pid == 0 {
		return
	}
	for i := 0; i < 100; i++ {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the job's process %d is still running", pid)
}

// waitForFile waits for a file to appear.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the file %s never appeared", path)
}

// TestTheExitCodeIsTheFirstNonZeroAcrossWakes covers the exit code
// across the runs a job wakes: the first run settles on the turn
// ceiling, the woken run succeeds, and the code is the ceiling's.
func TestTheExitCodeIsTheFirstNonZeroAcrossWakes(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	endpoint := newFakeEndpointSequence(t, []string{
		backgroundCall("echo done"),
		answer("the woken answer"),
	})

	code, stderr, stdout := runCLIWithStdout(t,
		"run",
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--max-turns", "1",
		"--working-dir", dir,
		"--log", logPath,
		"start a job",
	)
	if code != poo.ExitCode(poo.ReasonMaxTurns) {
		t.Fatalf("the exit code is %d, want %d; stderr: %s", code, poo.ExitCode(poo.ReasonMaxTurns), stderr)
	}
	// The answer is the woken run's: the one-shot waits for the jobs
	// before it picks what to print.
	if !strings.Contains(stdout, "the woken answer") {
		t.Errorf("stdout is %q, want it to hold the woken answer", stdout)
	}
}

// TestTheInterruptWithAJobRunningSettlesCancelled covers the
// interrupt: the run is cancelled, the exit code says so, and the job
// that was running stops with the session.
func TestTheInterruptWithAJobRunningSettlesCancelled(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	// The endpoint answers the first call and then stalls, so the run
	// is still going when the signal arrives.
	called := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + `{"choices":[{"index":0,"delta":{"content":"first"}}]}` + "\n\n"))
		w.(http.Flusher).Flush()
		once.Do(func() { close(called) })
		<-release
	}))
	defer server.Close()
	defer close(release)

	// The signal is caught here as well, so a test that fails cannot
	// kill the test binary.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	type result struct {
		code   int
		stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, stderr := runCLI(t,
			"run",
			"--base-url", server.URL+"/v1",
			"--model-id", "test-model",
			"--context-limit", "32768",
			"--working-dir", dir,
			"--log", logPath,
			"say hello",
		)
		done <- result{code: code, stderr: stderr}
	}()

	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("the endpoint was never called")
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}

	select {
	case got := <-done:
		if got.code != poo.ExitCode(poo.ReasonCancelled) {
			t.Errorf("the exit code is %d, want %d; stderr: %s",
				got.code, poo.ExitCode(poo.ReasonCancelled), got.stderr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not settle after the interrupt")
	}
}

// TestTheHeldResultsAreNamedOnStderr covers the held results: a run
// that holds a job's result says which ones it holds, and the results
// wait for the next load.
func TestTheHeldResultsAreNamedOnStderr(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.jsonl")
	settingsPath := filepath.Join(dir, "settings.yaml")
	settings := `version: 1
jobs:
  on_idle: hold
`
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	endpoint := newFakeEndpointSequence(t, []string{
		backgroundCall("echo done"),
		answer("done"),
	})

	code, stderr, _ := runCLIWithStdout(t,
		"run",
		"--settings", settingsPath,
		"--base-url", endpoint.URL+"/v1",
		"--model-id", "test-model",
		"--context-limit", "32768",
		"--max-turns", "1",
		"--working-dir", dir,
		"--log", logPath,
		"start a job",
	)
	if code != poo.ExitCode(poo.ReasonMaxTurns) {
		t.Fatalf("the exit code is %d, want %d; stderr: %s", code, poo.ExitCode(poo.ReasonMaxTurns), stderr)
	}
	if !strings.Contains(stderr, "held") {
		t.Errorf("stderr is %q, want it to name the held results", stderr)
	}
	if !strings.Contains(stderr, "j1") {
		t.Errorf("stderr is %q, want it to name the job it holds", stderr)
	}
}

// readPID reads the process id a job wrote.
func readPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

// processAlive reports whether a process is still running.
//
// The check is the process's own state: a process that was killed and
// not reaped is a zombie, and a zombie is not running.
func processAlive(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return false
	}
	return fields[2] != "Z"
}
