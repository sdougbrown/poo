package poo_test

import (
	"fmt"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// TestASystemPromptRecordCarriesTheRenderedPrompt covers the plan's
// rule: every rendered prompt that differs from the previous one is
// written whole, with its hash and variant.
func TestASystemPromptRecordCarriesTheRenderedPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.PromptVariant = "small-local"
	n := 0
	opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
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
