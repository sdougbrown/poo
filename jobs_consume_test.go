package poo_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The consumption of a job's result: a queued result is removed only
// when its delivered message is recorded, and a drain that fails leaves
// the results queued, in order.

// TestAFailedDeliveryLeavesTheResultQueued covers the plan's rule: a
// drain that fails to write leaves the results queued, in order.
func TestAFailedDeliveryLeavesTheResultQueued(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	// The sink refuses the message that carries the result, and only
	// that one: the first message it sees is the prompt's.
	sink := &failingSink{failRecord: "message", failAfter: 4}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	// The result is delivered, and the write of the message fails.
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
	// The session is broken, because the store refused a record.
	waitFor(t, "the session to break", func() bool {
		return s.Broken()
	})
}

// TestTheJobResultIsDeliveredInCompletionOrder covers the plan's rule:
// the job queue drains in completion order.
func TestTheJobResultIsDeliveredInCompletionOrder(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{ID: "call_1", Name: "job", Arguments: json.RawMessage(`{"text":"one"}`)},
			{ID: "call_2", Name: "job", Arguments: json.RawMessage(`{"text":"two"}`)},
		}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	waitFor(t, "the jobs to start", func() bool {
		return tool.Calls() == 2
	})
	// The jobs end one after the other, and their results are queued in
	// completion order. The first job is released first, so it has the
	// lower id.
	before := sink.count("job.ended")
	tool.Release()
	waitFor(t, "the first job to end", func() bool {
		return sink.count("job.ended") > before
	})
	tool.Release()
	waitFor(t, "the results to be delivered", func() bool {
		return countJobDeliveries(sink) == 2
	})
	// The queue hands the results out in the order they were finished,
	// so the input ids follow the jobs' order: the first job to finish
	// has the lower id.
	ids := map[string]bool{}
	for _, rec := range sink.allRecords() {
		ended, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		ids[ended.InputID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("the log holds %d job.ended records, want 2", len(ids))
	}
	if !ids["s1-ji1"] || !ids["s1-ji2"] {
		t.Errorf("the results are %v, want the two ids in completion order", ids)
	}
	// The deliveries follow the queue's order.
	var delivered []string
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDelivered)
		if !ok || d.Mode != poo.InputModeJob {
			continue
		}
		delivered = append(delivered, d.InputID)
	}
	if len(delivered) != 2 {
		t.Fatalf("the log holds %d job deliveries, want 2", len(delivered))
	}
	if delivered[0] >= delivered[1] {
		t.Errorf("the deliveries are %v, want them in completion order", delivered)
	}
}

// countJobDeliveries returns how many job results were delivered.
func countJobDeliveries(sink *recordingSink) int {
	n := 0
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDelivered)
		if !ok || d.Mode != poo.InputModeJob {
			continue
		}
		n++
	}
	return n
}

// TestTheJobsToolListsTheHeldJobs covers the plan's rule: the jobs tool
// lists the running and the held jobs.
func TestTheJobsToolListsTheHeldJobs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{calls: []poo.ToolCall{{
			ID:        "call_2",
			Name:      "jobs",
			Arguments: json.RawMessage(`{}`),
		}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	tool.Release()
	waitFor(t, "the result to be held", func() bool {
		return sink.count("input.held") == 1
	})
	// The held job is named by the session.
	held := s.HeldJobs()
	if len(held) != 1 || held[0] != "j1" {
		t.Errorf("the held jobs are %v, want [j1]", held)
	}
}

// TestCancelJobOnAnUnknownJobIsReported covers the plan's rule: a job
// that is unknown or already finished is not a failure of the call.
func TestCancelJobOnAnUnknownJobIsReported(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "cancel_job",
			Arguments: json.RawMessage(`{"job":"j9"}`),
		}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The model saw the error, and the run carried on.
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Tool == "cancel_job" && r.Outcome == poo.OutcomeError {
			found = true
		}
	}
	if !found {
		t.Error("the model did not see the error")
	}
}

// TestWaitJobsWaitsForTheRunningJobs covers the one-shot's wait: it
// ends when no job is running and no result is eligible to wake a run.
func TestWaitJobsWaitsForTheRunningJobs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The job is running, so the wait does not end.
	done := make(chan error, 1)
	go func() {
		done <- s.WaitJobs(context.Background())
	}()
	select {
	case err := <-done:
		t.Fatalf("the wait ended with %v, want it to wait for the job", err)
	case <-time.After(50 * time.Millisecond):
	}
	// The job ends, and the wait ends with it.
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the wait ended with %v, want no error", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the wait did not end after the job ended")
	}
}

// TestAJobResultDuringCompactingStaysQueued covers the plan's rule: a
// result that arrives while the view is compacting stays queued, and it
// is drained at the next turn end.
func TestAJobResultDuringCompactingStaysQueued(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	compactor := pootest.NewCompactor(true, poo.Compaction{Strategy: "test"})
	opts := jobOptions(t, tr, tool, sink)
	opts.Compactor = compactor

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	// The run compacts the view while the job is still running.
	waitFor(t, "the compactor to be asked", func() bool {
		return compactor.Calls() > 0
	})
	// The job ends: the result waits for the next turn end, and the
	// compactor's next view does not hold it.
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
	waitFor(t, "the result to be delivered", func() bool {
		return countJobDeliveries(sink) == 1
	})
	views := compactor.Views()
	if len(views) == 0 {
		t.Fatal("the compactor was never asked")
	}
	// The first view was taken before the result arrived, so it does
	// not hold it.
	for _, m := range views[0].Messages() {
		if m.Origin == poo.OriginJob {
			t.Error("the compactor's first view holds the job's result")
		}
	}
}
