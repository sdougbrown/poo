package conformance

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The marker protocol is the steer and follow-up flows' ordering guard:
// the tool polls for the marker and writes a receipt once it has seen
// it, and the flow waits for that receipt. The tests run the command the
// way the bash tool does, so the receipt they check comes from a shell.

// TestTheMarkerCommandAcknowledgesAMarkerItWaitsFor covers the tool's
// half: the command waits for a marker that is not there yet, sees it,
// and writes the receipt the flow waits for.
func TestTheMarkerCommandAcknowledgesAMarkerItWaitsFor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a directory with spaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("making the working directory: %v", err)
	}
	marker := filepath.Join(dir, "steer.marker")
	cmd := exec.Command("/bin/bash", "-c", markerCommandSteps(marker, markerWaitSteps))
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the marker command: %v", err)
	}
	// The marker does not exist until here, and the command writes the
	// receipt only for a marker it found, so the receipt proves the
	// command observed the marker rather than assumed it.
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the marker command did not end after the marker arrived: %v", err)
	}
	if _, err := os.Stat(markerReceipt(marker)); err != nil {
		t.Fatalf("the marker command did not acknowledge the marker: %v", err)
	}
	// The flow's wait accepts the acknowledgement the tool wrote.
	if err := awaitMarkerReceipt(marker, 5*time.Second); err != nil {
		t.Fatalf("the flow's wait rejected the tool's acknowledgement: %v", err)
	}
}

// TestTheMarkerCommandGivesUpWithoutAReceipt covers the timeout: a
// marker that never appears leaves no acknowledgement behind, so a wait
// that ran out cannot pass for an observation.
func TestTheMarkerCommandGivesUpWithoutAReceipt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "follow-up.marker")
	// Two steps are enough to watch the wait end without waiting the
	// flow's ten seconds.
	err := exec.Command("/bin/bash", "-c", markerCommandSteps(marker, 2)).Run()
	if err == nil {
		t.Fatal("the marker command ended successfully without a marker")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("the marker command ended %v, want a non-zero exit", err)
	}
	if _, statErr := os.Stat(markerReceipt(marker)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("the expired wait wrote a receipt: %v", statErr)
	}
}

// TestAMarkerTheToolNeverSeesDoesNotSatisfyTheFlow covers the flow's
// half: the marker the flow wrote on the host is not the proof. A tool
// that never saw it, because its sandbox looks elsewhere, writes no
// receipt and the wait fails.
func TestAMarkerTheToolNeverSeesDoesNotSatisfyTheFlow(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "steer.marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}
	if err := awaitMarkerReceipt(marker, 50*time.Millisecond); err == nil {
		t.Fatal("the wait accepted the flow's own marker as the tool's observation")
	}
}

// TestAReceiptTheFilesystemCannotReportFailsFast covers a broken path: when
// the receipt cannot be stat'ed at all, the wait reports that filesystem
// error instead of running out and blaming the marker ordering.
func TestAReceiptTheFilesystemCannotReportFailsFast(t *testing.T) {
	// The receipt's parent is a regular file, so every check fails with
	// "not a directory" rather than "not there yet".
	parent := filepath.Join(t.TempDir(), "not a directory")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatalf("writing the file that blocks the receipt path: %v", err)
	}
	start := time.Now()
	err := awaitMarkerReceipt(filepath.Join(parent, "steer.marker"), 5*time.Second)
	if err == nil {
		t.Fatal("the wait accepted a receipt the filesystem cannot report")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("the wait reported %v, want the filesystem error", err)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("the wait ran out after %v instead of reporting the filesystem error", elapsed)
	}
}

// TestAStaleReceiptDoesNotAcknowledgeThisRun covers the other direction:
// a receipt that was there before the run is cleared, so it cannot pass
// for the tool of this run.
func TestAStaleReceiptDoesNotAcknowledgeThisRun(t *testing.T) {
	s := &suite{t: t}
	marker := filepath.Join(t.TempDir(), "steer.marker")
	if err := os.WriteFile(markerReceipt(marker), []byte("seen"), 0o600); err != nil {
		t.Fatalf("writing the stale receipt: %v", err)
	}
	s.resetMarker(marker)
	if err := awaitMarkerReceipt(marker, 50*time.Millisecond); err == nil {
		t.Fatal("a stale receipt passed for this run's acknowledgement")
	}
}
