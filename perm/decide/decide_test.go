package decide_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/perm"
	permissive "github.com/sdougbrown/poo/perm/decide"
	"github.com/sdougbrown/poo/pootest"
)

// The asker asks the model one question, and the model's confidence
// decides. Anything else defers: a decision that could not be made is
// never an approval.

// TestTheDeciderAllowsAConfidentCall covers the plan's rule: a
// confident approval allows the call.
func TestTheDeciderAllowsAConfidentCall(t *testing.T) {
	asker := permissive.New(pootest.NewStubDecider(0.9), permissive.Options{})

	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision != perm.DecisionAllow {
		t.Errorf("the asker decided %q, want %q", decision, perm.DecisionAllow)
	}
}

// TestTheDeciderRefusesAConfidentCall covers the plan's rule: a
// confident refusal denies the call.
func TestTheDeciderRefusesAConfidentCall(t *testing.T) {
	asker := permissive.New(pootest.NewStubDecider(0.01), permissive.Options{})

	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision != perm.DecisionDeny {
		t.Errorf("the asker decided %q, want %q", decision, perm.DecisionDeny)
	}
}

// TestTheDeciderDefersAnUncertainCall covers the plan's rule: a model
// that is not confident defers.
func TestTheDeciderDefersAnUncertainCall(t *testing.T) {
	asker := permissive.New(pootest.NewStubDecider(0.5), permissive.Options{})

	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision != perm.DecisionDefer {
		t.Errorf("the asker decided %q, want %q", decision, perm.DecisionDefer)
	}
}

// TestTheDeciderDefersOnAFailure covers the plan's rule: a decider that
// failed never allows a call.
func TestTheDeciderDefersOnAFailure(t *testing.T) {
	decider := pootest.NewStubDecider(0.99)
	decider.SetError(errors.New("the endpoint is down"))
	asker := permissive.New(decider, permissive.Options{})

	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err == nil {
		t.Error("the asker reported no failure, want the decider's")
	}
	if decision != perm.DecisionDefer {
		t.Errorf("the asker decided %q, want %q", decision, perm.DecisionDefer)
	}
}

// TestTheDeciderIsUnavailableWithoutADecider covers the plan's rule: a
// nil decider means the asker is skipped.
func TestTheDeciderIsUnavailableWithoutADecider(t *testing.T) {
	asker := permissive.New(nil, permissive.Options{})

	if asker.Available() {
		t.Error("the asker is available, want it unavailable without a decider")
	}
	if asker.CanEverAnswer() {
		t.Error("the asker can answer, want it never to answer without a decider")
	}
	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision != perm.DecisionDefer {
		t.Errorf("the asker decided %q, want %q", decision, perm.DecisionDefer)
	}
}

// TestTheThresholdsAreConfigurable covers the plan's rule: the
// thresholds belong to the caller.
func TestTheThresholdsAreConfigurable(t *testing.T) {
	asker := permissive.New(pootest.NewStubDecider(0.6), permissive.Options{
		Allow: 0.5,
		Deny:  0.99,
	})

	decision, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision != perm.DecisionAllow {
		t.Errorf("the asker decided %q, want %q: the approve threshold is lower", decision, perm.DecisionAllow)
	}
}

// TestTheEvidenceComesFromTheHistory covers the plan's rule: the
// evidence is the recent conversation.
func TestTheEvidenceComesFromTheHistory(t *testing.T) {
	decider := pootest.NewStubDecider(0.9)
	asker := permissive.New(decider, permissive.Options{})
	history := poo.NewHistoryView([]poo.Message{
		poo.NewTextMessage(poo.RoleUser, "clean the build directory"),
	}, nil, nil)

	if _, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash", HistoryView: history}); err != nil {
		t.Fatalf("asking: %v", err)
	}

	states := decider.States()
	if len(states) != 1 {
		t.Fatalf("the decider was asked %d time(s), want 1", len(states))
	}
	payload, ok := states[0].(decide.EvidencePayload)
	if !ok {
		t.Fatalf("the state is %T, want the evidence payload", states[0])
	}
	if len(payload.Turns) != 1 {
		t.Fatalf("the evidence holds %d turn(s), want 1", len(payload.Turns))
	}
	if payload.Turns[0].Text != "clean the build directory" {
		t.Errorf("the evidence's turn is %q, want the user's request", payload.Turns[0].Text)
	}
}

// TestTheDeciderAsksOneQuestion covers the question: the model is asked
// whether the call may run, and nothing else.
func TestTheDeciderAsksOneQuestion(t *testing.T) {
	decider := pootest.NewStubDecider(0.9)
	asker := permissive.New(decider, permissive.Options{})

	if _, err := asker.Ask(context.Background(), perm.Ask{Tool: "bash"}); err != nil {
		t.Fatalf("asking: %v", err)
	}

	questions := decider.Questions()
	if len(questions) != 1 {
		t.Fatalf("the decider got %d call(s), want 1", len(questions))
	}
	if len(questions[0]) != 1 {
		t.Fatalf("the call carried %d question(s), want 1", len(questions[0]))
	}
	if questions[0][0].ID != permissive.QuestionID {
		t.Errorf("the question is %q, want %q", questions[0][0].ID, permissive.QuestionID)
	}
	if _, ok := questions[0][0].Kind.(decide.Bool); !ok {
		t.Errorf("the question's kind is %T, want a bool", questions[0][0].Kind)
	}
}

// TestTheBudgetIsTheLatencyBudget covers the plan's rule: the decide
// asker has a budget of its own.
func TestTheBudgetIsTheLatencyBudget(t *testing.T) {
	asker := permissive.New(pootest.NewStubDecider(0.9), permissive.Options{})

	if got := asker.Budget(); got != permissive.DefaultTimeout {
		t.Errorf("the asker's budget is %v, want %v", got, permissive.DefaultTimeout)
	}
	asker = permissive.New(pootest.NewStubDecider(0.9), permissive.Options{Timeout: 5 * time.Second})
	if got := asker.Budget(); got != 5*time.Second {
		t.Errorf("the asker's budget is %v, want 5s", got)
	}
}

// TestAnUncertainDecisionMovesOnToTheNextAsker covers the plan's rule:
// the decide asker defers, and the chain moves on.
func TestAnUncertainDecisionMovesOnToTheNextAsker(t *testing.T) {
	decider := pootest.NewStubDecider(0.5)
	next := &countingAsker{decision: perm.DecisionAllow}
	policy, err := perm.New(perm.Options{
		Askers: []perm.Asker{permissive.New(decider, permissive.Options{}), next},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	decision, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	})
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("the policy refused the call: %+v", decision)
	}
	if next.asked != 1 {
		t.Errorf("the next asker was asked %d time(s), want 1", next.asked)
	}
}

// TestAFailedDecisionMovesOnToTheNextAsker covers the plan's rule: a
// decider that failed never allows a call.
func TestAFailedDecisionMovesOnToTheNextAsker(t *testing.T) {
	decider := pootest.NewStubDecider(0.99)
	decider.SetError(errors.New("the endpoint is down"))
	next := &countingAsker{decision: perm.DecisionAllow}
	policy, err := perm.New(perm.Options{
		Askers: []perm.Asker{permissive.New(decider, permissive.Options{}), next},
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	decision, err := policy.BeforeTool(poo.HookContext{Context: context.Background()}, poo.ToolCallInfo{
		Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting},
	})
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("the policy refused the call: %+v", decision)
	}
	if next.asked != 1 {
		t.Errorf("the next asker was asked %d time(s), want 1", next.asked)
	}
}

// countingAsker answers with a fixed decision and counts its asks.
type countingAsker struct {
	// decision is what the asker answers.
	decision perm.Decision
	// asked counts the asks the asker was given.
	asked int
}

// Ask answers the ask.
func (a *countingAsker) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	a.asked++
	return a.decision, nil
}

// Available reports that the asker can answer.
func (a *countingAsker) Available() bool { return true }

// Budget is the asker's budget.
func (a *countingAsker) Budget() time.Duration { return time.Minute }
