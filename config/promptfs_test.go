package config_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
)

// A binary's prompt directory replaces the defaults the build assembles,
// and the settings still win over it.

// buildWithPrompt builds a runtime for a settings document and a prompt
// directory.
func buildWithPrompt(t *testing.T, body string, files fstest.MapFS) (*config.Runtime, error) {
	t.Helper()
	settings, err := config.Parse([]byte(body), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
		PromptFS:       files,
	})
}

// TestABinarysPromptDirectoryReplacesTheDefaults covers the first two
// layers: the directory's files replace the embedded base and a tool's
// section.
func TestABinarysPromptDirectoryReplacesTheDefaults(t *testing.T) {
	runtime, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
		"base.md":       {Data: []byte("the binary's base\n")},
		"tools/bash.md": {Data: []byte("the binary's bash\n")},
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	for _, want := range []string{"the binary's base", "the binary's bash"} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "You are a coding agent.") {
		t.Errorf("the prompt still holds the embedded base:\n%s", text)
	}
}

// TestTheSettingsWinOverABinarysPromptDirectory covers the precedence:
// the operator's system_prompt settings apply after the directory, so
// the settings' text is the one the prompt carries.
func TestTheSettingsWinOverABinarysPromptDirectory(t *testing.T) {
	body := theModelSettings + "system_prompt:\n  sections:\n    base: the operator's base\n"
	runtime, err := buildWithPrompt(t, body, fstest.MapFS{
		"base.md": {Data: []byte("the binary's base\n")},
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "the operator's base") {
		t.Errorf("the prompt does not hold the settings' base:\n%s", text)
	}
	if strings.Contains(text, "the binary's base") {
		t.Errorf("the prompt still holds the binary's base:\n%s", text)
	}
}

// TestAnUnknownPromptFileFailsTheBuild covers the unknown-name error on
// the build path: the file is named, and the build refuses it.
func TestAnUnknownPromptFileFailsTheBuild(t *testing.T) {
	_, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
		"nope.md": {Data: []byte("text\n")},
	})
	if err == nil {
		t.Fatal("the build accepted a prompt directory with an unknown file")
	}
	if !strings.Contains(err.Error(), "nope.md") {
		t.Errorf("the error %q does not name the file", err)
	}
}
