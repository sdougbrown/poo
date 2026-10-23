package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: the rebase the plan prescribes.

// TestRebaseUsesThePlanRatio covers the rule that the rebase uses 3.5
// chars/token over the whole view until the next usage report.
func TestRebaseUsesThePlanRatio(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: fix2Turn().calls, usage: &poo.UsageReport{InputTokens: 300, OutputTokens: 10}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 8000)}, sink)
	opts.Model.ContextLimit = 2000
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			c := fix2Summary()
			c.Replace.Messages = []poo.Message{poo.NewTextMessage(poo.RoleUser, strings.Repeat("s", 4200))}
			return c, nil
		},
	}
	s := newTestSession(t, opts)
	res := runSession(t, s, "start")
	if len(tr.Requests()) != 2 {
		t.Fatalf("4200-character summary fits 2000-token window at 3.5 chars/token but settled=%s source=%s requests=%d", res.Reason, res.Detail.Source, len(tr.Requests()))
	}
}
