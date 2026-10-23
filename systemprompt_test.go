package poo_test

import (
	"context"
	"strings"
	"testing"

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
		Transport:    tr,
		Tools:        map[string]poo.Tool{"job": tool},
		ToolOrder:    []string{"job"},
		Sink:         &recordingSink{},
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
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
