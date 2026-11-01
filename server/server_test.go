package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/base"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/session"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// servedSettings returns the settings a served session is built from:
// the named profile, a logged session, and the models the tests pick
// between.
//
// Every endpoint's URL carries credentials, so a header that writes a
// catalog model as it stands is visible.
func servedSettings(t *testing.T, profile, roles string) *config.Settings {
	t.Helper()
	document := `{
  "version": 1,
  "profile": "` + profile + `",
  "session": {"mode": "logged"},
  "endpoints": {
    "grace": {"endpoint": "http://user:secret@grace:4000/v1", "api_key_env": "POO_TEST_KEY"}
  },
  "models": {
    "chat-model": {"endpoint": "grace", "model_id": "chat-model", "api": "chat_completions", "tools_mode": "native", "ops": ["chat"], "inputs": ["text"], "context_limit": 32768},
    "decider-model": {"endpoint": "grace", "model_id": "decider-model", "api": "chat_completions", "tools_mode": "native", "ops": ["chat", "systemone", "decisions"], "inputs": ["text"], "context_limit": 8192},
    "other-decider": {"endpoint": "grace", "model_id": "other-decider", "api": "chat_completions", "tools_mode": "native", "ops": ["chat", "systemone", "decisions"], "inputs": ["text"], "context_limit": 8192}
  },
  "roles": ` + roles + `
}`
	settings, err := config.Parse([]byte(document), config.FormatJSON)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return settings
}

// newServer builds the server the tests create sessions on.
func newServer(t *testing.T, settings *config.Settings) *server.Server {
	t.Helper()
	srv, err := server.New(server.Options{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: search.Name,
		Tools: map[string]config.ToolFactory{
			"read": func(config.ToolOptions) poo.Tool { return read.New() },
			"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
		},
		Profiles: []profile.Profile{search.Profile()},
		Build:    poo.BuildInfo{Name: "poo-test", Version: "0.0.1"},
	})
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}
	return srv
}

// silentClient is a wire client that is connected but answers nothing:
// it did not declare the permission_answers capability, so the asker
// chain never asks it.
type silentClient struct{}

func (silentClient) Connected() bool { return true }
func (silentClient) PermissionAnswers() bool {
	return false
}
func (silentClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	return perm.DecisionDefer, nil
}

// answeringClient is a wire client that declared the permission_answers
// capability.
type answeringClient struct{}

func (answeringClient) Connected() bool { return true }
func (answeringClient) PermissionAnswers() bool {
	return true
}
func (answeringClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	return perm.DecisionDefer, nil
}

// TestAServedSessionTellsAClientThatCannotAnswerWhyItsAsksFallBack
// covers the session warning a served factory builds: a client that is
// connected but did not declare the permission_answers capability reads
// one warning with its session, and a client that declared it reads
// none.
func TestAServedSessionTellsAClientThatCannotAnswerWhyItsAsksFallBack(t *testing.T) {
	tests := []struct {
		name     string
		client   perm.Client
		wantAsks bool
	}{
		{
			name:     "a client that did not declare the capability",
			client:   silentClient{},
			wantAsks: true,
		},
		{
			name:     "a client that declared the capability",
			client:   answeringClient{},
			wantAsks: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := servedSettings(t, search.Name, `{"chat": "chat-model"}`)
			// The policy asks about every side-effecting call, so the
			// chain's wire asker is what would answer it.
			ask := "ask"
			settings.Permission.Unmatched = &ask
			srv := newServer(t, settings)
			sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
				Mode:       "logged",
				SessionDir: t.TempDir(),
			}, wire.FactoryEnv{Client: test.client})
			if err != nil {
				t.Fatalf("creating the session: %v", err)
			}
			defer func() {
				if err := sess.Close(); err != nil {
					t.Errorf("closing the session: %v", err)
				}
				if err := info.Release(); err != nil {
					t.Errorf("releasing the store: %v", err)
				}
			}()

			if got := len(info.Warnings); (test.wantAsks && got != 1) || (!test.wantAsks && got != 0) {
				t.Fatalf("the session carried %d warnings (%v), want %d", got, info.Warnings, map[bool]int{true: 1, false: 0}[test.wantAsks])
			}
			if test.wantAsks {
				warning := info.Warnings[0]
				if !strings.Contains(warning, "permission_answers") {
					t.Errorf("the warning is %q, want it to name the capability", warning)
				}
			}
		})
	}
}

// TestALoadedSessionTellsAClientThatCannotAnswerWhyItsAsksFallBack
// covers the load path's session warning: a client that is connected
// but did not declare the permission_answers capability reads the
// warning with the session it loads, too.
func TestALoadedSessionTellsAClientThatCannotAnswerWhyItsAsksFallBack(t *testing.T) {
	ctx := context.Background()
	settings := servedSettings(t, search.Name, `{"chat": "chat-model"}`)
	// The policy asks about every side-effecting call, so the chain's
	// wire asker is what would answer it.
	ask := "ask"
	settings.Permission.Unmatched = &ask
	srv := newServer(t, settings)
	dir := t.TempDir()
	sess, info, err := srv.Create(ctx, protocol.CreateParams{
		Mode:       "logged",
		SessionDir: dir,
	}, wire.FactoryEnv{Client: silentClient{}})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	id := sess.ID()
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if err := info.Release(); err != nil {
		t.Fatalf("releasing the store: %v", err)
	}

	loaded, info, err := srv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.FactoryEnv{Client: silentClient{}})
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	defer func() {
		if err := loaded.Close(); err != nil {
			t.Errorf("closing the loaded session: %v", err)
		}
		if err := info.Release(); err != nil {
			t.Errorf("releasing the store: %v", err)
		}
	}()

	if got := len(info.Warnings); got != 1 {
		t.Fatalf("the loaded session carried %d warnings (%v), want one", got, info.Warnings)
	}
	if warning := info.Warnings[0]; !strings.Contains(warning, "permission_answers") {
		t.Errorf("the warning is %q, want it to name the capability", warning)
	}
}

// The handshake names the default profile, and it is the profile a
// session whose params name none runs under.
func TestTheHandshakeNamesTheDefaultProfile(t *testing.T) {
	// The settings name no profile, so the binary's own default runs.
	srv := newServer(t, servedSettings(t, "", `{"chat": "chat-model"}`))
	if got := srv.DefaultProfile(); got == "" {
		t.Fatal("the server names no default profile")
	}
	sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
		Mode:       "logged",
		SessionDir: t.TempDir(),
	}, wire.FactoryEnv{})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Errorf("closing the session: %v", err)
		}
		if err := info.Release(); err != nil {
			t.Errorf("releasing the store: %v", err)
		}
	}()

	meta, err := session.MetadataOf(info.SessionDir, sess.ID())
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	if got := srv.DefaultProfile(); got != meta.Profile {
		t.Errorf("the default profile is %q, want the %q profile the session ran under", got, meta.Profile)
	}
}

// TestTheServedHeaderRecordsTheDeciderTheSessionRunsWith covers the
// header a served logged session writes: it names the decision model
// the session runs with, whether the settings named it or the request
// did, and it names it the way the log names a model.
func TestTheServedHeaderRecordsTheDeciderTheSessionRunsWith(t *testing.T) {
	settingsRoles := `{"chat": "chat-model", "decider": "decider-model"}`
	tests := []struct {
		name   string
		roles  string
		models *protocol.ModelRoles
		want   string
	}{
		{
			name:  "the settings' decider",
			roles: settingsRoles,
			want:  "decider-model",
		},
		{
			name:   "the request's decider",
			roles:  settingsRoles,
			models: &protocol.ModelRoles{Decider: "other-decider"},
			want:   "other-decider",
		},
		{
			name:  "no decider",
			roles: `{"chat": "chat-model"}`,
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := newServer(t, servedSettings(t, search.Name, test.roles))
			sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
				Mode:       "logged",
				SessionDir: t.TempDir(),
				Models:     test.models,
			}, wire.FactoryEnv{})
			if err != nil {
				t.Fatalf("creating the session: %v", err)
			}
			defer func() {
				if err := sess.Close(); err != nil {
					t.Errorf("closing the session: %v", err)
				}
				if err := info.Release(); err != nil {
					t.Errorf("releasing the store: %v", err)
				}
			}()

			meta, err := session.MetadataOf(info.SessionDir, sess.ID())
			if err != nil {
				t.Fatalf("reading the header: %v", err)
			}
			used, hasDecider := sess.DeciderModel()
			if test.want == "" {
				if hasDecider {
					t.Fatalf("the session runs with the %q decider, want none", used.Name)
				}
				if meta.Roles.Decider != nil {
					t.Fatalf("the header records the %q decider, want none", meta.Roles.Decider.Name)
				}
				return
			}
			if !hasDecider {
				t.Fatalf("the session runs with no decider, want %q", test.want)
			}
			if used.Name != test.want {
				t.Fatalf("the session runs with the %q decider, want %q", used.Name, test.want)
			}
			if meta.Roles.Decider == nil {
				t.Fatalf("the header records no decider, want %q", test.want)
			}
			if got := meta.Roles.Decider.Name; got != test.want {
				t.Errorf("the header records the %q decider, want %q", got, test.want)
			}
			if got := info.Models.Decider; got != test.want {
				t.Errorf("the wire reports the %q decider, want %q", got, test.want)
			}
			// The endpoint's URL carries credentials, and the header is
			// a file a consumer reads.
			if endpoint := meta.Roles.Decider.Endpoint; strings.Contains(endpoint, "secret") {
				t.Errorf("the header writes the decider's endpoint with its credentials: %q", endpoint)
			}
		})
	}
}

// TestAServedLoggedSessionWithADeciderLoads covers the restore readers: a
// header the server wrote is the header a later serve reads back, and the
// session keeps the decider it recorded.
func TestAServedLoggedSessionWithADeciderLoads(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		models *protocol.ModelRoles
		want   string
	}{
		{
			name: "the settings' decider",
			want: "decider-model",
		},
		{
			name:   "the request's decider",
			models: &protocol.ModelRoles{Decider: "other-decider"},
			want:   "other-decider",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := newServer(t, servedSettings(t, search.Name, `{"chat": "chat-model", "decider": "decider-model"}`))
			dir := t.TempDir()
			sess, info, err := srv.Create(ctx, protocol.CreateParams{
				Mode:       "logged",
				SessionDir: dir,
				Models:     test.models,
			}, wire.FactoryEnv{})
			if err != nil {
				t.Fatalf("creating the session: %v", err)
			}
			id := sess.ID()
			recorded, err := session.MetadataOf(dir, id)
			if err != nil {
				t.Fatalf("reading the header: %v", err)
			}
			if recorded.Roles.Decider == nil || recorded.Roles.Decider.Name != test.want {
				t.Fatalf("the header records the %v decider, want %q", recorded.Roles.Decider, test.want)
			}
			if err := sess.Close(); err != nil {
				t.Fatalf("closing the session: %v", err)
			}
			if err := info.Release(); err != nil {
				t.Fatalf("releasing the store: %v", err)
			}

			loaded, info, err := srv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.FactoryEnv{})
			if err != nil {
				t.Fatalf("loading the session: %v", err)
			}
			defer func() {
				if err := loaded.Close(); err != nil {
					t.Errorf("closing the loaded session: %v", err)
				}
				if err := info.Release(); err != nil {
					t.Errorf("releasing the store: %v", err)
				}
			}()

			used, hasDecider := loaded.DeciderModel()
			if !hasDecider || used.Name != test.want {
				t.Errorf("the loaded session runs with the %q decider, want %q", used.Name, test.want)
			}
			if got := info.Models.Decider; got != test.want {
				t.Errorf("the loaded wire reports the %q decider, want %q", got, test.want)
			}
		})
	}
}

// TestAServedSessionWhoseRecordedDeciderThisBuildCannotBuildIsIncompatible
// covers the load path's decider failure: the settings still name the model
// the header recorded, and it can no longer answer a question, so the session
// does not fit this build. A failed load leaves the session loadable.
func TestAServedSessionWhoseRecordedDeciderThisBuildCannotBuildIsIncompatible(t *testing.T) {
	ctx := context.Background()
	roles := `{"chat": "chat-model", "decider": "decider-model"}`
	srv := newServer(t, servedSettings(t, search.Name, roles))
	dir := t.TempDir()
	sess, info, err := srv.Create(ctx, protocol.CreateParams{
		Mode:       "logged",
		SessionDir: dir,
		Models:     &protocol.ModelRoles{Decider: "other-decider"},
	}, wire.FactoryEnv{})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	id := sess.ID()
	if err := sess.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if err := info.Release(); err != nil {
		t.Fatalf("releasing the store: %v", err)
	}

	// The same settings, with the recorded model still named but no longer
	// serving the operation a decider needs.
	degraded := servedSettings(t, search.Name, roles)
	model := degraded.Models["other-decider"]
	model.Ops = []string{"chat"}
	degraded.Models["other-decider"] = model
	degradedSrv := newServer(t, degraded)
	_, _, err = degradedSrv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.FactoryEnv{})
	if !errors.Is(err, poo.ErrSessionIncompatible) {
		t.Fatalf("loading the session: %v, want %v", err, poo.ErrSessionIncompatible)
	}
	// The client reads which decider the session ran with, and the settings'
	// endpoint URL carries credentials.
	if !strings.Contains(err.Error(), "other-decider") {
		t.Errorf("the load failure does not name the recorded decider: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the load failure carries the endpoint's credentials: %v", err)
	}

	// The store the failed load opened is released, so the session loads
	// from the settings that can still build its decider.
	loaded, info, err := srv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.FactoryEnv{})
	if err != nil {
		t.Fatalf("loading the session after the failed load: %v", err)
	}
	defer func() {
		if err := loaded.Close(); err != nil {
			t.Errorf("closing the loaded session: %v", err)
		}
		if err := info.Release(); err != nil {
			t.Errorf("releasing the store: %v", err)
		}
	}()
	if used, ok := loaded.DeciderModel(); !ok || used.Name != "other-decider" {
		t.Errorf("the loaded session runs with the %q decider, want %q", used.Name, "other-decider")
	}
}

// TestTheHandshakeProfilesWriteNoToolsForAToollessProfile covers the
// profile list's empty tool list: a profile that offers no tools writes
// null, which is what a consumer of the handshake reads today.
func TestTheHandshakeProfilesWriteNoToolsForAToollessProfile(t *testing.T) {
	settings := servedSettings(t, base.Name, `{"chat": "chat-model"}`)
	srv, err := server.New(server.Options{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
		DefaultProfile: base.Name,
		Profiles:       []profile.Profile{base.Profile()},
		Build:          poo.BuildInfo{Name: "poo-test", Version: "0.0.1"},
	})
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}
	encoded, err := json.Marshal(srv.Profiles())
	if err != nil {
		t.Fatalf("encoding the profiles: %v", err)
	}
	if got, want := string(encoded), `[{"name":"base","tools":null}]`; got != want {
		t.Errorf("the profiles encode as %s, want %s", got, want)
	}
}

// askingProfile is the profile a served session runs under when its
// model calls the side-effecting tool: the profile offers that tool
// alone, and every side-effecting call is asked about.
var askingProfile = profile.Profile{
	Name:      "asking",
	Tools:     []string{"test"},
	Unmatched: "ask",
}

// servedTool is a side-effecting tool the scripted model calls.
type servedTool struct{}

// Spec describes the tool.
func (servedTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "test",
		Description: "a tool the tests call",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Class:       poo.ClassSideEffecting,
	}
}

// Prepare validates the call's arguments.
func (servedTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return servedPrepared{}, nil
}

// Call runs the tool.
func (servedTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	return poo.ToolResult{Text: "the tool ran"}, nil
}

// servedPrepared is a validated call of the served tool.
type servedPrepared struct{}

// Summary describes the call.
func (servedPrepared) Summary() string { return "the test tool" }

// askOnlyClient is a wire client that declared the permission_answers
// capability and has no message seam: it answers through its Ask.
type askOnlyClient struct {
	mu sync.Mutex
	// decision is what the client answers.
	decision perm.Decision
	// asks holds the asks the client was given.
	asks []perm.Ask
}

// Connected reports whether a client is connected.
func (c *askOnlyClient) Connected() bool { return true }

// PermissionAnswers reports whether the client declared the capability.
func (c *askOnlyClient) PermissionAnswers() bool { return true }

// Ask answers the ask.
func (c *askOnlyClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asks = append(c.asks, ask)
	return c.decision, nil
}

// Asks returns the asks the client was given.
func (c *askOnlyClient) Asks() []perm.Ask {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]perm.Ask(nil), c.asks...)
}

// TestAServedSessionAsksAnAskOnlyClientThroughItsAsk covers the late
// client's fallback: a served session whose client has no message seam
// is asked through its Ask, and its decision reaches the permission
// answer.
func TestAServedSessionAsksAnAskOnlyClientThroughItsAsk(t *testing.T) {
	client := &askOnlyClient{decision: perm.DecisionAllow}
	srv, err := server.New(server.Options{
		Settings:   servedSettings(t, askingProfile.Name, `{"chat": "chat-model"}`),
		WorkingDir: t.TempDir(),
		Profiles:   []profile.Profile{askingProfile},
		Tools: map[string]config.ToolFactory{
			"test": func(config.ToolOptions) poo.Tool { return servedTool{} },
		},
		Build: poo.BuildInfo{Name: "poo-test", Version: "0.0.1"},
	})
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}
	ctx := context.Background()
	sess, info, err := srv.Create(ctx, protocol.CreateParams{
		Mode:       "logged",
		SessionDir: t.TempDir(),
	}, wire.FactoryEnv{Client: client})
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Errorf("closing the session: %v", err)
		}
		if err := info.Release(); err != nil {
			t.Errorf("releasing the store: %v", err)
		}
	}()

	// The session runs on a scripted transport: the model calls the
	// side-effecting tool, then ends the run.
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: "test", Arguments: json.RawMessage(`{}`)}}},
		pootest.Turn{Text: "done", Finish: "stop"},
	)
	model := poo.ModelInfo{
		Name:         "chat-model",
		ModelID:      "chat-model",
		API:          poo.APIChatCompletions,
		Ops:          []poo.Op{poo.OpChat},
		Inputs:       []poo.Modality{poo.ModalityText},
		ContextLimit: 32768,
		Tools:        poo.ToolCalling{Mode: poo.ToolsNative},
	}
	if _, err := sess.SetModel(model, transport); err != nil {
		t.Fatalf("switching the model: %v", err)
	}
	if _, err := sess.Prompt(ctx, "hi"); err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	if _, err := sess.Wait(ctx); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	// The client was asked through its Ask, and its allowance answered
	// the ask, so the tool ran.
	asks := client.Asks()
	if len(asks) != 1 {
		t.Fatalf("the client was asked %d times, want once", len(asks))
	}
	if asks[0].Tool != "test" {
		t.Errorf("the ask names the tool %q, want the test tool", asks[0].Tool)
	}
	line := theAnsweredLine(t, info.SessionDir, sess.ID())
	if !strings.Contains(line, `"decision":"allow"`) {
		t.Errorf("the answered record says %s, want the client's allowance", line)
	}
	if strings.Contains(line, `"message"`) {
		t.Errorf("the answered record carries a message: %s", line)
	}
}

// theAnsweredLine returns the permission.answered line of a session's
// log.
func theAnsweredLine(t *testing.T, dir, sessionID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("reading the session log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"permission.answered"`) {
			return line
		}
	}
	t.Fatal("the log holds no answered record")
	return ""
}
