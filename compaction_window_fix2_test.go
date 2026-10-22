package poo_test

import (
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: a provider window comes from the session's
// own transport.

// TestRejectWindowNotProducedByTransport covers the rule that a window
// comes from the session's own transport.
func TestRejectWindowNotProducedByTransport(t *testing.T) {
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return fix2Window(), nil },
	}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if sink.count("compaction") != 0 {
		t.Fatalf("accepted fabricated window without calling NativeCompactor: compactions=%d", sink.count("compaction"))
	}
}
