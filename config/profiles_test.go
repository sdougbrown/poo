package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile/code"
)

func TestInvalidProfileSettingsIdentifyTheField(t *testing.T) {
	for _, tt := range []struct {
		name, profile, field string
		definition           config.ProfileSettings
		code                 string
	}{
		{"empty name", "", "profiles", config.ProfileSettings{Base: "code"}, config.CodeInvalid},
		{"linked name", "code", "profiles.code", config.ProfileSettings{Base: "code"}, config.CodeInvalid},
		{"unknown base", "reviewer", "profiles.reviewer.base", config.ProfileSettings{Base: "missing"}, config.CodeInvalid},
		{"core tool", "reviewer", "profiles.reviewer.tools.disabled", config.ProfileSettings{Base: "code", Tools: config.ProfileTools{Disabled: []string{"finish"}}}, config.CodeUnsupported},
		{"unlinked tool", "reviewer", "profiles.reviewer.tools.disabled", config.ProfileSettings{Base: "code", Tools: config.ProfileTools{Disabled: []string{"wget"}}}, config.CodeUnsupported},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
			if err != nil {
				t.Fatal(err)
			}
			settings.Profiles = map[string]config.ProfileSettings{tt.profile: tt.definition}
			_, err = config.Build(config.BuildOptions{
				Settings: settings, DefaultProfile: "code", WorkingDir: t.TempDir(),
				Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors(),
			})
			field, _ := config.FieldOf(err)
			code, _ := config.CodeOf(err)
			if field != tt.field || code != tt.code {
				t.Fatalf("got %v; want %s at %s", err, tt.code, tt.field)
			}
		})
	}
}

func TestSystemPromptSettingsTakePrecedenceOverProfileFile(t *testing.T) {
	for _, tt := range []struct {
		name   string
		prompt config.PromptSettings
		want   string
	}{
		{"replace", config.PromptSettings{Replace: "Replacement prompt."}, "Replacement prompt."},
		{"set profile section", config.PromptSettings{Sections: map[string]string{"profile": "Explicit profile section."}}, "Explicit profile section."},
		{"remove profile section", config.PromptSettings{Remove: []string{"profile"}}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
			if err != nil {
				t.Fatal(err)
			}
			settings.Profile = "reviewer"
			settings.Profiles = map[string]config.ProfileSettings{
				"reviewer": {Base: "code", PromptFile: filepath.Join(dir, "missing.md")},
			}
			settings.SystemPrompt = tt.prompt
			runtime := buildIn(t, settings, config.Overrides{}, dir, theExecutors())
			edits, err := runtime.SessionPrompt(dir)
			if err != nil {
				t.Fatalf("the overridden prompt file was read: %v", err)
			}
			opts := runtime.Options
			opts.Prompt = append(slices.Clone(opts.Prompt), edits...)
			builder := poo.PromptBuilder(opts)
			if tt.want == "" {
				for _, section := range builder.Sections() {
					if section.Name == "profile" {
						t.Fatal("the removed profile section was rendered")
					}
				}
			} else if !strings.Contains(builder.Render(), tt.want) {
				t.Fatalf("the rendered prompt is missing %q", tt.want)
			}
		})
	}
}

// theSessionPrompt renders the prompt a new session under the settings
// would carry: the build's edits, then the session's own.
func theSessionPrompt(t *testing.T, runtime *config.Runtime, dir string) string {
	t.Helper()
	edits, err := runtime.SessionPrompt(dir)
	if err != nil {
		t.Fatalf("reading the session's prompt: %v", err)
	}
	opts := runtime.Options
	opts.Prompt = append(slices.Clone(opts.Prompt), edits...)
	return poo.PromptBuilder(opts).Render()
}

// TestAProfileAppendsItsPromptLines covers the compose: the profile's
// own lines ride after the linked base's prose, and the base's words
// are still there.
func TestAProfileAppendsItsPromptLines(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Profiles = map[string]config.ProfileSettings{
		"codex": {Base: "code", PromptAppend: "Stay on the rails."},
	}
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	base := code.Profile().Prompt
	if !strings.Contains(render, base) {
		t.Errorf("the appended lines displaced the base's own prose")
	}
	if !strings.Contains(render, "Stay on the rails.") {
		t.Errorf("the rendered prompt is missing the profile's own lines: %s", render)
	}
	if strings.Index(render, base) > strings.Index(render, "Stay on the rails.") {
		t.Errorf("the profile's own lines render before the base's prose")
	}
}

// TestAProfileAppendsAfterItsPromptFile covers the pair: the file
// replaces the section, and the append rides after the file's text.
func TestAProfileAppendsAfterItsPromptFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "codex.md")
	if err := os.WriteFile(file, []byte("The file's own words."), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Profiles = map[string]config.ProfileSettings{
		"codex": {Base: "code", PromptFile: file, PromptAppend: "Stay on the rails."},
	}
	runtime := buildIn(t, settings, config.Overrides{}, dir, theExecutors())
	render := theSessionPrompt(t, runtime, dir)
	if strings.Contains(render, code.Profile().Prompt) {
		t.Errorf("the prompt file did not replace the base's prose")
	}
	fileAt, baseAt := strings.Index(render, "The file's own words."), strings.Index(render, "Stay on the rails.")
	if fileAt < 0 || baseAt < 0 || fileAt > baseAt {
		t.Errorf("the append does not ride after the file's text: %s", render)
	}
}

// TestAProfileAppendYieldsToTheSystemPrompt covers the precedence: a
// system_prompt setting that owns the profile section leaves the
// profile's own lines out, the way it leaves the prompt file out.
func TestAProfileAppendYieldsToTheSystemPrompt(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Profiles = map[string]config.ProfileSettings{
		"codex": {Base: "code", PromptAppend: "Stay on the rails."},
	}
	settings.SystemPrompt = config.PromptSettings{Replace: "Replacement prompt."}
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	if strings.Contains(render, "Stay on the rails.") {
		t.Errorf("the system prompt's replacement did not own the section: %s", render)
	}
}
