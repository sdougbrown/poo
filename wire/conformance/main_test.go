package conformance_test

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain runs the tests under a settings file of the tests' own, and
// removes the manifest overrides inherited from the shell.
func TestMain(m *testing.M) {
	os.Exit(pootest.Main(m))
}
