package poo_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The prompt renders when the session is built, so a loaded file that
// cannot render fails the session and never reaches a run.

// promptDir loads one prompt directory for a session.
func promptDir(t *testing.T, files fstest.MapFS, sources prompt.Sources) *prompt.Dir {
	t.Helper()
	dir, err := prompt.Load(files, sources)
	if err != nil {
		t.Fatalf("loading the prompt directory: %v", err)
	}
	return dir
}

// TestAFreshSessionReportsThePromptItsRunCarries covers the rule that the
// prompt renders when the session is built: the state holds the prompt a
// run sends, before any run has started, and the run carries it as it is.
func TestAFreshSessionReportsThePromptItsRunCarries(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))

	state := s.PromptState()
	if state.Preview {
		t.Error("a built session reports its prompt as a preview")
	}
	if state.Text == "" {
		t.Fatal("the state holds no prompt")
	}
	runSession(t, s, "start")
	if got := systemPromptOf(t, tr.Requests()[0]); got != state.Text {
		t.Errorf("the run's prompt is %q, want the state's %q", got, state.Text)
	}
}

// TestASessionWithAPromptThatDoesNotRenderIsNotBuilt covers the core's
// guarantee: the construction refuses the template, names the file, and no
// run is ever started from it.
func TestASessionWithAPromptThatDoesNotRenderIsNotBuilt(t *testing.T) {
	opts := testOptions(t, scripted(nil), &echoTool{}, &recordingSink{})
	opts.SystemPrompt = ""
	opts.Prompt = []func(*prompt.Builder){promptDir(t, fstest.MapFS{
		"base.md": {Data: []byte("{{(index .Tools 0).Bad}}\n")},
	}, prompt.Sources{"base": nil}).Apply}

	s, err := poo.New(opts)
	if err == nil {
		_ = s.Close()
		t.Fatal("the session was built with a prompt that does not render")
	}
	var init *poo.ErrSessionInit
	if !errors.As(err, &init) {
		t.Errorf("the error is %T, want an ErrSessionInit", err)
	}
	if !strings.Contains(err.Error(), "base.md") {
		t.Errorf("the error %q does not name the file", err)
	}
}

// TestAFailedModelSwitchKeepsThePrompt covers the rule that the prompt
// renders again for a new model: a prompt that cannot render for the model
// refuses the switch, and the session keeps the model and the prompt it
// had.
func TestAFailedModelSwitchKeepsThePrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SystemPrompt = ""
	opts.Prompt = []func(*prompt.Builder){promptDir(t, fstest.MapFS{
		"base.md": {Data: []byte("{{if eq .Model \"other\"}}{{index .Tools 99}}{{end}}the base\n")},
	}, prompt.Sources{"base": nil}).Apply}
	s := newTestSession(t, opts)
	before := s.PromptState()

	_, err := s.SetModel(chatModel("other", 32768))
	if err == nil {
		t.Fatal("the switch to a model the prompt cannot render for was accepted")
	}
	if !strings.Contains(err.Error(), "base.md") {
		t.Errorf("the error %q does not name the file", err)
	}
	if got := s.ChatModel().ModelID; got != "test-model" {
		t.Errorf("the session talks to %q, want test-model", got)
	}
	after := s.PromptState()
	if after.Text != before.Text || after.Hash != before.Hash {
		t.Errorf("the refused switch changed the prompt to %q, want %q", after.Text, before.Text)
	}
	runSession(t, s, "start")
	if got := systemPromptOf(t, tr.Requests()[0]); got != before.Text {
		t.Errorf("the run's prompt is %q, want %q", got, before.Text)
	}
}

// TestAFailedVariantChangeKeepsThePrompt covers the variant the switch
// selects: a prompt that cannot render in it refuses the switch.
func TestAFailedVariantChangeKeepsThePrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SystemPrompt = ""
	opts.Prompt = []func(*prompt.Builder){promptDir(t, fstest.MapFS{
		"base.md":             {Data: []byte("the base\n")},
		"base.small-local.md": {Data: []byte("{{index .Tools 99}}\n")},
	}, prompt.Sources{"base": nil}).Apply}
	s := newTestSession(t, opts)
	before := s.PromptState()

	next := chatModel("other", 32768)
	next.PromptVariant = "small-local"
	if _, err := s.SetModel(next); err == nil {
		t.Fatal("the switch to a variant the prompt cannot render in was accepted")
	}
	if got := s.ChatModel().ModelID; got != "test-model" {
		t.Errorf("the session talks to %q, want test-model", got)
	}
	after := s.PromptState()
	if after.Text != before.Text || after.Hash != before.Hash {
		t.Errorf("the refused switch changed the prompt to %q, want %q", after.Text, before.Text)
	}
}

// TestAVariantThatDoesNotRenderIsRefused covers SetPromptVariant's own
// refusal: the session keeps the variant and the prompt it had.
func TestAVariantThatDoesNotRenderIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.SystemPrompt = ""
	opts.Prompt = []func(*prompt.Builder){promptDir(t, fstest.MapFS{
		"base.md":             {Data: []byte("the base\n")},
		"base.small-local.md": {Data: []byte("{{index .Tools 99}}\n")},
	}, prompt.Sources{"base": nil}).Apply}
	s := newTestSession(t, opts)
	before := s.PromptState()

	if err := s.SetPromptVariant("small-local"); err == nil {
		t.Fatal("the variant the prompt cannot render in was accepted")
	}
	after := s.PromptState()
	if after.Text != before.Text || after.Hash != before.Hash {
		t.Errorf("the refused variant changed the prompt to %q, want %q", after.Text, before.Text)
	}
	runSession(t, s, "start")
	if got := systemPromptOf(t, tr.Requests()[0]); got != before.Text {
		t.Errorf("the run's prompt is %q, want %q", got, before.Text)
	}
}

// TestAPromptChangeThatDoesNotRenderIsRefused covers SetPrompt: an edit
// that installs a template the session's data cannot render is refused,
// and the session keeps the prompt it had.
func TestAPromptChangeThatDoesNotRenderIsRefused(t *testing.T) {
	opts := testOptions(t, scripted(nil), &echoTool{}, &recordingSink{})
	s := newTestSession(t, opts)
	before := s.PromptState()

	failing := promptDir(t, fstest.MapFS{
		"base.md": {Data: []byte("{{index .Tools 99}}\n")},
	}, prompt.Sources{"base": nil})
	err := s.SetPrompt(failing.Apply)
	if err == nil {
		t.Fatal("the prompt change that does not render was accepted")
	}
	if !strings.Contains(err.Error(), "base.md") {
		t.Errorf("the error %q does not name the file", err)
	}
	after := s.PromptState()
	if after.Text != before.Text || after.Hash != before.Hash {
		t.Errorf("the refused change left the prompt %q, want %q", after.Text, before.Text)
	}
}
