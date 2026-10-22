package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/exec/bwrap"
	"github.com/sdougbrown/poo/wire/conformance"
)

// The conformance suite: the proof that the command speaks the public
// wire protocol.

// TestTheBinarySpeaksTheProtocol runs the suite against the command
// this package builds.
//
// The suite drives the binary over stdin and stdout, and every flow of
// the plan's table runs against it.
func TestTheBinarySpeaksTheProtocol(t *testing.T) {
	executor := os.Getenv("POO_CONFORMANCE_EXECUTOR")
	if executor == "" {
		executor = "local"
	}
	if executor == "bwrap" {
		if _, err := bwrap.New(bwrap.Options{}); err != nil {
			t.Skipf("skip: the bwrap executor is unavailable: %v", err)
		}
	}
	binary := buildTheBinary(t, nil)
	conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve", "--executor", executor},
		Profile: "code",
	})
}

// TestTheBinaryWithTheExtensionSpeaksTheProtocol runs the suite
// against the command built with the extension.
//
// The extension announces a capability, adds an optional field to a
// result and sends an event type the contract does not have. The suite
// follows the plan's forward-compatibility rule, so it passes with all
// three there.
func TestTheBinaryWithTheExtensionSpeaksTheProtocol(t *testing.T) {
	binary := buildTheBinary(t, []string{"conformanceext"})
	conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve"},
		Profile: "code",
	})
}

// buildTheBinary builds the command and returns the path of the
// binary.
//
// The suite drives a binary, not a function: the binary is the thing
// a client runs, so it is the thing the suite proves.
func buildTheBinary(t *testing.T, tags []string) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "poo")
	args := []string{"build", "-o", binary}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building the binary: %v\n%s", err, out)
	}
	return binary
}
