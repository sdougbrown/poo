package server_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/server"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// A model the settings declare under a bare key is also reachable by
// its endpoint-qualified name: `endpoint/model_id`. The models file's
// naming has always worked that way, so one spelling reaches a model
// however the settings declared it.

// TestASessionNamesAModelByItsEndpointQualifiedAlias covers the create
// path: a chat model and a decider named by their qualified forms build
// the same sessions the bare keys build, and the wire reports the
// canonical names the settings use.
func TestASessionNamesAModelByItsEndpointQualifiedAlias(t *testing.T) {
	srv := newServer(t, servedSettings(t, search.Name, `{"chat": "chat-model", "decider": "decider-model"}`))
	sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
		Mode:   "ephemeral",
		Models: &protocol.ModelRoles{Chat: "grace/chat-model", Decider: "grace/decider-model"},
	}, wire.FactoryEnv{})
	if err != nil {
		t.Fatalf("a session naming the qualified forms was refused: %v", err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Errorf("closing the session: %v", err)
		}
	}()
	if model := sess.ChatModel(); model.Name != "chat-model" {
		t.Errorf("the session runs the %q chat model, want \"chat-model\"", model.Name)
	}
	if got := info.Models.Decider; got != "decider-model" {
		t.Errorf("the wire reports the %q decider, want the canonical \"decider-model\"", got)
	}
}

// TestAConfigureNamesAModelByItsQualifiedAlias covers the switch path:
// a session moves to a model its qualified form names.
func TestAConfigureNamesAModelByItsQualifiedAlias(t *testing.T) {
	srv := newServer(t, servedSettings(t, search.Name, `{"chat": "chat-model"}`))
	sess, info, err := srv.Create(context.Background(), protocol.CreateParams{
		Mode: "ephemeral",
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
	result, err := srv.Configure(context.Background(), sess, protocol.ConfigureParams{
		Models: &protocol.ModelRoles{Chat: "grace/chat-model"},
	})
	if err != nil {
		t.Fatalf("switching to the qualified form was refused: %v", err)
	}
	if got := result.Models.Chat; got != "chat-model" {
		t.Errorf("the switch reports the %q chat model, want the canonical \"chat-model\"", got)
	}
}

// TestAQualifiedModelNameThatIsAmbiguousFailsTheServer covers the
// ambiguity rule: two settings models one qualified form points at are
// a construction error, never a silent pick.
func TestAQualifiedModelNameThatIsAmbiguousFailsTheServer(t *testing.T) {
	settings, err := config.Parse([]byte(`{
  "version": 1,
  "profile": "`+search.Name+`",
  "session": {"mode": "ephemeral"},
  "endpoints": {"grace": {"endpoint": "http://grace:4000/v1"}},
  "models": {
    "one": {"endpoint": "grace", "model_id": "twin", "api": "chat_completions", "ops": ["chat"], "inputs": ["text"], "context_limit": 32768},
    "two": {"endpoint": "grace", "model_id": "twin", "api": "chat_completions", "ops": ["chat"], "inputs": ["text"], "context_limit": 32768}
  },
  "roles": {"chat": "one"}
}`), config.FormatJSON)
	if err != nil {
		t.Fatalf("parsing the settings: %v", err)
	}
	_, err = server.New(server.Options{
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
	if err == nil {
		t.Fatal("the ambiguous qualified name built a server")
	}
	if !strings.Contains(err.Error(), "grace/twin") {
		t.Errorf("the failure does not name the ambiguous form: %v", err)
	}
}

// TestAQualifiedAliasForAnUnconfiguredEndpointIsUnknown covers the
// honesty rule: a settings model whose endpoint is not configured is
// unreachable, so its alias is the unknown-model error — never a
// silent fall back to the default model.
func TestAQualifiedAliasForAnUnconfiguredEndpointIsUnknown(t *testing.T) {
	settings, err := config.Parse([]byte(`{
  "version": 1,
  "profile": "`+search.Name+`",
  "session": {"mode": "ephemeral"},
  "endpoints": {"grace": {"endpoint": "http://grace:4000/v1"}},
  "models": {
    "chat-model": {"endpoint": "grace", "model_id": "chat-model", "api": "chat_completions", "ops": ["chat"], "inputs": ["text"], "context_limit": 32768},
    "ghost": {"endpoint": "nowhere", "model_id": "ghost-model", "api": "chat_completions", "ops": ["chat"], "inputs": ["text"], "context_limit": 32768}
  },
  "roles": {"chat": "chat-model"}
}`), config.FormatJSON)
	if err != nil {
		t.Fatalf("parsing the settings: %v", err)
	}
	srv := newServer(t, settings)
	_, _, err = srv.Create(context.Background(), protocol.CreateParams{
		Mode:   "ephemeral",
		Models: &protocol.ModelRoles{Chat: "nowhere/ghost-model"},
	}, wire.FactoryEnv{})
	if err == nil {
		t.Fatal("the alias of an unconfigured endpoint built a session")
	}
	if err != poo.ErrUnknownModel {
		t.Errorf("the failure is %v, want the unknown-model error", err)
	}
	// The bare key of the same unreachable model is refused the same
	// way, so the two spellings cannot disagree.
	_, _, err = srv.Create(context.Background(), protocol.CreateParams{
		Mode:   "ephemeral",
		Models: &protocol.ModelRoles{Chat: "ghost"},
	}, wire.FactoryEnv{})
	if err != poo.ErrUnknownModel {
		t.Errorf("the bare key's failure is %v, want the unknown-model error", err)
	}
	// A model the settings do serve keeps working beside the broken
	// one.
	sess, _, err := srv.Create(context.Background(), protocol.CreateParams{
		Mode:   "ephemeral",
		Models: &protocol.ModelRoles{Chat: "grace/chat-model"},
	}, wire.FactoryEnv{})
	if err != nil {
		t.Fatalf("the served model was refused: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("closing the session: %v", err)
	}
}

// TestAQualifiedModelNameNothingServesIsUnknown covers the miss: a
// qualified form no settings model answers is the unknown-model error,
// the way a bare key that matches nothing is.
func TestAQualifiedModelNameNothingServesIsUnknown(t *testing.T) {
	srv := newServer(t, servedSettings(t, search.Name, `{"chat": "chat-model"}`))
	_, _, err := srv.Create(context.Background(), protocol.CreateParams{
		Mode:   "ephemeral",
		Models: &protocol.ModelRoles{Chat: "grace/nope"},
	}, wire.FactoryEnv{})
	if err == nil {
		t.Fatal("a qualified name nothing serves built a session")
	}
	if err != poo.ErrUnknownModel {
		t.Errorf("the failure is %v, want the unknown-model error", err)
	}
}
