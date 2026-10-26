package poo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The prompt is rendered in a variant: a section that offers an
// alternative for that variant renders it, and a section that does not
// renders its default text. The variant is fixed for the length of a
// run, like the prompt itself.

// variantTool is a tool that brings a section of its own and offers an
// alternative text for it.
type variantTool struct {
	echoTool
}

// PromptSection renders the tool's own section.
func (t *variantTool) PromptSection(env prompt.Env) string {
	return "the tool section"
}

// PromptVariants renders the tool's alternative texts.
func (t *variantTool) PromptVariants(env prompt.Env) map[string]string {
	return map[string]string{
		"small-local": "the small local tool section",
	}
}

// variantOptions builds the options for a session that renders its
// prompt in a variant.
func variantOptions(t *testing.T, tr poo.ChatTransport, tool poo.Tool) poo.Options {
	t.Helper()
	return poo.Options{
		Transport:    tr,
		Tools:        map[string]poo.Tool{"echo": tool},
		ToolOrder:    []string{"echo"},
		Sink:         &recordingSink{},
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
	}
}

// withVariantSections puts a base section and its alternative in the
// builder.
func withVariantSections(b *prompt.Builder) {
	b.Set("base", "the default base")
	b.SetVariant("base", "small-local", "the small local base")
}

// TestTheVariantRendersTheSectionsAlternative covers the rule that the
// session's variant picks the alternative text of the sections that
// offer one.
func TestTheVariantRendersTheSectionsAlternative(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &echoTool{})
	s := newTestSession(t, opts,
		poo.WithPrompt(withVariantSections),
		poo.WithPromptVariant("small-local"))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the small local base") {
		t.Errorf("the prompt does not carry the variant's base: %q", got)
	}
	if strings.Contains(got, "the default base") {
		t.Errorf("the prompt carries the default base as well: %q", got)
	}
}

// TestTheDefaultPromptRendersTheDefaultTextWithoutAVariant covers the
// rule that a session that names no variant renders the default text of
// every section.
func TestTheDefaultPromptRendersTheDefaultTextWithoutAVariant(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &echoTool{})
	s := newTestSession(t, opts, poo.WithPrompt(withVariantSections))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the default base") {
		t.Errorf("the prompt does not carry the default base: %q", got)
	}
	if strings.Contains(got, "the small local base") {
		t.Errorf("the prompt carries the variant's base as well: %q", got)
	}
}

// TestAVariantNoSectionOffersChangesNothing covers the rule that a
// variant no section offers leaves the prompt as it is.
func TestAVariantNoSectionOffersChangesNothing(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &echoTool{})
	s := newTestSession(t, opts,
		poo.WithPrompt(withVariantSections),
		poo.WithPromptVariant("reasoning"))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the default base") {
		t.Errorf("the prompt does not carry the default base: %q", got)
	}
}

// TestTheToolContributesItsVariants covers the rule that a tool's
// section renders the alternative the tool offers for the variant.
func TestTheToolContributesItsVariants(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &variantTool{})
	s := newTestSession(t, opts, poo.WithPromptVariant("small-local"))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the small local tool section") {
		t.Errorf("the prompt does not carry the tool's variant: %q", got)
	}
	if strings.Contains(got, "the tool section") {
		t.Errorf("the prompt carries the tool's default section as well: %q", got)
	}
}

// TestTheToolContributesItsDefaultWithoutAVariant covers the rule that a
// tool that offers variants keeps its default text when no variant is
// named.
func TestTheToolContributesItsDefaultWithoutAVariant(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &variantTool{})
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(reqs))
	}
	got := systemPromptOf(t, reqs[0])
	if !strings.Contains(got, "the tool section") {
		t.Errorf("the prompt does not carry the tool's section: %q", got)
	}
	if strings.Contains(got, "the small local tool section") {
		t.Errorf("the prompt carries the tool's variant as well: %q", got)
	}
}

// TestTheVariantIsFixedForTheRun covers the rule that the prompt a run
// carries is rendered once: a variant that changes while the run goes
// on does not reach it.
func TestTheVariantIsFixedForTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the answer", calls: []poo.ToolCall{{Name: "echo", Arguments: []byte(`{}`)}}},
		{text: "the answer"},
	})
	var s *poo.Session
	tool := &echoTool{result: "echoed", call: func(ctx context.Context) (poo.ToolResult, error) {
		s.SetPromptVariant("other")
		return poo.ToolResult{Text: "echoed"}, nil
	}}
	opts := variantOptions(t, tr, tool)
	s = newTestSession(t, opts,
		poo.WithPrompt(withVariantSections),
		poo.WithPromptVariant("small-local"))
	runSession(t, s, "do the thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	for i, req := range reqs {
		got := systemPromptOf(t, req)
		if !strings.Contains(got, "the small local base") {
			t.Errorf("the prompt of the request %d does not carry the run's variant: %q", i, got)
		}
	}
}

// TestTheVariantChangesBetweenTheRuns covers the rule that the variant
// may change between the runs: the next run renders the prompt in the
// new variant.
func TestTheVariantChangesBetweenTheRuns(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := variantOptions(t, tr, &echoTool{})
	s := newTestSession(t, opts,
		poo.WithPrompt(withVariantSections),
		poo.WithPromptVariant("small-local"))
	runSession(t, s, "do the thing")

	s.SetPromptVariant("")
	runSession(t, s, "do the thing again")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	if got := systemPromptOf(t, reqs[0]); !strings.Contains(got, "the small local base") {
		t.Errorf("the first prompt does not carry the variant: %q", got)
	}
	if got := systemPromptOf(t, reqs[1]); !strings.Contains(got, "the default base") {
		t.Errorf("the second prompt does not carry the default base: %q", got)
	}
}
