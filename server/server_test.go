package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
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
    "grace": {"base_url": "http://user:secret@grace:4000/v1", "api_key_env": "POO_TEST_KEY"}
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
			}, wire.Env{})
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
			}, wire.Env{})
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

			loaded, info, err := srv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.Env{})
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
	}, wire.Env{})
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
	_, _, err = degradedSrv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.Env{})
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
	loaded, info, err := srv.Load(ctx, protocol.LoadParams{SessionID: id, SessionDir: dir}, wire.Env{})
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
