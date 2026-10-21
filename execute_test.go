package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// TestACallWhoseRecordFailsStartsNoEffect covers the failure table: a
// tool.call record the store refuses is terminal, so the tool never runs.
func TestACallWhoseRecordFailsStartsNoEffect(t *testing.T) {
	sink := &failingSink{failRecord: "tool.call"}
	tool := &echoTool{result: "effect executed"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if got := len(tool.Calls()); got != 0 {
		t.Fatalf("the tool ran %d time(s) after its tool.call record was refused, want 0", got)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
}
