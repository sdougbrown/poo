package perm_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The asker chain is the plan's: the askers are asked in order, and the
// fallback decides when the chain has no answer to give.

// TestADenyGivesTheModelAVisibleDeniedResult covers the plan's rule: a
// refusal is a model-visible denied result.
func TestADenyGivesTheModelAVisibleDeniedResult(t *testing.T) {
	asker := &fakeAsker{decision: perm.DecisionDeny, available: true}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
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
		t.Errorf("the tool ran %d time(s), want 0: the asker refused the call", tool.Calls())
	}
	want := "Blocked: the permission ask was denied by fake."
	if got := s.lastResult(t); got != want {
		t.Errorf("the model saw %q, want %q", got, want)
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["decision"] != string(perm.DecisionDeny) {
		t.Errorf("the answer's decision is %q, want %q", answered[0].Payload["decision"], perm.DecisionDeny)
	}
	if answered[0].Payload["answered_by"] != "fake" {
		t.Errorf("the answer's answered_by is %q, want the asker's name", answered[0].Payload["answered_by"])
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceAnswered {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceAnswered)
	}
}

// TestWithNoAskersTheFallbackApplies covers the plan's rule: a chain
// with no askers applies the fallback, and the answer says no asker was
// configured.
func TestWithNoAskersTheFallbackApplies(t *testing.T) {
	policy, err := perm.New(perm.Options{})
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
		t.Errorf("the tool ran %d time(s), want 0: the fallback refused the call", tool.Calls())
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceNoAsker {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceNoAsker)
	}
	if answered[0].Payload["answered_by"] != "fallback" {
		t.Errorf("the answer's answered_by is %q, want the fallback", answered[0].Payload["answered_by"])
	}
}

// TestTheAskIsAnsweredOnce covers the plan's rule: an ask resolves once,
// and the answer is recorded once.
func TestTheAskIsAnsweredOnce(t *testing.T) {
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "git status")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if asker.Asks() != 1 {
		t.Errorf("the asker was asked %d time(s), want 1", asker.Asks())
	}
	if got := len(s.events(t, poo.EventPermissionAsked)); got != 1 {
		t.Errorf("the session emitted %d ask event(s), want 1", got)
	}
	if got := len(s.events(t, poo.EventPermissionAnswered)); got != 1 {
		t.Errorf("the session emitted %d answer event(s), want 1", got)
	}
}

// TestTheAskNamesTheCall covers the ask event: it says what the run is
// waiting on.
func TestTheAskNamesTheCall(t *testing.T) {
	asker := &fakeAsker{decision: perm.DecisionAllow, available: true}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "git status")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	asked := s.events(t, poo.EventPermissionAsked)
	if len(asked) != 1 {
		t.Fatalf("the session emitted %d ask event(s), want 1", len(asked))
	}
	payload := asked[0].Payload
	if payload["tool"] != "bash" {
		t.Errorf("the ask names the tool %v, want bash", payload["tool"])
	}
	if payload["summary"] != "git status" {
		t.Errorf("the ask names the summary %v, want the call's summary", payload["summary"])
	}
	if payload["class"] != string(poo.ClassSideEffecting) {
		t.Errorf("the ask names the class %v, want %q", payload["class"], poo.ClassSideEffecting)
	}
	if payload["ask_id"] == "" {
		t.Error("the ask carries no id")
	}
}

// TestACancelWhileAnAskIsPendingDenies covers the plan's rule: a run
// that ends while the ask is pending resolves the ask as cancelled, and
// a cancellation never allows.
func TestACancelWhileAnAskIsPendingDenies(t *testing.T) {
	asker := &waitingAsker{asked: make(chan struct{})}
	policy, err := perm.New(perm.Options{
		Askers:   []perm.Asker{asker},
		Fallback: perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "rm -rf build")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	if _, err := s.session.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-asker.asked
	s.session.Cancel("the test cancelled the run")
	res, err := s.session.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: a cancellation never allows", tool.Calls())
	}
	answered := s.waitForEvent(t, poo.EventPermissionAnswered)
	if answered.Payload["provenance"] != poo.ProvenanceCancelled {
		t.Errorf("the answer's provenance is %q, want %q", answered.Payload["provenance"], poo.ProvenanceCancelled)
	}
	if answered.Payload["decision"] != string(perm.DecisionDeny) {
		t.Errorf("the answer's decision is %q, want %q", answered.Payload["decision"], perm.DecisionDeny)
	}
}

// TestTheUnmatchedAskWithNoAskersFallsBackToAllow covers the plan's
// rule: the unmatched action and the fallback are independent, so an ask
// with no asker falls back to the fallback's decision.
func TestTheUnmatchedAskWithNoAskersFallsBackToAllow(t *testing.T) {
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
}

// waitingAsker waits for its context to end.
//
// The asker never answers: the run is expected to end while it waits.
type waitingAsker struct {
	// asked is closed when the asker was asked.
	asked chan struct{}
	// once guards the close.
	once sync.Once
}

// Ask waits for the context to end.
func (a *waitingAsker) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	a.once.Do(func() { close(a.asked) })
	<-ctx.Done()
	return perm.DecisionDefer, nil
}

// Available reports that the asker can answer.
func (a *waitingAsker) Available() bool { return true }

// Budget is the asker's budget.
func (a *waitingAsker) Budget() time.Duration { return time.Minute }
