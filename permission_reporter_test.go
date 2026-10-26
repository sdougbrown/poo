package poo_test

import (
	"context"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/perm"
)

// allowingAsker answers every ask it is given.
type allowingAsker struct{}

func (allowingAsker) Available() bool       { return true }
func (allowingAsker) Budget() time.Duration { return time.Minute }
func (allowingAsker) Ask(context.Context, perm.Ask) (perm.Decision, error) {
	return perm.DecisionAllow, nil
}

// TestAnAnsweredAskLogsNoStaleInput covers the request's logging rule: only
// an input that arrives after the request resolved is stale. The chain
// hands its own terminal answer back to the reporter, and that echo is not
// a stale input.
func TestAnAnsweredAskLogsNoStaleInput(t *testing.T) {
	logger, capture := newCaptureLogger()
	policy, err := perm.New(perm.Options{Askers: []perm.Asker{allowingAsker{}}})
	if err != nil {
		t.Fatalf("building the policy: %v", err)
	}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{class: poo.ClassSideEffecting, result: "ok"}, sink)
	opts.Logger = logger
	s := newTestSession(t, opts, poo.WithPolicy(policy))

	if res := runSession(t, s, "start"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	waitFor(t, "the permission answer", func() bool { return sink.count("permission.answered") == 1 })
	// The log dispatch is asynchronous, so a record queued while the ask
	// resolved would reach the capture shortly after the answer does.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := capture.find("stale permission input dropped"); len(got) != 0 {
			t.Fatalf("an answered ask logged %d stale input(s): %+v", len(got), got[0].attrs)
		}
		time.Sleep(time.Millisecond)
	}
}
