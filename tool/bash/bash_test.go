package bash_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/bash"
)

// call runs a command through the tool and returns the result.
func call(t *testing.T, tool *bash.Tool, command string, env poo.ToolEnv) poo.ToolResult {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshalling args: %v", err)
	}
	prepared, err := tool.Prepare(raw, poo.PrepareEnv{WorkingDir: env.WorkingDir})
	if err != nil {
		t.Fatalf("preparing %q: %v", command, err)
	}
	res, err := tool.Call(context.Background(), prepared, env)
	if err != nil {
		t.Fatalf("calling %q: %v", command, err)
	}
	return res
}

// structured decodes the result's structured payload.
func structured(t *testing.T, res poo.ToolResult) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Structured, &out); err != nil {
		t.Fatalf("decoding the structured payload %q: %v", res.Structured, err)
	}
	return out
}

func TestOutputLargerThanTheCapIsElidedHeadAndTail(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{ObservationCap: 200}

	// 1000 numbered lines, so the head and the tail are both recognisable.
	res := call(t, tool, "for i in $(seq 1 1000); do echo line-$i; done", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})

	obs := structured(t, res)
	if _, ok := obs["output"]; ok {
		t.Fatalf("the long output was not elided: %v", obs)
	}
	head, _ := obs["output_head"].(string)
	tail, _ := obs["output_tail"].(string)
	if !strings.HasPrefix(head, "line-1\n") {
		t.Errorf("the head does not start the output: %q", head[:min(40, len(head))])
	}
	if !strings.HasSuffix(tail, "line-1000\n") {
		t.Errorf("the tail does not end the output: %q", tail[max(0, len(tail)-40):])
	}
	if len(head) > 100 || len(tail) > 100 {
		t.Errorf("the head and tail are each half the cap, got %d and %d", len(head), len(tail))
	}
	elided, _ := obs["elided_chars"].(float64)
	if elided <= 0 {
		t.Errorf("elided_chars is %v, want a positive count", elided)
	}
	if hint, _ := obs["hint"].(string); hint == "" {
		t.Error("the elided observation has no hint")
	}
}

func TestATimeoutReturnsPartialOutputWithTimedOut(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{Timeout: 300 * time.Millisecond}

	res := call(t, tool, "echo before-the-sleep; sleep 30; echo after-the-sleep", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})

	obs := structured(t, res)
	timedOut, _ := obs["timed_out"].(bool)
	if !timedOut {
		t.Errorf("timed_out is %v, want true", obs["timed_out"])
	}
	output, _ := obs["output"].(string)
	if !strings.Contains(output, "before-the-sleep") {
		t.Errorf("the partial output is missing: %q", output)
	}
	if strings.Contains(output, "after-the-sleep") {
		t.Errorf("the output holds text the command never printed: %q", output)
	}
	// A killed process has no exit code.
	if rc, ok := obs["returncode"]; ok && rc != nil {
		t.Errorf("returncode is %v, want null for a killed process", rc)
	}
}

func TestABackgroundJobDoesNotSurviveTheCall(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{Timeout: 5 * time.Second}

	// The shell prints the pid and exits. The child must not outlive the
	// call.
	res := call(t, tool, "sleep 300 & echo $! > pid.txt; echo started", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	obs := structured(t, res)
	if output, _ := obs["output"].(string); !strings.Contains(output, "started") {
		t.Fatalf("the command did not run: %v", obs)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "pid.txt"))
	if err != nil {
		t.Fatalf("reading the pid: %v", err)
	}
	pid := strings.TrimSpace(string(raw))

	// The call kills the group, so give it a moment to take effect.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the background process %s survived the call", pid)
}

// processAlive reports whether a pid names a live process.
func processAlive(pid string) bool {
	p, err := os.FindProcess(atoi(pid))
	if err != nil {
		return false
	}
	// Signal 0 checks for existence without disturbing the process.
	return p.Signal(syscall.Signal(0)) == nil
}

// atoi parses a decimal number, returning 0 on failure.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func TestAHugeOutputCommandDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{Timeout: 30 * time.Second}

	// A runaway producer writes far more than the capture cap. The call
	// must still return, and the captured output must be capped.
	start := time.Now()
	res := call(t, tool, "yes abcdefghijklmnopqrstuvwxyz | head -c 20000000", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})
	elapsed := time.Since(start)

	if elapsed > 20*time.Second {
		t.Errorf("the call took %v, want it to return promptly", elapsed)
	}
	obs := structured(t, res)
	if rc, _ := obs["returncode"].(float64); rc != 0 {
		t.Errorf("returncode is %v, want 0", obs["returncode"])
	}
	// The capture cap is 1 MiB, so the elided observation holds at most
	// two halves of the observation cap.
	total := len(obs["output_head"].(string)) + len(obs["output_tail"].(string))
	if total > 2*bash.ObservationCapMax {
		t.Errorf("the captured output is %d chars, want at most %d", total, 2*bash.ObservationCapMax)
	}
	// The producer really did write more than the capture cap, so the
	// elision is exercised rather than skipped.
	if elided, _ := obs["elided_chars"].(float64); elided <= 0 {
		t.Errorf("elided_chars is %v, want a positive count", obs["elided_chars"])
	}
	// The captured output filled the cap, so the elided middle is at
	// least the capture cap minus the two halves of the observation.
	if elided, _ := obs["elided_chars"].(float64); elided < bash.CaptureCap-2*bash.ObservationCapMax {
		t.Errorf("elided_chars is %v, want the capture cap to have been reached", obs["elided_chars"])
	}
}

func TestTheAPIKeyVariableIsScrubbed(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{Timeout: 5 * time.Second}

	env := append(os.Environ(), "POO_TEST_API_KEY=super-secret-value")
	env = bash.ChildEnv(env, []string{"POO_TEST_API_KEY"})

	res := call(t, tool, "echo \"key=[${POO_TEST_API_KEY}]\"; echo \"path=[$PATH]\"", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        env,
	})

	obs := structured(t, res)
	output, _ := obs["output"].(string)
	if strings.Contains(output, "super-secret-value") {
		t.Errorf("the scrubbed variable reached the child: %q", output)
	}
	if !strings.Contains(output, "key=[]") {
		t.Errorf("the scrubbed variable is not empty in the child: %q", output)
	}
	// The rest of the environment survives.
	if !strings.Contains(output, "path=[") || !strings.Contains(output, "/") {
		t.Errorf("the rest of the environment was dropped: %q", output)
	}
}

func TestPrepareRejectsUnknownFieldsAndEmptyCommands(t *testing.T) {
	tool := bash.New()
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
}

func TestTheEnvelopeNamesTheExitCode(t *testing.T) {
	dir := t.TempDir()
	tool := &bash.Tool{Timeout: 5 * time.Second}

	res := call(t, tool, "echo hello; exit 3", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: dir},
		Env:        os.Environ(),
	})

	// The model-facing text is the same envelope, and it names the exit
	// code as a number.
	obs := structured(t, res)
	if rc, ok := obs["returncode"].(float64); !ok || rc != 3 {
		t.Errorf("the structured returncode is %v, want 3", obs["returncode"])
	}
	if !strings.Contains(res.Text, `"returncode": 3`) {
		t.Errorf("the observation is %q, want it to name the exit code", res.Text)
	}
	// The observation is valid JSON, so the model can read it either way.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(res.Text), &decoded); err != nil {
		t.Errorf("the observation is not valid JSON: %v; %q", err, res.Text)
	}
}

func TestTheCommandRunsInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := bash.New()

	res := call(t, tool, "pwd", poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}, Env: os.Environ()})
	obs := structured(t, res)
	output, _ := obs["output"].(string)
	got := strings.TrimSpace(output)
	// macOS reports a symlinked temporary directory, so compare the
	// resolved paths.
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Errorf("the command ran in %q, want %q", got, want)
	}
}

func TestTheShellIsFreshForEveryCall(t *testing.T) {
	dir := t.TempDir()
	tool := bash.New()

	call(t, tool, "cd /tmp; export MARKER=set", poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}, Env: os.Environ()})
	res := call(t, tool, "echo \"marker=[$MARKER]\"; pwd", poo.ToolEnv{PrepareEnv: poo.PrepareEnv{WorkingDir: dir}, Env: os.Environ()})

	obs := structured(t, res)
	output, _ := obs["output"].(string)
	if strings.Contains(output, "marker=[set]") {
		t.Errorf("an exported variable survived the call: %q", output)
	}
}

func TestObservationCapIsContextRelative(t *testing.T) {
	// The plan: min(30000, floor(0.06 × context_limit × 3.5)).
	if got := bash.ObservationCap(32768); got != 6881 {
		t.Errorf("ObservationCap(32768) is %d, want 6881", got)
	}
	if got := bash.ObservationCap(262144); got != 30000 {
		t.Errorf("ObservationCap(262144) is %d, want 30000", got)
	}
}

func TestAProcessKilledBySignalIsAnError(t *testing.T) {
	tool := &bash.Tool{Timeout: 5 * time.Second}

	res := call(t, tool, "echo about-to-die; kill -9 $$", poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: t.TempDir()},
		Env:        os.Environ(),
	})

	obs := structured(t, res)
	if rc, ok := obs["returncode"]; ok && rc != nil {
		t.Fatalf("returncode is %v, want null for a process killed by a signal", rc)
	}
	if !res.IsError {
		t.Error("a process killed by a signal is not marked as an error")
	}
}
