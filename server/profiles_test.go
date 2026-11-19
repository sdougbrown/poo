package server_test

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
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/tool/edit"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

func TestCreateRejectsUnknownProfileBeforeModelLookup(t *testing.T) {
	srv := newServer(t, servedSettings(t, "search", `{"chat":"chat-model"}`))
	for _, model := range []string{"chat-model", "unknown-model"} {
		_, _, err := srv.Create(context.Background(), protocol.CreateParams{
			Profile: "unknown-profile", Models: &protocol.ModelRoles{Chat: model},
		}, wire.FactoryEnv{Client: silentClient{}})
		if !errors.Is(err, poo.ErrUnknownProfile) {
			t.Errorf("creating a session with an unknown profile and model %q: got %v, want unknown profile", model, err)
		}
	}
}

func TestConfiguredProfilesKeepGlobalToolExclusions(t *testing.T) {
	settings := servedSettings(t, "search", `{"chat":"chat-model"}`)
	settings.Tools.Disabled = []string{"edit"}
	settings.Profiles = map[string]config.ProfileSettings{
		"reviewer": {Base: "search", Tools: config.ProfileTools{Disabled: []string{"grep"}}},
	}
	base := search.Profile()
	base.Tools = append(base.Tools, "edit")
	dir := t.TempDir()
	srv, err := server.New(server.Options{
		Settings: settings, WorkingDir: dir, Profiles: []profile.Profile{base},
		Tools: map[string]config.ToolFactory{
			"read": func(config.ToolOptions) poo.Tool { return read.New() },
			"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
			"edit": func(config.ToolOptions) poo.Tool { return edit.New() },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
		Profile: "reviewer", WorkingDir: dir, SessionDir: dir,
	}, wire.FactoryEnv{Client: silentClient{}})
	if err != nil {
		t.Fatal(err)
	}
	defer info.Release()
	defer sess.Close()
	if len(info.Tools) != 1 || info.Tools[0].Name != "read" {
		t.Fatalf("reviewer tools = %+v, want read only", info.Tools)
	}
}

func TestConfiguredProfilesReadTheirOwnPromptAtSessionCreation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	settings := servedSettings(t, "search", `{"chat":"chat-model"}`)
	projectPrompt := true
	settings.Session.ProjectPrompt = &projectPrompt
	settings.Profiles = map[string]config.ProfileSettings{
		"reviewer": {Base: "search", PromptFile: "review.md", Tools: config.ProfileTools{Disabled: []string{"grep"}}},
		"explore":  {Base: "search", PromptFile: filepath.Join(t.TempDir(), "explore.md")},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(dir, "settings.json")
	write(settingsPath, string(data))
	write(settings.Profiles["explore"].PromptFile, "First explore the code.")
	loaded, err := config.Load(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	// The unselected review file does not exist while the server starts.
	srv := newServer(t, loaded)
	workspace := t.TempDir()
	instructions := filepath.Join(workspace, "AGENTS.md")
	write(instructions, "Use the project's test command.")
	create := func(name string) (*poo.Session, *wire.Info, error) {
		t.Helper()
		sess, info, err := srv.Create(ctx, protocol.CreateParams{
			Profile: name, WorkingDir: workspace, SessionDir: dir,
		}, wire.FactoryEnv{Client: silentClient{}})
		if err == nil {
			t.Cleanup(func() {
				_ = sess.Close()
				_ = info.Release()
			})
		}
		return sess, info, err
	}
	assertSession := func(name, text string, wantGrep bool) *poo.Session {
		t.Helper()
		sess, info, err := create(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := sess.PromptState(); !strings.Contains(got.Text, text) ||
			!slices.Contains(got.Sections, "base") || !slices.Contains(got.Sections, "profile") {
			t.Fatalf("profile %q did not render %q with the base prompt", name, text)
		}
		if got := slices.ContainsFunc(info.Tools, func(tool protocol.ToolInfo) bool { return tool.Name == "grep" }); got != wantGrep {
			t.Fatalf("profile %q offers grep = %v, want %v", name, got, wantGrep)
		}
		return sess
	}
	assertSession("explore", "First explore the code.", true)
	if _, _, err := create("reviewer"); err == nil || !strings.Contains(err.Error(), "profiles.reviewer.prompt_file") {
		t.Fatalf("selecting the missing file returned %v", err)
	}

	path := filepath.Join(dir, "review.md")
	write(path, "Review only the changed lines.")
	first := assertSession("reviewer", "Review only the changed lines.", false)
	write(path, "Review the tests too.")
	assertSession("reviewer", "Review the tests too.", false)
	if !strings.Contains(first.PromptState().Text, "Review only the changed lines.") {
		t.Fatal("editing the file changed a session that was already open")
	}
	if !slices.ContainsFunc(srv.Profiles(), func(p protocol.ProfileInfo) bool {
		return p.Name == "reviewer" && len(p.Tools) == 1 && p.Tools[0].Name == "read"
	}) {
		t.Fatalf("the handshake does not advertise the reviewer's tools: %+v", srv.Profiles())
	}

	unrun, unrunInfo, err := create("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := unrun.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unrunInfo.Release(); err != nil {
		t.Fatal(err)
	}
	unrunParams := protocol.LoadParams{SessionID: unrun.ID(), SessionDir: dir}
	client := wire.FactoryEnv{Client: silentClient{}}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if sess, info, err := srv.Load(ctx, unrunParams, client); sess != nil || info != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loading an unrun session without the prompt file: session=%v, info=%v, err=%v", sess != nil, info != nil, err)
	}
	write(path, "Use the updated review instructions.")
	unrunResumed, unrunResumedInfo, err := srv.Load(ctx, unrunParams, client)
	if err != nil {
		t.Fatalf("retrying after restoring the file: %v", err)
	}
	defer unrunResumedInfo.Release()
	defer unrunResumed.Close()
	for _, want := range []string{"Use the updated review instructions.", "Use the project's test command."} {
		if !strings.Contains(unrunResumed.PromptState().Text, want) {
			t.Errorf("the unrun session's prompt is missing %q", want)
		}
	}

	write(path, "Review the tests too.")
	logged, info, err := create("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	model := poo.ModelInfo{
		Name: "chat-model", ModelID: "chat-model", API: poo.APIChatCompletions,
		Ops: []poo.Op{poo.OpChat}, Inputs: []poo.Modality{poo.ModalityText},
		ContextLimit: 32768, Tools: poo.ToolCalling{Mode: poo.ToolsNative},
	}
	if _, err := logged.SetModel(model, pootest.NewTransport(pootest.Turn{Text: "done", Finish: "stop"})); err != nil {
		t.Fatal(err)
	}
	if _, err := logged.Prompt(ctx, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err := logged.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := logged.Close(); err != nil {
		t.Fatal(err)
	}
	if err := info.Release(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(instructions, "Use a different test command.")
	resumed, resumedInfo, err := srv.Load(ctx, protocol.LoadParams{
		SessionID: logged.ID(), SessionDir: dir,
	}, wire.FactoryEnv{Client: silentClient{}})
	if err != nil {
		t.Fatalf("resuming without the source file: %v", err)
	}
	defer resumedInfo.Release()
	defer resumed.Close()
	if text := resumed.PromptState().Text; !strings.Contains(text, "Review the tests too.") ||
		!strings.Contains(text, "Use the project's test command.") || strings.Contains(text, "Use a different test command.") {
		t.Fatal("the resumed session did not keep the recorded profile and project instructions")
	}
}
