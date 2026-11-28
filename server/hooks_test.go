package server_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/hook"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/wire"
	"github.com/sdougbrown/poo/wire/protocol"
)

// theGuardHook denies a read of any path.
func theGuardHook(message string) hook.Spec {
	return hook.Spec{
		Point: hook.PointBeforeTool,
		Match: hook.Match{Tool: "read"},
		Entry: "judge",
		Steps: map[string]hook.Step{
			"judge": {
				Question: hook.QuestionSpec{
					Kind:         hook.QuestionBool,
					Instructions: "Is the call one the guard stops?",
					True:         "the call is one",
					False:        "the call is another",
				},
				Branches: map[string]hook.Branch{
					"yes": {MinProbability: 0.9, Action: &hook.Action{Kind: hook.ActionDeny, Message: message}},
					"no":  {MinProbability: 0.5, Action: &hook.Action{Kind: hook.ActionAllow}},
				},
			},
		},
	}
}

// TestServedProfilesRunTheirOwnHooks covers the runtime a request builds:
// a profile that selects a hook runs it in the session the server
// serves, and a profile that selects none runs the same tool call with no
// hook at all.
func TestServedProfilesRunTheirOwnHooks(t *testing.T) {
	ctx := context.Background()
	settings := servedSettings(t, "search", `{"chat":"chat-model","decider":"decider-model"}`)
	settings.Hooks = map[string]hook.Spec{"guard": theGuardHook("the guard refuses the read")}
	settings.Profiles = map[string]config.ProfileSettings{
		"guarded": {Base: "search", Hooks: []string{"guard"}},
		"plain":   {Base: "search"},
	}
	srv := newServer(t, settings)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "note.txt"), []byte("the note"), 0o600); err != nil {
		t.Fatal(err)
	}
	decider := poo.ModelInfo{
		Name: "decider-model", ModelID: "decider-model", API: poo.APIChatCompletions,
		Ops: []poo.Op{poo.OpSystemOne}, Inputs: []poo.Modality{poo.ModalityText},
		ContextLimit: 8192, Tools: poo.ToolCalling{Mode: poo.ToolsNative},
	}
	model := poo.ModelInfo{
		Name: "chat-model", ModelID: "chat-model", API: poo.APIChatCompletions,
		Ops: []poo.Op{poo.OpChat}, Inputs: []poo.Modality{poo.ModalityText},
		ContextLimit: 32768, Tools: poo.ToolCalling{Mode: poo.ToolsNative},
	}
	for _, tt := range []struct {
		profile string
		want    []string
	}{
		{profile: "guarded", want: []string{"the guard refuses the read"}},
		{profile: "plain", want: []string{"the note"}},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			sess, info, err := srv.Create(ctx, protocol.CreateParams{
				Profile: tt.profile, WorkingDir: workDir, SessionDir: t.TempDir(),
			}, wire.FactoryEnv{Client: silentClient{}})
			if err != nil {
				t.Fatalf("creating the session: %v", err)
			}
			t.Cleanup(func() {
				_ = sess.Close()
				_ = info.Release()
			})
			transport := pootest.NewTransport(
				pootest.Turn{Calls: []pootest.Call{{Name: "read", ID: "c1", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
				pootest.Turn{Text: "done", Finish: "stop"},
			)
			if _, err := sess.SetModel(model, transport); err != nil {
				t.Fatalf("switching the model: %v", err)
			}
			if err := sess.SetDecider(decider, pootest.NewStubDecider(0.95)); err != nil {
				t.Fatalf("setting the decision model: %v", err)
			}
			if _, err := sess.Prompt(ctx, "read the note"); err != nil {
				t.Fatalf("prompting: %v", err)
			}
			if _, err := sess.Wait(ctx); err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}
			seen := servedRequestText(transport)
			for _, want := range tt.want {
				if !strings.Contains(seen, want) {
					t.Errorf("the model read no %q: %s", want, seen)
				}
			}
		})
	}
}

// servedRequestText returns the block text of every message the last
// provider request carried.
func servedRequestText(transport *pootest.Transport) string {
	requests := transport.Requests()
	if len(requests) == 0 {
		return ""
	}
	var text strings.Builder
	for _, message := range requests[len(requests)-1].Messages {
		for _, block := range message.Content {
			text.WriteString(block.Text)
			text.WriteString("\n")
		}
	}
	return text.String()
}
