package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/prompt"
)

// The fix-pass-2 reproducers: a later prompt layer wins over a variant.

// fix2PromptHook overrides the tools section in BeforeRun.
type fix2PromptHook struct{}

func (*fix2PromptHook) BeforeRun(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
	return poo.StartDecision{SystemPrompt: func(s prompt.Sections) {
		s.Set("tools.echo", "consumer override")
	}}, nil
}

// TestHookSetWinsOverAVariant covers the rule that a later layer's Set
// overrides the effective section whatever variant the prompt renders in.
func TestHookSetWinsOverAVariant(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := variantOptions(t, tr, &variantTool{})
	opts.BeforeRun = []poo.BeforeRunHook{&fix2PromptHook{}}
	s := newTestSession(t, opts, poo.WithPromptVariant("small-local"))
	runSession(t, s, "start")
	got := systemPromptOf(t, tr.Requests()[0])
	if !strings.Contains(got, "consumer override") {
		t.Fatalf("later BeforeRun Set ignored by tool variant: %q", got)
	}
}
