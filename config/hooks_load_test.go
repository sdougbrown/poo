package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/hook"
)

// theToolHook is a before_tool hook the settings or a file declares. Its
// fire cap is the spec's own, so a test can tell which copy of a name
// the load kept.
func theToolHook(fires int) hook.Spec {
	return hook.Spec{
		Point:          hook.PointBeforeTool,
		Match:          hook.Match{Tool: "bash"},
		MaxFiresPerRun: fires,
		Entry:          "classify",
		Steps: map[string]hook.Step{
			"classify": {
				Question: hook.QuestionSpec{
					Kind:         hook.QuestionBool,
					Instructions: "Is the call one the hook stops?",
					True:         "the call is the one",
					False:        "the call is another",
				},
				Branches: map[string]hook.Branch{
					"yes": {MinProbability: 0.9, Action: &hook.Action{Kind: hook.ActionAllow}},
					"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionDeny, Message: "refused"}},
				},
			},
		},
	}
}

// writeHookDocument writes a JSON document into the test's directory.
func writeHookDocument(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeProfilesFixture(t, path, string(data))
}

// theContractHooksFile is the hook the frozen settings fixture declares,
// in the file the settings name.
const theContractHooksFile = `
hooks:
  pre-push-gofmt:
    point: before_tool
    match:
      tool: bash
      command_contains: ["git push", "git commit"]
    max_fires_per_run: 3
    firing_timeout_ms: 8000
    on_uncertain: fail_open
    on_error: fail_open
    context:
      turns: 8
      include_tool_call: true
      include_working_dir: true
      probes:
        - name: unformatted_files
          command: ["gofmt", "-l", "."]
          timeout_ms: 3000
        - name: go_mod
          tool:
            name: read
            args: { path: "go.mod", limit: 30 }
    entry: is_pushing_go_code
    steps:
      is_pushing_go_code:
        timeout_ms: 1500
        question:
          kind: bool
          instructions: "Is the pending bash command committing or pushing Go changes while unformatted_files is non-empty?"
          true: "committing or pushing while unformatted Go files exist"
          false: "safe to proceed"
        branches:
          "yes":
            min_probability: 0.75
            next: classify_fix
          "no":
            min_probability: 0.5
            action: { kind: allow }
      classify_fix:
        timeout_ms: 1500
        question:
          kind: choice
          instructions: "Which instruction should be given to the agent?"
          options:
            - { name: run_gofmt, desc: "Tell the agent to run gofmt first" }
            - { name: allow_anyway, desc: "False positive; allow the command" }
        branches:
          run_gofmt:
            min_probability: 0.70
            action:
              kind: deny
              message: "Unformatted Go files were detected. Run gofmt -w . first."
          allow_anyway:
            min_probability: 0.5
            action: { kind: allow }
`

// TestTheHooksFixtureLoadsAndResolvesItsProfiles loads the frozen
// settings fixture: the hooks come from the file the settings name, the
// spec is what the operator wrote, and the profiles select and opt out
// of the hook.

func TestHooksFilesLayerBelowTheSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeHookDocument(t, filepath.Join(dir, "hooks", "team.json"),
		map[string]any{"hooks": map[string]hook.Spec{"shared": theToolHook(7), "from-team": theToolHook(2)}})
	writeHookDocument(t, filepath.Join(dir, "hooks", "extra.json"),
		map[string]any{"hooks": map[string]hook.Spec{"from-team": theToolHook(9), "from-extra": theToolHook(1)}})
	path := filepath.Join(dir, "settings.json")
	writeHookDocument(t, path, map[string]any{
		"version":     1,
		"hooks":       map[string]hook.Spec{"shared": theToolHook(3)},
		"hooks_files": []string{"hooks/team.json", "hooks/extra.json"},
	})

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		fires int
	}{
		{"shared", 3},
		{"from-team", 2},
		{"from-extra", 1},
	} {
		spec, ok := settings.Hooks[tt.name]
		if !ok {
			t.Fatalf("the %q hook is not loaded: %v", tt.name, settings.Hooks)
		}
		if spec.MaxFiresPerRun != tt.fires {
			t.Errorf("the %q hook fires %d times; want the copy that fires %d", tt.name, spec.MaxFiresPerRun, tt.fires)
		}
	}
}

// TestAHooksFileIsReadStrictly refuses a file that holds a key the file
// does not have, and a file that declares no hook at all.
func TestAHooksFileIsReadStrictly(t *testing.T) {
	for _, tt := range []struct {
		name  string
		file  string
		wants string
	}{
		{"unknown top-level key", `{"hook": {}}`, `unknown field "hook"`},
		{"unknown spec key", `{"hooks": {"mine": {"point": "before_tool", "entry": "s", "steps": {}, "fire_cap": 3}}}`, "fire_cap"},
		{"no hooks", `{"hooks": {}}`, "declares no hooks"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProfilesFixture(t, filepath.Join(dir, "checks.yaml"), tt.file)
			path := filepath.Join(dir, "settings.yaml")
			writeProfilesFixture(t, path, "version: 1\nhooks_files: [checks.yaml]\n")
			_, err := config.Load(path)
			if err == nil {
				t.Fatal("the file loaded; want a failure naming hooks_files")
			}
			if field, _ := config.FieldOf(err); field != "hooks_files" {
				t.Errorf("the failure names %q; want hooks_files", field)
			}
			if code, _ := config.CodeOf(err); code != config.CodeInvalid {
				t.Errorf("the failure's code is %q; want %q", code, config.CodeInvalid)
			}
			if !strings.Contains(err.Error(), tt.wants) {
				t.Errorf("the failure is %q; want it to mention %q", err, tt.wants)
			}
		})
	}
}

// TestASettingsHookThatCannotRunIsRefused checks a hook spec the runner
// could not execute at its point.
func TestASettingsHookThatCannotRunIsRefused(t *testing.T) {
	for _, tt := range []struct {
		name  string
		hooks string
		field string
	}{
		{
			"decision the point does not take",
			`
hooks:
  broken:
    point: before_run
    entry: decide
    steps:
      decide:
        question: { kind: bool, instructions: "?", true: "yes", false: "no" }
        branches:
          "yes": { min_probability: 0.9, action: { kind: deny, message: "no" } }
          "no": { min_probability: 0.5, action: { kind: pass } }
`,
			"hooks.broken",
		},
		{
			"empty hook name",
			`
hooks:
  "":
    point: before_run
    entry: decide
    steps:
      decide:
        question: { kind: bool, instructions: "?", true: "yes", false: "no" }
        branches:
          "yes": { min_probability: 0.9, action: { kind: pass } }
          "no": { min_probability: 0.5, action: { kind: pass } }
`,
			"hooks",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Parse([]byte("version: 1\n"+tt.hooks), config.FormatYAML)
			if err == nil {
				t.Fatal("the settings loaded; want the hook to be refused")
			}
			if field, _ := config.FieldOf(err); field != tt.field {
				t.Errorf("the failure names %q; want %q", field, tt.field)
			}
			if code, _ := config.CodeOf(err); code != config.CodeInvalid {
				t.Errorf("the failure's code is %q; want %q", code, config.CodeInvalid)
			}
		})
	}
}

// TestProfileHooksInheritAppendAndOptOut checks the chain a profile
// resolves to: its base's hooks, then its own, kept once each, with the
// names it turns off removed.
