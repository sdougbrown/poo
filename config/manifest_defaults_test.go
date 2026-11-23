package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theDefaultsPooDir isolates the user's config directory for a test and
// returns poo's directory under it. Every default directory the test
// writes goes below the one this returns.
func theDefaultsPooDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv(config.EnvSettings, "")
	root, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("the user config directory is not resolvable here: %v", err)
	}
	if !strings.HasPrefix(root, dir) {
		t.Skipf("this platform's user config directory is outside the test's directory: %s", root)
	}
	return filepath.Join(root, "poo")
}

// theDefaultFeedManifest writes one feed into the default feeds
// directory and returns the directory.
func theDefaultFeedManifest(t *testing.T, pooDir string) string {
	t.Helper()
	dir := filepath.Join(pooDir, "feeds")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("making the default feeds directory: %v", err)
	}
	body := `{"version": 1, "feeds": {"broker": {"description": "Messages from the broker.", "command": ["true"], "on_idle": "wake"}}}`
	if err := os.WriteFile(filepath.Join(dir, "feeds.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return dir
}

// theDefaultRegistry writes one CLI into the default cli-registry
// directory and returns the directory.
func theDefaultRegistry(t *testing.T, pooDir string) string {
	t.Helper()
	dir := filepath.Join(pooDir, "cli-registry")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("making the default registry directory: %v", err)
	}
	doc := `{"version": 1, "tools": {"ticker": {"description": "The ticker command.", "command": ["true"], "inputSchema": {"type": "object", "properties": {}, "additionalProperties": false}}}}`
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	return dir
}

// TestTheDefaultManifestDirectoriesLoad covers the defaults: manifests
// in poo's own feeds and cli-registry directories load beside the
// settings' paths, and a settings file that names neither loads them
// alone.
func TestTheDefaultManifestDirectoriesLoad(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	feedsDir := theDefaultFeedManifest(t, pooDir)
	registryDir := theDefaultRegistry(t, pooDir)
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Feeds == nil || runtime.Feeds.Len() != 1 || !runtime.Feeds.Has("broker") {
		t.Errorf("the default feeds directory loaded %v, want the one feed", runtime.Feeds)
	}
	if runtime.Registry == nil || runtime.Registry.Len() != 1 {
		t.Errorf("the default registry directory loaded %v, want the one entry", runtime.Registry)
	}
	if _, err := os.Stat(filepath.Join(feedsDir, "feeds.json")); err != nil {
		t.Errorf("the default feed manifest is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(registryDir, "registry.json")); err != nil {
		t.Errorf("the default registry manifest is gone: %v", err)
	}
}

// TestAMissingDefaultManifestDirectoryIsNoError covers the quiet case: a
// person with no default directories sees no change, and the build does
// not warn about directories that were never offered.
func TestAMissingDefaultManifestDirectoryIsNoError(t *testing.T) {
	theDefaultsPooDir(t)
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Feeds != nil && runtime.Feeds.Len() != 0 {
		t.Errorf("the build loaded feeds %v, want none", runtime.Feeds)
	}
	if runtime.Registry != nil && runtime.Registry.Len() != 0 {
		t.Errorf("the build loaded a registry %v, want none", runtime.Registry)
	}
}

// TestTheSettingsTurnTheManifestDefaultsOff covers the switch: a
// settings file that says the defaults are off runs with exactly the
// manifests it names.
func TestTheSettingsTurnTheManifestDefaultsOff(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	theDefaultFeedManifest(t, pooDir)
	theDefaultRegistry(t, pooDir)
	document := theModelSettings + "feeds:\n  defaults: false\ncli_registry:\n  defaults: false\n"
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Feeds != nil && runtime.Feeds.Len() != 0 {
		t.Errorf("the defaults are off, and the build loaded feeds %v", runtime.Feeds)
	}
	if runtime.Registry != nil && runtime.Registry.Len() != 0 {
		t.Errorf("the defaults are off, and the build loaded a registry %v", runtime.Registry)
	}
}

// TestTheDefaultsLoadBeforeTheSettingsPaths covers the order: the
// default directories are prepended, so a manifest they hold joins the
// same set the settings' paths build.
func TestTheDefaultsLoadBeforeTheSettingsPaths(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	feedsDir := theDefaultFeedManifest(t, pooDir)
	path := feedManifestNamed(t, "ticker")
	settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Feeds == nil || runtime.Feeds.Len() != 2 {
		t.Fatalf("the build loaded %v, want the default feed and the settings' one", runtime.Feeds)
	}
	if !runtime.Feeds.Has("broker") || !runtime.Feeds.Has("ticker") {
		t.Errorf("the build loaded the feeds %v, want broker and ticker", runtime.Feeds.Specs())
	}
	_ = feedsDir
}

// TestADefaultManifestNameDefinedTwiceFailsTheLoad covers the duplicate:
// a feed the defaults hold and a settings path defines is the same error
// as any duplicate name, and the failure names both files.
func TestADefaultManifestNameDefinedTwiceFailsTheLoad(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	feedsDir := theDefaultFeedManifest(t, pooDir)
	// A second directory defines the same feed name, and the settings
	// name it beside the defaults.
	second := filepath.Join(t.TempDir(), "manifests")
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatalf("making the second manifest directory: %v", err)
	}
	body := `{"version": 1, "feeds": {"broker": {"command": ["true"]}}}`
	if err := os.WriteFile(filepath.Join(second, "feeds.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(second)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      config.Overrides{},
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build loaded a feed the defaults and the settings both define")
	}
	if !strings.Contains(err.Error(), "broker") || !strings.Contains(err.Error(), feedsDir) {
		t.Errorf("the failure does not name the duplicate and both files: %v", err)
	}
}

// TestTheDefaultsJoinTheOverrides covers the overrides: the environment
// and the flags add to the settings' paths, and the defaults still sit
// in front of both.
func TestTheDefaultsJoinTheOverrides(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	theDefaultFeedManifest(t, pooDir)
	path := feedManifestNamed(t, "ticker")
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{FeedsPaths: []string{path}})
	if runtime.Feeds == nil || runtime.Feeds.Len() != 2 {
		t.Fatalf("the build loaded %v, want the default feed and the override's one", runtime.Feeds)
	}
}

// TestEveryDefaultManifestKindHasItsOffSwitch covers the kinds' list:
// each kind names a settings block whose defaults switch the settings
// know and honor, so a kind added to the list without a working off
// switch fails here.
func TestEveryDefaultManifestKindHasItsOffSwitch(t *testing.T) {
	for _, m := range config.DefaultManifests() {
		document := theModelSettings + m.SettingsKey + ":\n  defaults: false\n"
		settings, err := config.Parse([]byte(document), config.FormatYAML)
		if err != nil {
			t.Errorf("the %q settings block does not parse: %v", m.SettingsKey, err)
			continue
		}
		off := build(t, settings, config.Overrides{})
		switch m.SettingsKey {
		case "feeds":
			if off.Feeds != nil && off.Feeds.Len() != 0 {
				t.Errorf("the %q switch left feeds loaded: %v", m.SettingsKey, off.Feeds)
			}
		case "cli_registry":
			if off.Registry != nil && off.Registry.Len() != 0 {
				t.Errorf("the %q switch left a registry loaded: %v", m.SettingsKey, off.Registry)
			}
		default:
			t.Errorf("the kind %q names a settings block this test does not know", m.SettingsKey)
		}
	}
}

// TestAnEmptyDefaultManifestDirectoryIsNoRegistry covers the quiet
// directory: a default directory that exists but holds no manifests
// leaves the build exactly where it was, with no registry, no feeds,
// and no error a binary that links no call_cli would trip.
func TestAnEmptyDefaultManifestDirectoryIsNoRegistry(t *testing.T) {
	pooDir := theDefaultsPooDir(t)
	for _, kind := range []string{"feeds", "cli-registry"} {
		if err := os.MkdirAll(filepath.Join(pooDir, kind), 0o700); err != nil {
			t.Fatalf("making the empty %s directory: %v", kind, err)
		}
	}
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Registry != nil {
		t.Errorf("an empty default directory loaded a registry %v", runtime.Registry)
	}
	if runtime.Feeds != nil && runtime.Feeds.Len() != 0 {
		t.Errorf("an empty default directory loaded feeds %v", runtime.Feeds)
	}
	if runtime.Warning != "" {
		t.Errorf("an empty default directory warned: %s", runtime.Warning)
	}
}
