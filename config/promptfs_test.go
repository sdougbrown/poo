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

// TestAPromptFileThatDoesNotRenderFailsTheBuild covers the loaded
// templates the load-time check leaves alone: they load, and a build that
// carries them is refused with the file named, so no session is opened.
func TestAPromptFileThatDoesNotRenderFailsTheBuild(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"index out of range", "{{index .Tools 99}}"},
		{"nested call", `{{define "part"}}{{.Model}}{{end}}{{template "part" .Model}}`},
		{"builtin result", `{{(index .Tools 0).Bad}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := pootest.NewTransport(pootest.Turn{Text: "done"})
			_, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
				"base.md": {Data: []byte(tc.text)},
			})
			if err == nil {
				t.Fatal("the build accepted a prompt file that does not render")
			}
			code, ok := config.CodeOf(err)
			if !ok || code != config.CodeInvalid {
				t.Errorf("the code is %q, want %q", code, config.CodeInvalid)
			}
			if field, _ := config.FieldOf(err); field != "system_prompt" {
				t.Errorf("the field is %q, want system_prompt", field)
			}
			if !strings.Contains(err.Error(), "base.md") {
				t.Errorf("the error %q does not name the file", err)
			}
			if got := len(transport.Requests()); got != 0 {
				t.Errorf("the endpoint got %d requests, want none", got)
			}
		})
	}
}

// TestAPromptThatFailsInTheSessionsVariantFailsTheSession covers a
// template that renders as the build's default and not in the variant a
// session asks for: the session is not built, and the failure names the
// file.
func TestAPromptThatFailsInTheSessionsVariantFailsTheSession(t *testing.T) {
	runtime, err := buildWithPrompt(t, theModelSettings, fstest.MapFS{
		"base.small-local.md": {Data: []byte("{{index .Tools 99}}\n")},
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	store, err := runtime.OpenStore("ephemeral", "")
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	defer store.Close()

	transport := pootest.NewTransport(pootest.Turn{Text: "done"})
	runtime.Options.Transport = transport
	created := &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    "s1",
		Mode:         "ephemeral",
		CreatedAt:    "2026-10-22T12:00:00Z",
		WorkingDir:   t.TempDir(),
		Model:        poo.SessionModel{ID: "test-model"},
		ContextLimit: 32768,
		Profile:      "code",
		Build:        poo.BuildInfo{Name: "poo", Version: "0.1.0"},
	}
	sess, err := runtime.CreateSession(context.Background(), store, created, poo.WithPromptVariant("small-local"))
	if err == nil {
		_ = sess.Close()
		t.Fatal("the session was built with a prompt that does not render")
	}
	if sess != nil {
		t.Error("the failed creation returned a session")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeInvalid {
		t.Errorf("the code is %q, want %q", code, config.CodeInvalid)
	}
	if field, _ := config.FieldOf(err); field != "system_prompt" {
		t.Errorf("the field is %q, want system_prompt", field)
	}
	if !strings.Contains(err.Error(), "base.small-local.md") {
		t.Errorf("the error %q does not name the file", err)
	}
	if got := len(transport.Requests()); got != 0 {
		t.Errorf("the endpoint got %d requests, want none", got)
	}
}
