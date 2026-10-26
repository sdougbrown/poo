package poo_test

import (
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: a rewrite is atomic and copies before it
// changes anything.

// TestFailedRewriteIsAtomic covers the rule that a rewrite resolves
// every target before it commits.
func TestFailedRewriteIsAtomic(t *testing.T) {
	tr := scripted([]fakeResponse{
		fix2Turn(),
		{calls: []poo.ToolCall{{ID: "c2", Name: "echo", Arguments: []byte(`{"text":"two"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "original"}, sink)
	calls := 0
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			calls++
			if calls == 1 {
				return fix2Summary(), nil
			}
			return poo.Compaction{Strategy: "rewrite", Rewrite: []poo.ToolResultRewrite{
				{Handle: "r2", Content: poo.CompactedHandleLine("r2", "echo") + "\nunrecorded mutation"},
				{Handle: "r1", Content: poo.CompactedHandleLine("r1", "echo") + "\nmissing"},
			}}, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, m := range tr.Requests()[2].Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "unrecorded mutation") {
				t.Fatalf("failed rewrite changed view with no record: compactions=%d, result=%q", sink.count("compaction"), b.Text)
			}
		}
	}
}

// TestRewriteKeepsTheOriginalRecord covers the rule that a rewrite
// copies before it changes the message.
func TestRewriteKeepsTheOriginalRecord(t *testing.T) {
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "original"}, sink)
	opts.Compactor = &compactionCompactor{
		should: afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) {
			return poo.Compaction{Strategy: "rewrite", Rewrite: []poo.ToolResultRewrite{{
				Handle:  "r1",
				Content: poo.CompactedHandleLine("r1", "echo") + "\nsummary",
			}}}, nil
		},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	for _, r := range sink.messages() {
		if r.Role == poo.RoleTool && !strings.Contains(r.Content[0].Text, "original") {
			t.Fatalf("original message record mutated by rewrite: %q", r.Content[0].Text)
		}
	}
}
