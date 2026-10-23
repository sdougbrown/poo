package cliregistry_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/tool/cliregistry"
)

// TestCallCLIGoesThroughThePolicy covers the class: a registered CLI can
// do anything, so it is a side-effecting call, and a policy that refuses
// side-effecting calls refuses it as well. The fixture's record shows
// that nothing ran.
func TestCallCLIGoesThroughThePolicy(t *testing.T) {
	tests := []struct {
		name string
		rule perm.Action
		ran  int
		want string
	}{
		{name: "denied", rule: perm.ActionDeny, ran: 0, want: perm.DeniedText},
		{name: "allowed", rule: perm.ActionAllow, ran: 1, want: "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := fixtureCatalog(t)
			marker := markerPath(t)
			policy, err := perm.New(perm.Options{
				Rules: []perm.Rule{{Class: poo.ClassSideEffecting, Action: tt.rule}},
			})
			if err != nil {
				t.Fatalf("building the policy: %v", err)
			}
			transport := pootest.NewTransport(
				pootest.Turn{Calls: []pootest.Call{{
					Name:      cliregistry.CallToolName,
					Arguments: json.RawMessage(`{"name":"say","args":{}}`),
				}}},
				pootest.Turn{Text: "done"},
			)
			session, err := poo.New(poo.Options{
				Transport: transport,
				Tools:     map[string]poo.Tool{cliregistry.CallToolName: cliregistry.NewCall(catalog)},
				ToolOrder: []string{cliregistry.CallToolName},
				Sink:      pootest.NewSink(),
				SessionID: "s1",
				Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
			}, poo.WithPolicy(policy))
			if err != nil {
				t.Fatalf("building the session: %v", err)
			}
			defer session.Close()

			if _, err := session.Prompt(context.Background(), "say hello"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			if _, err := session.Wait(context.Background()); err != nil {
				t.Fatalf("waiting for the run: %v", err)
			}

			if got := markerLines(t, marker); got != tt.ran {
				t.Errorf("the fixture ran %d times, want %d", got, tt.ran)
			}
			text := lastToolResult(t, transport)
			if !strings.Contains(text, tt.want) {
				t.Errorf("the model saw %q, want %q in it", text, tt.want)
			}
		})
	}
}

// markerPath is the file the say fixture records its runs in.
func markerPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv(markerEnv)
	if path == "" {
		t.Fatalf("the fixture records no runs: %s is not set", markerEnv)
	}
	return path
}

// lastToolResult returns the text of the tool result the model saw last.
func lastToolResult(t *testing.T, transport *pootest.Transport) string {
	t.Helper()
	requests := transport.Requests()
	if len(requests) == 0 {
		t.Fatal("the model was never asked")
	}
	var out string
	for _, message := range requests[len(requests)-1].Messages {
		for _, block := range message.Content {
			if block.Kind == poo.ContentToolResult {
				out = block.Text
			}
		}
	}
	if out == "" {
		t.Fatal("the model saw no tool result")
	}
	return out
}
