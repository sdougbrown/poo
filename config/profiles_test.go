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

// theProfilesBody is the profiles block a model-family case builds
// from: the codex profile over the code base, with the switch the case
// names.
func theProfilesBody(name string, family bool) string {
	return "profiles:\n  " + name + ":\n    base: code\n    model_family: " +
		map[bool]string{true: "true", false: "false"}[family] + "\n"
}

// TestTheModelFamilyRendersWhenTheProfileTurnsItOn covers the family
// section: a profile that opts in renders the text the chat model's
// prompt variant names, and no other family's.
func TestTheModelFamilyRendersWhenTheProfileTurnsItOn(t *testing.T) {
	settings, err := config.Parse([]byte(`
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  gpt-model:
    endpoint: test
    model_id: gpt-6.2
    prompt_variant: gpt-6.2
    ops: [chat]
    context_limit: 32768
roles:
  chat: gpt-model
`+theProfilesBody("codex", true)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	if !strings.Contains(render, "the user's first look at your work") {
		t.Errorf("the rendered prompt is missing the gpt-6.2 family's guidance: %s", render)
	}
	if strings.Contains(render, "You finish the work you start") {
		t.Errorf("the rendered prompt carries another family's guidance")
	}
}

// TestTheModelFamilyIsOffByDefault covers the quiet default: a profile
// that names no model_family runs without the family section, even when
// the model carries a family's prompt variant.
func TestTheModelFamilyIsOffByDefault(t *testing.T) {
	settings, err := config.Parse([]byte(`
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  gpt-model:
    endpoint: test
    model_id: gpt-6.2
    prompt_variant: gpt-6.2
    ops: [chat]
    context_limit: 32768
roles:
  chat: gpt-model
`+theProfilesBody("codex", false)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	if strings.Contains(render, "the user's first look at your work") {
		t.Errorf("the family guidance rendered with the profile opted out: %s", render)
	}
}

// TestTheModelFamilyLeavesTheSwitchUnsetAlone covers the absent half
// of the guard: a profile that never names model_family runs without
// the family section, the same as an explicit false.
func TestTheModelFamilyLeavesTheSwitchUnsetAlone(t *testing.T) {
	settings, err := config.Parse([]byte(`
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  gpt-model:
    endpoint: test
    model_id: gpt-6.2
    prompt_variant: gpt-6.2
    ops: [chat]
    context_limit: 32768
roles:
  chat: gpt-model
profiles:
  codex:
    base: code
`), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	if strings.Contains(render, "the user's first look at your work") {
		t.Errorf("the family guidance rendered with the switch unset: %s", render)
	}
}

// TestTheModelFamilyLeavesUnknownVariantsAlone covers the unknown
// family: a model whose prompt variant names no family the prompt ships
// renders nothing in the section.
func TestTheModelFamilyLeavesUnknownVariantsAlone(t *testing.T) {
	settings, err := config.Parse([]byte(`
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  other-model:
    endpoint: test
    model_id: other-1
    prompt_variant: made-up-family
    ops: [chat]
    context_limit: 32768
roles:
  chat: other-model
`+theProfilesBody("codex", true)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	render := theSessionPrompt(t, runtime, t.TempDir())
	if strings.Contains(render, "the user's first look at your work") || strings.Contains(render, "You finish the work you start") {
		t.Errorf("the rendered prompt carries family guidance no model asked for: %s", render)
	}
	if !strings.Contains(runtime.Warning, "names no family the prompt ships") {
		t.Errorf("the build warned %q, want the note that names no family the prompt ships", runtime.Warning)
	}
}

// TestAProfileAppendsItsPromptFile covers the file form: the append
// file's text rides after the section, and the inline append and the
// file do not sit on one profile.
func TestAProfileAppendsItsPromptFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rails.md")
	if err := os.WriteFile(file, []byte("The file's rails."), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Profiles = map[string]config.ProfileSettings{
		"codex": {Base: "code", PromptAppendFile: file},
	}
	runtime := buildIn(t, settings, config.Overrides{}, dir, theExecutors())
	render := theSessionPrompt(t, runtime, dir)
	if !strings.Contains(render, "The file's rails.") {
		t.Errorf("the rendered prompt is missing the append file's text: %s", render)
	}
	if !strings.Contains(render, code.Profile().Prompt) {
		t.Errorf("the append displaced the base's prose")
	}
	settings.Profiles["codex"] = config.ProfileSettings{Base: "code", PromptAppend: "inline", PromptAppendFile: file}
	_, err = config.Build(config.BuildOptions{
		Settings: settings, DefaultProfile: "code", WorkingDir: dir,
		Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors(),
	})
	if err == nil {
		t.Fatal("the build accepted both an inline append and an append file")
	}
	if field, _ := config.FieldOf(err); field != "profiles.codex.prompt_append_file" {
		t.Errorf("the failure names %v, want profiles.codex.prompt_append_file", err)
	}
}

// TestAProfileAppendsAMissingFileFailsTheSession covers the read's
// failure: a prompt_append_file that is not there fails the session's
// creation, naming the field, instead of opening without the guidance
// the profile asked for.
func TestAProfileAppendsAMissingFileFailsTheSession(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Profiles = map[string]config.ProfileSettings{
		"codex": {Base: "code", PromptAppendFile: filepath.Join(t.TempDir(), "absent.md")},
	}
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	if _, err = runtime.SessionPrompt(t.TempDir()); err == nil {
		t.Fatal("the session opened without the append file's guidance")
	}
	if field, _ := config.FieldOf(err); field != "profiles.codex.prompt_append_file" {
		t.Errorf("the failure names %v, want profiles.codex.prompt_append_file", err)
	}
}

// TestAProfileAppendsItsFileBesideTheSettings covers the named file's
// resolution: a prompt_append_file without a leading path answers to
// the settings file's own directory, the way the prompt file does.
func TestAProfileAppendsItsFileBesideTheSettings(t *testing.T) {
	dir := t.TempDir()
	writeProfilesFixture(t, filepath.Join(dir, "rails.md"), "The rails ride along.")
	settings, err := loadSettingsWithExtra(t, dir, "profiles:\n  codex:\n    base: code\n    prompt_append_file: rails.md\n")
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	got := theSessionPrompt(t, runtime, t.TempDir())
	if !strings.Contains(got, "The rails ride along.") {
		t.Errorf("the rendered prompt is missing the append file's text: %s", got)
	}
	if !strings.Contains(got, code.Profile().Prompt) {
		t.Errorf("the append displaced the base's prose")
	}
}

// TestTheFamilyGuidanceSaysWhenNoChatModelRuns covers the no-model
// attribution: a build without a chat model warns that there is none,
// not that a model names no variant.
func TestTheFamilyGuidanceSaysWhenNoChatModelRuns(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"profiles:\n  codex:\n    base: code\n    model_family: true\n"), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	settings.Profile = "codex"
	settings.Roles = nil
	runtime := buildWith(t, config.BuildOptions{
		Settings: settings, NoChatModel: true, WorkingDir: t.TempDir(),
		Tools: theTools(), Profiles: theProfiles(), Executors: theExecutors(),
	})
	if !strings.Contains(runtime.Warning, "the build has no chat model") {
		t.Errorf("the build warned %q, want the no-chat-model note", runtime.Warning)
	}
}
