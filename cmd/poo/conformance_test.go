package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
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
// the plan's table runs against it: the command links every tool the
// flows script, so nothing skips.
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
	report := conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve", "--executor", executor},
		Profile: "code",
	})
	if len(report.Skipped) > 0 {
		t.Errorf("the suite skipped %d flows, want every flow: %v", len(report.Skipped), report.Skipped)
	}
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
	return build(t, ".", tags)
}

// build builds a command and returns the path of the binary.
func build(t *testing.T, pkg string, tags []string) string {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Base(pkg)
	if pkg == "." {
		name = "poo"
	}
	binary := filepath.Join(dir, name)
	args := []string{"build", "-o", binary}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, pkg)
	cmd := exec.Command("go", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building %s: %v\n%s", pkg, err, out)
	}
	return binary
}

// TestTheBinarySpeaksTheProtocolWithTheSearchProfile runs the suite
// against a profile with other tools: the flows that need a
// side-effecting tool skip, and the rest prove the protocol.
func TestTheBinarySpeaksTheProtocolWithTheSearchProfile(t *testing.T) {
	binary := buildTheBinary(t, nil)
	conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve"},
		Profile: "search",
	})
}

// TestAReadOnlyBinarySpeaksTheProtocol runs the suite against a binary
// that composes poo the way a custom binary does: the exported factory,
// the search profile, and read and grep alone, with no shell.
//
// The flows that script a tool the binary does not offer skip and say
// so; the others prove the protocol against the read-only build. The
// binary reads its identity from the environment, so the target sets it.
func TestAReadOnlyBinarySpeaksTheProtocol(t *testing.T) {
	binary := build(t, filepath.Join("..", "..", "wire", "conformance", "testdata", "readonly"), nil)
	report := conformance.Run(t, conformance.Target{
		Binary:  binary,
		Args:    []string{"serve"},
		Profile: "search",
		Env:     []string{"POO_READONLY_ID=conformance"},
	})

	want := []string{
		"permission gate",
		"steer",
		"interrupt",
		"follow-up message",
		"typed result",
		"handoff",
		"kill",
		"jobs",
		"attach and scrollback",
		"resume",
		"teardown",
	}
	var skipped []string
	for _, skip := range report.Skipped {
		skipped = append(skipped, skip.Flow)
		if skip.Reason == "" {
			t.Errorf("the skipped flow %q says no reason", skip.Flow)
		}
	}
	slices.Sort(skipped)
	sort.Strings(want)
	if !slices.Equal(skipped, want) {
		t.Errorf("the suite skipped %v, want %v", skipped, want)
	}
	if len(report.Run) == 0 {
		t.Error("the suite ran no flow against the read-only binary")
	}
}
