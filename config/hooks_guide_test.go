package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
)

// theGuideSettings returns the `# settings.yaml` example the decision
// hooks guide shows, as the text an operator would copy.
func theGuideSettings(t *testing.T) string {
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
		case inFence && line == "# settings.yaml":
			inTarget = true
			continue
		case inFence && inTarget:
			block = append(block, line)
		}
	}
	if len(block) == 0 {
		t.Fatalf("the guide shows no settings.yaml example")
	}
	return strings.Join(block, "\n")
}

// TestTheDecisionHooksGuideExampleLoads loads the guide's settings
// example the way an operator does: the hooks and the profiles it
// declares are checked, and a spec that could not run is a failure.
func TestTheDecisionHooksGuideExampleLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	writeProfilesFixture(t, path, theGuideSettings(t))

	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("the guide's example did not load: %v", err)
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
	if _, ok := settings.Profiles["strict-go"]; !ok {
		t.Errorf("the guide's strict-go profile is not loaded: %v", settings.Profiles)
	}
	if _, ok := settings.Profiles["relaxed-go"]; !ok {
		t.Errorf("the guide's relaxed-go profile is not loaded: %v", settings.Profiles)
	}
}
