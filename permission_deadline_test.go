package poo_test

import (
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

// The ask's record carries the deadline it expires on and the decision it
// falls back to, so a durable session that finds an unanswered ask knows
// both.

// TestTheAskedRecordCarriesTheDeadlineAndFallback covers the fields: the
// chain's timeout reaches the record as a wall-clock deadline, and the
// chain's fallback reaches it as the decision an expired ask resolves to.
func TestTheAskedRecordCarriesTheDeadlineAndFallback(t *testing.T) {
	const timeout = 2 * time.Minute
	asker := &askCapture{}
	policy, err := perm.New(perm.Options{
		Askers:       []perm.Asker{asker},
		ChainTimeout: timeout,
		Fallback:     perm.ActionAllow,
	})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	// The call is side-effecting, so the policy asks about it.
	tool := &echoTool{class: poo.ClassSideEffecting, result: "the effect"}
	s := newTestSession(t, testOptions(t, tr, tool, sink), poo.WithPolicy(policy))
	before := time.Now()
	runSession(t, s, "start")

	var asked *poo.PermissionAskedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.PermissionAskedRecord); ok {
			asked = r
		}
	}
	if asked == nil {
		t.Fatal("the log holds no permission.asked record")
	}
	if asked.Fallback != string(perm.ActionAllow) {
		t.Errorf("the ask's fallback is %q, want allow", asked.Fallback)
	}
	if asked.Deadline == "" {
		t.Fatal("the ask names no deadline")
	}
	deadline, err := time.Parse(time.RFC3339Nano, asked.Deadline)
	if err != nil {
		t.Fatalf("the ask's deadline %q is not RFC 3339: %v", asked.Deadline, err)
	}
	// The deadline is the chain's timeout from the moment the ask was
	// made, so it falls inside the window the run took.
	earliest := before.Add(timeout).Add(-time.Second)
	latest := time.Now().Add(timeout).Add(time.Second)
	if deadline.Before(earliest) || deadline.After(latest) {
		t.Errorf("the ask's deadline is %s, want about %s", deadline, before.Add(timeout))
	}
	if got := deadline.Sub(before); got < timeout-time.Minute || got > timeout+time.Minute {
		t.Errorf("the ask's deadline is %s after the run started, want about %s", got, timeout)
	}
}
