package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// writeProfilesFixture writes a file into the test's directory.
func writeProfilesFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// loadSettingsWithExtra writes a settings file holding the model
// settings plus the extra document, into dir, and loads it.
func loadSettingsWithExtra(t *testing.T, dir, extra string) (*config.Settings, error) {
	t.Helper()
	path := filepath.Join(dir, "settings.yaml")
	writeProfilesFixture(t, path, theModelSettings+extra)
	return config.Load(path)
}

// renderedProfilePrompt builds the runtime for the settings, selects the
// profile, and returns the prompt its profile section renders.
func renderedProfilePrompt(t *testing.T, settings *config.Settings, profile string) string {
	t.Helper()
	settings.Profile = profile
	runtime := buildIn(t, settings, config.Overrides{}, t.TempDir(), theExecutors())
	edits, err := runtime.SessionPrompt(t.TempDir())
	if err != nil {
		t.Fatalf("reading the profile's prompt: %v", err)
	}
	opts := runtime.Options
	opts.Prompt = append(opts.Prompt[len(opts.Prompt):], edits...)
	builder := poo.PromptBuilder(opts)
	return builder.Render()
}

func TestProfilesFilesDeclareProfilesTheSettingsSelect(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "team")
	if err := os.MkdirAll(team, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfilesFixture(t, filepath.Join(team, "profiles.yaml"), `
profiles:
  reviewer:
    base: code
    prompt_file: reviewer.md
`)
	writeProfilesFixture(t, filepath.Join(team, "reviewer.md"), "Reviewer prompt.")
	writeProfilesFixture(t, filepath.Join(dir, "own.json"),
		`{"profiles": {"editor": {"base": "base", "prompt_file": "editor.md"}}}`)
	writeProfilesFixture(t, filepath.Join(dir, "editor.md"), "Editor prompt.")

	settings, err := loadSettingsWithExtra(t, dir, `
profiles_files:
  - team/profiles.yaml
  - own.json
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ profile, want string }{
		{"reviewer", "Reviewer prompt."},
		{"editor", "Editor prompt."},
	} {
		if got := renderedProfilePrompt(t, settings, tt.profile); !strings.Contains(got, tt.want) {
			t.Errorf("the %s profile's prompt is missing %q", tt.profile, tt.want)
		}
	}
}

func TestProfilesFilePromptAnswersToTheFileFromARelativeSettingsPath(t *testing.T) {
	dir := t.TempDir()
	config.Chdir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".poo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfilesFixture(t, filepath.Join(dir, ".poo", "profiles.yaml"), `
profiles:
  reviewer:
    base: code
    prompt_file: reviewer.md
`)
	writeProfilesFixture(t, filepath.Join(dir, ".poo", "reviewer.md"), "Reviewer prompt.")
	writeProfilesFixture(t, filepath.Join(dir, ".poo", "settings.yaml"), theModelSettings+"profiles_files: [profiles.yaml]\n")

	settings, err := config.Load(".poo/settings.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedProfilePrompt(t, settings, "reviewer"); !strings.Contains(got, "Reviewer prompt.") {
		t.Errorf("the reviewer profile's prompt is missing %q", "Reviewer prompt.")
	}
}

func TestProfilesFilesNamedWithoutADirectoryAnswerToTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	config.Chdir(t, dir)
	writeProfilesFixture(t, filepath.Join(dir, "profiles.yaml"), `
profiles:
  reviewer:
    base: code
    prompt_file: reviewer.md
`)
	writeProfilesFixture(t, filepath.Join(dir, "reviewer.md"), "Reviewer prompt.")

	settings, err := config.Parse([]byte(theModelSettings+"profiles_files: [profiles.yaml]\n"), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedProfilePrompt(t, settings, "reviewer"); !strings.Contains(got, "Reviewer prompt.") {
		t.Errorf("the reviewer profile's prompt is missing %q", "Reviewer prompt.")
	}
}

func TestProfilesFilesLayerBelowTheSettings(t *testing.T) {
	dir := t.TempDir()
	writeProfilesFixture(t, filepath.Join(dir, "first.yaml"), `
profiles:
  reviewer:
    base: code
    prompt_file: first-reviewer.md
  approver:
    base: code
    prompt_file: first-approver.md
`)
	writeProfilesFixture(t, filepath.Join(dir, "second.yaml"), `
profiles:
  reviewer:
    base: code
    prompt_file: second-reviewer.md
  approver:
    base: code
    prompt_file: second-approver.md
`)
	writeProfilesFixture(t, filepath.Join(dir, "first-reviewer.md"), "The first file's reviewer prompt.")
	writeProfilesFixture(t, filepath.Join(dir, "first-approver.md"), "The first file's approver prompt.")
	writeProfilesFixture(t, filepath.Join(dir, "second-reviewer.md"), "The second file's reviewer prompt.")
	writeProfilesFixture(t, filepath.Join(dir, "second-approver.md"), "The second file's approver prompt.")
	writeProfilesFixture(t, filepath.Join(dir, "settings-reviewer.md"), "The settings' reviewer prompt.")

	settings, err := loadSettingsWithExtra(t, dir, `
profiles:
  reviewer:
    base: code
    prompt_file: settings-reviewer.md
profiles_files: [first.yaml, second.yaml]
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ profile, want string }{
		{"reviewer", "The settings' reviewer prompt."},
		{"approver", "The first file's approver prompt."},
	} {
		if got := renderedProfilePrompt(t, settings, tt.profile); !strings.Contains(got, tt.want) {
			t.Errorf("the %s profile's prompt is missing %q", tt.profile, tt.want)
		}
	}
}

func TestProfilesFileProfilesAreValidatedLikeTheSettingsOnes(t *testing.T) {
	for _, tt := range []struct {
		name, body, field, code string
	}{
		{"unknown base", "profiles:\n  reviewer:\n    base: missing\n", "profiles.reviewer.base", config.CodeInvalid},
		{"core tool", "profiles:\n  reviewer:\n    base: code\n    tools:\n      disabled: [finish]\n", "profiles.reviewer.tools.disabled", config.CodeUnsupported},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProfilesFixture(t, filepath.Join(dir, "profiles.yaml"), tt.body)
			settings, err := loadSettingsWithExtra(t, dir, "profiles_files: [profiles.yaml]\n")
			if err != nil {
				t.Fatal(err)
			}
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

func TestProfilesFilesThatDoNotReadFailTheLoad(t *testing.T) {
	for _, tt := range []struct{ name, body string }{
		{"unknown key", "profiles:\n  reviewer:\n    base: code\n    prompt: reviewer.md\n"},
		{"unknown top key", "profiles:\n  reviewer:\n    base: code\nversion: 1\n"},
		{"no profiles", "profiles: {}\n"},
		{"empty file", ""},
		{"not yaml", "\tprofiles: [\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProfilesFixture(t, filepath.Join(dir, "profiles.yaml"), tt.body)
			_, err := loadSettingsWithExtra(t, dir, "profiles_files: [profiles.yaml]\n")
			if err == nil {
				t.Fatal("the settings loaded")
			}
			field, _ := config.FieldOf(err)
			if field != "profiles_files" {
				t.Fatalf("got field %q; want profiles_files (%v)", field, err)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := loadSettingsWithExtra(t, dir, "profiles_files: [missing.yaml]\n")
		if err == nil {
			t.Fatal("the settings loaded")
		}
		field, _ := config.FieldOf(err)
		if field != "profiles_files" {
			t.Fatalf("got field %q; want profiles_files (%v)", field, err)
		}
	})
}
