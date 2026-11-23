package config_test

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/pootest"
)

// TestMain points the state root and the user config directory at
// directories of the tests' own: building a catalog reads the discovery
// cache under the state root, and the manifest defaults resolve under
// the user config directory, so neither a developer's own discovery.json
// nor a developer's own manifests may merge into a test's build.
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
	if err := pootest.IsolateUserConfigDir(dir); err != nil {
		println("config tests: pointing the user config directory at the test directory:", err.Error())
		os.Exit(1)
	}
	os.Exit(m.Run())
}
