package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/wire/protocol"
)

// wireClient is the connected client.
//
// It is the perm.Client the asker chain asks: the client answered the
// handshake, and it answers the asks. A client that did not declare
// the permission_answers capability is never asked, because it may not
// answer.
type wireClient struct {
	// srv is the server the client is connected to.
	srv *server
}

// Connected reports that a client is connected.
func (c *wireClient) Connected() bool { return true }

// PermissionAnswers reports that the client declared the capability.
func (c *wireClient) PermissionAnswers() bool {
	c.srv.mu.Lock()
	defer c.srv.mu.Unlock()
	return c.srv.clientInfo.Capabilities.PermissionAnswers
}

// Ask sends the ask to the client and waits for its answer.
//
// The request's id is the ask's id, so the client answers it without
// keeping a mapping of its own. A client that answers with an error, a
// client that answers with something that is not a decision, and a
// client that does not answer at all are all deferrals: the chain
// moves on to the next asker.
func (c *wireClient) Ask(ctx context.Context, ask perm.Ask) (perm.Decision, error) {
	if !c.PermissionAnswers() {
		return perm.DecisionDefer, nil
	}
	params := protocol.AskParams{
		SessionID: ask.SessionID,
		RunID:     ask.RunID,
		AskID:     ask.AskID,
		Tool:      ask.Tool,
		Summary:   ask.Summary,
		Class:     string(ask.Class),
		Reason:    string(ask.Reason),
	}
	if ask.RepeatCount != nil {
		params.RepeatCount = &protocol.RepeatCount{
			Exact: ask.RepeatCount.Exact,
			Near:  ask.RepeatCount.Near,
		}
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return perm.DecisionDefer, fmt.Errorf("wire: encoding the ask: %w", err)
	}
	id, err := json.Marshal(ask.AskID)
	if err != nil {
		return perm.DecisionDefer, fmt.Errorf("wire: encoding the ask id: %w", err)
	}
	answers := make(chan *protocol.Response, 1)
	key := idKey(id)
	c.srv.mu.Lock()
	c.srv.pending[key] = answers
	c.srv.mu.Unlock()
	defer func() {
		c.srv.mu.Lock()
		delete(c.srv.pending, key)
		c.srv.mu.Unlock()
	}()
	if err := c.srv.send(protocol.Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  protocol.MethodPermissionAsk,
		Params:  encoded,
	}); err != nil {
		return perm.DecisionDefer, err
	}
	budget := c.srv.bounds.AskBudget
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case answer := <-answers:
		return decisionOf(answer)
	case <-ctx.Done():
		return perm.DecisionDefer, ctx.Err()
	case <-timer.C:
		return perm.DecisionDefer, fmt.Errorf("wire: the client did not answer the ask within %s", budget)
	}
}

// decisionOf reads the decision out of a client's answer.
//
// An error answer and an answer that is not a decision are deferrals.
func decisionOf(answer *protocol.Response) (perm.Decision, error) {
	if answer == nil {
		return perm.DecisionDefer, errors.New("wire: the client sent no answer")
	}
	if answer.Error != nil {
		return perm.DecisionDefer, errors.New(answer.Error.Message)
	}
	var result protocol.AskResult
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		return perm.DecisionDefer, fmt.Errorf("wire: decoding the answer: %w", err)
	}
	switch result.Decision {
	case protocol.DecisionAllow:
		return perm.DecisionAllow, nil
	case protocol.DecisionDeny:
		return perm.DecisionDeny, nil
	}
	return perm.DecisionDefer, nil
}
