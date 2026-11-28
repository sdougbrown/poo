package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/hook"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/profile"
	"github.com/sdougbrown/poo/profile/search"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

// theHookSettings is a settings document whose model answers decisions
// and whose endpoint key is read from the environment.
const theHookSettings = `
version: 1
endpoints:
  test:
    endpoint: http://localhost:4000/v1
    api_key_env: POO_TEST_KEY
models:
  test-model:
    endpoint: test
    model_id: test-model
    api: chat_completions
    tools_mode: native
    ops: [chat, systemone, decisions]
    context_limit: 32768
roles:
  chat: test-model
  decider: test-model
`

// yesBranches is one bool question whose yes takes the first decision and
// whose no takes the second.
func yesBranches(yes, no *hook.Action) map[string]hook.Branch {
	return map[string]hook.Branch{
		"yes": {MinProbability: 0.9, Action: yes},
		"no":  {MinProbability: 0.5, Action: no},
	}
}

// theJudgeStep is the one bool question a test hook asks.
func theJudgeStep(instructions string, yes, no *hook.Action) map[string]hook.Step {
	return map[string]hook.Step{
		"judge": {
			Question: hook.QuestionSpec{
				Kind:         hook.QuestionBool,
				Instructions: instructions,
				True:         "it is",
				False:        "it is not",
			},
			Branches: yesBranches(yes, no),
		},
	}
}

// theStopHook denies the bash command whose text holds the word, so a
// session shows whether the hook ran by whether the command did.
func theStopHook(word, message string, fires int) hook.Spec {
	return hook.Spec{
		Point:          hook.PointBeforeTool,
		Match:          hook.Match{Tool: "bash", CommandContains: []string{word}},
		MaxFiresPerRun: fires,
		Entry:          "judge",
		Steps: theJudgeStep("Is the call the one the hook stops?",
			&hook.Action{Kind: hook.ActionDeny, Message: message},
			&hook.Action{Kind: hook.ActionAllow}),
	}
}

// theReadCapHook denies a read, and the cap is how many it stops per
// run.
func theReadCapHook(message string, fires int) hook.Spec {
	return hook.Spec{
		Point:          hook.PointBeforeTool,
		Match:          hook.Match{Tool: "read"},
		MaxFiresPerRun: fires,
		Entry:          "judge",
		Steps: theJudgeStep("Is the call one the hook stops?",
			&hook.Action{Kind: hook.ActionDeny, Message: message},
			&hook.Action{Kind: hook.ActionAllow}),
	}
}

// theAnnotation is an after-tool hook that annotates a read result.
func theAnnotation(message string) hook.Spec {
	return hook.Spec{
		Point: hook.PointAfterTool,
		Match: hook.Match{Tool: "read"},
		Entry: "judge",
		Steps: theJudgeStep("Should the result carry the annotation?",
			&hook.Action{Kind: hook.ActionAnnotate, Message: message},
			&hook.Action{Kind: hook.ActionKeep}),
	}
}

// theContinuation continues a settle the model ended.
func theContinuation(message string, fires int) hook.Spec {
	return hook.Spec{
		Point:          hook.PointBeforeSettle,
		Match:          hook.Match{Reason: hook.ReasonEndTurn},
		MaxFiresPerRun: fires,
		Entry:          "judge",
		Steps: theJudgeStep("Should the run go on?",
			&hook.Action{Kind: hook.ActionContinue, Message: message},
			&hook.Action{Kind: hook.ActionAccept}),
	}
}

// theProbeHook gathers probes before it allows a read.
func theProbeHook(probes ...hook.Probe) hook.Spec {
	return hook.Spec{
		Point:   hook.PointBeforeTool,
		Match:   hook.Match{Tool: "read"},
		Context: hook.ContextSpec{Probes: probes},
		Entry:   "judge",
		Steps: theJudgeStep("Is the call one the hook allows?",
			&hook.Action{Kind: hook.ActionAllow},
			&hook.Action{Kind: hook.ActionDeny, Message: "refused"}),
	}
}

// theStubDecider is the decision model a built session runs its hooks
// on: the first answer is the sure one, so every yes branch is taken.
func theStubDecider() *pootest.StubDecider {
	return pootest.NewStubDecider(0.95)
}

// theStubDeciderOption gives a session the stub in place of the model the
// settings named.
func theStubDeciderOption(stub *pootest.StubDecider) poo.Option {
	return poo.WithDecider(poo.ModelInfo{Name: "stub-decider", Ops: []poo.Op{poo.OpSystemOne}}, stub)
}

// theReadOnlyTools are the tools a binary with no bash links.
func theReadOnlyTools() map[string]config.ToolFactory {
	return map[string]config.ToolFactory{
		"read": func(config.ToolOptions) poo.Tool { return read.New() },
		"grep": func(config.ToolOptions) poo.Tool { return grep.New() },
	}
}

// hookSettings parses the settings document and gives it the hooks and
// the profile the test runs.
func hookSettings(t *testing.T, document string, hooks map[string]hook.Spec, definition config.ProfileSettings) *config.Settings {
	t.Helper()
	settings, err := config.Parse([]byte(document), config.FormatYAML)
	if err != nil {
		t.Fatalf("decoding the settings: %v", err)
	}
	settings.Hooks = hooks
	settings.Profiles = map[string]config.ProfileSettings{"hooked": definition}
	settings.Profile = "hooked"
	// Each test's sessions are its own: two tests that name the same
	// session would otherwise find each other's records.
	dir := t.TempDir()
	settings.Session.Dir = &dir
	return settings
}

// TestAHookProfileNeedsADeciderModel covers the check the build owes a
// hook: a hook asks the decision model the decider role names, so a
// profile that runs one cannot build without it.
func TestAHookProfileNeedsADeciderModel(t *testing.T) {
	settings := hookSettings(t, theModelSettings,
		map[string]hook.Spec{"guard": theStopHook("git push", "refused", 1)},
		config.ProfileSettings{Base: "code", Hooks: []string{"guard"}})
	_, err := config.Build(config.BuildOptions{
		Settings: settings, WorkingDir: t.TempDir(), Tools: theTools(),
		Profiles: theProfiles(), Executors: theExecutors(),
	})
	if err == nil {
		t.Fatal("the profile built its hook without a decision model")
	}
	if code, _ := config.CodeOf(err); code != config.CodeUnavailable {
		t.Errorf("the failure is %v, want an unavailable field", err)
	}
	if field, _ := config.FieldOf(err); field != "roles.decider" {
		t.Errorf("the failure names %q, want roles.decider", field)
	}
}

// TestAHookProfileWrapsTheDeciderItCouldNotResolve covers the failure's
// cause: a decider role that names a model the catalog does not hold
// leaves the hook with nothing to ask, and the build says what the
// catalog said.
func TestAHookProfileWrapsTheDeciderItCouldNotResolve(t *testing.T) {
	settings := hookSettings(t, withoutDeciderModel,
		map[string]hook.Spec{"guard": theStopHook("git push", "refused", 1)},
		config.ProfileSettings{Base: "code", Hooks: []string{"guard"}})
	_, err := config.Build(config.BuildOptions{
		Settings: settings, WorkingDir: t.TempDir(), Tools: theTools(),
		Profiles: theProfiles(), Executors: theExecutors(),
	})
	if err == nil {
		t.Fatal("the profile built its hook with a decider role nothing resolves")
	}
	if field, _ := config.FieldOf(err); field != "roles.decider" {
		t.Errorf("the failure names %q, want roles.decider", field)
	}
	if !strings.Contains(err.Error(), "missing-model") {
		t.Errorf("the failure is %q, want it to carry the catalog's reason", err)
	}
}

// TestAHookProfileRefusesASpecThatDoesNotCompile covers the build's own
// check: settings a consumer assembled can carry a hook the load never
// saw, and the failure names the hook and the profile that runs it.
func TestAHookProfileRefusesASpecThatDoesNotCompile(t *testing.T) {
	settings := hookSettings(t, theHookSettings,
		map[string]hook.Spec{"broken": {Point: hook.PointBeforeTool}},
		config.ProfileSettings{Base: "code", Hooks: []string{"broken"}})
	_, err := config.Build(config.BuildOptions{
		Settings: settings, WorkingDir: t.TempDir(), Tools: theTools(),
		Profiles: theProfiles(), Executors: theExecutors(),
	})
	if err == nil {
		t.Fatal("the profile built a hook that cannot run")
	}
	if code, _ := config.CodeOf(err); code != config.CodeInvalid {
		t.Errorf("the failure is %v, want an invalid value", err)
	}
	if field, _ := config.FieldOf(err); field != "hooks.broken" {
		t.Errorf("the failure names %q, want hooks.broken", field)
	}
	if !strings.Contains(err.Error(), `"hooked"`) {
		t.Errorf("the failure is %q, want it to name the profile", err)
	}
}

// TestACommandProbeNeedsAnExecutor covers the executor a hook's probe
// runs on: a profile whose only command is the probe's builds the
// executor, and a binary that links none refuses the hook by name.
func TestACommandProbeNeedsAnExecutor(t *testing.T) {
	t.Setenv("POO_TEST_KEY", "the-launcher-secret")
	probes := []hook.Probe{
		{Name: "unformatted", Command: []string{"sh", "-c", "printf unformatted"}},
		{Name: "credential", Command: []string{"sh", "-c", `printf '%s' "$POO_TEST_KEY"`}},
	}
	settings := hookSettings(t, theHookSettings,
		map[string]hook.Spec{"probe": theProbeHook(probes...)},
		config.ProfileSettings{Base: search.Name, Hooks: []string{"probe"}})
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Build(config.BuildOptions{
		Settings: settings, WorkingDir: workDir, Tools: theReadOnlyTools(),
		Profiles: []profile.Profile{search.Profile()}, Executors: nil,
	})
	if err == nil {
		t.Fatal("the profile built its command probe without an executor")
	}
	if code, _ := config.CodeOf(err); code != config.CodeUnavailable {
		t.Errorf("the failure is %v, want an unavailable field", err)
	}
	if field, _ := config.FieldOf(err); field != "hooks.probe" {
		t.Errorf("the failure names %q, want hooks.probe", field)
	}
	if !strings.Contains(err.Error(), "unformatted") {
		t.Errorf("the failure is %q, want it to name the probe", err)
	}

	runtime, err := config.Build(config.BuildOptions{
		Settings: settings, WorkingDir: workDir, Tools: theReadOnlyTools(),
		Profiles: []profile.Profile{search.Profile()}, Executors: theExecutors(),
	})
	if err != nil {
		t.Fatalf("building the runtime with an executor: %v", err)
	}
	closeRuntime(t, runtime)
	// The executor the build resolved runs the probe: its output is the
	// evidence the hook asks about, and the credential the session
	// scrubs is not in the environment the probe runs with.
	stub := theStubDecider()
	runtime.Options.Transport = pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: "read", ID: "c1", Arguments: json.RawMessage(`{"path":"go.mod"}`)}}},
		pootest.Turn{Text: "done", Finish: "stop"},
	)
	sess := createHookSession(t, runtime, "s1", workDir, theStubDeciderOption(stub))
	runSession(t, sess, "check the code")
	states := stub.States()
	if len(states) == 0 {
		t.Fatal("the hook asked no question")
	}
	encoded, err := json.Marshal(states[0])
	if err != nil {
		t.Fatalf("encoding the state: %v", err)
	}
	var evidence struct {
		Probes map[string]struct{ Output string } `json:"probes"`
	}
	if err := json.Unmarshal(encoded, &evidence); err != nil {
		t.Fatalf("decoding the state: %v", err)
	}
	if got := evidence.Probes["unformatted"].Output; got != "unformatted" {
		t.Errorf("the probe reported %q, want the executor's output", got)
	}
	if got := evidence.Probes["credential"].Output; got != "" {
		t.Errorf("the probe read %q, want the session's scrubbed environment", got)
	}
}

// TestAToolProbeNeedsAnOfferedReadOnlyTool covers the tool a probe
// reaches: it is a tool this session offers, and one a hook may read
// with. Anything else is refused by name rather than failing open at each
// firing.
func TestAToolProbeNeedsAnOfferedReadOnlyTool(t *testing.T) {
	for _, tt := range []struct {
		name    string
		tool    string
		profile config.ProfileSettings
		want    string
	}{
		{
			name:    "a side-effecting tool",
			tool:    "bash",
			profile: config.ProfileSettings{Base: "code"},
			want:    "cannot run as a read-only probe",
		},
		{
			name:    "a tool a probe may not name",
			tool:    "fetch",
			profile: config.ProfileSettings{Base: "code"},
			want:    "cannot run as a read-only probe",
		},
		{
			name:    "a disabled tool",
			tool:    "read",
			profile: config.ProfileSettings{Base: "code", Tools: config.ProfileTools{Disabled: []string{"read"}}},
			want:    "does not offer",
		},
		{
			name:    "a tool the binary does not link",
			tool:    "browser",
			profile: config.ProfileSettings{Base: "code"},
			want:    "does not offer",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			probe := hook.Probe{Name: "peek", Tool: &hook.ToolProbe{Name: tt.tool, Args: json.RawMessage(`{}`)}}
			definition := tt.profile
			definition.Hooks = []string{"probe"}
			settings := hookSettings(t, theHookSettings,
				map[string]hook.Spec{"probe": theProbeHook(probe)}, definition)
			_, err := config.Build(config.BuildOptions{
				Settings: settings, WorkingDir: t.TempDir(), Tools: theTools(),
				Profiles: theProfiles(), Executors: theExecutors(),
			})
			if err == nil {
				t.Fatal("the profile built a probe of a tool it cannot read with")
			}
			if code, _ := config.CodeOf(err); code != config.CodeInvalid {
				t.Errorf("the failure is %v, want an invalid value", err)
			}
			if field, _ := config.FieldOf(err); field != "hooks.probe" {
				t.Errorf("the failure names %q, want hooks.probe", field)
			}
			for _, want := range []string{tt.want, "peek", tt.tool} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the failure is %q, want it to say %q", err, want)
				}
			}
		})
	}
}

// TestTheBuildRunsTheProfilesHooksAtItsPoints covers the wiring: a
// session the runtime builds runs the profile's hooks at their points,
// and the decisions they take are the core's own side effects.
func TestTheBuildRunsTheProfilesHooksAtItsPoints(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "note.txt"), []byte("the note"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := hookSettings(t, theHookSettings,
		map[string]hook.Spec{
			"guard":    theStopHook("git push", "the guard refuses a push", 1),
			"annotate": theAnnotation("the hook reads this call too"),
			"look":     theContinuation("take one more look", 1),
		},
		config.ProfileSettings{Base: "code", Hooks: []string{"guard", "annotate", "look"}})
	runtime := buildIn(t, settings, config.Overrides{}, workDir, theExecutors())
	closeRuntime(t, runtime)
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{
			{Name: "read", ID: "c1", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
			{Name: "bash", ID: "c2", Arguments: json.RawMessage(`{"command":"git push && touch pushed.txt"}`)},
		}},
		pootest.Turn{Text: "done", Finish: "stop"},
		pootest.Turn{Text: "done again", Finish: "stop"},
	)
	runtime.Options.Transport = transport
	// The caller's own attach still runs: the build's attach composes
	// with it instead of replacing it.
	attaches := 0
	caller := func(context.Context, *poo.Attach) (func() error, error) {
		attaches++
		return nil, nil
	}
	sess := createHookSession(t, runtime, "s1", workDir,
		theStubDeciderOption(theStubDecider()), poo.WithSessionAttach(caller))
	runSession(t, sess, "clean up the tree")

	if attaches != 1 {
		t.Errorf("the caller's attach ran %d times, want once", attaches)
	}
	if _, err := os.Stat(filepath.Join(workDir, "pushed.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the denied command ran")
	}
	seen := requestText(transport)
	for _, want := range []string{
		"the note",
		"the hook reads this call too",
		"the guard refuses a push",
		"take one more look",
	} {
		if !strings.Contains(seen, want) {
			t.Errorf("the model read no %q: %s", want, seen)
		}
	}
	// The continuation is a round of its own: the model was asked again
	// after the stop the hook continued.
	if got := len(transport.Requests()); got != 3 {
		t.Errorf("the run made %d requests, want the stopped turn and the continuation", got)
	}
}

// TestTheHookFiringCapIsEverySessionsOwn covers the runners the attach
// gives each session: two sessions of one runtime each count their own
// firings, so the cap holds in both.
func TestTheHookFiringCapIsEverySessionsOwn(t *testing.T) {
	workDir := t.TempDir()
	for name, text := range map[string]string{"note.txt": "the note", "other.txt": "the other note"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	settings := hookSettings(t, theHookSettings,
		map[string]hook.Spec{"once": theReadCapHook("one read per run", 1)},
		config.ProfileSettings{Base: "code", Hooks: []string{"once"}})
	runtime := buildIn(t, settings, config.Overrides{}, workDir, theExecutors())
	closeRuntime(t, runtime)
	for _, id := range []string{"s1", "s2"} {
		transport := pootest.NewTransport(
			pootest.Turn{Calls: []pootest.Call{
				{Name: "read", ID: "c1", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
			}},
			pootest.Turn{Calls: []pootest.Call{
				{Name: "read", ID: "c2", Arguments: json.RawMessage(`{"path":"other.txt"}`)},
			}},
			pootest.Turn{Text: "done", Finish: "stop"},
		)
		runtime.Options.Transport = transport
		sess := createHookSession(t, runtime, id, workDir, theStubDeciderOption(theStubDecider()))
		runSession(t, sess, "read both notes")
		seen := requestText(transport)
		if got := strings.Count(seen, "one read per run"); got != 1 {
			t.Errorf("the session %q carries %d denials, want the cap's one", id, got)
		}
		if !strings.Contains(seen, "the other note") {
			t.Errorf("the session %q never read the second note: the cap took both reads", id)
		}
	}
}

// TestTheHookBudgetSitsUnderTheSessionsTimeout covers the deadline the
// runner keeps: it is the session's hook timeout, so a hook that runs out
// of time answers through its own policy before the core records a missed
// deadline.
func TestTheHookBudgetSitsUnderTheSessionsTimeout(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "note.txt"), []byte("the note"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := theReadCapHook("the guard refuses the read", 1)
	guard.OnError = hook.FailClosed
	guard.FailClosedMessage = "the guard could not decide, and the read is refused"
	settings := hookSettings(t, theHookSettings,
		map[string]hook.Spec{"guard": guard},
		config.ProfileSettings{Base: "code", Hooks: []string{"guard"}})
	runtime := buildIn(t, settings, config.Overrides{}, workDir, theExecutors())
	closeRuntime(t, runtime)
	transport := pootest.NewTransport(
		pootest.Turn{Calls: []pootest.Call{{Name: "read", ID: "c1", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
		pootest.Turn{Text: "done", Finish: "stop"},
	)
	runtime.Options.Transport = transport
	runtime.Options.HookTimeout = 3 * time.Second
	// The decision model never answers: the hook's own budget has to run
	// out under the session's deadline, not past it.
	hangs := decide.DeciderFunc(func(ctx context.Context, _ any, _ string, _ []decide.Question) ([]decide.Answer, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	sess := createHookSession(t, runtime, "s1", workDir,
		poo.WithDecider(poo.ModelInfo{Name: "stub-decider", Ops: []poo.Op{poo.OpSystemOne}}, hangs))
	runSession(t, sess, "read the note")
	if seen := requestText(transport); !strings.Contains(seen, guard.FailClosedMessage) {
		t.Errorf("the model read no failure decision: %s", seen)
	}
}

// closeRuntime closes the runtime's store once the test's sessions are
// closed.
func closeRuntime(t *testing.T, runtime *config.Runtime) {
	t.Helper()
	t.Cleanup(func() { _ = runtime.Store.Close() })
}

// createHookSession builds a session of the profile the test hooked.
func createHookSession(t *testing.T, runtime *config.Runtime, id, workDir string, fns ...poo.Option) *poo.Session {
	t.Helper()
	created := &poo.SessionCreatedRecord{
		RecordMeta: poo.NewRecordMeta("session.created"), SessionMetadata: poo.SessionMetadata{Format: poo.FormatVersion,
			SessionID:  id,
			Mode:       "logged",
			CreatedAt:  "2026-11-26T12:00:00Z",
			WorkingDir: workDir,
			Profile:    runtime.Profile.Name, Model: poo.SessionModel{ModelID: "test-model"},
			ContextLimit: 32768},
	}
	sess, err := runtime.CreateSession(context.Background(), runtime.Store, created, fns...)
	if err != nil {
		t.Fatalf("creating the session: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// runSession prompts the session and waits for the run to settle.
func runSession(t *testing.T, sess *poo.Session, prompt string) {
	t.Helper()
	if _, err := sess.Prompt(context.Background(), prompt); err != nil {
		t.Fatalf("prompting: %v", err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}
