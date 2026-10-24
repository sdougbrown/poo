package config_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
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

// TestTheReplaceSettingWinsOverABinarysPromptDirectory covers the
// precedence the guide states: the operator's whole-prompt replacement
// applies after the directory's files, so neither the directory's
// default nor its variant is the prompt.
func TestTheReplaceSettingWinsOverABinarysPromptDirectory(t *testing.T) {
	body := theModelSettings + "system_prompt:\n  replace: the operator's whole prompt\n"
	files := fstest.MapFS{
		"base.md":             {Data: []byte("the binary's base\n")},
		"base.small-local.md": {Data: []byte("the binary's small base\n")},
		"environment.md":      {Data: []byte("the binary's environment\n")},
	}
	runtime, err := buildWithPrompt(t, body, files)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	for _, variant := range []string{"", "small-local"} {
		got := poo.PromptBuilder(runtime.Options).WithVariant(variant).Render()
		want := "the operator's whole prompt\n"
		if got != want {
			t.Errorf("the prompt in variant %q is %q, want %q", variant, got, want)
		}
	}
	if strings.Contains(poo.PromptBuilder(runtime.Options).Render(), "the binary's") {
		t.Errorf("the prompt carries the directory's prose")
	}

	// Without a directory the replacement is the same whole prompt.
	plain, err := buildWithPrompt(t, body, nil)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if got := poo.PromptBuilder(plain.Options).Render(); got != "the operator's whole prompt\n" {
		t.Errorf("the prompt without a directory is %q", got)
	}
}

// TestTheSettingsAfterTheReplacementStillApply covers the rest of the
// precedence: a setting after the replacement edits the base it leaves.
func TestTheSettingsAfterTheReplacementStillApply(t *testing.T) {
	body := theModelSettings + "system_prompt:\n  replace: the operator's whole prompt\n  append: ' with more'\n"
	runtime, err := buildWithPrompt(t, body, fstest.MapFS{
		"base.md": {Data: []byte("the binary's base\n")},
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	got := poo.PromptBuilder(runtime.Options).Render()
	want := "the operator's whole prompt with more\n"
	if got != want {
		t.Errorf("the prompt is %q, want %q", got, want)
	}
}

// TestARenderErrorSettlesTheRun covers a loaded template that
// type-checks and still fails on the data a run supplies: the error
// names the file and settles the run instead of panicking the session.
func TestARenderErrorSettlesTheRun(t *testing.T) {
	runtime, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
		"base.md": {Data: []byte("{{index .Tools 99}}\n")},
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	runtime.Options.Transport = pootest.NewTransport(pootest.Turn{Text: "done"})
	sess, err := poo.New(runtime.Options)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	defer sess.Close()
	runID, err := sess.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	result, err := sess.WaitRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if result.Reason != poo.ReasonHookError {
		t.Errorf("the run settled as %s, want hook_error", result.Reason)
	}
	if result.Detail.Source != "system_prompt" {
		t.Errorf("the run's source is %q, want system_prompt", result.Detail.Source)
	}
	for _, want := range []string{"base.md", "index out of range"} {
		if !strings.Contains(result.Detail.Text, want) {
			t.Errorf("the settle text %q does not name %q", result.Detail.Text, want)
		}
	}
}

// TestATemplateThatOnlyFailsAtRenderSettlesTheRun covers the templates
// the load-time check leaves alone: they load, and the run settles as
// hook_error with the file named.
func TestATemplateThatOnlyFailsAtRenderSettlesTheRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"nested call", `{{define "part"}}{{.Model}}{{end}}{{template "part" .Model}}`},
		{"builtin result", `{{(index .Tools 0).Bad}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
				"base.md": {Data: []byte(tc.text)},
			})
			if err != nil {
				t.Fatalf("building the runtime: %v", err)
			}
			runtime.Options.Transport = pootest.NewTransport(pootest.Turn{Text: "done"})
			sess, err := poo.New(runtime.Options)
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			defer sess.Close()
			runID, err := sess.Prompt(context.Background(), "do the thing")
			if err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			result, err := sess.WaitRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			if result.Reason != poo.ReasonHookError {
				t.Errorf("the run settled as %s, want hook_error", result.Reason)
			}
			if result.Detail.Source != "system_prompt" {
				t.Errorf("the run's source is %q, want system_prompt", result.Detail.Source)
			}
			if !strings.Contains(result.Detail.Text, "base.md") {
				t.Errorf("the settle text %q does not name the file", result.Detail.Text)
			}
		})
	}
}
