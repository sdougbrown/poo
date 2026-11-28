package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theGuideExample returns the fenced YAML block the decision hooks guide
// labels with marker, as the text an operator would copy.
func theGuideExample(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join("..", "docs", "capabilities", "decision-hooks.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the guide: %v", err)
	}
	var (
		block    []string
		inFence  bool
		inTarget bool
	)
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			inFence = !inFence
			inTarget = false
			continue
		case inFence && line == marker:
			inTarget = true
			continue
		case inFence && inTarget:
			block = append(block, line)
		}
	}
	if len(block) == 0 {
		t.Fatalf("the guide shows no %s example", marker)
	}
	return strings.Join(block, "\n")
}

// TestTheDecisionHooksGuideSettingsLoad loads the guide's settings
// example the way an operator does: the hooks and the profiles it
// declares are checked, and a spec that could not run is a failure.
func TestTheDecisionHooksGuideSettingsLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeProfilesFixture(t, path, theGuideExample(t, "# settings.yaml"))

	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("the guide's settings example did not load: %v", err)
	}
	spec, ok := settings.Hooks["pre-push-gofmt"]
	if !ok {
		t.Fatalf("the guide's hook is not loaded: %v", settings.Hooks)
	}
	if spec.Entry != "is_pushing_go_code" || len(spec.Steps) != 2 {
		t.Errorf("the hook is %+v; want the two-step graph the guide shows", spec)
	}
	if len(spec.Context.Probes) != 2 {
		t.Errorf("the hook gathers %d probes; want the command and tool probes the guide shows", len(spec.Context.Probes))
	}
	for _, name := range []string{"strict-go", "relaxed-go"} {
		if _, ok := settings.Profiles[name]; !ok {
			t.Errorf("the guide's %s profile is not loaded: %v", name, settings.Profiles)
		}
	}
}

// TestTheDecisionHooksGuideHooksFileLoads loads the guide's hooks-file
// example through the settings that name it.
func TestTheDecisionHooksGuideHooksFileLoads(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfilesFixture(t, filepath.Join(dir, "hooks", "settle.yaml"), theGuideExample(t, "# hooks/settle.yaml"))
	path := filepath.Join(dir, "settings.yaml")
	writeProfilesFixture(t, path, "version: 1\nhooks_files: [hooks/settle.yaml]\n")

	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("the guide's hooks-file example did not load: %v", err)
	}
	spec, ok := settings.Hooks["keep-going"]
	if !ok {
		t.Fatalf("the guide's hook is not loaded: %v", settings.Hooks)
	}
	if spec.Point != "before_settle" || spec.MaxFiresPerRun != 1 {
		t.Errorf("the hook is %+v; want the before_settle hook the guide shows", spec)
	}
}
