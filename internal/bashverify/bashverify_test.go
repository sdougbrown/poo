// Package bashverify runs the plan's bash verify list against every
// executor the build links.
//
// The list is the plan's: the shell features, the timeouts, the
// process teardown, the environment scrubbing and the observation. Every
// executor has to pass all of it, so the same list runs against the
// local executor and against the sandboxed one.
//
// The package holds no code of its own. It lives outside the tool
// package because the tool package cannot link the executors it
// compares.
package bashverify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/bash"
)

// executorCase is one executor the list runs against.
type executorCase struct {
	// name is the executor's name.
	name string
	// build returns the executor for one test.
	build func(t *testing.T) bash.Executor
	// survivors returns what a command left behind, given the files it
	// wrote in the working directory.
	survivors func(t *testing.T, dir string) []string
	// alive reports whether a command's process is still running.
	alive func(t *testing.T, dir string) bool
}

// executorCases returns the executors the list runs against.
//
// A sandbox that cannot run is skipped with the reason logged: a host
// without unprivileged user namespaces cannot run the sandboxed
// executor, and the list says so rather than failing.
func executorCases(t *testing.T) []executorCase {
	t.Helper()
	cases := []executorCase{{
		name:      "local",
		build:     func(t *testing.T) bash.Executor { return bash.NewLocal() },
		survivors: localSurvivors,
		alive:     localAlive,
	}}
	sandbox, err := bwrap.New(bwrap.Options{})
	if err != nil {
		t.Logf("skip: the bwrap executor is unavailable: %v", err)
		return cases
	}
	cases = append(cases, executorCase{
		name:      "bwrap",
		build:     func(t *testing.T) bash.Executor { return sandbox },
		survivors: sandboxSurvivors,
		alive:     sandboxAlive,
	})
	return cases
}

// call runs a command through the tool and returns the result.
func call(t *testing.T, tool *bash.Tool, command, dir string, env []string) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling the arguments: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: dir})
	if err != nil {
		t.Fatalf("preparing %q: %v", command, err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        env,
	})
	if err != nil {
		t.Fatalf("calling %q: %v", command, err)
	}
	return res
}

// observation decodes the result's structured payload.
func observation(t *testing.T, res poo.ToolResult) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		t.Fatalf("decoding the structured payload %q: %v", res.Structured, err)
	}
	return out
}

// output returns the observation's output.
func output(t *testing.T, res poo.ToolResult) string {
	t.Helper()
	text, _ := observation(t, res)["output"].(string)
	return text
}

// TestTheShellFeatures covers the plan's shell features: pipes,
// redirects, heredocs, chaining and failure exit codes.
func TestTheShellFeatures(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := bash.New(bash.WithExecutor(c.build(t)))

			res := call(t, tool, "echo hello | tr a-z A-Z", dir, os.Environ())
			if got := strings.TrimSpace(output(t, res)); got != "HELLO" {
				t.Errorf("the pipe produced %q, want HELLO", got)
			}

			res = call(t, tool, "echo written > file.txt; cat file.txt", dir, os.Environ())
			if got := strings.TrimSpace(output(t, res)); got != "written" {
				t.Errorf("the redirect produced %q, want written", got)
			}

			res = call(t, tool, "cat <<'EOF'\nheredoc line\nEOF", dir, os.Environ())
			if got := strings.TrimSpace(output(t, res)); got != "heredoc line" {
				t.Errorf("the heredoc produced %q, want the heredoc line", got)
			}

			res = call(t, tool, "true && echo chained; false || echo fallback", dir, os.Environ())
			got := output(t, res)
			if !strings.Contains(got, "chained") || !strings.Contains(got, "fallback") {
				t.Errorf("the chaining produced %q, want both branches", got)
			}

			res = call(t, tool, "exit 3", dir, os.Environ())
			obs := observation(t, res)
			if rc, ok := obs["returncode"].(float64); !ok || rc != 3 {
				t.Errorf("the failure exit code is %v, want 3", obs["returncode"])
			}
		})
	}
}

// TestATimeoutReturnsThePartialOutput covers the plan's timeout: the
// call returns what the command produced before the timeout, and the
// model is told that it failed.
func TestATimeoutReturnsThePartialOutput(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := &bash.Tool{Timeout: 300 * time.Millisecond, Executor: c.build(t)}

			res := call(t, tool, "echo before-the-sleep; sleep 30; echo after-the-sleep", dir, os.Environ())
			if !res.IsError {
				t.Errorf("the timed-out call is not an error: %s", res.Text)
			}
			obs := observation(t, res)
			if timedOut, _ := obs["timed_out"].(bool); !timedOut {
				t.Errorf("timed_out is %v, want true", obs["timed_out"])
			}
			got := output(t, res)
			if !strings.Contains(got, "before-the-sleep") {
				t.Errorf("the partial output is missing: %q", got)
			}
			if strings.Contains(got, "after-the-sleep") {
				t.Errorf("the output holds text the command never printed: %q", got)
			}
			if rc, ok := obs["returncode"]; ok && rc != nil {
				t.Errorf("returncode is %v, want null for a killed process", obs["returncode"])
			}
		})
	}
}

// TestADescendantHoldingStdoutDoesNotHangTheCall covers the plan's
// bounded wait: a descendant that holds the output open after the
// shell exits does not keep the call waiting.
func TestADescendantHoldingStdoutDoesNotHangTheCall(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := &bash.Tool{Timeout: 30 * time.Second, Executor: c.build(t)}

			start := time.Now()
			res := call(t, tool, "(sleep 20; echo late) & echo started", dir, os.Environ())
			elapsed := time.Since(start)
			if elapsed > 15*time.Second {
				t.Errorf("the call took %v, want it to return without waiting for the descendant", elapsed)
			}
			if got := output(t, res); !strings.Contains(got, "started") {
				t.Errorf("the command did not run: %q", got)
			}
			if survivors := c.survivors(t, dir); len(survivors) > 0 {
				t.Errorf("the call left %v behind", survivors)
			}
		})
	}
}

// TestARunawayProducerDoesNotBlockTheCall covers the plan's capture
// cap: a producer that writes far more than the cap does not block on
// a full pipe.
func TestARunawayProducerDoesNotBlockTheCall(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := &bash.Tool{Timeout: 30 * time.Second, Executor: c.build(t)}

			start := time.Now()
			res := call(t, tool, "yes abcdefghijklmnopqrstuvwxyz | head -c 20000000", dir, os.Environ())
			elapsed := time.Since(start)
			if elapsed > 20*time.Second {
				t.Errorf("the call took %v, want it to return promptly", elapsed)
			}
			obs := observation(t, res)
			if rc, _ := obs["returncode"].(float64); rc != 0 {
				t.Errorf("returncode is %v, want 0", obs["returncode"])
			}
			// The producer really did write more than the capture cap,
			// so the elision is exercised rather than skipped.
			if elided, _ := obs["elided_chars"].(float64); elided <= 0 {
				t.Errorf("elided_chars is %v, want a positive count", obs["elided_chars"])
			}
		})
	}
}

// TestNothingOutlivesTheCall covers the plan's normal exit: the call
// returns at once, and the process the command left behind is gone.
func TestNothingOutlivesTheCall(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := &bash.Tool{Timeout: 10 * time.Second, Executor: c.build(t)}

			start := time.Now()
			res := call(t, tool, sleepCommand, dir, os.Environ())
			elapsed := time.Since(start)
			if elapsed > 5*time.Second {
				t.Errorf("the call took %v, want it to return at once", elapsed)
			}
			if got := output(t, res); !strings.Contains(got, "done") {
				t.Errorf("the command did not run: %q", got)
			}
			waitFor(t, "the process to die", func() bool { return len(c.survivors(t, dir)) == 0 })
			if survivors := c.survivors(t, dir); len(survivors) > 0 {
				t.Errorf("the call left %v behind", survivors)
			}
		})
	}
}

// sleepCommand starts a process that would outlive the call, and
// records what it has to be found by.
//
// The command writes the identifiers a test needs: the process's own
// pid, and the pid namespace it runs in. A sandboxed executor's pid is
// the namespace's, and the namespace is what names it from outside.
const sleepCommand = "readlink /proc/self/ns/pid > ns.txt; sleep 300 & echo $! > pid.txt; echo done"

// jobCommand is the sleep command as a job runs it: the shell waits
// for the process, so the job lasts until it is stopped.
const jobCommand = sleepCommand + "; wait"

// TestTheEnvironmentIsScrubbed covers the plan's environment rules: the
// variables an endpoint's key is read from are gone, a scrubbed glob
// removes the names it matches, and a passthrough limits the
// environment to the names on it.
func TestTheEnvironmentIsScrubbed(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := bash.New(
				bash.WithExecutor(c.build(t)),
				bash.WithScrub(bash.Scrub{
					Names: []string{"POO_TEST_API_KEY"},
					Globs: []string{"POO_TEST_SECRET_*"},
				}),
			)
			env := append(os.Environ(),
				"POO_TEST_API_KEY=super-secret-value",
				"POO_TEST_SECRET_ONE=also-secret",
				"POO_TEST_PLAIN=visible",
			)

			res := call(t, tool, `echo "key=[$POO_TEST_API_KEY] secret=[$POO_TEST_SECRET_ONE] plain=[$POO_TEST_PLAIN]"`, dir, env)
			got := output(t, res)
			if strings.Contains(got, "super-secret-value") || strings.Contains(got, "also-secret") {
				t.Errorf("a scrubbed variable reached the child: %q", got)
			}
			if !strings.Contains(got, "key=[]") || !strings.Contains(got, "secret=[]") {
				t.Errorf("a scrubbed variable is not empty in the child: %q", got)
			}
			if !strings.Contains(got, "plain=[visible]") {
				t.Errorf("the rest of the environment was dropped: %q", got)
			}

			// A passthrough limits the environment to exactly the names
			// on it. The shell adds its own variables (PATH among them)
			// to what it inherits, so the check is about the variables
			// the session handed over.
			tool = bash.New(
				bash.WithExecutor(c.build(t)),
				bash.WithScrub(bash.Scrub{Passthrough: []string{"POO_TEST_KEEP"}}),
			)
			env = []string{"POO_TEST_KEEP=kept", "POO_TEST_OTHER=dropped", "POO_TEST_THIRD=dropped"}
			res = call(t, tool, `echo "keep=[$POO_TEST_KEEP] other=[$POO_TEST_OTHER] third=[$POO_TEST_THIRD]"`, dir, env)
			got = output(t, res)
			if !strings.Contains(got, "keep=[kept]") {
				t.Errorf("the passthrough variable is missing: %q", got)
			}
			if !strings.Contains(got, "other=[]") || !strings.Contains(got, "third=[]") {
				t.Errorf("the environment holds more than the passthrough list: %q", got)
			}
		})
	}
}

// TestTheOutputIsElided covers the plan's observation: an output over
// the cap becomes a head and a tail, and the count of the dropped
// middle is reported.
func TestTheOutputIsElided(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := &bash.Tool{ObservationCap: 200, Executor: c.build(t)}

			res := call(t, tool, "for i in $(seq 1 1000); do echo line-$i; done", dir, os.Environ())
			obs := observation(t, res)
			if _, ok := obs["output"]; ok {
				t.Fatalf("the long output was not elided: %v", obs)
			}
			head, _ := obs["output_head"].(string)
			tail, _ := obs["output_tail"].(string)
			if !strings.HasPrefix(head, "line-1\n") {
				t.Errorf("the head does not start the output: %q", head)
			}
			if !strings.HasSuffix(tail, "line-1000\n") {
				t.Errorf("the tail does not end the output: %q", tail)
			}
			if elided, _ := obs["elided_chars"].(float64); elided <= 0 {
				t.Errorf("elided_chars is %v, want a positive count", obs["elided_chars"])
			}
		})
	}
}

// TestTheShellIsFreshForEveryCall covers the plan's stateless shell: a
// change one call makes is gone by the next one.
func TestTheShellIsFreshForEveryCall(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := bash.New(bash.WithExecutor(c.build(t)))

			call(t, tool, "cd /tmp; export MARKER=set", dir, os.Environ())
			res := call(t, tool, `echo "marker=[$MARKER]"`, dir, os.Environ())
			if got := output(t, res); strings.Contains(got, "marker=[set]") {
				t.Errorf("an exported variable survived the call: %q", got)
			}
		})
	}
}

// TestPrepareRejectsUnknownFields covers the plan's strict decoding: a
// field the tool does not know is a validation error.
func TestPrepareRejectsUnknownFields(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			tool := bash.New(bash.WithExecutor(c.build(t)))
			for _, raw := range []string{
				`{"command":"ls","extra":1}`,
				`{"command":""}`,
				`{}`,
				`not json`,
			} {
				if _, err := tool.Prepare(json.RawMessage(raw), poo.PrepareEnv{}); err == nil {
					t.Errorf("Prepare accepted %s", raw)
				}
			}
		})
	}
}

// TestABackgroundJobIsStoppedByCancelJob covers the plan's job rules:
// a background call returns at once, the job's process survives the end
// of the turn and the cancellation of the run, and cancel_job leaves no
// descendant behind.
func TestABackgroundJobIsStoppedByCancelJob(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tool := bash.New(bash.WithExecutor(c.build(t)))
			tr := pootest.NewTransport(
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"` + jobCommand + `","background":true}`),
				}}},
				// The second call holds the turn open, so the run is
				// still running when the test cancels it.
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"sleep 30"}`),
				}}},
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "cancel_job",
					Arguments: json.RawMessage(`{"job":"j1"}`),
				}}},
				pootest.Turn{Text: "the job is cancelled"},
			)
			sink := pootest.NewSink()
			s, err := poo.New(poo.Options{
				Transport:  tr,
				Tools:      map[string]poo.Tool{"bash": tool},
				ToolOrder:  []string{"bash"},
				Sink:       sink,
				SessionID:  "s1",
				Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
				WorkingDir: dir,
			})
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			t.Cleanup(func() { s.Close() })

			if _, err := s.Prompt(context.Background(), "start the long job"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			// The background call returns at once, and the job runs.
			waitFor(t, "the job to start", func() bool { return resultTexts(sink, "started job j1") > 0 })
			waitFor(t, "the job's process to start", func() bool { return c.alive(t, dir) })

			// The run is cancelled while the job runs. The job keeps
			// running: a cancellation stops the run, not its jobs.
			s.Cancel("cancel:test")
			if _, err := s.Wait(context.Background()); err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			if !c.alive(t, dir) {
				t.Fatal("the job's process died with the run")
			}

			// The model cancels the job.
			if _, err := s.Prompt(context.Background(), "cancel the job"); err != nil {
				t.Fatalf("starting the second run: %v", err)
			}
			if _, err := s.Wait(context.Background()); err != nil {
				t.Fatalf("waiting for the second run: %v", err)
			}
			waitFor(t, "the job's process to die", func() bool { return !c.alive(t, dir) })
			if survivors := c.survivors(t, dir); len(survivors) > 0 {
				t.Errorf("the cancelled job left %v behind", survivors)
			}
		})
	}
}

// hostEnvTool is a tool that runs in poo's own process, and records the
// environment it was given.
type hostEnvTool struct {
	env []string
}

func (h *hostEnvTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "host",
		Description: "A tool that runs in poo's own process.",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassReadOnly,
	}
}

func (h *hostEnvTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return hostPrepared{}, nil
}

func (h *hostEnvTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	h.env = env.Env
	return poo.ToolResult{Text: "the host tool ran"}, nil
}

// hostPrepared is a validated host tool call.
type hostPrepared struct{}

func (hostPrepared) Summary() string { return "host" }

// TestAScrubbedVariableStaysVisibleToAHostTool covers the plan's
// credential rule: the scrub is the shell's, and a tool that runs in
// poo's own process still sees the environment the session holds.
func TestAScrubbedVariableStaysVisibleToAHostTool(t *testing.T) {
	for _, c := range executorCases(t) {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			bashTool := bash.New(
				bash.WithExecutor(c.build(t)),
				bash.WithScrub(bash.Scrub{Names: []string{"POO_TEST_API_KEY"}}),
			)
			host := &hostEnvTool{}
			tr := pootest.NewTransport(
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "bash",
					Arguments: json.RawMessage(`{"command":"echo \"key=[$POO_TEST_API_KEY]\""}`),
				}}},
				pootest.Turn{Calls: []pootest.Call{{
					Name:      "host",
					Arguments: json.RawMessage(`{}`),
				}}},
				pootest.Turn{Text: "done"},
			)
			s, err := poo.New(poo.Options{
				Transport:  tr,
				Tools:      map[string]poo.Tool{"bash": bashTool, "host": host},
				ToolOrder:  []string{"bash", "host"},
				Sink:       pootest.NewSink(),
				SessionID:  "s1",
				Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
				WorkingDir: dir,
				Env:        []string{"POO_TEST_API_KEY=super-secret-value", "PATH=/usr/bin"},
			})
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			t.Cleanup(func() { s.Close() })

			if _, err := s.Prompt(context.Background(), "look at the key"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			if _, err := s.Wait(context.Background()); err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}

			// The shell did not see the key.
			res := call(t, bashTool, `echo "key=[$POO_TEST_API_KEY]"`, dir, []string{"POO_TEST_API_KEY=super-secret-value"})
			if got := output(t, res); strings.Contains(got, "super-secret-value") {
				t.Errorf("the scrubbed variable reached the shell: %q", got)
			}
			// The host tool, which runs in poo's own process, still
			// sees it.
			found := false
			for _, kv := range host.env {
				if kv == "POO_TEST_API_KEY=super-secret-value" {
					found = true
				}
			}
			if !found {
				t.Errorf("the host tool's environment is %v, want the key in it", host.env)
			}
		})
	}
}

// resultTexts counts the results that hold a text.
func resultTexts(sink *pootest.Sink, text string) int {
	count := 0
	for _, rec := range sink.Records() {
		result, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if strings.Contains(result.Text, text) {
			count++
		}
	}
	return count
}

// waitFor polls a condition until it holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// localSurvivors returns the processes a command left behind.
func localSurvivors(t *testing.T, dir string) []string {
	t.Helper()
	pid, ok := localPID(t, dir)
	if !ok {
		return nil
	}
	if processAlive(pid) {
		return []string{fmt.Sprintf("pid %d", pid)}
	}
	return nil
}

// localAlive reports whether the command's process is still running.
func localAlive(t *testing.T, dir string) bool {
	t.Helper()
	pid, ok := localPID(t, dir)
	if !ok {
		return false
	}
	return processAlive(pid)
}

// localPID returns the pid the command recorded.
func localPID(t *testing.T, dir string) (int, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "pid.txt"))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("the pid file holds %q, want a positive decimal pid", raw)
	}
	return pid, true
}

// processAlive reports whether a pid names a live process.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks for existence without disturbing the process.
	return p.Signal(syscall.Signal(0)) == nil
}

// sandboxSurvivors returns the processes a command left behind.
//
// A sandboxed command runs in a pid namespace of its own, and the
// namespace names it from outside: every process in it is a survivor.
func sandboxSurvivors(t *testing.T, dir string) []string {
	t.Helper()
	ns, ok := sandboxNamespace(t, dir)
	if !ok {
		return nil
	}
	var out []string
	for _, pid := range processesInNamespace(ns) {
		out = append(out, fmt.Sprintf("pid %d in %s", pid, ns))
	}
	return out
}

// sandboxAlive reports whether the command's process is still running.
func sandboxAlive(t *testing.T, dir string) bool {
	t.Helper()
	ns, ok := sandboxNamespace(t, dir)
	if !ok {
		return false
	}
	return len(processesInNamespace(ns)) > 0
}

// sandboxNamespace returns the pid namespace the command recorded.
func sandboxNamespace(t *testing.T, dir string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "ns.txt"))
	if err != nil {
		return "", false
	}
	ns := strings.TrimSpace(string(raw))
	if ns == "" {
		return "", false
	}
	return ns, true
}

// processesInNamespace returns the pids of the processes in a pid
// namespace.
//
// The namespace is an identity, not a pattern: a process is in it when
// its namespace link is the same one.
func processesInNamespace(ns string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		link, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
		if err != nil {
			continue
		}
		if link == ns {
			pids = append(pids, pid)
		}
	}
	return pids
}
