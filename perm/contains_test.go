package perm_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The command-contains matcher: a rule's substrings are text of
// the call's command, so a rule can name the risky parts of a command
// instead of its first words. The matcher is a tripwire and not a
// boundary: it compares text, and text can be spelled around it.

// TestTheContainsRuleAsksBeforeTheUnmatchedActionAllows covers the
// posture the matcher exists for: the policy allows what no rule
// covers, and a command that carries a substring the rule names is asked
// about instead.
func TestTheContainsRuleAsksBeforeTheUnmatchedActionAllows(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Rules: []perm.Rule{
			{Tool: "bash", CommandContains: []string{"rm -rf", "git push --force"}, Action: perm.ActionAsk},
		},
		Unmatched: perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "cd /work/x && rm -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the command carries a substring the rule asks about", tool.Calls())
	}
	if got := s.lastResult(t); got != "Blocked: the permission ask was denied by fallback." {
		t.Errorf("the model saw %q, want the fallback's refusal", got)
	}
}

// TestTheUnmatchedActionStillAllowsTheOtherCommands covers the other
// side of the tripwire: a command that carries no substring is unmatched,
// and the unmatched action decides.
func TestTheUnmatchedActionStillAllowsTheOtherCommands(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Rules: []perm.Rule{
			{Tool: "bash", CommandContains: []string{"rm -rf"}, Action: perm.ActionAsk},
		},
		Unmatched: perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "cd /work/x && go test ./...")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the command carries no substring, so the unmatched action allowed it", tool.Calls())
	}
}

// TestAContainsSubstringIsATripwireAndNotABoundary covers the matcher's
// own caveat: the comparison is text, so a command spelled around the
// substring matches nothing. The substring is a tripwire, not a boundary.
func TestAContainsSubstringIsATripwireAndNotABoundary(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Rules: []perm.Rule{
			{Tool: "bash", CommandContains: []string{"rm -rf"}, Action: perm.ActionAsk},
		},
		Unmatched: perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}

	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm  -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the command is spelled around the substring", tool.Calls())
	}
}
