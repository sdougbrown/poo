package bwrap

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pooexec "github.com/sdougbrown/poo/exec"
)

// newTestExecutor builds an executor without the probe: the gate tests
// exercise startup with a launcher script, which needs neither
// bubblewrap nor user namespaces, and each test names the deadline its
// own path is bounded by. The literal carries every field New sets, so
// a field added there surfaces here.
func newTestExecutor(t *testing.T, binary string, gateTimeout time.Duration) *Executor {
	t.Helper()
	opts := Options{Binary: binary}
	return &Executor{binary: binary, opts: opts, maskCache: map[string][]string{}, gateTimeout: gateTimeout}
}

// startCommand names the command the sandbox runs: a long-running one,
// so a test that kills the sandbox kills the command, and the wait
// reports the signal death rather than a command that was already gone.
func startCommand(t *testing.T) pooexec.Command {
	t.Helper()
	return pooexec.Command{Argv: []string{"/bin/sleep", "30"}, Dir: t.TempDir()}
}

// launcherScript writes a launcher that stands in for bubblewrap and
// its helper, and returns its path. The body runs where they would: the
// status descriptor the helper reports the gate on is 3, bubblewrap's
// status stream is 4, and the parent-death pipe is 5.
func launcherScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "launcher")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("writing the launcher: %v", err)
	}
	return path
}

// readyReport is the report the helper writes as its first act past the
// startup gate, marshalled the way the helper marshals it.
func readyReport(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(statusReport{Started: true})
	if err != nil {
		t.Fatalf("marshalling the ready report: %v", err)
	}
	return string(raw)
}

// TestAStartThatLosesTheLauncherFailsTheGate covers the launcher that
// dies before the gate: the ready report never comes, so Start kills
// the pre-gate process group and fails with the gate's error, and the
// failure arrives well before the deadline.
func TestAStartThatLosesTheLauncherFailsTheGate(t *testing.T) {
	exec := newTestExecutor(t, "/bin/false", 100*time.Millisecond)
	started := time.Now()
	_, err := exec.Start(context.Background(), startCommand(t), io.Discard)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("a launcher that died before the gate started a sandbox")
	}
	if !strings.Contains(err.Error(), "startup gate") {
		t.Errorf("the failure is %v, want the gate's error", err)
	}
	// The failure waits the grace for a report that never comes, so
	// the whole start is bounded by grace plus timeout.
	if elapsed > GateGrace+exec.gateTimeout {
		t.Errorf("the failed start took %v, want within %v", elapsed, GateGrace+exec.gateTimeout)
	}
}

// TestAStalledLauncherFailsOnTheDeadline covers the safety net: a
// launcher that neither reports ready nor exits holds Start until the
// deadline, and the timeout is the failure.
func TestAStalledLauncherFailsOnTheDeadline(t *testing.T) {
	exec := newTestExecutor(t, launcherScript(t, "sleep 30"), 100*time.Millisecond)
	_, err := exec.Start(context.Background(), startCommand(t), io.Discard)
	if err == nil {
		t.Fatal("a stalled launcher started a sandbox")
	}
	if !strings.Contains(err.Error(), "did not pass its startup gate") {
		t.Errorf("the failure is %v, want the deadline's error", err)
	}
}

// TestTheGatePassesWhenTheHelperReportsReady pins the primary behavior:
// Start returns because the helper's ready report arrived, well inside
// the deadline, on a host that can run the sandbox.
func TestTheGatePassesWhenTheHelperReportsReady(t *testing.T) {
	exec, err := New(Options{})
	if err != nil {
		t.Skipf("skip: the bwrap executor is unavailable: %v", err)
	}
	started := time.Now()
	p, err := exec.Start(context.Background(), startCommand(t), io.Discard)
	if err != nil {
		t.Fatalf("starting the sandbox: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= ReadyTimeout {
		t.Errorf("Start waited its whole deadline: %v", elapsed)
	}
	p.Kill()
	if _, err := p.Wait(); err == nil {
		t.Errorf("a killed sandbox's wait reports no failure")
	}
}

// TestAReadyReportPassesTheGateWithoutASandbox pins the same success
// path with no sandbox on the host at all: the launcher writes the
// helper's ready report on the status descriptor, and Start returns
// because that report arrived, not because namespaces can be created.
// The launcher stays alive, so the deadline would still fail this start
// if the report were not what released the gate.
func TestAReadyReportPassesTheGateWithoutASandbox(t *testing.T) {
	body := "printf '{\"child-pid\":%d}\\n' $$ >&4\n" +
		"printf '%s\\n' '" + readyReport(t) + "' >&3\n" +
		// The report is delivered: the status stream can end after it
		// instead of holding the sandbox's report open.
		"exec 3>&-\n" +
		"exec /bin/sleep 30"
	exec := newTestExecutor(t, launcherScript(t, body), time.Second)
	started := time.Now()
	p, err := exec.Start(context.Background(), startCommand(t), io.Discard)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("starting a launcher that reported ready: %v", err)
	}
	if elapsed >= exec.gateTimeout {
		t.Errorf("the start took %v: the ready report did not release the gate within %v", elapsed, exec.gateTimeout)
	}
	p.Kill()
	if _, err := p.Wait(); err == nil {
		t.Errorf("a killed sandbox's wait reports no failure")
	}
}

// TestALauncherThatDiesAsItReportsDoesNotFailTheGate covers the fast run
// the gate's grace exists for: the launcher writes the ready report and
// ends at once, so its status stream ends while the report is still in
// the status pipe. A delivered report must not become a gate failure.
// The status readers race for which end of the gate's wait reports
// first, and both are correct — the outcome asserted here is the one the
// grace is for.
func TestALauncherThatDiesAsItReportsDoesNotFailTheGate(t *testing.T) {
	body := "printf '%s\\n' '" + readyReport(t) + "' >&3\n" + "exec 3>&-"
	exec := newTestExecutor(t, launcherScript(t, body), time.Second)
	p, err := exec.Start(context.Background(), startCommand(t), io.Discard)
	if err != nil {
		t.Fatalf("starting a launcher whose ready report was already in the status pipe: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Errorf("a launcher that reported ready and ended is reported as %v", err)
	}
}

// TestTheContextCancelDuringTheGateTakesTheSandboxDown covers the
// context branch: a run whose owner cancels while the launcher holds
// the gate gets a sandbox, and the wait reports the kill.
func TestTheContextCancelDuringTheGateTakesTheSandboxDown(t *testing.T) {
	// The gate is a second long, the cancel fires at 50ms, and the
	// bound is 300ms: the cancel always beats the gate, and the bound
	// catches a start that ignores the cancel without failing on a
	// loaded machine's fork and scheduler delays.
	exec := newTestExecutor(t, launcherScript(t, "sleep 30"), time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	p, err := exec.Start(ctx, startCommand(t), io.Discard)
	if err != nil {
		t.Fatalf("starting the sandbox: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 300*time.Millisecond {
		t.Errorf("Start waited past the cancel: %v", elapsed)
	}
	p.Kill()
	if code, err := p.Wait(); err == nil {
		t.Errorf("a killed sandbox's wait reports no failure: code=%d", code)
	}
}
