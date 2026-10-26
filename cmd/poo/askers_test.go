package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The serve command's chain is the one the settings name. An absent
// list is the binary's own chain, which asks the wire, and an explicit
// empty list asks nobody. The load-time warning is how the two show: a
// policy that can ask, with no asker that can ever answer, names the
// fallback.

// TestTheServeChainIsTheBinarysDefault covers the default: settings
// that name no chain keep the wire the binary offers.
func TestTheServeChainIsTheBinarysDefault(t *testing.T) {
	code, stderr := runServeWithPermission(t, "permission:\n  unmatched: ask\n")
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if strings.Contains(stderr, "falls back") {
		t.Errorf("stderr is %q, want no warning: the wire asker can answer", stderr)
	}
}

// TestAnEmptyChainAsksNobodyInServe covers the explicit empty list: the
// settings' chain replaces the binary's, and no asker is left.
func TestAnEmptyChainAsksNobodyInServe(t *testing.T) {
	code, stderr := runServeWithPermission(t, "permission:\n  unmatched: ask\n  askers: []\n")
	if code != 0 {
		t.Fatalf("the exit code is %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "falls back") {
		t.Errorf("stderr is %q, want the warning about the fallback", stderr)
	}
}

// runServeWithPermission runs `poo serve` with a settings document and
// returns its exit code and stderr.
//
// The server's input is empty, so it reads one end of file and returns
// without serving a session.
func runServeWithPermission(t *testing.T, permission string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"+permission), 0o600); err != nil {
		t.Fatalf("writing the settings: %v", err)
	}
	withEmptyStdin(t)
	return runCLI(t,
		"serve",
		"--settings", path,
		"--endpoint", "http://127.0.0.1:1/v1",
		"--model-id", "test-model",
		"--context-limit", "8192",
		"--ephemeral",
	)
}

// withEmptyStdin replaces the process stdin with the null device for
// the test's duration, so a CLI that reads stdin reads nothing.
func withEmptyStdin(t *testing.T) {
	t.Helper()
	stdin := os.Stdin
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening the empty input: %v", err)
	}
	os.Stdin = null
	t.Cleanup(func() {
		os.Stdin = stdin
		null.Close()
	})
}
