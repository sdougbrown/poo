package perm_test

import (
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// TestReviewAnInterruptResolvesTheAskAsInterrupted covers the plan's
// interrupt row: pending permission asks resolve as deny with provenance
// "interrupted". An interrupt ends the activity, not the run.
func TestReviewAnInterruptResolvesTheAskAsInterrupted(t *testing.T) {
	asker := &waitingAsker{asked: make(chan struct{})}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	if _, err := s.session.Prompt(t.Context(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-asker.asked
	if _, err := s.session.Send("stop", poo.Interrupt); err != nil {
		t.Fatalf("interrupting: %v", err)
	}
	res, err := s.session.Wait(t.Context())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn: an interrupt does not cancel the run", res.Reason)
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0", tool.Calls())
	}
	answered := s.waitForEvent(t, poo.EventPermissionAnswered)
	if answered.Payload["decision"] != string(perm.DecisionDeny) {
		t.Errorf("the answer's decision is %v, want deny", answered.Payload["decision"])
	}
	if answered.Payload["provenance"] != "interrupted" {
		t.Errorf("the answer's provenance is %v, want %q", answered.Payload["provenance"], "interrupted")
	}
}
