package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: a replacement must be visible.

// TestReplacementMustBeVisible covers the rule that a replacement is
// non-empty visible user content.
func TestReplacementMustBeVisible(t *testing.T) {
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			c := fix2Summary()
			c.Replace.Messages = []poo.Message{{}}
			return c, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if sink.count("compaction") != 0 {
		t.Fatalf("accepted empty invisible replacement: compactions=%d, messages=%+v", sink.count("compaction"), tr.Requests()[1].Messages)
	}
}
