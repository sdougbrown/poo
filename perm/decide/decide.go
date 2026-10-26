// Package decide answers a permission ask with a decision model.
//
// The asker asks one question: may this call run? A confident answer
// decides the ask, and anything else defers to the next asker in the
// chain. The package adds no verdict of its own: the model makes the
// judgment, and a model that is unsure never allows a call.
package decide

import (
	"context"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/perm"
)

// The defaults the asker uses when nothing says otherwise.
const (
	// DefaultApprove is the probability at which a call is allowed.
	DefaultApprove = 0.85
	// DefaultDeny is the probability at which a call is refused.
	DefaultDeny = 0.99
	// DefaultTimeout is the per-call latency budget.
	DefaultTimeout = 1500 * time.Millisecond
)

// The question the asker asks.
//
// The question is one yes-or-no question, and the descriptions say what
// each answer means, so the model is not left to guess.
const (
	// QuestionID names the question.
	QuestionID = "allow"
	// Instructions ask the question.
	Instructions = "May this tool call run? Answer yes when the call is safe and matches what the user asked for, and no when it should be refused."
)

// Options configure the asker.
type Options struct {
	// Approve is the probability at which a call is allowed. It
	// defaults to DefaultApprove.
	Approve float64
	// Deny is the probability at which a call is refused. It defaults
	// to DefaultDeny.
	Deny float64
	// Timeout is the per-call latency budget. It defaults to
	// DefaultTimeout.
	Timeout time.Duration
	// Turns is how many turns of evidence the question carries. It
	// defaults to poo.DefaultEvidenceTurns.
	Turns int
}

// ModelAsker answers a permission ask with a decision model.
//
// The asker is unavailable without a decider: there is no model to ask,
// and the chain skips it.
type ModelAsker struct {
	// decider is the decision model.
	decider decide.Decider
	// approve is the probability that allows a call.
	approve float64
	// deny is the probability that refuses a call.
	deny float64
	// timeout is the per-call latency budget.
	timeout time.Duration
	// turns is how many turns of evidence the question carries.
	turns int
}

// The asker is one link of the permission chain.
var _ perm.Asker = (*ModelAsker)(nil)

// New returns an asker that asks the given decision model.
//
// A nil decider makes the asker unavailable: the chain skips it, and
// the fallback decides.
func New(decider decide.Decider, opts Options) *ModelAsker {
	approve := opts.Approve
	if approve == 0 {
		approve = DefaultApprove
	}
	deny := opts.Deny
	if deny == 0 {
		deny = DefaultDeny
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	turns := opts.Turns
	if turns <= 0 {
		turns = poo.DefaultEvidenceTurns
	}
	return &ModelAsker{
		decider: decide.Recorded(decider, nil, "permission"),
		approve: approve,
		deny:    deny,
		timeout: timeout,
		turns:   turns,
	}
}

// Available reports whether there is a model to ask.
func (a *ModelAsker) Available() bool {
	return a.decider != nil
}

// CanEverAnswer reports whether the asker may answer an ask at some
// point in this build.
//
// A build with a decider can always ask it, and a build without one can
// never.
func (a *ModelAsker) CanEverAnswer() bool {
	return a.decider != nil
}

// Budget is the per-call latency budget.
func (a *ModelAsker) Budget() time.Duration {
	return a.timeout
}

// AskerName is the name the asker's answers carry.
func (a *ModelAsker) AskerName() string { return "decide" }

// Ask asks the decision model about the call.
//
// A confident answer decides: a probability at or above the approve
// threshold allows the call, and a probability at or above the deny
// threshold refuses it. Anything else defers, and so does a decider
// that failed, timed out or answered badly: a decision that could not
// be made is not a permission grant.
func (a *ModelAsker) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	if a.decider == nil {
		return perm.DecisionDefer, nil
	}
	question := a.question()
	state := decide.Evidence(
		poo.EvidenceTurns(ask.HistoryView, a.turns),
		decide.EvidenceOptions{
			WorkingDir: ask.WorkingDir,
			Call: &decide.Call{
				Tool:      ask.Tool,
				Summary:   ask.Summary,
				Class:     string(ask.Class),
				Reason:    string(ask.Reason),
				Arguments: ask.Arguments,
			},
		},
	)
	answers, err := a.decider.Decide(ctx, state, question.Instructions, []decide.Question{question})
	if err != nil {
		return perm.DecisionDefer, err
	}
	// A malformed answer is not a permission grant: the model answered, but
	// the answer cannot be used, so the ask defers to the next asker.
	if err := decide.ValidateAnswers([]decide.Question{question}, answers); err != nil {
		return perm.DecisionDefer, nil
	}
	answer, ok := answerFor(answers, question.ID)
	if !ok {
		return perm.DecisionDefer, nil
	}
	if answer.Probabilities["yes"] >= a.approve {
		return perm.DecisionAllow, nil
	}
	if answer.Probabilities["no"] >= a.deny {
		return perm.DecisionDeny, nil
	}
	return perm.DecisionDefer, nil
}

// question returns the question the asker asks.
func (a *ModelAsker) question() decide.Question {
	return decide.Question{
		ID:           QuestionID,
		Instructions: Instructions,
		Kind: decide.Bool{
			True:  "the call is safe and matches what the user asked for",
			False: "the call should be refused",
		},
	}
}

// answerFor returns the answer to a question.
func answerFor(answers []decide.Answer, id string) (decide.Answer, bool) {
	for _, answer := range answers {
		if answer.ID == id {
			return answer, true
		}
	}
	return decide.Answer{}, false
}
