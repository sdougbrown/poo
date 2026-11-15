package pootest_test

import (
	"os"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
)

// TestMain runs the tests the way a package under this helper does.
func TestMain(m *testing.M) {
	os.Exit(pootest.Main(m))
}

// TestTheSuiteRunsUnderItsOwnSettings covers the helper: discovery
// resolves to the file Main wrote, and the file parses to a settings
// document with no model and no asker. A renamed constant, a body that
// stops parsing, or a variable Main never set would otherwise leave
// every suite reading the operator's settings with nothing failing.
func TestTheSuiteRunsUnderItsOwnSettings(t *testing.T) {
	if os.Getenv(config.EnvSettings) == "" {
		t.Fatalf("the suite runs with %s unset, so discovery reads the operator's settings", config.EnvSettings)
	}
	path, err := config.FindSettings("", t.TempDir())
	if err != nil {
		t.Fatalf("the discovery failed: %v", err)
	}
	if path == "" {
		t.Fatal("the discovery found no settings file")
	}
	if path != os.Getenv(config.EnvSettings) {
		t.Errorf("the discovery found %s, not the file the helper wrote: %s", path, os.Getenv(config.EnvSettings))
	}
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("the suite's settings do not parse: %v", err)
	}
	if chats := settings.Roles["chat"]; chats != "" {
		t.Errorf("the suite's settings name a chat model: %q", chats)
	}
	if askers := settings.Permission.Askers; len(askers) > 0 {
		t.Errorf("the suite's settings name askers: %v", askers)
	}
}
