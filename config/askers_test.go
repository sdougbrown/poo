package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The asker chain is the one list whose emptiness says something: an
// explicit empty list is a chain with no askers, and an absent list
// takes the binary's default chain.

// TestAnEmptyAskerChainIsNotAnAbsentOne covers the decode in the three
// formats: the key's presence survives to the settings, and an empty
// chain is a chain the settings name.
func TestAnEmptyAskerChainIsNotAnAbsentOne(t *testing.T) {
	tests := []struct {
		format config.Format
		absent string
		empty  string
		named  string
	}{
		{
			format: config.FormatJSON,
			absent: `{"version": 1}`,
			empty:  `{"version": 1, "permission": {"askers": []}}`,
			named:  `{"version": 1, "permission": {"askers": ["wire"]}}`,
		},
		{
			format: config.FormatTOML,
			absent: "version = 1\n",
			empty:  "version = 1\n[permission]\naskers = []\n",
			named:  "version = 1\n[permission]\naskers = [\"wire\"]\n",
		},
		{
			format: config.FormatYAML,
			absent: "version: 1\n",
			empty:  "version: 1\npermission:\n  askers: []\n",
			named:  "version: 1\npermission:\n  askers: [wire]\n",
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			absent := parseSettings(t, tt.absent, tt.format)
			if absent.Permission.AskersNamed() {
				t.Error("the absent chain is named, want it to take the binary's own chain")
			}

			empty := parseSettings(t, tt.empty, tt.format)
			if !empty.Permission.AskersNamed() {
				t.Error("the empty chain is not named, want the settings to have named it")
			}
			if len(empty.Permission.Askers) != 0 {
				t.Errorf("the empty chain holds %v, want no askers", empty.Permission.Askers)
			}

			named := parseSettings(t, tt.named, tt.format)
			if !named.Permission.AskersNamed() {
				t.Error("the named chain is not named")
			}
			if len(named.Permission.Askers) != 1 || named.Permission.Askers[0] != "wire" {
				t.Errorf("the named chain is %v, want [wire]", named.Permission.Askers)
			}
		})
	}
}

// TestTheBuildKeepsAnEmptyChainEmpty covers the build: an explicit
// empty chain asks nobody, and an absent one takes the binary's askers.
//
// The warning is how the difference shows: a policy that can ask, with
// no asker that can ever answer, warns about the fallback.
func TestTheBuildKeepsAnEmptyChainEmpty(t *testing.T) {
	tests := []struct {
		name    string
		askers  string
		askers0 int
		warns   bool
	}{
		{name: "absent", askers: "", askers0: 1, warns: false},
		{name: "empty", askers: "  askers: []\n", askers0: 0, warns: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fix1AskClient{}
			runtime := askerRuntime(t, tt.askers, client)
			if got := len(runtime.Askers); got != tt.askers0 {
				t.Errorf("the chain holds %d asker(s), want %d", got, tt.askers0)
			}
			if warned := runtime.Warning != ""; warned != tt.warns {
				t.Errorf("the warning is %q, want a warning: %t", runtime.Warning, tt.warns)
			}
		})
	}
}

// TestTheEmptyChainDoesNotAskTheWire covers the session: an ask rule
// with no askers resolves to its fallback at once, and a capable wire
// client is never asked.
func TestTheEmptyChainDoesNotAskTheWire(t *testing.T) {
	client := &fix1AskClient{}
	runtime := askerRuntime(t, "  askers: []\n", client)
	transport, session, elapsed := runWithTheReadCall(t, runtime)

	asks := client.asks
	if len(asks) != 0 {
		t.Fatalf("the client was asked %d time(s), want 0: the chain has no askers", len(asks))
	}
	// The chain timeout is a minute, and nothing was asked, so the ask
	// cannot have waited on a budget.
	if elapsed > 30*time.Second {
		t.Errorf("the ask took %s, want no wait: nothing was asked", elapsed)
	}
	answered := permissionAnswers(t, session)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceNoAsker {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceNoAsker)
	}
	if answered[0].Payload["answered_by"] != "fallback" {
		t.Errorf("the answer's answered_by is %q, want the fallback", answered[0].Payload["answered_by"])
	}
	if text := requestText(transport); strings.Contains(text, fixtureReadText) {
		t.Errorf("the model read the file, want the fallback to have refused the call: %q", text)
	}
	if text := requestText(transport); !strings.Contains(text, "denied by fallback") {
		t.Errorf("the model saw %q, want the ask's refusal", text)
	}
}

// TestAnAbsentChainAsksTheWire covers the other side: the same session,
// with the chain absent, asks the client the binary offers.
func TestAnAbsentChainAsksTheWire(t *testing.T) {
	client := &fix1AskClient{}
	runtime := askerRuntime(t, "", client)
	transport, session, _ := runWithTheReadCall(t, runtime)

	asks := client.asks
	if len(asks) != 1 {
		t.Fatalf("the client was asked %d time(s), want 1: the binary's chain is the wire", len(asks))
	}
	answered := permissionAnswers(t, session)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceAnswered {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceAnswered)
	}
	if text := requestText(transport); !strings.Contains(text, fixtureReadText) {
		t.Errorf("the model saw %q, want the file's text", text)
	}
}

// fixtureReadText is the text of the file the session reads.
const fixtureReadText = "fixture-readable-text"

// parseSettings decodes a settings document, failing the test when it
// does not.
func parseSettings(t *testing.T, document string, format config.Format) *config.Settings {
	t.Helper()
	settings, err := config.Parse([]byte(document), format)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	return settings
}

// askerRuntime builds the code profile's runtime from a settings
// document whose chain is the given YAML snippet, with a wire client
// the chain may ask.
func askerRuntime(t *testing.T, askers string, client perm.Client) *config.Runtime {
	t.Helper()
	document := theModelSettings + `permission:
  rules:
    - tool: read
      action: ask
  chain_timeout_s: 60
` + askers + "session:\n  dir: " + t.TempDir() + "\n"
	settings := parseSettings(t, document, config.FormatYAML)
	runtime, err := config.Build(config.BuildOptions{
		Settings:       settings,
		WorkingDir:     t.TempDir(),
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
	t.Cleanup(func() {
		if runtime.Store != nil {
			runtime.Store.Close()
		}
	})
	return runtime
}

// runWithTheReadCall runs a session that reads a file in its working
// directory, and returns the transport, the session and how long the
// run took.
func runWithTheReadCall(t *testing.T, runtime *config.Runtime) (*pootest.Transport, *poo.Session, time.Duration) {
	t.Helper()
	workDir := t.TempDir()
	path := filepath.Join(workDir, "note.txt")
	if err := os.WriteFile(path, []byte(fixtureReadText+"\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	arguments, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatalf("encoding the arguments: %v", err)
	}
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: "read", ID: "c1", Arguments: arguments}}},
		pootest.Turn{Text: "done"},
	)
	runtime.Options.Transport = transport
	created := &poo.SessionCreated{
		RecordMeta:   poo.NewRecordMeta("session.created"),
		Format:       poo.FormatVersion,
		SessionID:    "s1",
		Mode:         "logged",
		CreatedAt:    "2026-10-22T12:00:00Z",
		WorkingDir:   workDir,
		Model:        poo.SessionModel{ModelID: "test-model"},
		ContextLimit: 32768,
		Profile:      "code",
	}
	session, err := runtime.CreateSession(context.Background(), runtime.Store, created)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	start := time.Now()
	if _, err := session.Prompt(context.Background(), "read the note"); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return transport, session, time.Since(start)
}

// requestText returns the block text of every message the last provider
// request carried, tool results included.
func requestText(transport *pootest.Transport) string {
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

// permissionAnswers returns the session's answered events.
func permissionAnswers(t *testing.T, session *poo.Session) []poo.Event {
	t.Helper()
	var answers []poo.Event
	for _, event := range session.Events() {
		if event.Name == poo.EventPermissionAnswered {
			answers = append(answers, event)
		}
	}
	return answers
}
