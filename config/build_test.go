package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

// theTools are the tools a binary links.
func theTools() map[string]config.ToolFactory {
	return map[string]config.ToolFactory{
		"bash": func(opts config.ToolOptions) poo.Tool {
			return bash.New(bash.WithExecutor(opts.Executor), bash.WithScrub(opts.Scrub))
		},
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
		"edit": func(config.ToolOptions) poo.Tool { return edit.New() },
	}
}

// theProfiles are the profiles a binary links.
func theProfiles() []profile.Profile {
	return []profile.Profile{base.Profile(), code.Profile()}
}

// theExecutors are the executors a binary links.
func theExecutors() *bash.Registry {
	registry := bash.NewRegistry()
	registry.Register("local", func(bash.Options) (bash.Executor, error) {
		return bash.NewLocal(), nil
	})
	return registry
}

// build builds a runtime for the settings.
func build(t *testing.T, settings *config.Settings, overrides config.Overrides) *config.Runtime {
	t.Helper()
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	return runtime
}

// theSettings is a settings document with a model in it.
const theModelSettings = `
version: 1
endpoints:
  test:
    base_url: http://localhost:4000/v1
    api_key_env: POO_TEST_KEY
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat]
    context_limit: 32768
roles:
  chat: test-model
`

// TestTheBuildPicksTheProfileAndItsTools covers the profile: the
// session offers the tools the profile names, and the policy takes the
// profile's action for a call no rule covers.
func TestTheBuildPicksTheProfileAndItsTools(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if runtime.Profile.Name != "code" {
		t.Errorf("the profile is %q, want code", runtime.Profile.Name)
	}
	want := []string{"bash", "read", "grep", "edit"}
	if len(runtime.Options.ToolOrder) != len(want) {
		t.Fatalf("the tools are %v, want %v", runtime.Options.ToolOrder, want)
	}
	for i, name := range want {
		if runtime.Options.ToolOrder[i] != name {
			t.Errorf("the tool %d is %q, want %q", i, runtime.Options.ToolOrder[i], name)
		}
	}
}

// TestTheBuildTakesTheUnmatchedActionFromTheProfile covers the
// precedence: the profile's action applies, and the settings change it.
func TestTheBuildTakesTheUnmatchedActionFromTheProfile(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Policy == nil {
		t.Fatal("the build has no policy")
	}
	// The code profile allows every call no rule covers.
	decision, err := runtime.Policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	})
	if err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if decision.Allowed != true {
		t.Error("the policy refused a call the code profile allows")
	}

	asked := "ask"
	settings.Permission.Unmatched = &asked
	runtime = build(t, settings, config.Overrides{})
	decision, err = runtime.Policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	})
	if err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if decision.Allowed {
		t.Error("the policy allowed a call the settings ask about")
	}
}

// TestTheBuildWarnsWhenNoAskerCanAnswer covers the load-time warning:
// the policy can ask, and no asker can answer, so every ask falls back.
func TestTheBuildWarnsWhenNoAskerCanAnswer(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"permission:\n  unmatched: ask\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if !strings.Contains(runtime.Warning, "falls back") {
		t.Errorf("the warning is %q, want it to name the fallback", runtime.Warning)
	}
}

// TestTheBuildScrubsTheCredentials covers the credentials: the names the
// endpoints' keys are read from are gone from the child environment.
func TestTheBuildScrubsTheCredentials(t *testing.T) {
	t.Setenv("POO_TEST_KEY", "secret")
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	for _, entry := range runtime.Env {
		if strings.HasPrefix(entry, "POO_TEST_KEY=") {
			t.Error("the child environment holds the credential")
		}
	}
}

// TestTheBuildRefusesAToolTheBinaryLacks covers the rule: a settings
// file cannot enable a tool the binary does not link.
func TestTheBuildRefusesAToolTheBinaryLacks(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"permission:\n  rules:\n    - tool: wget\n      action: allow\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build succeeded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnsupported {
		t.Errorf("the failure is %v, want an unsupported setting", err)
	}
}

// TestTheBuildRefusesAnExecutorTheBinaryLacks covers the same rule for
// the executors.
func TestTheBuildRefusesAnExecutorTheBinaryLacks(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"tools:\n  bash:\n    executor: bwrap\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build succeeded, want a failure")
	}
	code, ok := config.CodeOf(err)
	if !ok || code != config.CodeUnsupported {
		t.Errorf("the failure is %v, want an unsupported setting", err)
	}
}

// TestTheBuildMakesThePrompt covers the prompt: the settings' sections
// are in the prompt, and the removed ones are not.
func TestTheBuildMakesThePrompt(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "base.md")
	if err := os.WriteFile(baseFile, []byte("the base section"), 0o644); err != nil {
		t.Fatalf("writing the base file: %v", err)
	}
	settings, err := config.Parse([]byte(theModelSettings+`system_prompt:
  base_file: `+baseFile+`
  append: " and more"
  sections:
    house: the house style
  remove: [environment]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	builder := prompt.New(prompt.Env{})
	for _, edit := range runtime.Prompt {
		edit(builder)
	}
	text := builder.Render()
	if !strings.Contains(text, "the base section and more") {
		t.Errorf("the prompt is %q, want the base file and the appended text", text)
	}
	if !strings.Contains(text, "the house style") {
		t.Errorf("the prompt is %q, want the settings' section", text)
	}
	if strings.Contains(text, "Environment:") {
		t.Errorf("the prompt is %q, want the removed section gone", text)
	}
}

// TestTheBuildMakesTheStore covers the session's home: the store is
// the settings' directory, and the ephemeral mode has none.
func TestTheBuildMakesTheStore(t *testing.T) {
	dir := t.TempDir()
	settings, err := config.Parse([]byte(theModelSettings+"session:\n  dir: "+dir+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	if runtime.Store == nil {
		t.Fatal("the build has no store")
	}
	if runtime.Store.Dir() != dir {
		t.Errorf("the store is in %q, want %q", runtime.Store.Dir(), dir)
	}
	if err := runtime.Store.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	runtime = build(t, settings, config.Overrides{SessionMode: "ephemeral"})
	if runtime.Store != nil {
		t.Error("the ephemeral mode has a store")
	}
}

// TestTheBuildResolvesTheRoles covers the catalog: the chat role names
// the model, and the settings' sampling reaches the descriptor.
func TestTheBuildResolvesTheRoles(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"sampling:\n  test-model:\n    temperature: 0.5\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	model, err := runtime.ChatModel()
	if err != nil {
		t.Fatalf("resolving the chat model: %v", err)
	}
	if model.ModelID != "test-model" {
		t.Errorf("the chat model is %q, want test-model", model.ModelID)
	}
	if model.Sampling.Temperature == nil || *model.Sampling.Temperature != 0.5 {
		t.Errorf("the sampling is %v, want the settings' temperature", model.Sampling.Temperature)
	}
	if _, err := runtime.Transport(model); err != nil {
		t.Errorf("building the transport: %v", err)
	}
}

// TestTheCatalogFailsWithoutAChatModel covers the catalog's rule: a
// session cannot run without a chat model.
func TestTheCatalogFailsWithoutAChatModel(t *testing.T) {
	settings, err := config.Parse([]byte("version: 1\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err == nil {
		t.Fatal("the build succeeded, want a failure")
	}
}

// The permission policy is the core's, and the chain is the askers'.
var _ perm.Asker = (*perm.WireAsker)(nil)

// The context is what the store's prune runs under.
var _ = context.Background

// TestTheRuntimeCreatesAndResumesASession covers the factory: the
// runtime creates a session in the store, and the session is loaded
// again from the records it wrote.
func TestTheRuntimeCreatesAndResumesASession(t *testing.T) {
	dir := t.TempDir()
	settings, err := config.Parse([]byte(theModelSettings+"session:\n  dir: "+dir+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	created := &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    "s1",
		Mode:         "logged",
		CreatedAt:    "2026-10-22T12:00:00Z",
		WorkingDir:   t.TempDir(),
		Model:        poo.SessionModel{ID: "test-model"},
		ContextLimit: 32768,
		Profile:      "code",
		Build:        poo.BuildInfo{Name: "poo", Version: "0.1.0"},
	}
	sess, err := runtime.CreateSession(context.Background(), created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if _, err := sess.Prompt(context.Background(), "say hello"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	resumed, err := runtime.ResumeSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	defer resumed.Session.Close()
	if resumed.Loaded.ID != "s1" {
		t.Errorf("the loaded session is %q, want s1", resumed.Loaded.ID)
	}
	if len(resumed.Loaded.Records) == 0 {
		t.Error("the loaded session holds no records")
	}
}
