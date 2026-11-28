package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// A side-effecting call's start is on the log before the tool runs, so a
// crash that cuts the call short leaves a record of the effect that may
// have happened.

// toolStartedRecords returns the tool.started records a session wrote, in
// order.
func toolStartedRecords(sink *recordingSink) []*poo.ToolStartedRecord {
	var out []*poo.ToolStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolStartedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// TestASideEffectingCallsStartIsRecordedBeforeTheToolRuns covers the
// write-ahead rule: the call's start is in the store by the time the tool
// is entered, so an effect a crash cut short is still on the log.
func TestASideEffectingCallsStartIsRecordedBeforeTheToolRuns(t *testing.T) {
	sink := &recordingSink{}
	held := make(chan []*poo.ToolStartedRecord, 1)
	tool := &echoTool{class: poo.ClassSideEffecting, call: func(ctx context.Context) (poo.ToolResult, error) {
		// The tool is running, so the record must already be there: the
		// session writes it before it calls the tool.
		held <- toolStartedRecords(sink)
		return poo.ToolResult{Text: "the effect"}, nil
	}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if res := runSession(t, s, "start"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}

	inStore := <-held
	if len(inStore) != 1 {
		t.Fatalf("the tool ran with %d tool.started records in the store, want 1", len(inStore))
	}
	rec := inStore[0]
	if rec.RunID == "" {
		t.Error("the record names no run")
	}
	if rec.Turn != 1 {
		t.Errorf("the record names the turn %d, want 1", rec.Turn)
	}
	if rec.Handle != "r1" {
		t.Errorf("the record names the handle %q, want r1", rec.Handle)
	}
	if rec.CallID != "c1" {
		t.Errorf("the record names the call id %q, want c1", rec.CallID)
	}
	if rec.Tool != "echo" {
		t.Errorf("the record names the tool %q, want echo", rec.Tool)
	}
	// The start is written once, and it precedes the result it belongs to.
	if n := len(toolStartedRecords(sink)); n != 1 {
		t.Errorf("the log holds %d tool.started records, want 1", n)
	}
	start, result := -1, -1
	for i, name := range sink.Names() {
		switch name {
		case "tool.started":
			start = i
		case "tool.result":
			result = i
		}
	}
	if start < 0 || !(start < result) {
		t.Errorf("the records are %v, want tool.started before tool.result", sink.Names())
	}
}

// TestAReadOnlyCallsStartIsNotRecorded covers the other half of the rule:
// a read-only call changes nothing, so there is no effect for a durable
// session to replay and the log carries no start.
func TestAReadOnlyCallsStartIsNotRecorded(t *testing.T) {
	sink := &recordingSink{}
	tool := &echoTool{class: poo.ClassReadOnly}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "start")

	if n := len(toolStartedRecords(sink)); n != 0 {
		t.Errorf("the log holds %d tool.started records for a read-only call, want none", n)
	}
	if n := sink.count("tool.result"); n != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", n)
	}
}

// TestAJobsStartIsRecordedBeforeTheJobRuns covers the job path: the
// record is written on the job's own goroutine, before the job's tool is
// entered, so a job that outlives its turn is durable from its first
// moment.
func TestAJobsStartIsRecordedBeforeTheJobRuns(t *testing.T) {
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "done"},
	})
	job := pootest.NewJobTool("job", poo.BackgroundAlways)
	job.SetResult("the job's output")
	s := newTestSession(t, jobOptions(t, tr, job, sink))

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-job.Started()

	// The job's tool has been entered, so its start is already stored.
	held := toolStartedRecords(sink)
	if len(held) != 1 {
		t.Fatalf("the job ran with %d tool.started records in the store, want 1", len(held))
	}
	rec := held[0]
	if rec.RunID == "" {
		t.Error("the record names no run")
	}
	if rec.Turn != 1 {
		t.Errorf("the record names the turn %d, want 1", rec.Turn)
	}
	if rec.Tool != "job" {
		t.Errorf("the record names the tool %q, want job", rec.Tool)
	}
	if rec.CallID != "call_one" {
		t.Errorf("the record names the call id %q, want call_one", rec.CallID)
	}
	if rec.Handle == "" {
		t.Error("the record names no handle")
	}

	job.ReleaseAll()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if n := len(toolStartedRecords(sink)); n != 1 {
		t.Errorf("the log holds %d tool.started records, want 1", n)
	}
}
