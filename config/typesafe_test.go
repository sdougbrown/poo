package config_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
)

// typesafeServer is a scripted TypeSafe endpoint: it answers the systemone
// operation and remembers the bodies it was sent.
type typesafeServer struct {
	*httptest.Server
	// mu guards the requests.
	mu sync.Mutex
	// bodies holds the request bodies the server received, in order.
	bodies []string
}

// newTypesafeServer starts a server that answers the questions it is given.
func newTypesafeServer(t *testing.T) *typesafeServer {
	t.Helper()
	s := &typesafeServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(raw))
		s.mu.Unlock()
		var ask struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(raw, &ask)
		answers := map[string]any{}
		for id := range ask.Questions {
			answers[id] = map[string]any{
				"type":          "choice",
				"choice":        "approve",
				"confidence":    1.0,
				"probabilities": map[string]float64{"approve": 1.0, "deny": 0.0},
			}
		}
		body, _ := json.Marshal(map[string]any{
			"model":   "jev-1.14.0",
			"answers": answers,
			"usage":   map[string]any{"input_tokens": 120, "output_tokens": 12},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// body returns the request body at the given index.
func (s *typesafeServer) body(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

// typesafeSettings writes a settings file with the roles the test names,
// and returns its path.
func typesafeSettings(t *testing.T, dir, roles string) string {
	t.Helper()
	return writeCodexSettings(t, dir, `version: 1
auth_file: auth.json
session:
  mode: ephemeral
roles:
  `+roles+`
`)
}

// TestTheBuiltinTypesafeModelIsOfferedOnItsKey covers the offering: the
// built-in endpoint wakes when its key is there, and Jev answers decision
// questions and no chat turns, so the model serves the decider's operation
// and not the chat one. The endpoint's model list is not the shape the probe
// reads, so the endpoint is not asked for one.
func TestTheBuiltinTypesafeModelIsOfferedOnItsKey(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoint is offered or which key answers.
	t.Setenv("TYPESAFE_API_KEY", "")

	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"typesafe": "ts_sk_test"})
	settings, err := config.Load(typesafeSettings(t, dir, "chat: gpt-4o-mini\n  decider: typesafe/jev-latest"))
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}

	model, _, err := runtime.Decider()
	if err != nil {
		t.Fatalf("resolving the decider: %v", err)
	}
	if model.Name != "typesafe/jev-latest" {
		t.Errorf("the decider is %q, want the built-in typesafe model", model.Name)
	}
	if model.Endpoint != "https://api.typesafe.ai/v1" {
		t.Errorf("the endpoint is %q, want the TypeSafe one", model.Endpoint)
	}
	if !model.HasOp(poo.OpSystemOne) {
		t.Error("the model does not serve the systemone operation, want a decider")
	}
	if model.HasOp(poo.OpChat) {
		t.Error("the model serves a chat op, want a decisions-only model")
	}
	if model.ContextLimit != 64000 {
		t.Errorf("the model's context limit is %d, want the record's", model.ContextLimit)
	}
	if got := runtime.EndpointKey(model); got != "ts_sk_test" {
		t.Errorf("the key is %q, want the stored credential", got)
	}
	if !catalogEndpoint(t, settings, "typesafe").SkipProbe {
		t.Error("the typesafe endpoint is probed, want the record to be the word on what it serves")
	}
}

// TestTheBuiltinTypesafeModelSleepsOnAnotherProvidersCredential covers the
// sharper gate: a credential file holding another provider's credential does
// not wake an endpoint this key cannot reach, and a decider role naming a
// sleeping model is a failure the caller that asks for the decider sees.
func TestTheBuiltinTypesafeModelSleepsOnAnotherProvidersCredential(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoint is offered or which key answers.
	t.Setenv("TYPESAFE_API_KEY", "")

	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"ollama-cloud": "an-ollama-key"})
	settings, err := config.Load(typesafeSettings(t, dir, "chat: gpt-4o-mini\n  decider: typesafe/jev-latest"))
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	_, _, err = runtime.Decider()
	if err == nil {
		t.Fatal("the runtime offered the typesafe decider on an Ollama key, want it refused")
	}
	if !strings.Contains(err.Error(), "typesafe/jev-latest") {
		t.Errorf("the refusal is %q, want it to name the model", err.Error())
	}
}

// TestTheBuiltinTypesafeModelWakesOnTheVariable covers the variable's own
// rung: with no TypeSafe entry in the credential file, the variable the
// endpoint reads is what offers it.
func TestTheBuiltinTypesafeModelWakesOnTheVariable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TYPESAFE_API_KEY", "ts_sk_from_env")
	writeOpencodeAuth(t, dir, map[string]string{"ollama-cloud": "an-ollama-key"})

	settings, err := config.Load(typesafeSettings(t, dir, "chat: gpt-4o-mini\n  decider: typesafe/jev-latest"))
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	model, _, err := runtime.Decider()
	if err != nil {
		t.Fatalf("resolving the decider: %v", err)
	}
	if got := runtime.EndpointKey(model); got != "ts_sk_from_env" {
		t.Errorf("the key is %q, want the variable's", got)
	}
}

// TestTheBuiltinTypesafeModelCannotBeTheChatModel covers the role the models
// cannot serve: Jev answers questions and no chat turns, so a chat role
// naming it is a settings failure that names the operation.
func TestTheBuiltinTypesafeModelCannotBeTheChatModel(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide whether the built-in endpoint is offered or which key answers.
	t.Setenv("TYPESAFE_API_KEY", "")

	dir := t.TempDir()
	writeOpencodeAuth(t, dir, map[string]string{"typesafe": "ts_sk_test"})
	settings, err := config.Load(typesafeSettings(t, dir, "chat: typesafe/jev-latest"))
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	_, err = buildCodex(t, settings, &http.Client{})
	if err == nil {
		t.Fatal("the build put Jev in the chat role, want it refused")
	}
	if !strings.Contains(err.Error(), "chat") {
		t.Errorf("the refusal is %q, want it to name the chat operation", err.Error())
	}
}

// TestTheTypesafeDeciderFoldsTheCallInstructions covers the wire the
// built-in record promises: a typesafe-served model's decider answers what
// each question asks, so the call's framing rides with the questions and the
// request carries no instructions field of its own.
func TestTheTypesafeDeciderFoldsTheCallInstructions(t *testing.T) {
	// The variable is pinned empty so the environment a test runs in cannot
	// decide which key answers.
	t.Setenv("TYPESAFE_API_KEY", "")

	dir := t.TempDir()
	srv := newTypesafeServer(t)
	path := writeCodexSettings(t, dir, `version: 1
session:
  mode: ephemeral
endpoints:
  typesafe:
    endpoint: `+srv.URL+`
    api_key: ts_sk_test
models:
  jev:
    endpoint: typesafe
    model_id: jev-latest
    ops: [systemone]
roles:
  chat: gpt-4o-mini
  decider: jev
`)
	settings, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading the settings: %v", err)
	}
	runtime, err := buildCodex(t, settings, &http.Client{})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	_, decider, err := runtime.Decider()
	if err != nil {
		t.Fatalf("resolving the decider: %v", err)
	}
	questions := []decide.Question{{
		ID:           "q1",
		Instructions: "should the tool call be allowed?",
		Kind: decide.Choice{Options: []decide.Option{
			{Name: "approve", Desc: "the call is safe to allow"},
			{Name: "deny", Desc: "the call should be refused"},
		}},
	}}
	if _, err := decider.Decide(context.Background(), map[string]string{"cwd": "/tmp"},
		"the permission asker's framing", questions); err != nil {
		t.Fatalf("asking the question: %v", err)
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal([]byte(srv.body(0)), &req); err != nil {
		t.Fatalf("the request does not decode: %v", err)
	}
	if _, ok := req["instructions"]; ok {
		t.Error("the request carries an instructions field, want the framing on the questions")
	}
	var answered map[string]struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(req["questions"], &answered); err != nil {
		t.Fatalf("the questions do not decode: %v", err)
	}
	if got, want := answered["q1"].Instructions,
		"the permission asker's framing should the tool call be allowed?"; got != want {
		t.Errorf("the question's instructions are %q, want the framing folded in: %q", got, want)
	}
}
