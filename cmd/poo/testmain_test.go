package main

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain runs the tests under a settings file of the tests' own, so a
// run a test starts cannot discover the operator's settings, and
// removes the manifest overrides inherited from the shell.
func TestMain(m *testing.M) {
	os.Unsetenv("POO_FEEDS_PATH")
	os.Unsetenv("POO_CLI_REGISTRY_PATH")
	os.Exit(pootest.Main(m))
}
