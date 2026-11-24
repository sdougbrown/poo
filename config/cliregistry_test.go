package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// registryEntry is one registered tool. The commands are the registry's own
// business: no case calls one.
func registryEntry(description string) map[string]any {
	return map[string]any{
		"description": description,
		"command":     []string{"true"},
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	}
}

// writeRegistryDoc writes a registry document and returns its directory.
func writeRegistryDoc(t *testing.T, doc map[string]any) string {
	t.Helper()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	return dir
}

// registry writes a registry with the named entries and returns its
// directory.
func registry(t *testing.T, names ...string) string {
	t.Helper()
	tools := map[string]any{}
	for _, name := range names {
		tools[name] = registryEntry("The " + name + " command")
	}
	return writeRegistryDoc(t, map[string]any{"version": 1, "tools": tools})
}

// gatedRegistry writes a registry of one entry that reads and one the rules
// gate on the profile condition poo supplies itself.
func gatedRegistry(t *testing.T, conditions map[string]any) string {
	t.Helper()
	return writeRegistryDoc(t, map[string]any{
		"version": 1,
		"tools": map[string]any{
			"pr-view": registryEntry("Read a pull request"),
			"pr-edit": registryEntry("Edit a pull request"),
		},
		"availability": map[string]any{
			"conditions": conditions,
			"rules": []any{map[string]any{
				"type": "enabledWhen", "field": "pr-edit",
				"when":   map[string]any{"op": "cond", "condition": "profile_side_effecting"},
				"reason": "the profile is read-only",
			}},
		},
	})
}

// profileCondition is the condition set poo supplies on its own.
func profileCondition() map[string]any {
	return map[string]any{
		"profile_side_effecting": map[string]any{"type": "boolean"},
	}
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
// registry the settings name is loaded when the session is built, and its
// entries become tools of their own, in name order.
//
// Two entries are short enough to show, so each one is offered directly: the
// model reads the tool's own name and arguments instead of a catalog line, and
// the call tool and the catalog's section would only say the same thing twice.
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
	order := runtime.Options.ToolOrder
	for _, name := range []string{"deploy", "logs"} {
		if !hasTool(order, name) {
			t.Errorf("the session offers %v, and not the %q entry", order, name)
		}
		tool := runtime.Options.Tools[name]
		if tool == nil {
			t.Fatalf("the session offers %q and holds no tool for it", name)
		}
		spec := tool.Spec()
		if spec.Description != "The "+name+" command" {
			t.Errorf("the %q tool describes itself as %q", name, spec.Description)
		}
		if spec.Class != poo.ClassSideEffecting {
			t.Errorf("the %q tool is %q, want side-effecting", name, spec.Class)
		}
		if spec.Background != poo.BackgroundAllowed {
			t.Errorf("the %q tool may run in the background: %q", name, spec.Background)
		}
	}
	deploy, logs := slices.Index(order, "deploy"), slices.Index(order, "logs")
	if deploy < 0 || logs < 0 || deploy > logs {
		t.Errorf("the session offers the entries in the order %v, want deploy before logs", order)
	}
	if hasTool(order, cliregistry.CallToolName) {
		t.Errorf("the session offers call_cli beside the entries it offers directly: %v", order)
	}
	if hasTool(order, "find_tool") {
		t.Errorf("the session offers find_tool over a catalog of two entries: %v", order)
	}
	// The profile the listing shows carries the same tools the session does.
	var listed []string
	for _, tool := range runtime.Profile.Tools {
		listed = append(listed, tool.Name)
	}
	for _, name := range []string{"deploy", "logs"} {
		if !hasTool(listed, name) {
			t.Errorf("the profile offers %v, and not the %q entry", listed, name)
		}
	}

	text := poo.PromptBuilder(runtime.Options).Render()
	for _, unwanted := range []string{"Registered tools:", "deploy: The deploy command"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("the prompt carries %q, and the model reads the tools themselves: %q", unwanted, text)
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
	_, err = tool.Prepare(json.RawMessage(`{"command":"nope"}`), poo.PrepareEnv{})
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
	if !hasTool(offered["code"], "deploy") {
		t.Errorf("the code profile offers %v, and not the catalog's entry", offered["code"])
	}
	if hasTool(offered["code"], cliregistry.CallToolName) {
		t.Errorf("the code profile offers call_cli beside the entry it offers directly: %v", offered["code"])
	}
	if hasTool(offered["search"], "deploy") {
		t.Errorf("the search profile offers %v", offered["search"])
	}
	// A session that cannot call a registered tool says nothing about the
	// catalog.
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "deploy: The deploy command") {
		t.Errorf("the prompt lists a catalog the session cannot call: %q", text)
	}
}

// TestAResumedSessionKeepsOneCatalog covers the resume: a session that
// recorded its prompt keeps that prompt whole, so the sections the build
// installs are not applied a second time on top of it.
//
// The catalog here is one the session searches, because that is the shape
// whose section the build installs; an elevated catalog has no section to
// apply twice.
func TestAResumedSessionKeepsOneCatalog(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		names = append(names, fmt.Sprintf("entry-%03d", i))
	}
	dir := registry(t, names...)
	settings, err := config.Parse([]byte(withDecider+"cli_registry:\n  paths: [\""+dir+"\"]\nsession:\n  mode: logged\n  dir: "+t.TempDir()+"\n"), config.FormatYAML)
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
	if got := strings.Count(resumed.Session.PromptState().Text, "Registered tools:"); got != 1 {
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
	if strings.Contains(text, "Registered tools:") {
		t.Error("the prompt carries the automatic catalog too")
	}
}

// TestTheFindThresholdIsThirtyEntries covers the boundary: a catalog of thirty
// entries is short enough to offer entry by entry, and one entry more is
// searched instead.
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

			order := runtime.Options.ToolOrder
			if got := hasTool(order, "find_tool"); got != tt.search {
				t.Errorf("the session offers find_tool: %v, want %v", got, tt.search)
			}
			// The count decides the shape: at the threshold the entries are
			// tools of their own, and past it the catalog is behind the call.
			if got := hasTool(order, "entry-000"); got == tt.search {
				t.Errorf("the session offers the first entry: %v, want %v", got, !tt.search)
			}
			if got := hasTool(order, cliregistry.CallToolName); got != tt.search {
				t.Errorf("the session offers call_cli: %v, want %v", got, tt.search)
			}
			// Neither shape lists the entries in the prompt: the elevated one
			// reads them from the tool list, and the searched one asks for them.
			text := poo.PromptBuilder(runtime.Options).Render()
			if strings.Contains(text, "entry-000:") {
				t.Errorf("the prompt lists the catalog: %q", text)
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
	if !hasTool(runtime.Options.ToolOrder, "deploy") {
		t.Errorf("the session offers %v, and not the catalog's entry", runtime.Options.ToolOrder)
	}
}

// TestARegistryPathMayNameTheHomeDirectory covers the path: a
// `cli_registry.paths` entry that starts with ~ is resolved against the
// home directory, not the directory the session works in.
func TestARegistryPathMayNameTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "reg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("making the registry directory: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"version": 1,
		"tools":   map[string]any{"deploy": registryEntry("The deploy command")},
	})
	if err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), body, 0o600); err != nil {
		t.Fatalf("writing the registry: %v", err)
	}
	settings, err := config.Parse([]byte(registrySettings("~/reg")), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: "code",
		Tools:          theTools(),
		Profiles:       theProfiles(),
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if runtime.Registry == nil || runtime.Registry.Len() != 1 {
		t.Fatalf("the build loaded %v, want the one entry under the home directory", runtime.Registry)
	}
	if !hasTool(runtime.Options.ToolOrder, "deploy") {
		t.Errorf("the session offers %v, and not the catalog's entry", runtime.Options.ToolOrder)
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
// applies only when the session lists the catalog the file describes.
func TestAPromptDirectoriesCatalogFollowsTheSession(t *testing.T) {
	// The catalog is longer than a prompt shows, and the session has no
	// decision model, so it lists the whole catalog behind the call tool.
	names := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		names = append(names, fmt.Sprintf("entry-%03d", i))
	}
	dir := registry(t, names...)
	prompts := fstest.MapFS{
		"catalog.md": {Data: []byte("Call {{.Call}} with the registered tools.\n")},
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
	if text := poo.PromptBuilder(code.Options).Render(); !strings.Contains(text, "Call call_cli with the registered tools.") {
		t.Errorf("the code session's prompt does not carry the directory's catalog: %q", text)
	}

	readonly := buildPrompt(registrySettings(dir)+"profile: search\n", true)
	text := poo.PromptBuilder(readonly.Options).Render()
	if strings.Contains(text, "Call call_cli") {
		t.Errorf("the read-only session's prompt carries the directory's catalog: %q", text)
	}
	if strings.Contains(text, "Registered tools:") {
		t.Errorf("the read-only session's prompt carries the embedded catalog: %q", text)
	}
}

// TestAPromptDirectoriesCatalogLoadsWithoutARegistry covers the binary that
// links the registry's tools and runs a session whose settings name no
// registry: the catalog section is one the binary knows, so a directory
// carrying catalog.md loads, and the session offers none of it, so the section
// renders nothing.
func TestAPromptDirectoriesCatalogLoadsWithoutARegistry(t *testing.T) {
	prompts := fstest.MapFS{
		"catalog.md": {Data: []byte("Call {{.Call}} with the registered tools.\n")},
	}
	buildPrompt := func(t *testing.T, tools map[string]config.ToolFactory, profileName string, profiles []profile.Profile) (*config.Runtime, error) {
		t.Helper()
		settings, err := config.Parse([]byte(theModelSettings), config.FormatYAML)
		if err != nil {
			t.Fatalf("decoding the settings: %v", err)
		}
		return config.Build(config.BuildOptions{
			Settings:       settings,
			WorkingDir:     t.TempDir(),
			DefaultProfile: profileName,
			Tools:          tools,
			Profiles:       profiles,
			Executors:      theExecutors(),
			PromptFS:       prompts,
		})
	}
	profiles := []profile.Profile{base.Profile(), code.Profile(), search.Profile()}

	runtime, err := buildPrompt(t, theTools(), "code", profiles)
	if err != nil {
		t.Fatalf("a binary that links call_cli could not load catalog.md for a session naming no registry: %v", err)
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if strings.Contains(text, "Call call_cli") || strings.Contains(text, "Registered tools:") {
		t.Errorf("a session that offers no catalog renders one: %q", text)
	}

	// The section belongs to the binaries that link the tools: one that does
	// not still has no catalog for a directory to replace.
	if _, err := buildPrompt(t, readOnlyTools(), search.Name, []profile.Profile{search.Profile()}); err == nil || !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("a binary that links no call_cli accepted catalog.md: %v", err)
	}
	// A nil factory is how a binary declares a tool it does not link, so the
	// section stays unknown to it too.
	nilCall := theTools()
	nilCall[cliregistry.CallToolName] = nil
	if _, err := buildPrompt(t, nilCall, "code", profiles); err == nil || !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("a binary whose call_cli factory is nil accepted catalog.md: %v", err)
	}
}

// gatedBuild builds a session whose settings name the gated registry, with the
// profiles a real binary links.
func gatedBuild(t *testing.T, dir, profileName string) (*config.Runtime, error) {
	t.Helper()
	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: "+profileName+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: profileName,
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), search.Profile()},
		Executors:      theExecutors(),
	})
}

// thresholdRegistry writes a registry of entries entries, the first gated of
// which the profile condition gates: a read-only profile is left with the rest.
func thresholdRegistry(t *testing.T, entries, gated int) string {
	t.Helper()
	tools := map[string]any{}
	rules := make([]any, 0, gated)
	for i := 0; i < entries; i++ {
		name := fmt.Sprintf("entry-%03d", i)
		tools[name] = registryEntry("The " + name + " command")
		if i >= gated {
			continue
		}
		rules = append(rules, map[string]any{
			"type": "enabledWhen", "field": name,
			"when":   map[string]any{"op": "cond", "condition": "profile_side_effecting"},
			"reason": "the profile is read-only",
		})
	}
	return writeRegistryDoc(t, map[string]any{
		"version": 1,
		"tools":   tools,
		"availability": map[string]any{
			"conditions": profileCondition(),
			"rules":      rules,
		},
	})
}

// TestAReadOnlyProfileSearchesOnlyWhatItsRulesLeave covers the search over a
// narrowed catalog: the rules leave a read-only profile fewer entries than the
// threshold, so the session lists them and offers no search, however many
// entries the manifests loaded.
func TestAReadOnlyProfileSearchesOnlyWhatItsRulesLeave(t *testing.T) {
	dir := thresholdRegistry(t, 40, 35)
	runtime, err := buildGated(t, dir, search.Name, theTools())
	if err != nil {
		t.Fatalf("building the read-only runtime: %v", err)
	}
	// The session has the decider a search would ask, so the count the
	// session searches is the only thing keeping the search off.
	if runtime.Options.Decider == nil {
		t.Fatal("the case has no decider, so it proves nothing about the threshold")
	}
	if runtime.Registry == nil || runtime.Registry.Len() != 5 {
		t.Fatalf("the read-only session offers %v, want the five entries the rules leave", runtime.Registry)
	}
	// The listing's search is the entries' own: the read-only profile is left
	// with five of them, which is short enough to offer entry by entry.
	if !hasTool(runtime.Options.ToolOrder, "entry-035") {
		t.Errorf("the read-only session offers %v, and not the entries the rules leave", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the read-only session offers call_cli beside the entries it offers directly: %v", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session searches a catalog narrowed to five entries: %v", runtime.Options.ToolOrder)
	}
	for i := 35; i < 40; i++ {
		if name := fmt.Sprintf("entry-%03d", i); !hasTool(runtime.Options.ToolOrder, name) {
			t.Errorf("the session offers %v, and not the entry the rules leave: %s", runtime.Options.ToolOrder, name)
		}
	}
	for i := 0; i < 35; i++ {
		if name := fmt.Sprintf("entry-%03d", i); hasTool(runtime.Options.ToolOrder, name) {
			t.Errorf("the session offers the entry %s the rules withhold: %v", name, runtime.Options.ToolOrder)
		}
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if strings.Contains(text, "Registered tools:") {
		t.Errorf("the prompt lists a catalog the session offers natively: %q", text)
	}

	// The listing decides the search the same way the session does: the
	// same profile, under its own fact, agrees about the tool.
	assertListedFind(t, runtime, search.Name, false)
	writeCapable, err := buildGated(t, dir, code.Name, theTools())
	if err != nil {
		t.Fatalf("building the write-capable runtime: %v", err)
	}
	assertListedFind(t, writeCapable, code.Name, true)
}

// assertListedFind checks the search a listed profile offers.
func assertListedFind(t *testing.T, runtime *config.Runtime, profileName string, want bool) {
	t.Helper()
	for _, listed := range runtime.Profiles {
		if listed.Name != profileName {
			continue
		}
		names := make([]string, 0, len(listed.Tools))
		for _, tool := range listed.Tools {
			names = append(names, tool.Name)
		}
		if got := hasTool(names, "find_tool"); got != want {
			t.Errorf("the listing offers %q the tools %v, and its search is %v", profileName, names, got)
		}
		return
	}
	t.Fatalf("the listing offers %v, and not the %q profile", runtime.Profiles, profileName)
}

// TestASearchIsRequiredOnlyOverAWholeCatalog covers the link requirement: a
// binary that links call_cli alone serves a registry its rules narrow below the
// threshold, and is refused for one that leaves more entries than a prompt
// shows.
func TestASearchIsRequiredOnlyOverAWholeCatalog(t *testing.T) {
	runtime, err := buildGated(t, thresholdRegistry(t, 40, 35), search.Name, callOnlyTools())
	if err != nil {
		t.Fatalf("a binary linking call_cli alone was refused a registry narrowed to five entries: %v", err)
	}
	if !hasTool(runtime.Options.ToolOrder, "entry-035") {
		t.Errorf("the session offers %v, and not the entries the rules leave", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the session offers call_cli beside the entries it offers directly: %v", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session offers a search the binary does not link: %v", runtime.Options.ToolOrder)
	}

	// The same binary over a catalog the rules leave whole: forty entries are
	// more than a prompt shows, so the session needs the search it lacks.
	_, err = buildGated(t, thresholdRegistry(t, 40, 0), code.Name, callOnlyTools())
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a binary linking no find_tool accepted a forty-entry catalog: %v", err)
	}
	if failure.Code != config.CodeUnsupported || failure.Field != "cli_registry.paths" {
		t.Errorf("the failure is %+v, want unsupported on cli_registry.paths", failure)
	}
	if !strings.Contains(failure.Message, "find_tool") {
		t.Errorf("the message %q does not name the tool", failure.Message)
	}
}

// buildGatedDoc builds a runtime from the settings document doc, which carries
// the model, over the registry at dir, with the tools the binary links and the
// decider a search would ask.
func buildGatedDoc(t *testing.T, doc, dir, profileName string, tools map[string]config.ToolFactory) (*config.Runtime, error) {
	t.Helper()
	settings, err := config.Parse([]byte(doc+"cli_registry:\n  paths: [\""+dir+"\"]\nprofile: "+profileName+"\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: profileName,
		Tools:          tools,
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), search.Profile()},
		Executors:      theExecutors(),
	})
}

// buildGated builds a runtime over the registry at dir, with the tools the
// binary links and the decider a search would ask.
func buildGated(t *testing.T, dir, profileName string, tools map[string]config.ToolFactory) (*config.Runtime, error) {
	t.Helper()
	return buildGatedDoc(t, withDecider, dir, profileName, tools)
}

// TestADisabledSearchLeavesTheCatalogListed covers the operator's disabled
// list: a session that turns find_tool off over a long catalog lists the
// catalog whole, and never points the model at a search it does not offer.
func TestADisabledSearchLeavesTheCatalogListed(t *testing.T) {
	runtime, err := buildGatedDoc(t, withDecider+"tools:\n  disabled: [find_tool]\n", thresholdRegistry(t, 40, 0), code.Name, theTools())
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if hasTool(runtime.Options.ToolOrder, "find_tool") {
		t.Errorf("the session offers a search the settings disable: %v", runtime.Options.ToolOrder)
	}
	text := poo.PromptBuilder(runtime.Options).Render()
	if !strings.Contains(text, "Call one with call_cli") {
		t.Errorf("the catalog is not listed for a session that cannot search: %q", text)
	}
	if strings.Contains(text, "too many to list") {
		t.Errorf("the prompt names a search the session does not offer: %q", text)
	}

	// The call tool's hint follows the same fact as the prompt.
	call := runtime.Options.Tools[cliregistry.CallToolName]
	if call == nil {
		t.Fatal("the session has no call_cli tool")
	}
	_, err = call.Prepare(json.RawMessage(`{"command":"nope"}`), poo.PrepareEnv{})
	if err == nil {
		t.Fatal("an unknown name prepared a call")
	}
	if strings.Contains(err.Error(), "search it") {
		t.Errorf("the hint points at a search the session does not offer: %q", err)
	}
	if !strings.Contains(err.Error(), "call one the prompt lists") {
		t.Errorf("the hint %q does not point at the listed catalog", err)
	}
}

// callOnlyTools are the tools a binary links when it offers the catalog but no
// search over it.
func callOnlyTools() map[string]config.ToolFactory {
	tools := theTools()
	delete(tools, "find_tool")
	return tools
}

// TestAReadOnlyProfileIsOfferedTheEntriesItsRulesLeave covers the per-entry
// rule: a profile with no side-effecting tool earns the registry when the
// manifests declare conditions, and the catalog it offers holds the entries
// the rules leave. A profile that can already change things is offered every
// entry the rules leave for it.
func TestAReadOnlyProfileIsOfferedTheEntriesItsRulesLeave(t *testing.T) {
	dir := gatedRegistry(t, profileCondition())

	runtime, err := gatedBuild(t, dir, search.Name)
	if err != nil {
		t.Fatalf("building the read-only runtime: %v", err)
	}
	if runtime.Registry == nil {
		t.Fatal("the read-only build loaded no registry")
	}
	if got := strings.Join(runtime.Registry.Names(), ","); got != "pr-view" {
		t.Errorf("the read-only session offers %q, want pr-view alone", got)
	}
	// One entry is short enough to show, so it is a tool of its own: the entry
	// the rules withhold is not registered at all, and the one they leave is.
	if !hasTool(runtime.Options.ToolOrder, "pr-view") {
		t.Errorf("the read-only session offers %v, and not the entry the rules leave", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, "pr-edit") {
		t.Errorf("the read-only session offers the entry the rules withhold: %v", runtime.Options.ToolOrder)
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the read-only session offers call_cli beside the entry it offers directly: %v", runtime.Options.ToolOrder)
	}
	if _, ok := runtime.Registry.Lookup("pr-edit"); ok {
		t.Error("the read-only session can still name the gated entry")
	}
	// The tool the session builds reads the narrowed catalog, not the one the
	// manifests loaded: the withheld entry has no tool, and the kept one does.
	if runtime.Options.Tools["pr-edit"] != nil {
		t.Error("the built catalog offered the entry the rules withhold")
	}
	if runtime.Options.Tools["pr-view"] == nil {
		t.Error("the built catalog offered no tool for the entry the rules leave")
	}
	// The prompt says nothing about a catalog: the model reads the tools.
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "Registered tools:") {
		t.Errorf("the prompt lists a catalog the session offers natively: %q", text)
	}

	// The listing the binary offers says what the session does: the same
	// profile earns the same tool there.
	var listed *poo.Profile
	for i := range runtime.Profiles {
		if runtime.Profiles[i].Name == search.Name {
			listed = &runtime.Profiles[i]
			break
		}
	}
	if listed == nil {
		t.Fatalf("the listing offers %v, and not the %q profile", runtime.Profiles, search.Name)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if !hasTool(names, "pr-view") {
		t.Errorf("the listing offers the %q profile %v, and its session offers the entry pr-view", listed.Name, names)
	}
	if hasTool(names, "pr-edit") {
		t.Errorf("the listing offers the %q profile %v, and its session withholds pr-edit", listed.Name, names)
	}
	// A profile that offers no tools earns no command runner, whatever the
	// rules leave for the profiles beside it.
	for _, listed := range runtime.Profiles {
		if listed.Name != base.Profile().Name {
			continue
		}
		listedNames := make([]string, 0, len(listed.Tools))
		for _, tool := range listed.Tools {
			listedNames = append(listedNames, tool.Name)
		}
		if hasTool(listedNames, "pr-view") || hasTool(listedNames, cliregistry.CallToolName) {
			t.Errorf("the listing offers the %q profile %v", listed.Name, listedNames)
		}
	}

	// A profile that can change things gets every entry the rules leave.
	runtime, err = gatedBuild(t, dir, code.Name)
	if err != nil {
		t.Fatalf("building the read-write runtime: %v", err)
	}
	if got := strings.Join(runtime.Registry.Names(), ","); got != "pr-edit,pr-view" {
		t.Errorf("the read-write session offers %q, want both entries", got)
	}
}

// TestARegistryThatGatesNothingStaysClosedToAReadOnlyProfile covers the
// registry whose manifests declare no conditions: a registered tool can change
// anything, so a profile with no side-effecting tool is offered none of them.
func TestARegistryThatGatesNothingStaysClosedToAReadOnlyProfile(t *testing.T) {
	dir := registry(t, "deploy")
	runtime, err := gatedBuild(t, dir, search.Name)
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the read-only session offers %v", runtime.Options.ToolOrder)
	}
	// The build still holds the catalog the settings named, and neither the
	// prompt nor a tool reads it: the profile earns none.
	if runtime.Registry == nil || runtime.Registry.Len() != 1 {
		t.Fatalf("the read-only build holds %v, want the one entry", runtime.Registry)
	}
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "deploy") {
		t.Errorf("the read-only session's prompt lists the entry: %q", text)
	}

	// The same holds for a profile that names a registry tool itself: the
	// tools are the profile's, and the catalog they read is still none. Here
	// the rules leave nothing for a profile that can change things.
	dir = writeRegistryDoc(t, map[string]any{
		"version": 1,
		"tools":   map[string]any{"deploy": registryEntry("The deploy command")},
		"availability": map[string]any{
			"conditions": profileCondition(),
			"rules": []any{map[string]any{
				"type": "enabledWhen", "field": "deploy",
				"when": map[string]any{
					"op": "not",
					"expr": map[string]any{
						"op": "cond", "condition": "profile_side_effecting",
					},
				},
			}},
		},
	})
	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: files\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	files := profile.Profile{Name: "files", Tools: []string{"read", "call_cli"}, Unmatched: "allow"}
	runtime, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: files.Name,
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), search.Profile(), files},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime for a profile that names call_cli: %v", err)
	}
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "deploy") {
		t.Errorf("the prompt of a profile that names call_cli lists the entry: %q", text)
	}
}

// TestAProfileThatNamesCallCLIIsOfferedTheCatalogOnce covers the overlap: a
// profile that names the registry's own tool already offers it, so joining the
// catalog to that profile must not offer the tool a second time. The session and
// the listing both say call_cli once, and the catalog is still offered.
func TestAProfileThatNamesCallCLIIsOfferedTheCatalogOnce(t *testing.T) {
	dir := registry(t, "deploy", "logs")
	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: files\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	files := profile.Profile{Name: "files", Tools: []string{"read", "call_cli"}, Unmatched: "allow"}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: files.Name,
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), search.Profile(), files},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime for a profile that names call_cli: %v", err)
	}

	if got := countTool(runtime.Options.ToolOrder, cliregistry.CallToolName); got != 1 {
		t.Errorf("the session offers call_cli %d times in %v, want once", got, runtime.Options.ToolOrder)
	}
	var names []string
	for _, tool := range runtime.Profile.Tools {
		names = append(names, tool.Name)
	}
	if got := countTool(names, cliregistry.CallToolName); got != 1 {
		t.Errorf("the profile offers call_cli %d times in %v, want once", got, names)
	}

	// The listing says the same thing the session does.
	for _, listed := range runtime.Profiles {
		if listed.Name != files.Name {
			continue
		}
		listedNames := make([]string, 0, len(listed.Tools))
		for _, tool := range listed.Tools {
			listedNames = append(listedNames, tool.Name)
		}
		if got := countTool(listedNames, cliregistry.CallToolName); got != 1 {
			t.Errorf("the listing offers call_cli %d times in %v, want once", got, listedNames)
		}
	}

	// A profile that names the tool itself still earns the catalog it reads.
	if text := poo.PromptBuilder(runtime.Options).Render(); !strings.Contains(text, "deploy: The deploy command") {
		t.Errorf("the prompt of a profile that names call_cli lists no catalog: %q", text)
	}
}

// countTool counts the occurrences of a tool name.
func countTool(names []string, name string) int {
	count := 0
	for _, listed := range names {
		if listed == name {
			count++
		}
	}
	return count
}

// TestAnEntryNamedLikeAToolTheSessionOffersKeepsTheCallTool covers the
// collision: a manifest that names a tool the session already has keeps the
// call tool for the whole catalog, so the entry never shadows the tool the
// model was already offered.
func TestAnEntryNamedLikeAToolTheSessionOffersKeepsTheCallTool(t *testing.T) {
	dir := registry(t, "bash")
	settings, err := config.Parse([]byte(registrySettings(dir)), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	runtime := build(t, settings, config.Overrides{})

	if !hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
	bash := runtime.Options.Tools["bash"]
	if bash == nil {
		t.Fatal("the session offers no bash tool")
	}
	if spec := bash.Spec(); spec.Description == "The bash command" {
		t.Errorf("the catalog entry shadowed the bash tool the profile offers: %q", spec.Description)
	}
	if text := poo.PromptBuilder(runtime.Options).Render(); !strings.Contains(text, "Registered tools:") {
		t.Errorf("a session that keeps the call tool does not list the catalog: %q", text)
	}
}

// TestAnEntryNamedLikeAReservedToolKeepsTheCallTool covers the reserved names
// a profile need not offer: a manifest may not take a name poo's own tools
// answer to, even under a profile that links none of them.
func TestAnEntryNamedLikeAReservedToolKeepsTheCallTool(t *testing.T) {
	dir := registry(t, "fetch")
	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: files\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	files := profile.Profile{Name: "files", Tools: []string{"edit"}}
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: files.Name,
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile(), code.Profile(), files},
		Executors:      theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if runtime.Options.Tools["fetch"] != nil {
		t.Error("the session registered a catalog entry under a reserved name")
	}
	if !hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the session offers %v, and not call_cli", runtime.Options.ToolOrder)
	}
}

// TestARegistryThatWithholdsEveryEntryIsOfferedWhole covers the empty result of
// the rules: a profile with tools whose rules leave no entry earns no catalog,
// so the session offers neither the call tool nor a section.
func TestARegistryThatWithholdsEveryEntryIsOfferedWhole(t *testing.T) {
	runtime, err := gatedBuild(t, thresholdRegistry(t, 5, 5), search.Name)
	if err != nil {
		t.Fatalf("building the read-only runtime: %v", err)
	}
	if hasTool(runtime.Options.ToolOrder, cliregistry.CallToolName) {
		t.Errorf("the session offers %v, and the rules leave no entry", runtime.Options.ToolOrder)
	}
	if text := poo.PromptBuilder(runtime.Options).Render(); strings.Contains(text, "Registered tools:") {
		t.Errorf("the prompt carries a catalog the rules emptied: %q", text)
	}
	// The build still holds the catalog the settings named.
	if runtime.Registry == nil || runtime.Registry.Len() != 5 {
		t.Fatalf("the build holds %v, want the five entries the manifests loaded", runtime.Registry)
	}
}

// TestARegistryThatDeclaresAConditionPooCannotSupplyIsRefused covers the
// contract: poo supplies the profile fact and nothing else, so a manifest that
// reads another condition fails the build naming it, rather than judging an
// entry against a value nobody supplied.
func TestARegistryThatDeclaresAConditionPooCannotSupplyIsRefused(t *testing.T) {
	conditions := profileCondition()
	conditions["messaging_enabled"] = map[string]any{"type": "boolean"}
	dir := gatedRegistry(t, conditions)

	_, err := gatedBuild(t, dir, code.Name)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the build of a registry with an unsupplied condition is %v, want a settings failure", err)
	}
	if failure.Field != "cli_registry.paths" {
		t.Errorf("the failure names the field %q, want cli_registry.paths", failure.Field)
	}
	if !strings.Contains(failure.Message, "messaging_enabled") {
		t.Errorf("the message %q does not name the condition", failure.Message)
	}
	if !strings.Contains(failure.Message, config.ProfileSideEffectingCondition) {
		t.Errorf("the message %q does not name the condition poo supplies", failure.Message)
	}
}

// TestAConditionPooCannotSupplyIsRefusedWhateverTheProfile covers where the
// check runs: the declared set belongs to the manifests, so a build whose
// profiles offer no tools still refuses a condition poo cannot supply.
func TestAConditionPooCannotSupplyIsRefusedWhateverTheProfile(t *testing.T) {
	conditions := profileCondition()
	conditions["messaging_enabled"] = map[string]any{"type": "boolean"}
	dir := gatedRegistry(t, conditions)

	settings, err := config.Parse([]byte(registrySettings(dir)+"profile: base\n"), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	_, err = config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: base.Name,
		Tools:          theTools(),
		Profiles:       []profile.Profile{base.Profile()},
		Executors:      theExecutors(),
	})
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("a build whose profile offers no tools accepted an unsupplied condition: %v", err)
	}
	if failure.Field != "cli_registry.paths" {
		t.Errorf("the failure names the field %q, want cli_registry.paths", failure.Field)
	}
	if !strings.Contains(failure.Message, "messaging_enabled") {
		t.Errorf("the message %q does not name the condition", failure.Message)
	}
}

// TestARegistryThatDeclaresTheConditionAsAnotherTypeIsRefused covers the type
// half of the same contract: poo supplies a boolean, so a manifest declaring the
// condition as anything else would judge entries against a value it never
// receives.
func TestARegistryThatDeclaresTheConditionAsAnotherTypeIsRefused(t *testing.T) {
	conditions := map[string]any{
		config.ProfileSideEffectingCondition: map[string]any{"type": "string"},
	}
	dir := gatedRegistry(t, conditions)

	_, err := gatedBuild(t, dir, code.Name)
	var failure *config.Error
	if !errors.As(err, &failure) {
		t.Fatalf("the build of a registry declaring the condition as a string is %v, want a settings failure", err)
	}
	if failure.Field != "cli_registry.paths" {
		t.Errorf("the failure names the field %q, want cli_registry.paths", failure.Field)
	}
	if !strings.Contains(failure.Message, "boolean") {
		t.Errorf("the message %q does not say what poo supplies", failure.Message)
	}
}

// TestTheBuildRefusesAnUnknownSessionReference covers the tool manifest's
// load-time validation, the same one a feed's gets: a session reference
// the session scope does not carry fails the build naming the tool,
// instead of failing every call at the model.
func TestTheBuildRefusesAnUnknownSessionReference(t *testing.T) {
	for _, value := range []string{"session:run_id", "session:"} {
		entry := registryEntry("A tool with a bad session reference.")
		entry["env"] = map[string]string{"SESSION": value}
		dir := writeRegistryDoc(t, map[string]any{"version": 1, "tools": map[string]any{"probe": entry}})
		settings, err := config.Parse([]byte(registrySettings(dir)), config.FormatYAML)
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
			t.Errorf("the build accepted the session reference %q", value)
			continue
		}
		if !strings.Contains(err.Error(), `"probe"`) || !strings.Contains(err.Error(), value) {
			t.Errorf("the failure %v does not name the tool and the reference", err)
		}
	}
}
