package perm_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The policy is the plan's: ordered rules, the first match wins, and an
// unmatched side-effecting call takes the unmatched action. A
// read-only call no rule matches is allowed.

// TestTheFirstMatchingRuleWins covers the ordering: the first rule that
// matches decides, and the rules after it are not consulted.
func TestTheFirstMatchingRuleWins(t *testing.T) {
	rules := []perm.Rule{
		{Tool: "bash", Action: perm.ActionDeny},
		{Class: poo.ClassSideEffecting, Action: perm.ActionAllow},
	}
	policy, err := perm.New(perm.Options{Rules: rules})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "git status")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the first rule refused the call", tool.Calls())
	}
	if got := s.lastResult(t); got != perm.DeniedText {
		t.Errorf("the model saw %q, want %q", got, perm.DeniedText)
	}
}

// TestTheLaterRuleDecidesWhenTheFirstDoesNotMatch covers the ordering
// the other way round: a rule that does not match leaves the decision
// to the next one.
func TestTheLaterRuleDecidesWhenTheFirstDoesNotMatch(t *testing.T) {
	rules := []perm.Rule{
		{Tool: "grep", Action: perm.ActionDeny},
		{Tool: "bash", CommandPrefix: "git status", Action: perm.ActionAllow},
	}
	policy, err := perm.New(perm.Options{Rules: rules})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "git status")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the second rule allowed the call", tool.Calls())
	}
}

// TestAReadOnlyCallNoRuleMatchesIsAllowed covers the unmatched call: a
// read-only call is allowed, whatever the unmatched action says.
func TestAReadOnlyCallNoRuleMatchesIsAllowed(t *testing.T) {
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionDeny})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{readCall("c1", "a.go", 0)}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: a read-only call is allowed", tool.Calls())
	}
}

// TestTheUnmatchedActionAppliesToSideEffectingCalls covers the
// unmatched action: a side-effecting call no rule matches takes it.
func TestTheUnmatchedActionAppliesToSideEffectingCalls(t *testing.T) {
	policy, err := perm.New(perm.Options{Unmatched: perm.ActionDeny})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the unmatched action refused the call", tool.Calls())
	}
	if got := s.lastResult(t); got != perm.DeniedText {
		t.Errorf("the model saw %q, want %q", got, perm.DeniedText)
	}
}

// TestTheUnmatchedActionAndTheFallbackAreIndependent covers the plan's
// rule: the unmatched action says what happens when the policy asks,
// and the fallback says what happens when the chain has no answer. The
// two are separate.
func TestTheUnmatchedActionAndTheFallbackAreIndependent(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Unmatched: perm.ActionAsk,
		Fallback:  perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the fallback allowed the call", tool.Calls())
	}
	asked := s.events(t, poo.EventPermissionAsked)
	if len(asked) != 1 {
		t.Fatalf("the session emitted %d ask event(s), want 1", len(asked))
	}
	if asked[0].Payload["reason"] != poo.AskReasonPolicy {
		t.Errorf("the ask's reason is %q, want %q", asked[0].Payload["reason"], poo.AskReasonPolicy)
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceNoAsker {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceNoAsker)
	}
	if answered[0].Payload["decision"] != string(perm.DecisionAllow) {
		t.Errorf("the answer's decision is %q, want %q", answered[0].Payload["decision"], perm.DecisionAllow)
	}
}

// TestThePrefixMatchesTheCommandText covers the command prefix: the
// command's leading whitespace is trimmed and the rest is compared as
// text.
func TestThePrefixMatchesTheCommandText(t *testing.T) {
	rules := []perm.Rule{
		{Tool: "bash", CommandPrefix: "git status", Action: perm.ActionAllow},
	}
	policy, err := perm.New(perm.Options{Rules: rules, Unmatched: perm.ActionDeny})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "  git status --short")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the prefix matched the command", tool.Calls())
	}
}

// TestThePrefixIsLexicalAndNotABoundary covers the plan's own caveat: the
// prefix compares the command's text, so a compound command matches its
// first part. The prefix is a heuristic, not a safety boundary.
func TestThePrefixIsLexicalAndNotABoundary(t *testing.T) {
	rules := []perm.Rule{
		{Tool: "bash", CommandPrefix: "git status", Action: perm.ActionAllow},
	}
	policy, err := perm.New(perm.Options{Rules: rules, Unmatched: perm.ActionDeny})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "git status && rm -rf x")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the prefix matched the compound command", tool.Calls())
	}
}

// TestACommandThatDoesNotMatchThePrefixIsRefused covers the other side
// of the prefix: a command that does not start with it is unmatched.
func TestACommandThatDoesNotMatchThePrefixIsRefused(t *testing.T) {
	rules := []perm.Rule{
		{Tool: "bash", CommandPrefix: "git status", Action: perm.ActionAllow},
	}
	policy, err := perm.New(perm.Options{Rules: rules, Unmatched: perm.ActionDeny})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm -rf x")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the command does not match the prefix", tool.Calls())
	}
}

// TestAMalformedPolicyIsAConstructionError covers the plan's rule: a
// malformed policy never reaches a session.
func TestAMalformedPolicyIsAConstructionError(t *testing.T) {
	tests := []struct {
		name string
		opts perm.Options
	}{
		{
			name: "a rule that matches nothing",
			opts: perm.Options{Rules: []perm.Rule{{Action: perm.ActionDeny}}},
		},
		{
			name: "a rule that matches two things",
			opts: perm.Options{Rules: []perm.Rule{{Tool: "bash", Class: poo.ClassSideEffecting, Action: perm.ActionDeny}}},
		},
		{
			name: "an unknown action",
			opts: perm.Options{Rules: []perm.Rule{{Tool: "bash", Action: "maybe"}}},
		},
		{
			name: "an unknown unmatched action",
			opts: perm.Options{Unmatched: "maybe"},
		},
		{
			name: "a fallback that is not a decision",
			opts: perm.Options{Fallback: perm.ActionAsk},
		},
		{
			name: "a command prefix on a rule that is not a bash rule",
			opts: perm.Options{Rules: []perm.Rule{{Tool: "read", CommandPrefix: "git", Action: perm.ActionAllow}}},
		},
		{
			name: "a command prefix on a class rule",
			opts: perm.Options{Rules: []perm.Rule{{Class: poo.ClassSideEffecting, CommandPrefix: "git", Action: perm.ActionAllow}}},
		},
		{
			name: "an unknown doom loop action",
			opts: perm.Options{DoomLoop: perm.DoomLoop{Action: "maybe"}},
		},
		{
			name: "a doom loop threshold of no count",
			opts: perm.Options{DoomLoop: perm.DoomLoop{Threshold: -1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := perm.New(tt.opts)
			if err == nil {
				t.Fatalf("the policy was built, want a construction error")
			}
			if policy != nil {
				t.Errorf("the policy is %v, want nil", policy)
			}
			if !strings.Contains(err.Error(), "perm:") {
				t.Errorf("the error is %q, want it to name the package", err)
			}
		})
	}
}

// TestAnEmptyPolicyPassesWithoutSideEffectingTools covers the plan's
// rule: a build with no side-effecting tools passes with an empty
// policy.
func TestAnEmptyPolicyPassesWithoutSideEffectingTools(t *testing.T) {
	policy, err := perm.New(perm.Options{})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{readCall("c1", "a.go", 0)}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	res := s.run(t)

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1", tool.Calls())
	}
}
