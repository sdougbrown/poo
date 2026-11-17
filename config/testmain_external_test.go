package config_test

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain points the state root at a directory of the tests' own: building a
// catalog reads the discovery cache under the state root, and a developer's own
// discovery.json must not merge into a test's catalog.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "poo-test-state-")
	if err != nil {
		println("config tests: making the state directory:", err.Error())
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	if err := pootest.IsolateStateRoot(dir); err != nil {
		println("config tests: pointing the state root at the test directory:", err.Error())
		os.Exit(1)
	}
	os.Exit(m.Run())
}
