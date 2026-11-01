package perm_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
	"github.com/sdougbrown/poo/pootest"
)

// messageAsker answers with a fixed answer that carries a message for
// the model.
type messageAsker struct {
	fakeAsker
	// message is the note the answer carries.
	message string
}

// AskWithMessage answers the ask.
func (a *messageAsker) AskWithMessage(ctx context.Context, ask perm.Ask) (perm.Answer, error) {
	decision, err := a.Ask(ctx, ask)
	if err != nil {
		return perm.Answer{Decision: perm.DecisionDefer}, err
	}
	return perm.Answer{Decision: decision, Message: a.message}, nil
}

// answeredRecord returns the permission.answered record of an ask, and
// fails when the log does not hold exactly one.
func answeredRecord(t *testing.T, s *session) *poo.PermissionAnsweredRecord {
	t.Helper()
	var found *poo.PermissionAnsweredRecord
	for _, rec := range s.sink.Records() {
		answered, ok := rec.(*poo.PermissionAnsweredRecord)
		if !ok || answered.AskID == "" {
			continue
		}
		if found != nil {
			t.Fatal("the log holds more than one answered ask")
		}
		found = answered
	}
	if found == nil {
		t.Fatal("the log holds no answered ask")
	}
	return found
}

// TestAnAnswerMessageReachesTheDeniedResult covers the plan's rule: an
// answer that carries a message shows the message to the model, and
// the log keeps it.
func TestAnAnswerMessageReachesTheDeniedResult(t *testing.T) {
	asker := &messageAsker{fakeAsker: fakeAsker{decision: perm.DecisionDeny, available: true}, message: "ask your operator first"}
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
	want := "Blocked: the permission ask was denied by fake: ask your operator first."
	if got := s.lastResult(t); got != want {
		t.Errorf("the model saw %q, want %q", got, want)
	}
	rec := answeredRecord(t, s)
	if rec.Message != "ask your operator first" {
		t.Errorf("the record's message is %q, want the answer's", rec.Message)
	}
}

// TestAnAnswerMessageRidesOnAnAllowance covers the same rule for an
// allowance: the tool runs, and the log keeps the message.
func TestAnAnswerMessageRidesOnAnAllowance(t *testing.T) {
	asker := &messageAsker{fakeAsker: fakeAsker{decision: perm.DecisionAllow, available: true}, message: "mind the working tree"}
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{asker}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tool := &bashTool{}
	s := newSession(t, []pootest.Turn{
		{Calls: []pootest.Call{bashCall("c1", "echo hi")}},
		{Text: "done"},
	}, map[string]poo.Tool{"bash": tool}, []string{"bash"}, policy)

	s.run(t)

	if tool.Calls() != 1 {
		t.Errorf("the tool ran %d time(s), want 1: the asker allowed the call", tool.Calls())
	}
	rec := answeredRecord(t, s)
	if rec.Decision != string(perm.DecisionAllow) {
		t.Errorf("the record's decision is %q, want %q", rec.Decision, perm.DecisionAllow)
	}
	if rec.Message != "mind the working tree" {
		t.Errorf("the record's message is %q, want the answer's", rec.Message)
	}
}

// TestAnAnswerWithoutAMessageWritesTheOldRecord covers the forward
// compatibility: an answer without a message writes the record the
// older core wrote, byte for byte.
func TestAnAnswerWithoutAMessageWritesTheOldRecord(t *testing.T) {
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

	if got := s.lastResult(t); got != "Blocked: the permission ask was denied by fake." {
		t.Errorf("the model saw %q, want the plain refusal", got)
	}
	rec := answeredRecord(t, s)
	if rec.Message != "" {
		t.Errorf("the record's message is %q, want none", rec.Message)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("encoding the record: %v", err)
	}
	if strings.Contains(string(line), `"message"`) {
		t.Errorf("the record carries a message field: %s", line)
	}
}
