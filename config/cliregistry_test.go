package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/code"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// theRegistrySettings is the settings the registry tests build from: a
// model, a chat role, and a registry the case names.
func registrySettings(paths ...string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, fmt.Sprintf("%q", path))
	}
	return theModelSettings + "cli_registry:\n  paths: [" + strings.Join(quoted, ", ") + "]\n"
}

// withDecider is a settings document with a chat model that also serves
// the decider role.
const withDecider = `
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  test-model:
    endpoint: test
    model_id: test-model
    ops: [chat, systemone, decisions]
    context_limit: 32768
roles:
  chat: test-model
  decider: test-model
`

// registry writes a registry with the named entries and returns its
// directory. The commands are the registry's own business: no case calls
// one.
func registry(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	tools := map[string]any{}
	for _, name := range names {
		tools[name] = map[string]any{
			"description": "The " + name + " command",
			"command":     []string{"true"},
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		}
	}
	body, err := json.Marshal(map[string]any{"version": 1, "tools": tools})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	return dir
}

// hasTool reports whether a tool order holds a name.
func hasTool(order []string, name string) bool {
	for _, tool := range order {
		if tool == name {
			return true
		}
	}
	return false
}

// TestARegistryReachesTheSessionAndItsPrompt covers the settings block: a
// registry the settings name is loaded when the session is built, the
// profile offers the tool that calls it, and the prompt carries the
// catalog.
func TestARegistryReachesTheSessionAndItsPrompt(t *testing.T) {
	dir := registry(t, "deploy", "logs")
	settings, err := config.Parse([]byte(registrySettings(dir)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if runtime.Registry == nil {
		t.Fatal("the build loaded no registry")
	}
	if runtime.Registry.Len() != 2 {
		t.Errorf("the registry holds %d entries, want 2", runtime.Registry.Len())
	}
	if !hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
	// Two entries are short enough to list, so nothing searches them and
	// no decision model is configured.
	if hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session offers find_tool over a catalog of two entries")
	}

	text := poo.PromptBuilder(runtime.Options).Render()
	for _, want := range []string{"deploy: The deploy command", "logs: The logs command"} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt %q does not carry %q", text, want)
		}
	}
}

// TestALargeRegistryIsSearchedInsteadOfListed covers the search: with a
// decision model and more entries than the prompt should hold, the
// session offers find_tool and the catalog section says so instead of
// listing the entries.
func TestALargeRegistryIsSearchedInsteadOfListed(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		names = append(names, fmt.Sprintf("entry-%03d", i))
	}
	dir := registry(t, names...)
	// The decider role needs a model that answers systemone questions.
	settings, err := config.Parse([]byte(withDecider+"cli_registry:\n  paths: [\""+dir+"\"]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if !hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
	if !hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session offers %v, and not find_tool", runtime.Options.ToolOrder)
	}
	// The search asks the session's decision model, so the session has
	// one.
	if runtime.Options.Decider == nil {
		t.Error("the session has no decision model to search with")
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "find_tool") {
		t.Errorf("the prompt does not name the search: %q", text)
	}
	if strings.Contains(text, "entry-000:") {
		t.Errorf("the prompt lists the catalog it should search: %q", text)
	}
}

// withoutDeciderModel is withDecider's twin: the decider role names a
// model the catalog does not hold, so the settings ask for a decider and
// the session does not get one.
const withoutDeciderModel = `
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
models:
  test-model:
    endpoint: test
    model_id: test-model
    ops: [chat, systemone, decisions]
    context_limit: 32768
roles:
  chat: test-model
  decider: missing-model
`

// TestASearchNeedsTheSessionsDecider covers the gate: the search is
// offered, and the catalog hidden behind it, only when the session has
// the decider the search asks, not when the settings name a decider
// role the catalog cannot resolve.
func TestASearchNeedsTheSessionsDecider(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		names = append(names, fmt.Sprintf("entry-%03d", i))
	}
	dir := registry(t, names...)
	settings, err := config.Parse([]byte(withoutDeciderModel+"cli_registry:\n  paths: [\""+dir+"\"]\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if runtime.Options.Decider != nil {
		t.Fatal("the session resolved a decider, so the case proves nothing")
	}
	if !hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session offers find_tool without a decider to ask: %v", runtime.Options.ToolOrder)
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "entry-000:") {
		t.Error("the prompt does not list the catalog the session can call")
	}
	if strings.Contains(text, "find_tool") {
		t.Error("the prompt names a search the session does not offer")
	}

	// An unknown name points at the search only when there is one.
	tool := runtime.Options.Tools[cliregistry.CallToolName]
	if tool == nil {
		t.Fatal("the session has no call_cli tool")
	}
	_, err = tool.Prepare(json.RawMessage(`{"name":"nope"}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("an unknown name prepared a call")
	}
	if strings.Contains(err.Error(), "search it") {
		t.Errorf("the hint points at a search the session does not offer: %q", err)
	}
	if !strings.Contains(err.Error(), "200 entries") {
		t.Errorf("the hint %q does not say how long the catalog is", err)
	}
}

// TestAReadOnlyProfileStaysReadOnly covers which profiles the registry
// joins: the tools come with the profiles that already offer a
// side-effecting one, so a session that run under the read-only profile
// offers nothing that can change anything.
func TestAReadOnlyProfileStaysReadOnly(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: search\n"), config.FormatYAML)
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
		t.Fatalf("building the runtime: %v", err)
	}
	if hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the search session offers %v", runtime.Options.ToolOrder)
	}
	offered := map[string][]string{}
	for _, p := range runtime.Profiles {
		for _, tool := range p.Tools {
			offered[p.Name] = append(offered[p.Name], tool.Name)
		}
	}
	if !hasTool(offered["code"], "call_cli") {
		t.Errorf("the code profile offers %v, and not call_cli", offered["code"])
	}
	if hasTool(offered["search"], "call_cli") {
		t.Errorf("the search profile offers %v", offered["search"])
	}
	// A session that cannot call a registered CLI says nothing about the
	// catalog.
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "deploy: The deploy command") {
		t.Errorf("the prompt lists a catalog the session cannot call: %q", text)
	}
}

// TestAResumedSessionKeepsOneCatalog covers the resume: a session that
// recorded its prompt keeps that prompt whole, so the sections the build
// installs are not applied a second time on top of it.
func TestAResumedSessionKeepsOneCatalog(t *testing.T) {
	dir := registry(t, "deploy", "logs")
	settings, err := config.Parse([]byte(registrySettings(dir)+"session:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})
	defer runtime.Store.Close()
	runtime.Options.Transport = pootest.NewTransport(pootest.Turn{Text: "done"})

	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  "s1",
			Mode:       "logged",
			CreatedAt:  "2026-10-22T12:00:00Z",
			WorkingDir: runtime.Options.WorkingDir,

			Profile: "code",
			Tools:   runtime.Profile.Tools, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	session, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	if _, err := session.Prompt(context.Background(), "say hello"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	resumed, err := runtime.ResumeSession(context.Background(), runtime.Store, "s1")
	if err != nil {
		t.Fatalf("resuming the session: %v", err)
	}
	defer resumed.Session.Close()
	if got := strings.Count(resumed.Session.PromptState().Text, "Registered CLIs:"); got != 1 {
		t.Errorf("the resumed prompt carries the catalog %d times, want once", got)
	}
}

// TestTheOperatorsCatalogSectionWins covers the order: the automatic
// catalog is the default, and an explicit `sections.catalog` is the
// operator's text, so it is the one the prompt carries.
func TestTheOperatorsCatalogSectionWins(t *testing.T) {
	dir := registry(t, "deploy", "logs")
	settings, err := config.Parse([]byte(registrySettings(dir)+`system_prompt:
  sections:
    catalog: the operator's own catalog
`), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "the operator's own catalog") {
		t.Error("the prompt does not carry the operator's catalog section")
	}
	if strings.Contains(text, "Registered CLIs:") {
		t.Error("the prompt carries the automatic catalog too")
	}
}

// TestTheFindThresholdIsThirtyEntries covers the boundary: a catalog the
// prompt can show whole is listed, and one entry more is searched.
func TestTheFindThresholdIsThirtyEntries(t *testing.T) {
	tests := []struct {
		entries int
		search  bool
	}{
		{entries: 30, search: false},
		{entries: 31, search: true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d entries", tt.entries), func(t *testing.T) {
			names := make([]string, 0, tt.entries)
			for i := 0; i < tt.entries; i++ {
				names = append(names, fmt.Sprintf("entry-%03d", i))
			}
			settings, err := config.Parse([]byte(withDecider+"cli_registry:\n  paths: [\""+registry(t, names...)+"\"]\n"), config.FormatYAML)
			if err != nil {
				t.Fatalf("decoding the settings: %v", err)
			}
			runtime := build(t, settings, config.Overrides{})

			if got := hasTool(runtime.Options.ToolOrder, "find_tool"); got != tt.search {
				t.Errorf("the session offers find_tool: %v, want %v", got, tt.search)
			}
			text := poo.PromptBuilder(runtime.Options).Render()
			if listed := strings.Contains(text, "entry-000:"); listed == tt.search {
				t.Errorf("the prompt lists the catalog: %v, and the session searches it: %v", listed, tt.search)
			}
			if got := strings.Count(text, "Registered CLIs:"); got != 1 {
				t.Errorf("the prompt carries the catalog %d times, want once", got)
			}
		})
	}
}

// TestARegistryPathIsRelativeToTheWorkingDirectory covers the path: a
// relative `cli_registry.paths` entry is resolved against the directory
// the session works in, not the directory the process was started in.
func TestARegistryPathIsRelativeToTheWorkingDirectory(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings("registry.json")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     dir,
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if runtime.Registry == nil || runtime.Registry.Len() != 1 {
		t.Fatalf("the build loaded %v, want the one entry beside the working directory", runtime.Registry)
	}
	if !hasTool(runtime.Options.ToolOrder, "call_cli") {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
}

// TestARegistryPathThatIsNotThereNamesTheField covers a path the load
// cannot read: the failure is the settings field the operator wrote.
func TestARegistryPathThatIsNotThereNamesTheField(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "registry.json")
	settings, err := config.Parse([]byte(registrySettings(missing)), config.FormatYAML)
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
		t.Fatal("the build accepted a registry path that is not there")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the failure is %T, want a config error", err)
	}
	if failure.Field != "cli_registry.paths" {
		t.Errorf("the failure names %q, want cli_registry.paths", failure.Field)
	}
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("the failure %q does not keep the cause", err)
	}
}

// TestARegistryThatDoesNotLoadIsAnError covers the settings check: a
// registry the settings name and the binary cannot load fails the build,
// and the failure names the field the operator wrote.
func TestARegistryThatDoesNotLoadIsAnError(t *testing.T) {
	one := registry(t, "deploy")
	two := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(one, two)), config.FormatYAML)
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
		t.Fatal("the build accepted a registry it cannot load")
	}
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Errorf("the failure is %T, want a config error", err)
	}
	if failure.Field != "cli_registry.paths" {
		t.Errorf("the failure names %q, want cli_registry.paths", failure.Field)
	}
}

// TestARegistryWithoutCallCLIIsRefused covers the settings' requirement:
// a catalog is useless to a binary that links no tool over it, so the
// paths are refused by name even when the running profile is read-only.
func TestARegistryWithoutCallCLIIsRefused(t *testing.T) {
	dir := registry(t, "deploy")
	settings, err := config.Parse([]byte(registrySettings(dir)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = readOnlyBuild(t, settings, nil)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a binary without call_cli accepted cli_registry.paths: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "cli_registry.paths" {
		t.Errorf("the failure is %+v, want unsupported on cli_registry.paths", failure)
	}
	if !strings.Contains(failure.Message, "call_cli") {
		t.Errorf("the message %q does not name the tool", failure.Message)
	}
}

// TestAPromptDirectoriesCatalogFollowsTheSession covers the catalog a
// directory supplies: the file loads for one directory, and its text
// applies only when the session offers a tool that reads the catalog.
func TestAPromptDirectoriesCatalogFollowsTheSession(t *testing.T) {
	dir := registry(t, "deploy")
	prompts := fstest.MapFS{
		"catalog.md": {Data: []byte("Call {{.Call}} with the registered CLIs.\n")},
	}
	buildPrompt := func(body string, readonly bool) *config.Runtime {
		t.Helper()
		settings, err := config.Parse([]byte(body), config.FormatYAML)
		if err != nil {
			t.Fatalf("decoding the settings: %v", err)
		}
		profiles := []profile.Profile{base.Profile(), code.Profile(), search.Profile()}
		runtime, err := config.Build(config.BuildOptions{
			Settings:       settings,
			WorkingDir:     t.TempDir(),
			DefaultProfile: "code",
			Tools:          theTools(),
			Profiles:       profiles,
			Executors:      theExecutors(),
			PromptFS:       prompts,
		})
		if err != nil {
			t.Fatalf("building the runtime: %v", err)
		}
		if readonly == hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
			t.Fatalf("the session offers %v, and the case says readonly=%v", runtime.Options.ToolOrder, readonly)
		}
		return runtime
	}

	code := buildPrompt(registrySettings(dir), false)
	if text := poo.PromptBuilder(code.Options).Render(); !strings.Contains(text, "Call call_cli with the registered CLIs.") {
		t.Errorf("the code session's prompt does not carry the directory's catalog: %q", text)
	}

	readonly := buildPrompt(registrySettings(dir)+"profile: search\n", true)
	text := poo.PromptBuilder(readonly.Options).Render()
	if strings.Contains(text, "Call call_cli") {
		t.Errorf("the read-only session's prompt carries the directory's catalog: %q", text)
	}
	if strings.Contains(text, "Registered CLIs:") {
		t.Errorf("the read-only session's prompt carries the embedded catalog: %q", text)
	}
}
