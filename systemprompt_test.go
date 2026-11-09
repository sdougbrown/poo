package poo_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
)

// The system prompt is assembled from named sections and rendered once
// per run. The tests drive a session and read the prompt out of the
// request the transport received.

// systemPromptOf returns the system prompt a request carries.
func systemPromptOf(t *testing.T, req poo.ChatRequest) string {
	t.Helper()
	text := ""
	for _, msg := range req.Messages {
		if msg.Role == poo.RoleSystem {
			text = msg.Text()
		}
	}
	return text
}

// TestThePromptCarriesTheCoreSection covers the core's own section: the
// environment names where the session works and which model it talks
// to.
func TestThePromptCarriesTheCoreSection(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	for _, needle := range []string{"Environment:", "test-model"} {
		if !strings.Contains(got, needle) {
			t.Errorf("the prompt does not mention %q: %q", needle, got)
		}
	}
}

// TestThePromptCarriesTheToolSections covers the rule that a tool's
// section is in the prompt when the tool is linked: a job-capable tool
// makes the core register its own jobs tool, and the jobs tool brings
// its section.
func TestThePromptCarriesTheToolSections(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)

	opts := poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{"job": tool},
		ToolOrder:  []string{"job"},
		Sink:       &recordingSink{},
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "Background jobs:") {
		t.Errorf("the prompt does not carry the jobs tool's section: %q", got)
	}
}

// TestTheToolSectionGoesAwayWithTheTool covers the other half of the
// rule: a session with no job-capable tool has no jobs tool, and the
// prompt says nothing about jobs.
func TestTheToolSectionGoesAwayWithTheTool(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if strings.Contains(got, "Background jobs:") {
		t.Errorf("the prompt mentions jobs in a session with none: %q", got)
	}
}

// TestTheConsumersPromptMutationReachesThePrompt covers the Go
// consumer's layer: the mutation the consumer gave the session changes
// the prompt the model reads.
func TestTheConsumersPromptMutationReachesThePrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts, poo.WithPrompt(func(b *prompt.Builder) {
		b.Set("base", "the consumer's contract")
		b.Remove("environment")
	}))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the consumer's contract") {
		t.Errorf("the prompt does not carry the consumer's section: %q", got)
	}
	if strings.Contains(got, "Environment:") {
		t.Errorf("the prompt carries a section the consumer removed: %q", got)
	}
}

// TestTheRawSystemPromptIsTheWholePrompt covers the raw prompt: it is
// the prompt, and the sections are not used.
func TestTheRawSystemPromptIsTheWholePrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = "the whole prompt"
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	if got := systemPromptOf(t, reqs[0]); got != "the whole prompt" {
		t.Errorf("the prompt is %q, want the whole prompt", got)
	}
}

// TestThePromptIsTheSameForEveryRun covers the rule that the prompt is
// rendered the same way every time: the same session renders the same
// prompt for each of its runs.
func TestThePromptIsTheSameForEveryRun(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}, {text: "the second answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")
	runSession(t, s, "do the other thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	first := systemPromptOf(t, reqs[0])
	second := systemPromptOf(t, reqs[1])
	if first != second {
		t.Errorf("the second run's prompt is %q, want %q", second, first)
	}
}

// TestThePromptIsWrittenToTheRecord covers the record: the system
// message is in the session's record, so a reader of the log sees the
// prompt the model read.
func TestThePromptIsWrittenToTheRecord(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.SystemPrompt = "the whole prompt"
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	found := false
	for _, msg := range sink.messages() {
		if msg.Role != poo.RoleSystem {
			continue
		}
		found = true
		text := poo.Message{Role: msg.Role, Content: msg.Content}.Text()
		if !strings.Contains(text, "the whole prompt") {
			t.Errorf("the record holds the system message %q, want the prompt", text)
		}
	}
	if !found {
		t.Error("the record holds no system message")
	}
}

// TestThePromptIsSentWithTheFirstRequestOnly covers the run's prompt:
// the system message is in the view, and the run's later requests carry
// it too.
func TestThePromptIsSentWithTheFirstRequestOnly(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = "the whole prompt"
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	if got := systemPromptOf(t, reqs[0]); got != "the whole prompt" {
		t.Errorf("the prompt is %q, want the whole prompt", got)
	}
}

// TestTheEnvironmentSectionTrimsTheModelsTrailingNewlines covers the
// dynamic end of the core environment section: a model id that ends in a
// newline renders the same section as the id without one.
func TestTheEnvironmentSectionTrimsTheModelsTrailingNewlines(t *testing.T) {
	build := func(modelID string) string {
		return poo.PromptBuilder(poo.Options{
			Model:      poo.ModelInfo{ModelID: modelID},
			WorkingDir: "/fixed/work",
		}).Render()
	}
	want := build("test-model")
	for _, modelID := range []string{"test-model\n", "test-model\n\n"} {
		if got := build(modelID); got != want {
			t.Errorf("the prompt for model %q is %q, want %q", modelID, got, want)
		}
	}
}

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

// TestASystemPromptRecordCarriesTheRenderedPrompt covers the rule:: every rendered prompt that differs from the previous one is
// written whole, with its hash and variant.
func TestASystemPromptRecordCarriesTheRenderedPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.PromptVariant = "small-local"
	n := 0
	opts.BeforeRun = []poo.BeforeRunHook{beforeRunFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
		n++
		text := fmt.Sprintf("prompt-%d", n)
		return poo.StartDecision{SystemPrompt: func(s prompt.Sections) { s.Replace(text) }}, nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	var records []*poo.SystemPromptRecord
	for _, r := range sink.allRecords() {
		if rec, ok := r.(*poo.SystemPromptRecord); ok {
			records = append(records, rec)
		}
	}
	if len(records) != 2 {
		t.Fatalf("the log holds %d system_prompt records, want 2", len(records))
	}
	for i, want := range []string{"prompt-1", "prompt-2"} {
		if records[i].Text != want {
			t.Errorf("record %d holds %q, want %q", i, records[i].Text, want)
		}
		if records[i].Hash == "" {
			t.Errorf("record %d carries no hash", i)
		}
		if records[i].Variant != "small-local" {
			t.Errorf("record %d names variant %q, want small-local", i, records[i].Variant)
		}
	}
}

// TestAnUnchangedPromptIsNotRecordedAgain covers the rule that only a
// prompt that differs from the previous one is written.
func TestAnUnchangedPromptIsNotRecordedAgain(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	runSession(t, s, "one")
	runSession(t, s, "two")

	if got := sink.count("system_prompt"); got != 1 {
		t.Fatalf("the log holds %d system_prompt records, want 1", got)
	}
}

// secondRunPrompt rewrites the prompt of the session's second run.
type secondRunPrompt struct {
	mu   sync.Mutex
	runs int
	text string
}

// BeforeRun changes the second run's prompt.
func (h *secondRunPrompt) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	h.mu.Lock()
	h.runs++
	second := h.runs == 2
	h.mu.Unlock()
	if !second {
		return poo.StartDecision{}, nil
	}
	return poo.StartDecision{SystemPrompt: func(s prompt.Sections) { s.Set("base", h.text) }}, nil
}

// TestTheSecondRunsRequestUsesOnlyItsOwnPrompt covers the rule:
// the prompt is rendered once per run and frozen for that run. An earlier
// run's system message stays in the record but is not sent again.
func TestTheSecondRunsRequestUsesOnlyItsOwnPrompt(t *testing.T) {
	const second = "THE SECOND RUN'S PROMPT"
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:  tr,
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		BeforeRun:  []poo.BeforeRunHook{&secondRunPrompt{text: second}},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	requests := tr.Requests()
	if len(requests) < 2 {
		t.Fatalf("the transport saw %d request(s), want at least 2", len(requests))
	}
	systems := 0
	for _, m := range requests[1].Messages {
		if m.Role == poo.RoleSystem {
			systems++
		}
	}
	if systems != 1 {
		t.Errorf("the second run's request carries %d system message(s), want 1", systems)
	}
	if got := requests[1].Messages[0]; got.Role != poo.RoleSystem {
		t.Errorf("the second run's first message is %q, want a system message", got.Role)
	} else if !strings.Contains(got.Text(), second) {
		t.Errorf("the second run's first message is %q, want the run's own prompt %q", got.Text(), second)
	}
}

// TestARawSystemPromptIsSentVerbatim covers the field's contract: a raw
// Options.SystemPrompt is the whole prompt, byte for byte. The builder's
// trailing newline belongs to a rendered prompt, not to a raw one.
func TestARawSystemPromptIsSentVerbatim(t *testing.T) {
	for _, raw := range []string{"the raw prompt", "the raw prompt\n"} {
		t.Run("raw", func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "done"}})
			opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
			opts.SystemPrompt = raw
			s := newTestSession(t, opts)
			runSession(t, s, "start")
			reqs := tr.Requests()
			if len(reqs) != 1 {
				t.Fatalf("the transport got %d request(s), want 1", len(reqs))
			}
			if got := systemText(reqs[0].Messages); got != raw {
				t.Errorf("the system prompt is %q, want %q", got, raw)
			}
		})
	}
}
