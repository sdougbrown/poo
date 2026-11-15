package main

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain runs the tests under a settings file of the tests' own, so a
// run a test starts cannot discover the operator's settings.
func TestMain(m *testing.M) {
	os.Exit(pootest.Main(m))
}
