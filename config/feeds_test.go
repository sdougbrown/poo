package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theFeedsSettings are the settings the feeds tests build from: a model,
// a chat role, and the feeds block the case names.
func theFeedsSettings(paths ...string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "\""+path+"\"")
	}
	return theModelSettings + "feeds:\n  paths: [" + strings.Join(quoted, ", ") + "]\n"
}

// feedManifest writes a manifest with one feed and returns its path.
func feedManifest(t *testing.T) string {
	t.Helper()
	body := `{"version": 1, "feeds": {"broker": {"description": "Messages from the broker.", "command": ["true"], "on_idle": "wake"}}}`
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// TestTheSettingsRoundTripTheirFeedsBlock covers the settings: the feeds
// block decodes into the settings' Feeds, paths and all.
func TestTheSettingsRoundTripTheirFeedsBlock(t *testing.T) {
	settings, err := config.Parse([]byte(theFeedsSettings("/tmp/manifests")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	if len(settings.Feeds.Paths) != 1 || settings.Feeds.Paths[0] != "/tmp/manifests" {
		t.Errorf("the settings hold feeds.paths %v", settings.Feeds.Paths)
	}
}

// TestAFeedManifestReachesTheRuntime covers the build: manifests the
// settings name are loaded once, and the runtime holds the specs.
func TestAFeedManifestReachesTheRuntime(t *testing.T) {
	path := feedManifest(t)
	settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Feeds == nil || runtime.Feeds.Len() != 1 {
		t.Fatalf("the build loaded %v, want the one feed", runtime.Feeds)
	}
	specs := runtime.Feeds.Specs()
	if specs[0].Name != "broker" || specs[0].OnIdle != "wake" || strings.Join(specs[0].Command, " ") != "true" {
		t.Errorf("the spec is %+v", specs[0])
	}
}

// TestAFeedPathRelativeToTheWorkingDirectory covers the path: a relative
// `feeds.paths` entry is resolved against the directory the session
// works in, not the directory the process was started in.
func TestAFeedPathRelativeToTheWorkingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "manifests")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("making the manifest directory: %v", err)
	}
	body := `{"version": 1, "feeds": {"broker": {"command": ["true"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "feeds.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings("manifests")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     filepath.Dir(dir),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if runtime.Feeds == nil || runtime.Feeds.Len() != 1 {
		t.Fatalf("the build loaded %v, want the one feed beside the working directory", runtime.Feeds)
	}
}

// TestAFeedPathThatDoesNotLoadNamesTheField covers the failure: a
// feeds.paths entry the loader cannot read fails the build, and the
// failure names the settings field the operator wrote.
func TestAFeedPathThatDoesNotLoadNamesTheField(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "feeds.json")
	settings, err := config.Parse([]byte(theFeedsSettings(missing)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build accepted a feeds path that is not there")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a config error", err)
	}
	if failure.Field != "feeds.paths" {
		t.Errorf("the failure names %q, want feeds.paths", failure.Field)
	}
	if !strings.Contains(err.Error(), "no such file") {
		t.Errorf("the failure %q does not keep the cause", err)
	}
}

// TestAFeedManifestThatDoesNotLoadNamesTheField covers the settings
// check: a manifest the settings name and the loader refuses fails the
// build, and the failure names the field the operator wrote.
func TestAFeedManifestThatDoesNotLoadNamesTheField(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "feeds.json")
	body := `{"version": 1, "feeds": {"broker": {"command": ["true"], "restart_at": 5}}}`
	if err := os.WriteFile(broken, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(broken)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build accepted a manifest it cannot load")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a config error", err)
	}
	if failure.Field != "feeds.paths" {
		t.Errorf("the failure names %q, want feeds.paths", failure.Field)
	}
	if !strings.Contains(err.Error(), "restart_at") {
		t.Errorf("the failure %q does not name the unknown field", err)
	}
}
