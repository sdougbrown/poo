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

// Main runs a package's tests under a settings file of the tests' own,
// and returns the code TestMain passes to os.Exit.
//
// Discovery finds the file through POO_SETTINGS, which is set rather
// than unset: an empty value is no value to the discovery, and
// FindSettings walks past it to the project's .poo directory and then
// the user's config directory, whose settings a test cannot serve. A
// test that names its own file overrides the variable with t.Setenv and
// is unaffected.
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
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(settingsFile), 0o644); err != nil {
		println("poo tests: writing the test settings:", err.Error())
		return 1
	}
	if err := os.Setenv("POO_SETTINGS", path); err != nil {
		println("poo tests: pointing POO_SETTINGS at the test settings:", err.Error())
		return 1
	}
	return m.Run()
}
