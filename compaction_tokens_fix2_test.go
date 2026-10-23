package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: tokens_after is the rebased estimate.

// TestTokensAfterUsesTheRebasedEstimate covers the rule that
// tokens_after is measured after the rebase.
func TestTokensAfterUsesTheRebasedEstimate(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: fix2Turn().calls, usage: &poo.UsageReport{InputTokens: 7000, OutputTokens: 10}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: strings.Repeat("x", 1000)}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return fix2Summary(), nil },
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, r := range sink.allRecords() {
		if c, ok := r.(*poo.CompactionRecord); ok && c.TokensAfter >= 7000 {
			t.Fatalf("tokens_after=%d retains pre-compaction usage baseline 7000 for short summary", c.TokensAfter)
		}
	}
}
