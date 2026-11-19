package config_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
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
