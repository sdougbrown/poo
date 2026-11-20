package config_test

// The profile's declared feeds: a profile asserts the feeds it needs,
// and the operator's feeds.paths still control which feeds load. A
// declared feed the settings do not load is skipped with a warning.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
)

// theFeedsProfileSettings are the settings the declared-feeds tests
// build from, with the feeds block the case names.
func theFeedsProfileSettings(paths ...string) string {
	return theFeedsSettings(paths...) + "profile: apron\n"
}

// twoFeedManifest writes a manifest with a broker feed and a ticker
// feed, and returns its path.
func twoFeedManifest(t *testing.T) string {
	t.Helper()
	body := `{"version": 1, "feeds": {
		"broker": {"description": "Messages from the broker.", "command": ["true"]},
		"ticker": {"description": "The ticker's beats.", "command": ["true"]}
	}}`
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return path
}

// buildWithProfiles builds a runtime whose binary links the named
// profiles, so a case may link one that declares feeds. The settings
// name the profile the session runs under.
func buildWithProfiles(t *testing.T, settings *config.Settings, linked []profile.Profile) *config.Runtime {
	t.Helper()
	opts := config.BuildOptions{
		Settings:       settings,
		Overrides:      config.Overrides{},
		WorkingDir:     t.TempDir(),
		Executors:      theExecutors(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       linked,
	}
	runtime, err := config.Build(opts)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return runtime
}

// theFeedsProfiles are the profiles a binary links: the code profile
// and an apron profile that declares two feeds.
func theFeedsProfiles(feeds ...string) []profile.Profile {
	return []profile.Profile{
		{Name: "code", Tools: []string{"read"}},
		{Name: "apron", Tools: []string{"read"}, Feeds: feeds},
	}
}

// TestAProfileDeclaresTheFeedsItNeeds covers the build: the chosen
// profile carries its declared feeds, the settings' feeds all still run,
// and a declared feed the settings load warns about nothing.
func TestAProfileDeclaresTheFeedsItNeeds(t *testing.T) {
	path := twoFeedManifest(t)
	settings, err := config.Parse([]byte(theFeedsProfileSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWithProfiles(t, settings, theFeedsProfiles("broker", "ticker"))
	if strings.Contains(runtime.Warning, "declares the feed") {
		t.Errorf("the build warned %q, want no declared feed named", runtime.Warning)
	}
	if names := runtime.Profile.Feeds; strings.Join(names, ",") != "broker,ticker" {
		t.Errorf("the profile declares %v, want broker,ticker", names)
	}
	// The operator's feeds run whole: the ticker the profile does not
	// declare runs beside the broker it does.
	running := make([]string, 0, len(runtime.Options.Feeds))
	for _, spec := range runtime.Options.Feeds {
		running = append(running, spec.Name)
	}
	if strings.Join(running, ",") != "broker,ticker" {
		t.Errorf("the session runs %v, want broker,ticker", running)
	}
}

// TestADeclaredFeedTheSettingsDoNotLoadWarns covers the warning: the
// profile declares a feed the settings do not load, and the build
// says so rather than failing or starting anything.
func TestADeclaredFeedTheSettingsDoNotLoadWarns(t *testing.T) {
	path := twoFeedManifest(t)
	settings, err := config.Parse([]byte(theFeedsProfileSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := buildWithProfiles(t, settings, theFeedsProfiles("broker", "events"))
	if runtime.Warning == "" {
		t.Fatal("the build warned nothing, want the missing feed named")
	}
	if !strings.Contains(runtime.Warning, `"events"`) {
		t.Errorf("the warning does not name the missing feed: %q", runtime.Warning)
	}
	if strings.Contains(runtime.Warning, `"broker"`) {
		t.Errorf("the warning names the broker, which the settings load: %q", runtime.Warning)
	}
	running := make([]string, 0, len(runtime.Options.Feeds))
	for _, spec := range runtime.Options.Feeds {
		running = append(running, spec.Name)
	}
	if strings.Join(running, ",") != "broker,ticker" {
		t.Errorf("the session runs %v, want broker,ticker", running)
	}
}

// TestASettingsProfileAddsFeedsToItsBase covers the variant: a profile
// the settings define declares feeds that add to the base's own, kept
// once each, so a declaration says what the whole profile needs and a
// restatement warns about nothing twice.
func TestASettingsProfileAddsFeedsToItsBase(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+
		"profile: watcher\n"+
		"profiles:\n  watcher:\n    base: code\n    feeds: [broker, events, broker]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	linked := []profile.Profile{
		{Name: "code", Tools: []string{"read"}, Feeds: []string{"broker"}},
	}
	opts := config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		Executors:      theExecutors(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       linked,
	}
	runtime, err := config.Build(opts)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if names := runtime.Profile.Feeds; strings.Join(names, ",") != "broker,events" {
		t.Errorf("the profile declares %v, want broker,events", names)
	}
	// The listing restore reads carries the declaration too: the feeds
	// of the profile this build defines for the recorded name.
	var listed []string
	for _, p := range runtime.Options.Profiles {
		if p.Name == "watcher" {
			listed = p.Feeds
		}
	}
	if strings.Join(listed, ",") != "broker,events" {
		t.Errorf("the listed profile declares %v, want broker,events", listed)
	}
	if runtime.Warning == "" {
		t.Fatal("the build warned nothing, want both missing feeds named")
	}
	if strings.Count(runtime.Warning, `"broker"`) != 1 || strings.Count(runtime.Warning, `"events"`) != 1 {
		t.Errorf("the warning does not name each declared feed once: %q", runtime.Warning)
	}
}

// TestASettingsProfileRefusesAnEmptyDeclaredFeed covers the validation:
// a declared feed name cannot be empty, so a warning about a nameless
// feed never reads as a bug in poo.
func TestASettingsProfileRefusesAnEmptyDeclaredFeed(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+
		"profile: watcher\n"+
		"profiles:\n  watcher:\n    base: code\n    feeds: [\"\"]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	opts := config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		Executors:      theExecutors(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       []profile.Profile{{Name: "code", Tools: []string{"read"}}},
	}
	_, err = config.Build(opts)
	if err == nil {
		t.Fatal("the build accepted an empty declared feed name")
	}
	if !strings.Contains(err.Error(), "feeds") {
		t.Errorf("the failure does not name the field: %v", err)
	}
}
