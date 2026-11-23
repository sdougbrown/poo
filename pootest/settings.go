package pootest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// userConfigVars are the environment variables the user config directory
// resolves from, one per platform: XDG_CONFIG_HOME on Linux, HOME on
// darwin, APPDATA on windows. A test binary that points all three at the
// same directory isolates the config root on every one of them.
var userConfigVars = []string{"XDG_CONFIG_HOME", "HOME", "APPDATA"}

// IsolateUserConfigDirT is IsolateUserConfigDir for one test: the
// variables restore when the test ends, and a platform drift fails the
// test instead of leaking the developer's own manifests into it.
func IsolateUserConfigDirT(t *testing.T, dir string) {
	t.Helper()
	before := make(map[string]*string, len(userConfigVars))
	for _, name := range userConfigVars {
		if value, set := os.LookupEnv(name); set {
			copied := value
			before[name] = &copied
		}
	}
	t.Cleanup(func() {
		for name, value := range before {
			if value == nil {
				os.Unsetenv(name)
				continue
			}
			os.Setenv(name, *value)
		}
	})
	if err := IsolateUserConfigDir(dir); err != nil {
		t.Fatal(err)
	}
}

// IsolateUserConfigDir points the user config directory at dir.
//
// The manifest defaults resolve under the user config directory, and the
// settings discovery searches it as a fallback: without this, a
// developer's own manifests merge into a test's build, and a test's
// build fails on the machine of anyone who uses the feature. It is the
// one place a package's TestMain asks for that, so the rule cannot
// drift between the binaries that need it.
func IsolateUserConfigDir(dir string) error {
	for _, name := range userConfigVars {
		if err := os.Setenv(name, dir); err != nil {
			return err
		}
	}
	// The variables are the platform's own, and a Go release that reads
	// a different one would leave the developer's own config in the
	// tests' view. The resolved directory is checked against dir, so
	// such a drift fails the suite loudly instead of leaking silently.
	resolved, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if resolved != dir && !strings.HasPrefix(resolved, dir+string(filepath.Separator)) {
		return fmt.Errorf("the user config directory resolved to %s, outside the test's %s", resolved, dir)
	}
	return nil
}

// ServeSubcommand is the subcommand the repl starts its server as:
// os.Executable() ServeSubcommand. The guard in Main compares
// os.Args[1] against it, so a rename on either side fails a test
// instead of silently disabling the guard.
const ServeSubcommand = "serve"

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
//
// The user config directory points at the same directory, so the
// manifest defaults and the discovery fallback read nothing of the
// developer's own.
//
// A test binary started as ServeSubcommand is refused before anything
// else: the repl starts its server as os.Executable() ServeSubcommand,
// which under go test is the test binary, so a test that reached the
// real server launch would re-run the suite recursively.
func Main(m *testing.M) int {
	if len(os.Args) > 1 && os.Args[1] == ServeSubcommand {
		fmt.Fprintf(os.Stderr, "poo tests: the test binary was started as %q: a test reached the real server launch instead of refusing first\n", ServeSubcommand)
		return 2
	}
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
	if err := IsolateUserConfigDir(dir); err != nil {
		println("poo tests: pointing the user config directory at the test directory:", err.Error())
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
