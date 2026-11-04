package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

func TestManifestOverridesResolveAgainstWorkingDirectory(t *testing.T) {
	feedPath := feedManifest(t)
	registryPath := registry(t, "deploy")
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Build(config.BuildOptions{Settings: settings, WorkingDir: filepath.Dir(feedPath), Overrides: config.Overrides{FeedsPaths: []string{filepath.Base(feedPath)}, CLIRegistryPaths: []string{registryPath}}, DefaultProfile: "code", Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors()})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Feeds == nil || runtime.Feeds.Len() != 1 || runtime.Registry == nil || runtime.Registry.Len() != 1 {
		t.Fatalf("missing manifests: feeds %v, registry %v", runtime.Feeds, runtime.Registry)
	}
}

func TestManifestOverridesRejectDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		settings    func(string) string
		override    func(string) config.Overrides
		path        func(*testing.T) string
	}{
		{"feeds", "feeds.paths", func(p string) string { return theFeedsSettings(p) }, func(p string) config.Overrides { return config.Overrides{FeedsPaths: []string{p}} }, feedManifest},
		{"registry", "cli_registry.paths", func(p string) string { return registrySettings(p) }, func(p string) config.Overrides { return config.Overrides{CLIRegistryPaths: []string{p}} }, func(t *testing.T) string { return registry(t, "deploy") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.path(t)
			other := tc.path(t)
			settings, err := config.Parse([]byte(tc.settings(p)), config.FormatYAML)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("POO_FEEDS_PATH", "")
			t.Setenv("POO_CLI_REGISTRY_PATH", "")
			env := "POO_FEEDS_PATH"
			if tc.name == "registry" {
				env = "POO_CLI_REGISTRY_PATH"
			}
			t.Setenv(env, other)
			for _, overrides := range []config.Overrides{tc.override(other), config.EnvOverrides(config.Overrides{})} {
				_, err = config.Build(config.BuildOptions{Settings: settings, Overrides: overrides, DefaultProfile: "code", Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors()})
				var failure *config.Error
				if !errors.As(err, &failure) || failure.Field != tc.field || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
					t.Fatalf("got %v, want duplicate in %s", err, tc.field)
				}
			}
		})
	}
}

func TestBuildDoesNotOverwriteSettingsPathStorage(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	feedPaths := make([]string, 2, 4)
	feedPaths[0], feedPaths[1] = feedManifest(t), "sentinel"
	registryPaths := make([]string, 2, 4)
	registryPaths[0], registryPaths[1] = registry(t, "first"), "sentinel"
	settings.Feeds.Paths = feedPaths[:1]
	settings.CLIRegistry.Paths = registryPaths[:1]
	build(t, settings, config.Overrides{FeedsPaths: []string{feedManifestNamed(t, "second")}, CLIRegistryPaths: []string{registry(t, "second")}})
	if feedPaths[1] != "sentinel" || registryPaths[1] != "sentinel" {
		t.Fatalf("settings storage changed: %v %v", feedPaths, registryPaths)
	}
}

func feedManifestNamed(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "feed.json")
	if err := os.WriteFile(p, []byte(fmt.Sprintf(`{"version":1,"feeds":{"%s":{"command":["true"]}}}`, name)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnvOverridesDoesNotShareCallerSlices(t *testing.T) {
	feeds := make([]string, 1, 3)
	feeds[0] = "caller"
	registries := make([]string, 1, 3)
	registries[0] = "caller"
	t.Setenv("POO_FEEDS_PATH", "feed1"+string(filepath.ListSeparator)+string(filepath.ListSeparator)+"feed2")
	t.Setenv("POO_CLI_REGISTRY_PATH", "registry1")
	first := config.EnvOverrides(config.Overrides{FeedsPaths: feeds, CLIRegistryPaths: registries})
	t.Setenv("POO_FEEDS_PATH", "other")
	t.Setenv("POO_CLI_REGISTRY_PATH", "other")
	_ = config.EnvOverrides(config.Overrides{FeedsPaths: feeds, CLIRegistryPaths: registries})
	if strings.Join(first.FeedsPaths, ",") != "caller,feed1,feed2" || strings.Join(first.CLIRegistryPaths, ",") != "caller,registry1" {
		t.Fatalf("overrides changed: %+v", first)
	}
}
