package cliregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// execTool loads a catalog with one CLI and returns the tool over it.
//
// The manifest is the description, the command and the extra fields a
// case names; the arguments are none, because these cases are about how a
// command runs rather than what it is given.
func execTool(t *testing.T, command []string, extra map[string]any) *cliregistry.CallTool {
	t.Helper()
	body := map[string]any{
		"description": "A fixture CLI",
		"command":     command,
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	}
	for key, value := range extra {
		body[key] = value
	}
	catalog := load(t, writeManifest(t, t.TempDir(), "probe.json", map[string]any{"probe": body}))
	return cliregistry.NewCall(catalog)
}

// sandbox returns the bwrap executor.
//
// The tests that name it are about the sandbox, so a host that cannot
// create one fails rather than skipping: a skipped sandbox test proves
// nothing about the sandbox.
func sandbox(t *testing.T) *bwrap.Executor {
	t.Helper()
	exec, err := bwrap.New(bwrap.Options{})
	if err != nil {
		t.Fatalf("the bwrap executor is unavailable: %v", err)
	}
	return exec
}

// runCall prepares and runs one call with no arguments.
func runCall(t *testing.T, tool *cliregistry.CallTool, env poo.ToolEnv) (poo.ToolResult, error) {
	t.Helper()
	prepared, err := prepare(t, tool, `{"name":"probe","args":{}}`)
	if err != nil {
		return poo.ToolResult{}, err
	}
	return tool.Call(context.Background(), prepared, env)
}

// waitForPID polls for the process id a fixture recorded.
func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the fixture never recorded its pid")
	return 0
}

// gone reports that a process no longer exists.
func gone(pid int) bool { return syscall.Kill(pid, 0) != nil }

// TestTheCLIRunsInTheSessionDirectory covers the directory: a registered
// CLI runs where every other tool runs, not wherever poo's own process
// happens to be.
func TestTheCLIRunsInTheSessionDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := execTool(t, []string{"/bin/pwd"}, nil)

	res, err := runCall(t, tool, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
	if output != dir+"\n" {
		t.Errorf("the CLI ran in %q, want the session's %q", strings.TrimSpace(output), dir)
	}
}

// TestTheManifestDirectoryWins covers the other half of the directory
// rule: a manifest that fixes its working directory runs there, because
// that is the directory the plan validated.
func TestTheManifestDirectoryWins(t *testing.T) {
	dir := t.TempDir()
	tool := execTool(t, []string{"/bin/pwd"}, map[string]any{
		"cwd": map[string]any{"mode": "allow", "roots": []string{dir}},
	})

	res, err := runCall(t, tool, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
	if output != dir+"\n" {
		t.Errorf("the CLI ran in %q, want the manifest's %q", strings.TrimSpace(output), dir)
	}
}

// TestAScrubbedVariableIsNotResolved covers the environment: a manifest's
// env:NAME is resolved against the session's scrubbed environment, so a
// name the setting removed — or a passthrough list left out — is a
// reference the call cannot honour.
func TestAScrubbedVariableIsNotResolved(t *testing.T) {
	const name = "POO_CLI_FIXTURE_KEY"
	t.Setenv(name, "not-a-secret")

	tool := execTool(t, []string{"/bin/sh", "-c", `printf '%s\n' "$` + name + `"`}, map[string]any{
		"env": map[string]string{name: "env:" + name},
	})

	cases := []struct {
		name string
		env  []string
	}{
		{"scrubbed", bash.ScrubEnv(os.Environ(), bash.Scrub{Names: []string{name}})},
		{"passthrough", bash.ScrubEnv(os.Environ(), bash.Scrub{Passthrough: []string{"PATH"}})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runCall(t, tool, poo.ToolEnv{Env: c.env})
			if err == nil {
				t.Fatalf("the call resolved a variable the session's environment does not hold")
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the failure %q does not name the variable", err)
			}
		})
	}

	// The same call with the variable in the session's environment
	// reads it there and nowhere else.
	t.Run("present", func(t *testing.T) {
		res, err := runCall(t, tool, poo.ToolEnv{Env: os.Environ()})
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
		if output != "not-a-secret\n" {
			t.Errorf("the CLI printed %q, want the session's value", output)
		}
	})
}

// TestTheCLIRunsUnderBothExecutors covers the two executors: a CLI runs
// through whichever one the session was built with, with the session's
// directory, and sees only the environment its plan resolved.
func TestTheCLIRunsUnderBothExecutors(t *testing.T) {
	const name = "POO_CLI_BOTH_KEY"
	t.Setenv(name, "not-a-secret")
	dir := t.TempDir()
	command := []string{"/bin/sh", "-c", `printf 'cwd=%s\n' "$(pwd)"; printf 'key=%s\n' "$` + name + `"`}

	cases := []struct {
		name string
		exec bash.Executor
	}{
		{"local", bash.NewLocal()},
		{"bwrap", sandbox(t)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool := execTool(t, command, nil).WithExecutor(c.exec)
			res, err := runCall(t, tool, poo.ToolEnv{
				PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
				Env:        bash.ScrubEnv(os.Environ(), bash.Scrub{Names: []string{name}}),
			})
			if err != nil {
				t.Fatalf("running the CLI: %v", err)
			}
			output, _ := observation(t, res)[bash.EnvelopeOutput].(string)
			if !strings.Contains(output, "cwd="+dir+"\n") {
				t.Errorf("the CLI ran in the wrong directory: %q", output)
			}
			if !strings.Contains(output, "key=\n") {
				t.Errorf("the CLI read a variable the scrub removed: %q", output)
			}
		})
	}
}

// TestALocalCLIReadsTheHostEnvironmentThroughProcfs records the local
// executor's limit: the CLI's own environment is the resolved one, and
// the environment of the process that started it is poo's, which /proc
// still shows.
func TestALocalCLIReadsTheHostEnvironmentThroughProcfs(t *testing.T) {
	assertProcfsSecret(t, bash.NewLocal(), true)
}

// TestTheSandboxHidesPoosEnvironment covers what the sandbox adds: its own
// pid namespace, so /proc/$PPID/environ holds the environment poo
// resolved, not the host's.
func TestTheSandboxHidesPoosEnvironment(t *testing.T) {
	assertProcfsSecret(t, sandbox(t), false)
}

// The variable and value the procfs helper is started with.
const (
	procKey    = "POO_CLI_PROC_KEY"
	procSecret = "parent-environ-secret"
)

// procfsHelperEnv marks the re-executed test binary.
const procfsHelperEnv = "POO_CLI_PROC_HELPER"

// assertProcfsSecret runs the CLI that reads its parent's environment
// through the executor and checks whether the secret is there.
//
// The CLI runs in a test binary of its own, because /proc holds the
// environment a process was started with: a test's Setenv is not part of
// it, so the secret has to be in the environment the parent was started
// with.
func assertProcfsSecret(t *testing.T, executor bash.Executor, want bool) {
	t.Helper()
	if os.Getenv(procfsHelperEnv) == "1" {
		tool := execTool(t, []string{"/bin/sh", "-c",
			`tr '\0' '\n' < "/proc/$PPID/environ" | grep '^` + procKey + `='`, "probe"}, nil).
			WithExecutor(executor)
		res, err := runCall(t, tool, poo.ToolEnv{
			PrepareEnv: poo.PrepareEnv{WorkingDir: t.TempDir()},
			Env:        os.Environ(),
		})
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		// The parent reads the observation either way: a grep that
		// found nothing is a successful command with no output.
		fmt.Print(res.Text)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("finding the test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^"+t.Name()+"$", "-test.timeout=60s")
	cmd.Env = []string{
		procfsHelperEnv + "=1",
		procKey + "=" + procSecret,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the helper failed: %v: %s", err, out)
	}
	if got := strings.Contains(string(out), procKey+"="+procSecret); got != want {
		t.Errorf("the CLI read the parent's environment through /proc: %v, want %v (%s)", got, want, out)
	}
}

// TestAFailedCLIReportsBothStreams covers the failure the envelope
// describes: a command that exits outside its success codes keeps what it
// wrote, on both streams.
func TestAFailedCLIReportsBothStreams(t *testing.T) {
	tool := execTool(t, []string{"/bin/sh", "-c", "printf failed-stdout; printf failed-stderr >&2; exit 3"}, nil)

	res, err := runCall(t, tool, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an exit of 3 is not an error: %q", res.Text)
	}
	obs := observation(t, res)
	if output, _ := obs[bash.EnvelopeOutput].(string); output != "failed-stdout" {
		t.Errorf("the observation lost the stdout of a failed call: %q", output)
	}
	if stderr, _ := obs[bash.EnvelopeStderr].(string); stderr != "failed-stderr" {
		t.Errorf("the observation lost the stderr of a failed call: %q", stderr)
	}
	if code, ok := obs[bash.EnvelopeReturncode].(float64); !ok || code != 3 {
		t.Errorf("the return code is %v, want 3", obs[bash.EnvelopeReturncode])
	}
}

// TestATimedOutCLIReportsItsPartialOutput covers the timeout: what the
// command wrote before poo stopped it is what the model has to debug it
// with.
func TestATimedOutCLIReportsItsPartialOutput(t *testing.T) {
	tool := execTool(t, []string{"/bin/sh", "-c",
		"printf partial-stdout; printf partial-stderr >&2; exec sleep 30"},
		map[string]any{"timeout": "200ms"})

	start := time.Now()
	res, err := runCall(t, tool, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	took := time.Since(start)
	if !res.IsError {
		t.Fatalf("a timed-out CLI is not an error: %q", res.Text)
	}
	obs := observation(t, res)
	if timedOut, _ := obs[bash.EnvelopeTimedOut].(bool); !timedOut {
		t.Errorf("the observation does not say the call timed out: %q", res.Text)
	}
	if output, _ := obs[bash.EnvelopeOutput].(string); output != "partial-stdout" {
		t.Errorf("the observation lost the partial stdout: %q", output)
	}
	if stderr, _ := obs[bash.EnvelopeStderr].(string); stderr != "partial-stderr" {
		t.Errorf("the observation lost the partial stderr: %q", stderr)
	}
	if took > 10*time.Second {
		t.Errorf("the call took %v after a 200ms timeout", took)
	}
}

// TestASpaceArrayBindingIsOneFlagPerElement covers the repeated flag: an
// array bound with the space style is one flag and one value per element,
// with no repeated flag of its own.
func TestASpaceArrayBindingIsOneFlagPerElement(t *testing.T) {
	tool := execTool(t, []string{"/bin/sh", "-c", `printf '<%s>\n' "$@"`, "probe"}, map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "tags", "style": "space", "flag": "--tag"}},
	})

	res := call(t, tool, `{"name":"probe","args":{"tags":["one","two"]}}`, poo.ToolEnv{})
	if output, _ := observation(t, res)[bash.EnvelopeOutput].(string); output != "<--tag>\n<one>\n<--tag>\n<two>\n" {
		t.Errorf("the space-bound array is %q, want one flag and one value per element", output)
	}
}

// TestADashLeadingValueCannotBecomeAFlag covers the value that looks like
// an option: a manifest that passes a value as a separate argument cannot
// be steered into an option it does not declare, so the call is refused
// before anything runs.
func TestADashLeadingValueCannotBecomeAFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatalf("creating the nested directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "hidden-file"), []byte("hidden"), 0o600); err != nil {
		t.Fatalf("writing the nested file: %v", err)
	}
	tool := execTool(t, []string{"/bin/ls", "-1", dir}, map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"color": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "color", "style": "space", "flag": "--color"}},
	})

	_, err := prepare(t, tool, `{"name":"probe","args":{"color":"--recursive"}}`)
	if err == nil {
		t.Fatal("a value that looks like an option prepared")
	}
	var invalid *poo.ErrInvalidArgs
	if !errors.As(err, &invalid) {
		t.Fatalf("the failure is %T, want a *poo.ErrInvalidArgs: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--recursive") {
		t.Errorf("the failure %q does not name the value", err)
	}

	// The same argument, bound positionally, is an operand: the library
	// escapes it, and the call is allowed.
	positional := execTool(t, []string{"/bin/sh", "-c", `printf '<%s>\n' "$@"`, "probe"}, map[string]any{
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"words": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"additionalProperties": false,
		},
		"bindings": []any{map[string]any{"property": "words", "style": "positional"}},
	})
	res := call(t, positional, `{"name":"probe","args":{"words":["ok","-rf"]}}`, poo.ToolEnv{})
	if output, _ := observation(t, res)[bash.EnvelopeOutput].(string); output != "<ok>\n<-->\n<-rf>\n" {
		t.Errorf("the positional value was not escaped: %q", output)
	}
}

// TestACancelledCallStopsTheCLI covers the cancellation: the run's
// context reaches the command, so the call returns and nothing it started
// is left running.
func TestACancelledCallStopsTheCLI(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "pid")
	t.Setenv("FIXTURE_PID", pidPath)
	tool := execTool(t, []string{script(t, "slow")}, map[string]any{
		"timeout": "60s",
		"env":     map[string]string{"FIXTURE_PID": "env:FIXTURE_PID"},
	})
	prepared, err := prepare(t, tool, `{"name":"probe","args":{}}`)
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = tool.Call(ctx, prepared, poo.ToolEnv{})
	}()
	pid := waitForPID(t, pidPath)
	defer func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the cancelled call did not return; the CLI (pid %d) is still running", pid)
	}
	if !gone(pid) {
		t.Fatalf("the cancelled call left the CLI running (pid %d)", pid)
	}
}

// TestAClosedSessionStopsTheCLI covers the session: closing it stops a
// CLI a run started, the way it stops a shell command.
func TestAClosedSessionStopsTheCLI(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "pid")
	t.Setenv("FIXTURE_PID", pidPath)
	tool := execTool(t, []string{script(t, "slow")}, map[string]any{
		"timeout": "60s",
		"env":     map[string]string{"FIXTURE_PID": "env:FIXTURE_PID"},
	})

	policy, err := perm.New(perm.Options{
		Rules: []perm.Rule{{Class: poo.ClassSideEffecting, Action: perm.ActionAllow}},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{
			Name:      cliregistry.CallToolName,
			Arguments: json.RawMessage(`{"name":"probe","args":{}}`),
		}}},
		pootest.Turn{Text: "done"},
	)
	session, err := poo.New(poo.Options{
		Transport: transport,
		Tools:     map[string]poo.Tool{cliregistry.CallToolName: tool},
		ToolOrder: []string{cliregistry.CallToolName},
		Sink:      pootest.NewSink(),
		SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}, poo.WithPolicy(policy))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	if _, err := session.Prompt(context.Background(), "run the slow CLI"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	pid := waitForPID(t, pidPath)
	defer func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()

	start := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	took := time.Since(start)
	if !gone(pid) {
		t.Fatalf("the close returned after %v and left the CLI running (pid %d)", took, pid)
	}
	if took > 5*time.Second {
		t.Errorf("the close took %v", took)
	}
}

// TestASameGroupChildDoesNotOutliveTheCall covers the ordinary
// descendant: a CLI that leaves a child in its own process group returns
// at once, and the child is gone when the call is.
func TestASameGroupChildDoesNotOutliveTheCall(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child")
	tool := execTool(t, []string{"/bin/sh", "-c",
		`sleep 30 </dev/null >/dev/null 2>&1 & printf '%s' $! > "$1"; printf parent-done`, "probe", pidPath},
		map[string]any{"timeout": "2s"})

	start := time.Now()
	res, err := runCall(t, tool, poo.ToolEnv{})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	took := time.Since(start)
	pid := waitForPID(t, pidPath)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()

	if output, _ := observation(t, res)[bash.EnvelopeOutput].(string); output != "parent-done" {
		t.Errorf("the observation lost the CLI's output: %q", output)
	}
	if timedOut, _ := observation(t, res)[bash.EnvelopeTimedOut].(bool); timedOut {
		t.Errorf("a CLI whose child holds no pipe reported a timeout after %v", took)
	}
	if !gone(pid) {
		t.Fatalf("the CLI's same-group child (pid %d) outlived the call", pid)
	}
}

// startedExecutor counts the commands an executor was asked to start.
type startedExecutor struct {
	bash.Executor
	starts atomic.Int64
}

// Start counts the command and hands it to the executor it wraps.
func (s *startedExecutor) Start(ctx context.Context, c bash.Command, out io.Writer) (bash.Process, error) {
	s.starts.Add(1)
	return s.Executor.Start(ctx, c, out)
}

// TestAnAlreadyCancelledCallStartsNothing covers the local executor: a
// call whose context has already ended reaches no process, so a fast CLI
// cannot write its side effects first.
func TestAnAlreadyCancelledCallStartsNothing(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	exec := &startedExecutor{Executor: bash.NewLocal()}
	tool := execTool(t, []string{"/bin/sh", "-c", `printf executed > "$1"`, "probe", marker}, nil).WithExecutor(exec)
	prepared, err := prepare(t, tool, `{"name":"probe","args":{}}`)
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := tool.Call(ctx, prepared, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}})
	if err != nil {
		t.Fatalf("running the CLI: %v", err)
	}
	if got := exec.starts.Load(); got != 0 {
		t.Errorf("the executor was asked to start %d commands for a cancelled call", got)
	}
	if !res.IsError {
		t.Errorf("a cancelled call is not an error: %q", res.Text)
	}
	obs := observation(t, res)
	if timedOut, _ := obs[bash.EnvelopeTimedOut].(bool); timedOut {
		t.Errorf("a cancelled call reported a timeout: %q", res.Text)
	}
	if rc, ok := obs[bash.EnvelopeReturncode]; ok && rc != nil {
		t.Errorf("returncode is %v, want null for a cancelled call", rc)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the cancelled call ran the CLI")
	}
}

// TestAnAlreadyCancelledSandboxDoesNotExecute covers the sandbox: the
// call is the cancel observation and the CLI never ran, so the marker
// file it writes is not there.
func TestAnAlreadyCancelledSandboxDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	tool := execTool(t, []string{"/bin/sh", "-c", `printf executed >> "$1"`, "probe", marker}, nil).
		WithExecutor(sandbox(t))
	prepared, err := prepare(t, tool, `{"name":"probe","args":{}}`)
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 3; i++ {
		res, err := tool.Call(ctx, prepared, poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}})
		if err != nil {
			t.Fatalf("running the CLI: %v", err)
		}
		if !res.IsError {
			t.Errorf("a cancelled call is not an error: %q", res.Text)
		}
		obs := observation(t, res)
		if timedOut, _ := obs[bash.EnvelopeTimedOut].(bool); timedOut {
			t.Errorf("a cancelled call reported a timeout: %q", res.Text)
		}
		if rc, ok := obs[bash.EnvelopeReturncode]; ok && rc != nil {
			t.Errorf("returncode is %v, want null for a cancelled call", rc)
		}
	}
	raw, err := os.ReadFile(marker)
	if err == nil {
		t.Errorf("the already-cancelled sandbox calls ran the CLI: %q", raw)
	}
}
