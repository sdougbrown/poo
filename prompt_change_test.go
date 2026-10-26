package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo/prompt"
)

// The prompt of a session may be changed between its runs.

// TestThePromptChangeIsInTheNextRun covers the change a consumer
// makes: the prompt a run carries is rendered at the start of the run,
// so a change made between the runs is in the next run's prompt and not
// in the first one's.
func TestThePromptChangeIsInTheNextRun(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}, {text: "the second answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.SystemPrompt = ""
	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	if err := s.SetPrompt(func(b *prompt.Builder) {
		b.Set("house", "the house style")
	}); err != nil {
		t.Fatalf("changing the prompt: %v", err)
	}
	runSession(t, s, "do the other thing")

	reqs := tr.Requests()
	if len(reqs) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(reqs))
	}
	first := systemPromptOf(t, reqs[0])
	second := systemPromptOf(t, reqs[1])
	if strings.Contains(first, "the house style") {
		t.Errorf("the first run's prompt is %q, want the change to come after it", first)
	}
	if !strings.Contains(second, "the house style") {
		t.Errorf("the second run's prompt is %q, want the change in it", second)
	}
}
