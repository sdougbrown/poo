package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/codemode"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/prompt"
	"github.com/sdougbrown/poo/tool/bash"
	"github.com/sdougbrown/poo/tool/cliregistry"
	toolcodemode "github.com/sdougbrown/poo/tool/codemode"
	"github.com/sdougbrown/poo/tool/edit"
	fetchtool "github.com/sdougbrown/poo/tool/fetch"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

// stubEngine is an engine that never runs, for the wiring tests.
type stubEngine struct{}

func (stubEngine) Run(ctx context.Context, opts codemode.RunOptions) (codemode.RunResult, error) {
	return codemode.RunResult{}, nil
}

// theTools are the tools a binary links.
func theTools() map[string]config.ToolFactory {
	return map[string]config.ToolFactory{
		"bash": func(opts config.ToolOptions) poo.Tool {
			return bash.New(bash.WithExecutor(opts.Executor), bash.WithScrub(opts.Scrub))
		},
		"read":     func(config.ToolOptions) poo.Tool { return read.New() },
		"grep":     func(config.ToolOptions) poo.Tool { return grep.New() },
		"edit":     func(config.ToolOptions) poo.Tool { return edit.New() },
		"codemode": func(config.ToolOptions) poo.Tool { return toolcodemode.New(stubEngine{}) },
		"fetch":    func(config.ToolOptions) poo.Tool { return fetchtool.New() },
		"call_cli": func(opts config.ToolOptions) poo.Tool {
			return cliregistry.NewCall(opts.Registry).WithSearch(opts.Search).WithExecutor(opts.Executor)
		},
		"find_tool": func(opts config.ToolOptions) poo.Tool {
			return cliregistry.NewFind(opts.Registry)
		},
	}
}

// theProfiles are the profiles a binary links.
func theProfiles() []profile.Profile {
	return []profile.Profile{base.Profile(), code.Profile()}
}

// theExecutors are the executors a binary links.
func theExecutors() *bash.Registry {
	registry := bash.NewRegistry()
	registry.Register("local", func(exec.Options) (exec.Executor, error) {
		return bash.NewLocal(), nil
	})
	return registry
}

// build builds a runtime for the settings.
func build(t *testing.T, settings *config.Settings, overrides config.Overrides) *config.Runtime {
	t.Helper()
	return buildIn(t, settings, overrides, t.TempDir(), theExecutors())
}

// buildIn builds a runtime for the settings in a working directory, with
// the executors the binary links.
func buildIn(t *testing.T, settings *config.Settings, overrides config.Overrides, workingDir string, executors *bash.Registry) *config.Runtime {
	t.Helper()
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		Overrides:      overrides,
		WorkingDir:     workingDir,
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      executors,
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
    endpoint: http://localhost:4000/v1
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
	want := []string{"bash", "read", "grep", "edit", "fetch", "codemode"}
	if len(runtime.Options.ToolOrder) != len(want) {
		t.Fatalf("the tools are %v, want %v", runtime.Options.ToolOrder, want)
	}
	for i, name := range want {
		if runtime.Options.ToolOrder[i] != name {
			t.Errorf("the tool %d is %q, want %q", i, runtime.Options.ToolOrder[i], name)
		}
	}
	// The profile offers the core's job tools as well: the bash tool
	// may run in the background, so the model needs a way to see the
	// jobs and a way to stop one.
	names := map[string]bool{}
	for _, tool := range runtime.Profile.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"bash", "read", "grep", "edit", "jobs", "cancel_job"} {
		if !names[name] {
			t.Errorf("the profile does not offer the %q tool", name)
		}
	}
	// The profiles the binary offers are the ones it links.
	if len(runtime.Profiles) != 2 {
		t.Errorf("the build offers %d profiles, want 2", len(runtime.Profiles))
	}
}

// TestTheCodemodeSettingsReachTheSession covers the mapping: every
// tools.codemode field lands on the session's settings, which the tool
// passes the engine as a run's limits.
func TestTheCodemodeSettingsReachTheSession(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`
tools:
  codemode:
    timeout_s: 17
    script_max_bytes: 2048
    output_cap_chars: 4096
    exec_steps: 250000
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	got := runtime.Options.Settings
	for _, field := range []struct {
		name string
		got  int
		want int
	}{
		{"timeout_s", got.CodemodeTimeoutS, 17},
		{"script_max_bytes", got.CodemodeScriptMaxBytes, 2048},
		{"output_cap_chars", got.CodemodeOutputCapChars, 4096},
		{"exec_steps", got.CodemodeExecSteps, 250000},
	} {
		if field.got != field.want {
			t.Errorf("tools.codemode.%s reached the session as %d, want %d", field.name, field.got, field.want)
		}
	}
}

// TestTheFetchSettingsReachTheSession covers the mapping: every
// tools.fetch field lands on the session's settings, which the tool
// reads per call.
func TestTheFetchSettingsReachTheSession(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`
tools:
  fetch:
    timeout_s: 12
    max_bytes: 1048576
    user_agent: probe/1.0
    allow_private_hosts: true
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	got := runtime.Options.Settings
	if got.FetchTimeoutS != 12 {
		t.Errorf("tools.fetch.timeout_s reached the session as %d, want 12", got.FetchTimeoutS)
	}
	if got.FetchMaxBytes != 1048576 {
		t.Errorf("tools.fetch.max_bytes reached the session as %d, want 1048576", got.FetchMaxBytes)
	}
	if got.FetchUserAgent != "probe/1.0" {
		t.Errorf("tools.fetch.user_agent reached the session as %q, want probe/1.0", got.FetchUserAgent)
	}
	if !got.FetchAllowPrivateHosts {
		t.Errorf("tools.fetch.allow_private_hosts reached the session as false, want true")
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

// TestTheSettingsPromptIsTheSessionPrompt covers the prompt the
// session carries: the settings' sections are in it, and not only in
// the build's own account of them.
func TestTheSettingsPromptIsTheSessionPrompt(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`system_prompt:
  sections:
    house: the house style
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "the house style") {
		t.Errorf("the prompt is %q, want the settings' section", text)
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

// TestTheSessionWorksInTheDirectoryItsHeaderNames covers the working
// directory: a session works in the directory its header names, not in
// the directory the build was made in.
func TestTheSessionWorksInTheDirectoryItsHeaderNames(t *testing.T) {
	dir := t.TempDir()
	settings, err := config.Parse([]byte(theModelSettings+"session:\n  dir: "+dir+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()

	workDir := t.TempDir()
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-10-22T12:00:00Z",
			WorkingDir: workDir,

			Profile: "code", Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer sess.Close()

	if !strings.Contains(sess.PromptState().Text, workDir) {
		t.Errorf("the prompt does not name the working directory %q", workDir)
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

	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-10-22T12:00:00Z",
			WorkingDir: t.TempDir(),

			Profile: "code", Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if _, err := sess.Prompt(context.Background(), "say hello"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	resumed, err := runtime.ResumeSession(context.Background(), runtime.Store, "s1")
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

// readOnlyTools are the tools a binary that can change nothing links.
func readOnlyTools() map[string]config.ToolFactory {
	return map[string]config.ToolFactory{
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
	}
}

// readOnlyBuild builds a runtime for a binary with the read-only tools.
func readOnlyBuild(t *testing.T, settings *config.Settings, executors *bash.Registry) (*config.Runtime, error) {
	t.Helper()
	return config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: search.Name,
		Tools:          readOnlyTools(),
		Profiles:       []profile.Profile{search.Profile()},
		Executors:      executors,
	})
}

// TestAnExecutorWithoutTheBashToolIsRefused covers the seam's settings
// side: tools.bash.executor belongs to the bash tool, so a binary that
// does not link the tool refuses the setting by name.
func TestAnExecutorWithoutTheBashToolIsRefused(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`
tools:
  bash:
    executor: bwrap
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = readOnlyBuild(t, settings, nil)
	if err == nil {
		t.Fatal("a binary without bash built a settings file that names an executor")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %v, want a settings failure", err)
	}
	if failure.Field != "tools.bash.executor" {
		t.Errorf("the failure names the field %q, want tools.bash.executor", failure.Field)
	}
	if !strings.Contains(failure.Message, "bash") {
		t.Errorf("the message %q does not name the tool", failure.Message)
	}
}

// TestAReadOnlyBuildNeedsNoExecutor covers the other half: a profile that
// offers no bash builds without an executor, so a read-only binary passes
// no registry.
func TestAReadOnlyBuildNeedsNoExecutor(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := readOnlyBuild(t, settings, nil)
	if err != nil {
		t.Fatalf("building a read-only runtime: %v", err)
	}
	want := []string{"read", "grep"}
	if len(runtime.Options.ToolOrder) != len(want) {
		t.Fatalf("the tools are %v, want %v", runtime.Options.ToolOrder, want)
	}
	for i, name := range want {
		if runtime.Options.ToolOrder[i] != name {
			t.Errorf("the tool %d is %q, want %q", i, runtime.Options.ToolOrder[i], name)
		}
	}
}

// TestABashSettingWithoutTheBashToolIsRefused covers every leaf the bash
// tool owns: a binary that does not link the tool refuses the leaf by
// name, and an explicit zero is still present.
func TestABashSettingWithoutTheBashToolIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, leaf, document string }{
		{"timeout", "tools.bash.timeout_s", "tools:\n  bash: {timeout_s: 17}\n"},
		{"zero timeout", "tools.bash.timeout_s", "tools:\n  bash: {timeout_s: 0}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := config.Parse([]byte(theModelSettings+tc.document), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			_, err = readOnlyBuild(t, settings, nil)
			var failure *config.Error
			if !errors.As(err, &failure) {
				t.Fatalf("a binary without bash accepted %s: %v", tc.leaf, err)
			}
			if failure.Code != config.CodeUnsupported || failure.Field != tc.leaf {
				t.Errorf("the failure is %+v, want unsupported on %s", failure, tc.leaf)
			}
			if !strings.Contains(failure.Message, "bash") {
				t.Errorf("the message %q does not name the tool", failure.Message)
			}
		})
	}
}

// TestACLIRegistryWithoutTheBashToolBuildsAnExecutor covers the executor's
// other caller: call_cli runs a command too, so a binary that offers it
// gets the session's executor, and a binary that carries no executor at
// all is told so rather than left without one.
func TestACLIRegistryWithoutTheBashToolBuildsAnExecutor(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(dir)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	files := profile.Profile{Name: "files", Tools: []string{"edit"}}
	var got exec.Executor
	build := func(executors *bash.Registry) (*config.Runtime, error) {
		return config.Build(config.BuildOptions{
			Settings:       settings,
			WorkingDir:     t.TempDir(),
			DefaultProfile: files.Name,
			Tools: map[string]config.ToolFactory{
				"edit": func(config.ToolOptions) poo.Tool { return edit.New() },
				"call_cli": func(opts config.ToolOptions) poo.Tool {
					got = opts.Executor
					return cliregistry.NewCall(opts.Registry).WithExecutor(opts.Executor)
				},
			},
			Profiles:  []profile.Profile{files},
			Executors: executors,
		})
	}

	runtime, err := build(theExecutors())
	if err != nil {
		t.Fatalf("building a runtime with call_cli and no bash: %v", err)
	}
	if !hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
	if got == nil {
		t.Error("call_cli was built with no executor")
	}

	_, err = build(nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a binary with call_cli and no executor built anyway: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "tools.bash.executor" {
		t.Errorf("the failure is %+v, want unsupported on tools.bash.executor", failure)
	}
	if !strings.Contains(failure.Message, "links no executor") {
		t.Errorf("the message %q does not say the binary links no executor", failure.Message)
	}
}

// TestANilBashFactoryIsNotLinked covers the linked predicate: a map
// entry with no constructor is not a tool the binary offers, so the
// executor setting it names is refused.
func TestANilBashFactoryIsNotLinked(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"tools:\n  bash: {executor: local}\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	tools := readOnlyTools()
	tools["bash"] = nil
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "base",
		Tools:          tools,
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a nil bash factory accepted tools.bash.executor: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "tools.bash.executor" {
		t.Errorf("the failure is %+v, want unsupported on tools.bash.executor", failure)
	}
}

// TestADeclaredCredentialStaysInTheSessionEnv covers the half a launcher's
// credential must reach: poo's own subsystems resolve against the session
// environment, so the variable stays there. The model-invoked side is
// TestTheDeclaredCredentialIsNotInAModelTool.
func TestADeclaredCredentialStaysInTheSessionEnv(t *testing.T) {
	t.Setenv("POO_TEST_CREDENTIAL", "launched-by-the-parent")
	t.Setenv("POO_CREDENTIAL_ENV", "POO_TEST_CREDENTIAL")
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.EnvOverrides(config.Overrides{}))

	if !slices.Contains(runtime.DeclaredCredentialEnvNames, "POO_TEST_CREDENTIAL") {
		t.Fatalf("declared names = %v, want POO_TEST_CREDENTIAL", runtime.DeclaredCredentialEnvNames)
	}
	// The session environment keeps it: the feed and a cli entry's bindings
	// resolve against this.
	found := false
	for _, entry := range runtime.Env {
		if entry == "POO_TEST_CREDENTIAL=launched-by-the-parent" {
			found = true
		}
	}
	if !found {
		t.Error("the session environment lost the credential poo's own subsystems need")
	}
	// The declaration names credentials; it is not one itself, and the session
	// environment does not need it.
	for _, entry := range runtime.Env {
		if strings.HasPrefix(entry, "POO_CREDENTIAL_ENV=") {
			t.Errorf("the session environment holds the declaration itself: %s", entry)
		}
	}
}

// TestTheDeclaredCredentialIsNotInAModelTool is the other half of the rule: the
// session environment keeps a launcher's credential for poo's own subsystems,
// and the child a model-invoked tool runs in does not have it.
func TestTheDeclaredCredentialIsNotInAModelTool(t *testing.T) {
	t.Setenv("POO_TEST_CREDENTIAL", "launched-by-the-parent")
	t.Setenv("POO_CREDENTIAL_ENV", "POO_TEST_CREDENTIAL")
	settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.EnvOverrides(config.Overrides{}))
	tool, ok := runtime.Options.Tools[config.ToolBash]
	if !ok {
		t.Fatalf("the session offers %v", runtime.Options.ToolOrder)
	}
	prepared, err := tool.Prepare(json.RawMessage(`{"command":"printenv POO_TEST_CREDENTIAL || echo ABSENT"}`), poo.PrepareEnv{
		WorkingDir: runtime.Options.WorkingDir,
	})
	if err != nil {
		t.Fatalf("preparing the call: %v", err)
	}
	res, err := tool.Call(context.Background(), prepared, poo.ToolEnv{
		PrepareEnv: poo.PrepareEnv{WorkingDir: runtime.Options.WorkingDir},
		Env:        runtime.Options.Env,
		Settings:   runtime.Options.Settings,
	})
	if err != nil {
		t.Fatalf("running bash: %v", err)
	}
	if strings.Contains(res.Text, "launched-by-the-parent") {
		t.Fatalf("a model-invoked tool saw the credential: %s", res.Text)
	}
	if !strings.Contains(res.Text, "ABSENT") {
		t.Fatalf("the probe did not report absence: %s", res.Text)
	}
}

// TestTheSettingsDisableAProfileTool covers the lever: a disabled tool is
// gone from the tools the session builds, from the schemas it sends, from
// the prompt's own section for it and from the profile the client lists.
func TestTheSettingsDisableAProfileTool(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [grep]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	for _, spec := range runtime.ToolSpecs() {
		if spec.Name == "grep" {
			t.Error("the session sends the disabled tool's schema")
		}
	}
	// The tools around it are untouched, so the setting removes the name it
	// lists and nothing else.
	if _, ok := runtime.Options.Tools["read"]; !ok {
		t.Errorf("the session dropped a tool the settings did not disable: %v", runtime.Options.ToolOrder)
	}
	for _, tool := range runtime.Profile.Tools {
		if tool.Name == "grep" {
			t.Error("the profile still offers the disabled tool")
		}
	}
	// The bash tool is still offered, so the core still adds the job tools
	// the model needs to see and stop a job.
	for _, name := range []string{profile.JobsToolName, profile.CancelJobToolName} {
		if !slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool { return tool.Name == name }) {
			t.Errorf("the profile lost the %q tool with a background tool still offered", name)
		}
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if strings.Contains(text, "Grep:") {
		t.Error("the prompt still carries the disabled tool's section")
	}
	if !strings.Contains(text, "Read:") {
		t.Error("the prompt lost a section the settings did not disable")
	}
	// The profiles the client reads offer what a session running them
	// offers, so the disabled tool is gone from the listed profile too.
	for _, listed := range runtime.Profiles {
		if listed.Name != runtime.Profile.Name {
			continue
		}
		if slices.ContainsFunc(listed.Tools, func(tool poo.ToolInfo) bool { return tool.Name == "grep" }) {
			t.Error("the listed profile still offers the disabled tool")
		}
	}
}

// TestADisablingABackgroundToolTakesTheJobToolsWithIt covers the tools the
// core adds for another tool: with bash disabled nothing the session offers
// may run in the background, so the job tools are not offered either.
func TestADisablingABackgroundToolTakesTheJobToolsWithIt(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [bash]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	want := []string{"read", "grep", "edit", "fetch", "codemode"}
	if !slices.Equal(runtime.Options.ToolOrder, want) {
		t.Errorf("the tools are %v, want %v", runtime.Options.ToolOrder, want)
	}
	for _, name := range []string{profile.JobsToolName, profile.CancelJobToolName} {
		if slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool { return tool.Name == name }) {
			t.Errorf("the profile offers the %q tool with no background tool left", name)
		}
	}
}

// TestADisabledToolTheProfileDoesNotOfferChangesNothing covers the other
// side of the lever: the list is the operator's, so a name the profile
// running does not offer builds, and that profile keeps every tool it has.
func TestADisabledToolTheProfileDoesNotOfferChangesNothing(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [bash]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: search.Name,
		Tools:          theTools(),
		Profiles:       []profile.Profile{search.Profile()},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the search profile with bash disabled: %v", err)
	}
	want := []string{"read", "grep"}
	if !slices.Equal(runtime.Options.ToolOrder, want) {
		t.Errorf("the tools are %v, want %v", runtime.Options.ToolOrder, want)
	}
}

// TestAProfileTheSettingsDisableEveryToolOfIsNotListed covers the listing's
// runnable rule: a profile the disabled list empties cannot run, so the
// binary does not offer it.
func TestAProfileTheSettingsDisableEveryToolOfIsNotListed(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [read, grep]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), search.Profile()},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building under the code profile with the search profile's tools disabled: %v", err)
	}
	for _, listed := range runtime.Profiles {
		if listed.Name == search.Name {
			t.Errorf("the binary offers the emptied %q profile with the tools %v", listed.Name, listed.Tools)
		}
	}
}

// TestAProfileTheSettingsDisableEveryToolOfCannotRun covers the session's
// half of the same rule: the listing does not offer a profile the settings
// empty, so selecting that profile is the operator's configuration error
// rather than a session with no tool at all.
func TestAProfileTheSettingsDisableEveryToolOfCannotRun(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [read, grep]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = readOnlyBuild(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the build selected a profile the settings empty: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "tools.disabled" {
		t.Errorf("the failure is %+v, want unsupported on tools.disabled", failure)
	}
	if !strings.Contains(failure.Message, search.Name) {
		t.Errorf("the message %q does not name the profile", failure.Message)
	}
}

// TestADisabledRegistryToolIsNotOffered covers a registry tool the
// settings disable: call_cli comes back from the registry rather than from
// the profile, so the disabled list still takes it off the session that
// builds.
func TestADisabledRegistryToolIsNotOffered(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(dir)+`tools:
  disabled: [call_cli]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v", runtime.Options.ToolOrder)
	}
	if slices.ContainsFunc(runtime.Profile.Tools, func(tool poo.ToolInfo) bool { return tool.Name == "call_cli" }) {
		t.Error("the profile still offers the disabled registry tool")
	}
	// The profile's own tools are untouched, so the setting removes the
	// name it lists and nothing else.
	if !hasTool(runtime.Options.ToolOrder, "bash") {
		t.Errorf("the session dropped a tool the settings did not disable: %v", runtime.Options.ToolOrder)
	}
}

// TestADisabledSideEffectingToolEarnsNoRegistryTool covers the order the
// registry joins in: a registered CLI can change anything, so it comes
// with a profile that already offers a side-effecting tool. A profile
// whose only such tool the settings disable has none left, and earns no
// call_cli.
func TestADisabledSideEffectingToolEarnsNoRegistryTool(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(dir)+`tools:
  disabled: [edit]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	files := profile.Profile{Name: "files", Tools: []string{"edit", "read"}}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), files},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}

	var listed []string
	for _, p := range runtime.Profiles {
		if p.Name != files.Name {
			continue
		}
		for _, tool := range p.Tools {
			listed = append(listed, tool.Name)
		}
	}
	if len(listed) == 0 {
		t.Fatalf("the binary does not offer the %q profile", files.Name)
	}
	if hasTool(listed, "call_cli") {
		t.Errorf("the %q profile offers %v, and its only side-effecting tool is disabled", files.Name, listed)
	}
}

// TestTheSettingsRefuseToDisableAToolTheBinaryLacks covers the rule the
// profile's own tool names follow: a disabled name the binary does not
// link is a failure, because no profile could ever honour it.
func TestTheSettingsRefuseToDisableAToolTheBinaryLacks(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+`tools:
  disabled: [wget]
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = readOnlyBuild(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a binary without wget accepted the disabled name: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "tools.disabled" {
		t.Errorf("the failure is %+v, want unsupported on tools.disabled", failure)
	}
	if !strings.Contains(failure.Message, "wget") {
		t.Errorf("the message %q does not name the tool", failure.Message)
	}
}

// TestTheSettingsRefuseToDisableACoreTool covers the tools that are not the
// profile's to give up: the core adds them itself, so a list that names one
// is a failure rather than a setting nothing can remove.
func TestTheSettingsRefuseToDisableACoreTool(t *testing.T) {
	for _, name := range config.CoreToolNames {
		t.Run(name, func(t *testing.T) {
			settings, err := config.Parse([]byte(theModelSettings+"tools:\n  disabled: ["+name+"]\n"), config.FormatYAML)
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
			var failure *config.Error
			if !errors.As(err, &failure) {
				t.Fatalf("the build disabled the core's %q tool: %v", name, err)
			}
			if failure.Code != config.CodeUnsupported || failure.Field != "tools.disabled" {
				t.Errorf("the failure is %+v, want unsupported on tools.disabled", failure)
			}
			if !strings.Contains(failure.Message, name) {
				t.Errorf("the message %q does not name the tool", failure.Message)
			}
		})
	}
}

// askClient records the asks a policy makes.
type askClient struct {
	asks []perm.Ask
}

func (c *askClient) Connected() bool         { return true }
func (c *askClient) PermissionAnswers() bool { return true }
func (c *askClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	c.asks = append(c.asks, ask)
	return perm.DecisionAllow, nil
}

// TestThePolicyGetsTheResolvedWorkingDirectory covers the build's
// working directory: a consumer that leaves BuildOptions.WorkingDir
// empty still gets the directory the tools work in.
func TestThePolicyGetsTheResolvedWorkingDirectory(t *testing.T) {
	settings, err := config.Parse([]byte(theModelSettings+"permission:\n  unmatched: ask\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	client := &askClient{}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
		Askers:         []string{"wire"},
		Wire:           client,
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if _, err := runtime.Policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	}); err != nil {
		t.Fatalf("asking the policy: %v", err)
	}
	if len(client.asks) != 1 {
		t.Fatalf("the policy asked %d time(s), want 1", len(client.asks))
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if client.asks[0].WorkingDir != want {
		t.Errorf("the ask's working directory is %q, want the resolved %q", client.asks[0].WorkingDir, want)
	}
}
