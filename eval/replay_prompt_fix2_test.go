package eval

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// TestRecordedSystemPromptsUseTheRecord covers the rule that a replay
// uses the recorded system_prompt text and never the system message.
func TestRecordedSystemPromptsUseTheRecord(t *testing.T) {
	path := writeLog(t,
		`{"record":"session.created","seq":1,"ts":"2026-10-20T00:00:00Z","format":"poo-session-v0","session_id":"s"}`,
		`{"record":"system_prompt","seq":2,"ts":"2026-10-20T00:00:00Z","run_id":"r","variant":"small-local","text":"the recorded prompt","hash":"abc"}`,
	)
	log, err := ParseLog(path)
	if err != nil {
		t.Fatal(err)
	}
	got := recordedSystemPrompts(log)
	if len(got) != 1 || got[0] != "the recorded prompt" {
		t.Fatalf("the recorded prompts are %q, want the system_prompt record's text", got)
	}
}

// TestARecordedPromptRepeatsForLaterRuns covers the rule that a replay
// never falls back to a freshly rendered prompt: the last recorded
// prompt is the one the later runs carried.
func TestARecordedPromptRepeatsForLaterRuns(t *testing.T) {
	h := &recordedPrompt{prompts: []string{"one", "two"}}
	for i, want := range []string{"one", "two", "two", "two"} {
		decision, err := h.BeforeRun(poo.HookContext{}, poo.RunTrigger{})
		if err != nil {
			t.Fatal(err)
		}
		if decision.SystemPrompt == nil {
			t.Fatalf("run %d got no prompt", i)
		}
		var got string
		decision.SystemPrompt(fakeSections{set: func(text string) { got = text }})
		if got != want {
			t.Errorf("run %d got %q, want %q", i, got, want)
		}
	}
}

// fakeSections records the replacement text.
type fakeSections struct{ set func(string) }

func (s fakeSections) Set(string, string)            {}
func (s fakeSections) Append(string, string)         {}
func (s fakeSections) Remove(string)                 {}
func (s fakeSections) Insert(string, string, string) {}
func (s fakeSections) Replace(text string)           { s.set(text) }
func (s fakeSections) Names() []string               { return nil }
