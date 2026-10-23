package perm

import (
	"context"
	"time"
)

// DefaultWireBudget is how long a wire client may take to answer an
// ask.
//
// The budget is the client's own: the chain's deadline is the outer
// limit, and a client that takes longer than this passes the ask on.
const DefaultWireBudget = 60 * time.Second

// Client is the wire client the wire asker asks.
//
// The wire implements it: the client is connected, it declared the
// permission_answers capability, and it answers the ask. The ask
// carries the fields the wire's permission/ask request carries.
type Client interface {
	// Connected reports whether a client is connected.
	Connected() bool
	// PermissionAnswers reports whether the connected client declared
	// the permission_answers capability.
	//
	// A client that did not declare it is never asked: it may not
	// answer, and asking it would wait for a reply that never comes.
	PermissionAnswers() bool
	// Ask sends the ask to the client and waits for its answer.
	//
	// An error is a deferral: the chain moves on to the next asker.
	Ask(ctx context.Context, ask Ask) (Decision, error)
}

// WireAsker asks a wire client.
//
// The asker is the seam the wire implements: the wire sends the ask as
// its permission/ask request and reads the answer from the client's
// reply.
type WireAsker struct {
	// client is the wire client the asker asks.
	client Client
	// budget is how long the client may take to answer.
	budget time.Duration
}

// NewWireAsker returns an asker that asks the given client.
//
// A budget of zero means the default budget.
func NewWireAsker(client Client, budget time.Duration) *WireAsker {
	if budget <= 0 {
		budget = DefaultWireBudget
	}
	return &WireAsker{client: client, budget: budget}
}

// Available reports whether a client can answer right now.
//
// The client must be connected and must have declared the
// permission_answers capability: a client that did not declare it may
// not answer, and the asker never waits for a reply that cannot come.
func (a *WireAsker) Available() bool {
	if a.client == nil {
		return false
	}
	return a.client.Connected() && a.client.PermissionAnswers()
}

// CanEverAnswer reports whether the asker may answer an ask at some
// point in this build.
//
// A build with no wire at all has no client, and the asker can never
// answer: the load-time warning says so.
func (a *WireAsker) CanEverAnswer() bool {
	return a.client != nil
}

// Budget is how long the client may take to answer.
func (a *WireAsker) Budget() time.Duration {
	return a.budget
}

// AskerName is the name the asker's answers carry.
func (a *WireAsker) AskerName() string { return "wire" }

// Ask sends the ask to the client and waits for its answer.
//
// A client that is not there, a failure and an answer that is neither an
// allowance nor a refusal are all deferrals.
func (a *WireAsker) Ask(ctx context.Context, ask Ask) (Decision, error) {
	if !a.Available() {
		return DecisionDefer, nil
	}
	decision, err := a.client.Ask(ctx, ask)
	if err != nil {
		return DecisionDefer, err
	}
	switch decision {
	case DecisionAllow, DecisionDeny:
		return decision, nil
	}
	return DecisionDefer, nil
}
