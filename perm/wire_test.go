package perm_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// The wire is the next stage's: the asker is the seam the wire
// implements. The client is the wire's own, and the ask carries what
// the wire's permission/ask request carries.

// fakeClient is a wire client under test.
type fakeClient struct {
	mu sync.Mutex
	// connected reports whether a client is connected.
	connected bool
	// answers reports whether the client declared the permission_answers
	// capability.
	answers bool
	// decision is what the client answers.
	decision perm.Decision
	// message is the note the client's answer carries, when the client
	// has the message seam.
	message string
	// err is the failure the client returns, when it is set.
	err error
	// asks holds the asks the client was given.
	asks []perm.Ask
}

// Connected reports whether a client is connected.
func (c *fakeClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// PermissionAnswers reports whether the client declared the capability.
func (c *fakeClient) PermissionAnswers() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answers
}

// Ask answers the ask.
func (c *fakeClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	c.mu.Lock()
	c.asks = append(c.asks, ask)
	decision := c.decision
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return perm.DecisionDefer, err
	}
	return decision, nil
}

// AskWithMessage answers the ask with the client's message.
func (c *fakeClient) AskWithMessage(ctx context.Context, ask perm.Ask) (perm.Answer, error) {
	decision, err := c.Ask(ctx, ask)
	if err != nil {
		return perm.Answer{Decision: perm.DecisionDefer}, err
	}
	return perm.Answer{Decision: decision, Message: c.message}, nil
}

// Asks returns the asks the client was given.
func (c *fakeClient) Asks() []perm.Ask {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]perm.Ask(nil), c.asks...)
}

// plainClient is a client with no message seam: it implements only the
// Client the asker falls back to.
type plainClient struct {
	mu sync.Mutex
	// decision is what the client answers.
	decision perm.Decision
	// asks holds the asks the client was given.
	asks []perm.Ask
}

// Connected reports whether a client is connected.
func (c *plainClient) Connected() bool { return true }

// PermissionAnswers reports whether the client declared the capability.
func (c *plainClient) PermissionAnswers() bool { return true }

// Ask answers the ask.
func (c *plainClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asks = append(c.asks, ask)
	return c.decision, nil
}

// Asks returns the asks the client was given.
func (c *plainClient) Asks() []perm.Ask {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]perm.Ask(nil), c.asks...)
}

// TestTheWireAskerNeedsAClientThatDeclaredTheCapability covers the rule: the wire asker is available only when a client is
// connected and declared the permission_answers capability.
func TestTheWireAskerNeedsAClientThatDeclaredTheCapability(t *testing.T) {
	tests := []struct {
		name      string
		client    *fakeClient
		available bool
	}{
		{
			name:      "a connected client that declared the capability",
			client:    &fakeClient{connected: true, answers: true},
			available: true,
		},
		{
			name:      "a connected client that did not declare the capability",
			client:    &fakeClient{connected: true},
			available: false,
		},
		{
			name:      "a client that is not connected",
			client:    &fakeClient{answers: true},
			available: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asker := perm.NewWireAsker(tt.client, 0)
			if got := asker.Available(); got != tt.available {
				t.Errorf("the asker is available %t, want %t", got, tt.available)
			}
		})
	}
}

// TestTheWireAskerWithoutAClientIsNeverAvailable covers a build with no
// wire at all: the asker can never answer, and the load-time warning
// says so.
func TestTheWireAskerWithoutAClientIsNeverAvailable(t *testing.T) {
	asker := perm.NewWireAsker(nil, 0)
	if asker.Available() {
		t.Error("the asker is available, want it to be unavailable without a client")
	}
	if asker.CanEverAnswer() {
		t.Error("the asker can answer, want it never to answer without a client")
	}
}

// TestTheWireAskCarriesTheDoomLoopReasonAndTheRepeatCount covers the
// wire's permission/ask: it carries the reason and the counts that
// triggered the loop.
func TestTheWireAskCarriesTheDoomLoopReasonAndTheRepeatCount(t *testing.T) {
	client := &fakeClient{connected: true, answers: true, decision: perm.DecisionAllow}
	asker := perm.NewWireAsker(client, 0)
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

	asks := client.Asks()
	if len(asks) != 1 {
		t.Fatalf("the client was asked %d time(s), want 1", len(asks))
	}
	ask := asks[0]
	if ask.Reason != poo.AskReasonDoomLoop {
		t.Errorf("the ask's reason is %q, want %q", ask.Reason, poo.AskReasonDoomLoop)
	}
	if ask.RepeatCount == nil {
		t.Fatal("the ask carries no repeat count")
	}
	if ask.RepeatCount.Exact != 3 {
		t.Errorf("the ask's exact count is %d, want 3", ask.RepeatCount.Exact)
	}
	// The payload the wire sends carries the reason and the counts.
	raw, err := json.Marshal(ask)
	if err != nil {
		t.Fatalf("encoding the ask: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decoding the ask: %v", err)
	}
	if payload["reason"] != string(poo.AskReasonDoomLoop) {
		t.Errorf("the payload's reason is %v, want %q", payload["reason"], poo.AskReasonDoomLoop)
	}
	counts, ok := payload["repeat_count"].(map[string]any)
	if !ok {
		t.Fatalf("the payload carries no repeat count: %s", raw)
	}
	if counts["exact"] != float64(3) {
		t.Errorf("the payload's exact count is %v, want 3", counts["exact"])
	}
}

// TestTheAskFallsBackWithoutACapableClient covers the rule: a
// client that did not declare the capability is never asked, and the
// fallback applies at once.
func TestTheAskFallsBackWithoutACapableClient(t *testing.T) {
	client := &fakeClient{connected: true, answers: false, decision: perm.DecisionAllow}
	asker := perm.NewWireAsker(client, 0)
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

	if asks := client.Asks(); len(asks) != 0 {
		t.Errorf("the client was asked %d time(s), want 0: it may not answer", len(asks))
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d time(s), want 0: the fallback refused the call", tool.Calls())
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceDefault {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceDefault)
	}
}

// TestTheWireAskerDefersOnAClientError covers the rule: a
// client error is a deferral, and the chain moves on.
func TestTheWireAskerDefersOnAClientError(t *testing.T) {
	client := &fakeClient{connected: true, answers: true, err: errors.New("the client is gone")}
	asker := perm.NewWireAsker(client, 0)
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
		t.Errorf("the tool ran %d time(s), want 0: the fallback refused the call", tool.Calls())
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["provenance"] != poo.ProvenanceDefault {
		t.Errorf("the answer's provenance is %q, want %q", answered[0].Payload["provenance"], poo.ProvenanceDefault)
	}
}

// TestTheWireAskerAnswersWhenTheClientAnswers covers the happy path: the
// client's answer decides the ask.
func TestTheWireAskerAnswersWhenTheClientAnswers(t *testing.T) {
	client := &fakeClient{connected: true, answers: true, decision: perm.DecisionAllow}
	asker := perm.NewWireAsker(client, 0)
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

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the client allowed the call", tool.Calls())
	}
	answered := s.events(t, poo.EventPermissionAnswered)
	if len(answered) != 1 {
		t.Fatalf("the session emitted %d answer event(s), want 1", len(answered))
	}
	if answered[0].Payload["answered_by"] != "wire" {
		t.Errorf("the answer's answered_by is %q, want the wire", answered[0].Payload["answered_by"])
	}
}

// TestTheWireAskerCarriesTheClientMessage covers the message seam: the
// asker hands the client's message to the chain, and a client without
// the seam still answers without one.
func TestTheWireAskerCarriesTheClientMessage(t *testing.T) {
	client := &fakeClient{connected: true, answers: true, decision: perm.DecisionDeny, message: "not while I am watching"}
	asker := perm.NewWireAsker(client, 0)

	answer, err := asker.AskWithMessage(context.Background(), perm.Ask{AskID: "a1"})
	if err != nil {
		t.Fatalf("the ask failed: %v", err)
	}
	if answer.Decision != perm.DecisionDeny {
		t.Errorf("the answer's decision is %q, want %q", answer.Decision, perm.DecisionDeny)
	}
	if answer.Message != "not while I am watching" {
		t.Errorf("the answer's message is %q, want the client's", answer.Message)
	}

	// A client that implements only Client is asked through its Ask,
	// and its decision reaches the answer with no message.
	plain := &plainClient{decision: perm.DecisionDeny}
	asker = perm.NewWireAsker(plain, 0)
	answer, err = asker.AskWithMessage(context.Background(), perm.Ask{AskID: "a2"})
	if err != nil {
		t.Fatalf("the ask failed: %v", err)
	}
	if answer.Decision != perm.DecisionDeny {
		t.Errorf("the answer's decision is %q, want %q", answer.Decision, perm.DecisionDeny)
	}
	if answer.Message != "" {
		t.Errorf("the answer's message is %q, want none", answer.Message)
	}
	asks := plain.Asks()
	if len(asks) != 1 || asks[0].AskID != "a2" {
		t.Errorf("the client was asked %+v, want the one ask a2", asks)
	}
}

// TestTheWireAskerNormalizesAnUnknownAnswer covers the client that
// answers with neither an allowance nor a refusal: the asker defers,
// whatever the answer carries.
func TestTheWireAskerNormalizesAnUnknownAnswer(t *testing.T) {
	client := &fakeClient{connected: true, answers: true, decision: perm.Decision("later"), message: "soon"}
	asker := perm.NewWireAsker(client, 0)

	answer, err := asker.AskWithMessage(context.Background(), perm.Ask{AskID: "a1"})
	if err != nil {
		t.Fatalf("the ask failed: %v", err)
	}
	if answer.Decision != perm.DecisionDefer {
		t.Errorf("the answer's decision is %q, want a deferral", answer.Decision)
	}
	if answer.Message != "" {
		t.Errorf("the answer's message is %q, want none: a deferral answers nothing", answer.Message)
	}
}
