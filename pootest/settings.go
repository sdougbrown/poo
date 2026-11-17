package pootest

import (
	"os"
	"path/filepath"
	"testing"
)

// settingsFile is the settings document the tests run under: the
// smallest one that parses, so discovery finds no model and no asker a
// test binary cannot serve.
const settingsFile = "version: 1\n"

// settingsEnv is the variable the settings discovery reads a file from.
//
// It spells the name config.EnvSettings holds rather than importing it:
// the session package's tests reach this package, and this package
// reaching config would close a cycle through the session package.
// settings_test.go holds the two names together.
const settingsEnv = "POO_SETTINGS"

// manifestOverrides are the environment variables a shell may carry from
// an operator's own session, naming manifests or credentials this
// package's tests do not have. They are the ones config.EnvOverrides
// reads, which this package cannot import: settings_test.go holds the
// two lists together.
var manifestOverrides = []string{"POO_FEEDS_PATH", "POO_CLI_REGISTRY_PATH", "POO_CREDENTIAL_ENV", "POO_SKILLS_PATH"}

// ManifestOverrides returns the environment variables Main removes, in
// the order it removes them.
//
// A consumer whose own TestMain scrubs by hand needs the same list, and
// one that reads it here cannot fall behind it.
func ManifestOverrides() []string {
	return append([]string(nil), manifestOverrides...)
}

// StateRootEnv is the variable a test points at a directory of its own.
//
// It spells the name provider/catalog holds in catalog.StateRootEnv rather than importing
// it: this package is reached by the session package's tests, and the catalog reaches the
// session package. settings_test.go holds the two names together, so a rename on either
// side fails there instead of leaving a test binary reading a developer's own discovery
// cache.
const StateRootEnv = "XDG_STATE_HOME"

// IsolateStateRoot points the state root at dir.
//
// The commands read and write the discovery cache under the state root, and the build
// resolves the root before any other directory: without this, a developer's own
// discovery.json merges into a test's catalog and a test's refresh overwrites it.
//
// It is the one place a package's TestMain asks for that, so the rule cannot drift
// between the binaries that need it.
func IsolateStateRoot(dir string) error {
	return os.Setenv(StateRootEnv, dir)
}

// Main runs a package's tests under a settings file of the tests' own,
// and returns the code TestMain passes to os.Exit.
//
// Discovery finds the file through settingsEnv, which is set
// rather than unset: an empty value is no value to the discovery, and
// FindSettings walks past it to the project's .poo directory and then
// the user's config directory, whose settings a test cannot serve. A
// test that names its own file overrides the variable with t.Setenv and
// is unaffected. The manifest overrides inherited from the shell go
// with it.
//
// The file lives in a directory of the process's own, so packages
// running at the same time never write over one another, and the
// directory is removed once the tests finish.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "poo-test-settings-")
	if err != nil {
		println("poo tests: making the settings directory:", err.Error())
		return 1
	}
	defer os.RemoveAll(dir)
	// The commands read and write the discovery cache under the state root, and a
	// build resolves the state root before the config root: a developer's own
	// discovery.json must not merge into a test's catalog, and a test's refresh
	// must not overwrite it.
	if err := IsolateStateRoot(dir); err != nil {
		println("poo tests: pointing the state root at the test directory:", err.Error())
		return 1
	}
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(settingsFile), 0o644); err != nil {
		println("poo tests: writing the test settings:", err.Error())
		return 1
	}
	if err := os.Setenv(settingsEnv, path); err != nil {
		println("poo tests: pointing the settings discovery at the test settings:", err.Error())
		return 1
	}
	for _, name := range manifestOverrides {
		os.Unsetenv(name)
	}
	return m.Run()
}
