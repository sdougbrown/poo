package pootest_test

import (
	"os"
	"slices"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/provider/catalog"
)

// TestMain runs the tests the way a package under this helper does,
// with the manifest overrides an operator's shell may carry already in
// the environment, so the helper's scrub has something to remove.
func TestMain(m *testing.M) {
	for _, name := range pootest.ManifestOverrides() {
		os.Setenv(name, "inherited-from-the-shell")
	}
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

// TestTheManifestScrubCoversWhatTheSettingsRead covers the coupling: a
// name config.EnvOverrides learns and this package does not scrub would
// leave the operator's manifests and credentials in the suite's
// environment.
func TestTheManifestScrubCoversWhatTheSettingsRead(t *testing.T) {
	if got, want := pootest.ManifestOverrides(), config.EnvOverrideNames; !slices.Equal(got, want) {
		t.Errorf("the suite scrubs %v, and the settings read %v", got, want)
	}
}

// TestTheManifestOverridesDoNotReachTheSuite covers Main's scrub: the
// shell's overrides are gone by the time the tests run, so a package
// under this helper cannot read a manifest the operator named.
func TestTheManifestOverridesDoNotReachTheSuite(t *testing.T) {
	for _, name := range pootest.ManifestOverrides() {
		if value, ok := os.LookupEnv(name); ok {
			t.Errorf("the suite carries %s=%s from the shell", name, value)
		}
	}
}

// TestTheStateRootNameIsTheCatalogs covers the name two test binaries set by hand: this
// package spells it, provider/catalog reads it, and a rename on either side would leave
// every test binary that isolates the root pointing at a variable nothing reads — so the
// tests would quietly run against a developer's own discovery cache.
func TestTheStateRootNameIsTheCatalogs(t *testing.T) {
	if pootest.StateRootEnv != catalog.StateRootEnv {
		t.Errorf("the state root is %q here and %q in the catalog", pootest.StateRootEnv, catalog.StateRootEnv)
	}
}
