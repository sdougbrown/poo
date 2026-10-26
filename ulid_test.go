package poo_test

import (
	"context"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

var ulidShape = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// TestRunAndInputIDsAreULIDs covers the plan's Identity section: every
// run has a run_id that is a ULID, and an input id has the same shape.
func TestRunAndInputIDsAreULIDs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	inputID, err := s.Send("and this", poo.Steer)
	if err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	for what, id := range map[string]string{"run": runID, "input": inputID} {
		if !ulidShape.MatchString(id) {
			t.Errorf("the %s id %q is not a ULID", what, id)
		}
	}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStarted); ok && r.RunID != runID {
			t.Errorf("run.started names %q, want %q", r.RunID, runID)
		}
	}
}

// TestULIDsAreUniqueAndSortByTime covers the generator: ids from
// different milliseconds sort in time order, and ids never repeat.
func TestULIDsAreUniqueAndSortByTime(t *testing.T) {
	var ids []string
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		for j := 0; j < 200; j++ {
			id := poo.NewULID()
			if seen[id] {
				t.Fatalf("the id %q repeated", id)
			}
			seen[id] = true
		}
		ids = append(ids, poo.NewULID())
		time.Sleep(2 * time.Millisecond)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("ids from successive milliseconds do not sort in time order: %v", ids)
	}
}
