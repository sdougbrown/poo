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

// TestOverrideFailuresNameTheirSources checks diagnostics for missing runtime capabilities.
func TestOverrideFailuresNameTheirSources(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("POO_CLI_REGISTRY_PATH", registry(t, "deploy"))
	tools := theTools()
	delete(tools, "call_cli")
	_, err = config.Build(config.BuildOptions{Settings: settings, Overrides: config.EnvOverrides(config.Overrides{}), DefaultProfile: "code", Tools: tools, Profiles: theProfiles(), Executors: theExecutors()})
	if err == nil || !strings.Contains(err.Error(), "POO_CLI_REGISTRY_PATH") {
		t.Fatalf("registry failure does not name its environment source: %v", err)
	}
}

// TestFeedOverrideFailureNamesItsSource checks a feed without an executor.
func TestFeedOverrideFailureNamesItsSource(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("POO_FEEDS_PATH", feedManifest(t))
	_, err = config.Build(config.BuildOptions{Settings: settings, Overrides: config.EnvOverrides(config.Overrides{}), DefaultProfile: "code", Tools: theTools(), Profiles: theProfiles()})
	if err == nil || !strings.Contains(err.Error(), "POO_FEEDS_PATH") {
		t.Fatalf("feed failure does not name its environment source: %v", err)
	}
}

// TestManifestEnvironmentIsScrubbed checks the environment passed to session commands.
func TestManifestEnvironmentIsScrubbed(t *testing.T) {
	t.Setenv("POO_FEEDS_PATH", feedManifest(t))
	t.Setenv("POO_CLI_REGISTRY_PATH", registry(t, "deploy"))
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime := build(t, settings, config.EnvOverrides(config.Overrides{}))
	for _, entry := range runtime.Env {
		if strings.HasPrefix(entry, "POO_FEEDS_PATH=") || strings.HasPrefix(entry, "POO_CLI_REGISTRY_PATH=") {
			t.Errorf("manifest variable reached the session environment: %s", entry)
		}
	}
}

// TestManifestOverridesResolveAgainstWorkingDirectory checks relative override paths.
func TestManifestOverridesResolveAgainstWorkingDirectory(t *testing.T) {
	feedPath := feedManifest(t)
	registryDir := registry(t, "deploy")
	registryPath := filepath.Join(registryDir, "registry.json")
	feedPathInRegistry := filepath.Join(registryDir, "feeds.json")
	body, err := os.ReadFile(feedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedPathInRegistry, body, 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Build(config.BuildOptions{Settings: settings, WorkingDir: filepath.Dir(registryPath), Overrides: config.Overrides{FeedsPaths: []string{filepath.Base(feedPathInRegistry)}, CLIRegistryPaths: []string{filepath.Base(registryPath)}}, DefaultProfile: "code", Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors()})
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

// TestWithOverridesReportsTheMergedManifestPaths covers the effective
// settings: they hold the settings' manifest paths followed by the
// overrides', in fresh lists, so the caller's settings stay as they were.
func TestWithOverridesReportsTheMergedManifestPaths(t *testing.T) {
	settings := &config.Settings{}
	settings.Feeds.Paths = make([]string, 1, 4)
	settings.Feeds.Paths[0] = "settings-feeds"
	settings.CLIRegistry.Paths = []string{"settings-registry"}
	effective := config.WithOverrides(settings, config.Overrides{
		FeedsPaths:       []string{"flag-feeds"},
		CLIRegistryPaths: []string{"flag-registry"},
	})
	if got := fmt.Sprint(effective.Feeds.Paths); got != "[settings-feeds flag-feeds]" {
		t.Errorf("the effective feed paths are %s", got)
	}
	if got := fmt.Sprint(effective.CLIRegistry.Paths); got != "[settings-registry flag-registry]" {
		t.Errorf("the effective registry paths are %s", got)
	}
	if got := fmt.Sprint(settings.Feeds.Paths[:cap(settings.Feeds.Paths)][1]); got != "" {
		t.Errorf("the merge wrote %q into the settings' spare capacity", got)
	}
}
