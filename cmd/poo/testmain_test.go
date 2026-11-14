package main

import (
	"os"
	"path/filepath"
	"testing"
)

// testSettings is the settings file the tests run under: the smallest
// file that parses, so discovery — the project's .poo, then the user's
// config directory — cannot hand a run the operator's own settings,
// whose models and askers a test binary cannot serve.
const testSettings = "version: 1\n"

// TestMain points POO_SETTINGS at the test settings and removes the
// manifest overrides inherited from the shell.
//
// The variable is set, not unset, because an empty value is no value to
// the discovery: FindSettings reads an empty POO_SETTINGS as absent and
// walks on to the user's config directory, which is the leak this
// closes. A test that names its own file overrides the variable with
// t.Setenv and is unaffected.
func TestMain(m *testing.M) {
	path := filepath.Join(os.TempDir(), "poo-test-settings.yaml")
	if err := os.WriteFile(path, []byte(testSettings), 0o644); err != nil {
		println("poo tests: writing the test settings:", err.Error())
		os.Exit(1)
	}
	os.Setenv("POO_SETTINGS", path)
	os.Unsetenv("POO_FEEDS_PATH")
	os.Unsetenv("POO_CLI_REGISTRY_PATH")
	os.Exit(m.Run())
}
