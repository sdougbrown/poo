package perm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The doom loop is a permission case: the core counts the repeats, and
// the policy decides what a loop does. The action is applied before the
// rules, and a loop of read-only calls is a loop like any other.

// TestThreeIdenticalCallsTriggerTheDoomLoop covers the exact threshold:
// the third identical call is the one the action applies to.
func TestThreeIdenticalCallsTriggerTheDoomLoop(t *testing.T) {
	policy, err := perm.New(perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionDeny}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 0),
			readCall("c2", "a.go", 0),
			readCall("c3", "a.go", 0),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if tool.Calls() != 2 {
		t.Errorf("the tool ran %d time(s), want 2: the third call was refused", tool.Calls())
	}
	denied := deniedResult(t, s, "r3")
	want := fmt.Sprintf(perm.DoomLoopDeniedText, 3)
	if denied != want {
		t.Errorf("the model saw %q, want %q", denied, want)
	}
}

// TestTheNearKeyTriggersTheDoomLoop covers the near threshold: five
// reads of the same path with shifting offsets are the same loop.
func TestTheNearKeyTriggersTheDoomLoop(t *testing.T) {
	policy, err := perm.New(perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionDeny}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 1),
			readCall("c2", "a.go", 2),
			readCall("c3", "a.go", 3),
			readCall("c4", "a.go", 4),
			readCall("c5", "a.go", 5),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if tool.Calls() != 4 {
		t.Errorf("the tool ran %d time(s), want 4: the fifth call was refused", tool.Calls())
	}
	denied := deniedResult(t, s, "r5")
	want := fmt.Sprintf(perm.DoomLoopDeniedText, 5)
	if denied != want {
		t.Errorf("the model saw %q, want %q", denied, want)
	}
}

// TestTheDoomLoopIsAppliedBeforeTheRules covers the plan's order: the
// loop's action decides, whatever the rules say about the call.
func TestTheDoomLoopIsAppliedBeforeTheRules(t *testing.T) {
	policy, err := perm.New(perm.Options{
		Rules:    []perm.Rule{{Tool: "read", Action: perm.ActionAllow}},
		DoomLoop: perm.DoomLoop{Action: perm.ActionDeny},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 0),
			readCall("c2", "a.go", 0),
			readCall("c3", "a.go", 0),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if tool.Calls() != 2 {
		t.Errorf("the tool ran %d time(s), want 2: the loop's action refused the third call", tool.Calls())
	}
}

// TestTheDoomLoopAllowResetsTheCount covers the plan's rule: an allowed
// loop starts over, and the next call of the same shape is counted from
// one.
func TestTheDoomLoopAllowResetsTheCount(t *testing.T) {
	policy, err := perm.New(perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionAllow}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 0),
			readCall("c2", "a.go", 0),
			readCall("c3", "a.go", 0),
		}},
		{Calls: []pootest.Call{readCall("c4", "a.go", 0)}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if tool.Calls() != 4 {
		t.Errorf("the tool ran %d time(s), want 4: the loop was allowed", tool.Calls())
	}
	if got := s.session.RepeatCountsFor("r4").Exact; got != 1 {
		t.Errorf("the fourth call's exact count is %d, want 1: the allowed loop started over", got)
	}
}

// TestTheDoomLoopAsksTheChain covers the ask: the chain is asked about
// the loop, with the reason and the counts that triggered it.
func TestTheDoomLoopAsksTheChain(t *testing.T) {
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{
		DoomLoop: perm.DoomLoop{Action: perm.ActionAsk},
		Askers:   []perm.Asker{asker},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 0),
			readCall("c2", "a.go", 0),
			readCall("c3", "a.go", 0),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	if asker.Asks() != 1 {
		t.Fatalf("the asker was asked %d time(s), want 1", asker.Asks())
	}
	ask := asker.LastAsk()
	if ask.Reason != poo.ReasonDoomLoop {
		t.Errorf("the ask's reason is %q, want %q", ask.Reason, poo.ReasonDoomLoop)
	}
	if ask.RepeatCount == nil {
		t.Fatal("the ask carries no repeat count")
	}
	if ask.RepeatCount.Exact != 3 {
		t.Errorf("the ask's exact count is %d, want 3", ask.RepeatCount.Exact)
	}
	if tool.Calls() != 3 {
		t.Errorf("the tool ran %d time(s), want 3: the asker allowed the call", tool.Calls())
	}
	asked := s.events(t, poo.EventPermissionAsked)
	if len(asked) != 1 {
		t.Fatalf("the session emitted %d ask event(s), want 1", len(asked))
	}
	if asked[0].Payload["reason"] != poo.ReasonDoomLoop {
		t.Errorf("the ask event's reason is %q, want %q", asked[0].Payload["reason"], poo.ReasonDoomLoop)
	}
}

// TestADoomLoopAskThatIsDeniedIsStillTheLoop covers the ask action: the
// loop asked, the ask was refused, and the model is told what the loop
// was, with the count, rather than who refused the ask.
func TestADoomLoopAskThatIsDeniedIsStillTheLoop(t *testing.T) {
	tests := []struct {
		name string
		// askers is the chain the ask goes to. No askers means the
		// fallback decides.
		askers []perm.Asker
	}{
		{name: "the fallback"},
		{name: "a client's answer", askers: []perm.Asker{&fakeAsker{decision: perm.DecisionDeny, available: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The doom loop's default action is to ask.
			policy, err := perm.New(perm.Options{Askers: tt.askers})
			if err != nil {
				t.Fatalf("building the policy: %v", err)
			}
			tool := &readTool{}
			s := newSession(t, []pootest.Turn{
				{Calls: []pootest.Call{
					readCall("c1", "a.go", 0),
					readCall("c2", "a.go", 0),
					readCall("c3", "a.go", 0),
				}},
				{Text: "done"},
			}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

			s.run(t)

			denied := deniedResult(t, s, "r3")
			want := fmt.Sprintf(perm.DoomLoopDeniedText, 3)
			if denied != want {
				t.Errorf("the model saw %q, want %q", denied, want)
			}
		})
	}
}

// TestTheDoomLoopDenialIsVisible covers the plan's rule: the model sees
// the outcome of a loop, and nothing is silently skipped.
func TestTheDoomLoopDenialIsVisible(t *testing.T) {
	policy, err := perm.New(perm.Options{DoomLoop: perm.DoomLoop{Action: perm.ActionDeny}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &readTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{
			readCall("c1", "a.go", 0),
			readCall("c2", "a.go", 0),
			readCall("c3", "a.go", 0),
		}},
		{Text: "done"},
	}, map[string]poo.Tool{"read": tool}, []string{"read"}, policy)

	s.run(t)

	results := s.results(t)
	if len(results) != 3 {
		t.Fatalf("the model saw %d result(s), want 3: every call is answered", len(results))
	}
	refusals := 0
	for _, text := range results {
		if strings.Contains(text, "repeated 3 times") {
			refusals++
			if !strings.HasPrefix(text, "[r3 · read]") {
				t.Errorf("the refusal is %q, want the third call's", text)
			}
		}
	}
	if refusals != 1 {
		t.Errorf("the model saw %d refusal(s), want 1", refusals)
	}
}

// deniedResult returns the text of the refusal the model saw for a
// call.
func deniedResult(t *testing.T, s *session, handle string) string {
	t.Helper()
	for _, text := range s.results(t) {
		if !strings.HasPrefix(text, "["+handle+" ·") {
			continue
		}
		return resultText(text)
	}
	t.Fatalf("the model saw no result for %s: %v", handle, s.results(t))
	return ""
}
