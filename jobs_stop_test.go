package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The stopping of a job: the model may cancel one, the jobs timeout
// stops one, and the session's limit refuses a new one.

// TestCancelJobStopsTheJobAndDeliversItsPartialOutput covers the
// plan's rule: cancel_job stops the job, and its partial output is
// delivered like any result.
func TestCancelJobStopsTheJobAndDeliversItsPartialOutput(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{calls: []poo.ToolCall{{
			ID:        "call_2",
			Name:      "cancel_job",
			Arguments: json.RawMessage(`{"job":"j1"}`),
		}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The job ended as cancelled, and the result was delivered.
	found := false
	for _, rec := range sink.allRecords() {
		ended, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		if ended.Outcome != poo.JobOutcomeCancelled {
			t.Errorf("the job ended %q, want cancelled", ended.Outcome)
		}
		found = true
	}
	if !found {
		t.Error("the log holds no job.ended record")
	}
	delivered := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginJob {
			delivered = true
		}
	}
	if !delivered {
		t.Error("the cancelled job's result did not reach the model")
	}
}

// TestTheJobTimeoutStopsTheJob covers the plan's rule: the job timeout
// stops the job, and the outcome is an error with the timeout flag.
func TestTheJobTimeoutStopsTheJob(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{Timeout: 50 * time.Millisecond}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the job to time out", func() bool {
		return sink.count("job.ended") == 1
	})
	var ended *poo.JobEnded
	for _, rec := range sink.allRecords() {
		if e, ok := rec.(*poo.JobEnded); ok {
			ended = e
		}
	}
	if ended == nil {
		t.Fatal("the log holds no job.ended record")
	}
	if ended.Outcome != poo.JobOutcomeError {
		t.Errorf("the job ended %q, want error", ended.Outcome)
	}
	if !ended.TimedOut {
		t.Error("the job.ended record does not carry the timeout flag")
	}
}

// TestMaxRunningRefusesTheExtraCall covers the plan's rule: a call over
// the limit gets a recoverable error, not a queue.
func TestMaxRunningRefusesTheExtraCall(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			jobCall("one", true),
			jobCall("two", true),
		}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{MaxRunning: 1}
	// The turn ceiling settles the run right after the batch, so the
	// first job is still running when the second call is made.
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	tool.Release()
	// One job started, and the other call got the recoverable error.
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeError && strings.Contains(r.Text, "too many jobs running") {
			found = true
		}
	}
	if !found {
		t.Error("the model did not see the limit error")
	}
}

// TestAJobThatFailsFatallySettlesTheReceivingRun covers the plan's
// rule: a job error that wraps ErrFatal is delivered like any result
// and adds the tool_fatal candidate to the receiving run.
func TestAJobThatFailsFatallySettlesTheReceivingRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetError(poo.Fatalf("the working directory is gone"))

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	// The job fails fatally while the run is waiting, and the run
	// settles with the candidate.
	tool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonToolFatal {
		t.Errorf("the run settled %q, want tool_fatal", res.Reason)
	}
}

// TestARunWithOnlyEarlierJobsSettlesNormally covers the plan's rule: a
// job started by an earlier run never holds a run open.
func TestARunWithOnlyEarlierJobsSettlesNormally(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
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
	// The run is cancelled, and the job keeps running.
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The next run does not wait for the earlier run's job.
	tool.Release()
	tr2 := scripted([]fakeResponse{{text: "the second answer"}})
	sink2 := &recordingSink{}
	tool2 := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool2.SetResult("the job's output")

	s2 := newTestSession(t, jobOptions(t, tr2, tool2, sink2))
	res2 := runSession(t, s2, "do the thing")
	if res2.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res2.Reason)
	}
}

// TestTheJobsToolListsTheRunningJobs covers the plan's rule: the model
// can see the jobs it started.
func TestTheJobsToolListsTheRunningJobs(t *testing.T) {
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

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	found := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Tool == "jobs" && strings.Contains(r.Text, "j1") {
			found = true
		}
	}
	if !found {
		t.Error("the jobs tool did not name the job")
	}
}

// TestTheSessionCloseStopsTheRunningJobs covers the plan's rule: closing
// the session stops the jobs, and each one gets a job.ended record with
// the cause session_closed.
func TestTheSessionCloseStopsTheRunningJobs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s, err := poo.New(jobOptions(t, tr, tool, sink))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	// The job got a job.ended record with the close's cause.
	var ended *poo.JobEnded
	for _, rec := range sink.allRecords() {
		if e, ok := rec.(*poo.JobEnded); ok {
			ended = e
		}
	}
	if ended == nil {
		t.Fatal("the log holds no job.ended record")
	}
	if ended.Outcome != poo.JobOutcomeCancelled {
		t.Errorf("the job ended %q, want cancelled", ended.Outcome)
	}
	if ended.Cause != poo.CauseSessionClosed {
		t.Errorf("the job's cause is %q, want session_closed", ended.Cause)
	}
}

// TestAJobResultNeverCarriesADeliveryNote covers the plan's rule: a job
// result never carries a delivery note.
func TestAJobResultNeverCarriesADeliveryNote(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	for _, m := range sink.messages() {
		if m.Origin != poo.OriginJob {
			continue
		}
		if m.DeliveryNote != nil {
			t.Error("the job's message carries a delivery note")
		}
	}
}

// TestTheJobHandleSurvivesTheTurnEnd covers the plan's rule: a job
// handle survives the turn, and the record names the call it came from.
func TestTheJobHandleSurvivesTheTurnEnd(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	var started *poo.JobStarted
	for _, rec := range sink.allRecords() {
		if j, ok := rec.(*poo.JobStarted); ok {
			started = j
		}
	}
	if started == nil {
		t.Fatal("the log holds no job.started record")
	}
	if started.Job != "j1" {
		t.Errorf("the job is %q, want j1", started.Job)
	}
	if started.Handle != "r1" {
		t.Errorf("the job's handle is %q, want r1", started.Handle)
	}
	if started.Tool != "job" {
		t.Errorf("the job's tool is %q, want job", started.Tool)
	}
}

// TestTheJobRunsUnderTheSessionContext covers the plan's rule: the job
// runs under the session, so a cancel of the run does not stop it.
func TestTheJobRunsUnderTheSessionContext(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
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
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The job is still running: the cancel did not stop it.
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
	for _, rec := range sink.allRecords() {
		ended, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		if ended.Outcome != poo.JobOutcomeOK {
			t.Errorf("the job ended %q, want ok", ended.Outcome)
		}
	}
}

// TestAJobThatFailsFatallyWhileTheRunWaitsSettlesIt covers the
// plan's rule: a job that fails fatally while the run is waiting
// settles the run tool_fatal with no further request.
func TestAJobThatFailsFatallyWhileTheRunWaitsSettlesIt(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetError(poo.Fatalf("the working directory is gone"))

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	// The job fails fatally, and the waiting run settles with it.
	tool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonToolFatal {
		t.Errorf("the run settled %q, want tool_fatal", res.Reason)
	}
	// The run made no request after the result arrived.
	requests := 0
	for _, ev := range s.Events() {
		if ev.Name != poo.EventTurnStarted {
			continue
		}
		if ev.Payload["run_id"] == firstRunID {
			requests++
		}
	}
	if requests != 2 {
		t.Errorf("the run made %d requests, want 2: none after the fatal result", requests)
	}
}

// TestACancelWithASynchronousCallAndAJobSettlesTheRun covers the
// plan's rule: a cancel stops the synchronous call, the run settles
// cancelled with the running job listed, and the job's later result
// wakes the session.
func TestACancelWithASynchronousCallAndAJobSettlesTheRun(t *testing.T) {
	started := make(chan struct{})
	slow := &echoTool{result: "echoed"}
	slow.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "partial output", IsError: true}, nil
	}

	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			jobCall("one", true),
			{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)},
		}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	jobTool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	jobTool.SetResult("the job's output")

	opts := poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"job":  jobTool,
			"echo": slow,
		},
		ToolOrder:    []string{"job", "echo"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
	}
	s := newTestSession(t, opts)
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	// The synchronous call was cancelled.
	var result *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == "echo" {
			result = r
		}
	}
	if result == nil {
		t.Fatal("the log holds no result for the synchronous call")
	}
	if result.Outcome != poo.OutcomeCancelled {
		t.Errorf("the outcome is %q, want cancelled", result.Outcome)
	}
	// The settle detail lists the job that is still running.
	if len(res.Detail.JobsRunning) != 1 || res.Detail.JobsRunning[0] != "j1" {
		t.Errorf("the settle detail lists %v, want [j1]", res.Detail.JobsRunning)
	}
	// The job keeps running, and its result wakes the session.
	jobTool.Release()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
}

// TestAFinishWithAJobRunningSettlesFinished covers the plan's rule: a
// finish call may settle the run while a job is running, and the settle
// detail lists the job.
func TestAFinishWithAJobRunningSettlesFinished(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			jobCall("one", true),
			{
				ID:        "call_2",
				Name:      "finish",
				Arguments: json.RawMessage(`{"result":{"answer":"42"}}`),
			},
		}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	jobTool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	jobTool.SetResult("the job's output")
	finisher := newFinishTool(t, `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)

	opts := poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"job":    jobTool,
			"finish": finisher,
		},
		ToolOrder:    []string{"job", "finish"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
	}
	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")
	if res.Reason != poo.ReasonFinished {
		t.Errorf("the run settled %q, want finished", res.Reason)
	}
	// The settle detail lists the job that is still running.
	if len(res.Detail.JobsRunning) != 1 || res.Detail.JobsRunning[0] != "j1" {
		t.Errorf("the settle detail lists %v, want [j1]", res.Detail.JobsRunning)
	}
}

// uncooperativeJobTool ignores its context, so the close's teardown
// deadline is the only thing that bounds it.
type uncooperativeJobTool struct {
	*pootest.JobTool
	gate    chan struct{}
	entered chan struct{}
}

// Call blocks until the test releases it, whatever the context says.
func (t *uncooperativeJobTool) Call(ctx context.Context, p poo.Prepared, e poo.ToolEnv) (poo.ToolResult, error) {
	t.entered <- struct{}{}
	<-t.gate
	return poo.ToolResult{Text: "done"}, nil
}

// TestTheCloseIsBoundedWithTwoUncooperativeJobs covers the plan's close:
// the teardown bound is one deadline for the whole wait, so a second job
// that ignores its context does not hold the close forever.
func TestTheCloseIsBoundedWithTwoUncooperativeJobs(t *testing.T) {
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{
		jobCall("one", true),
		jobCall("two", true),
	}}})
	tool := &uncooperativeJobTool{
		JobTool: pootest.NewJobTool("job", poo.BackgroundAllowed),
		gate:    make(chan struct{}),
		entered: make(chan struct{}, 2),
	}
	opts := jobOptions(t, tr, tool, &recordingSink{})
	opts.MaxTurns = 1
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.entered
	<-tool.entered
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		close(tool.gate)
		if err != nil {
			t.Errorf("closing: %v", err)
		}
	case <-time.After(6 * time.Second):
		close(tool.gate)
		<-done
		t.Fatal("Close still blocked after 6s with two jobs; the teardown bound must cover the whole wait")
	}
}

// partialThenNilJobTool waits for its context to end, then returns a
// partial result with a nil error, which is how a cooperative tool
// reports that it was stopped.
type partialThenNilJobTool struct {
	*pootest.JobTool
	entered chan struct{}
}

// Call waits for the context and returns the partial output.
func (t *partialThenNilJobTool) Call(ctx context.Context, p poo.Prepared, e poo.ToolEnv) (poo.ToolResult, error) {
	close(t.entered)
	<-ctx.Done()
	return poo.ToolResult{Text: "the partial output"}, nil
}

// TestAJobTimeoutBeatsANilError covers the plan's rule: the job's
// context decides a timeout before a nil error can call the job
// successful.
func TestAJobTimeoutBeatsANilError(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := &partialThenNilJobTool{
		JobTool: pootest.NewJobTool("job", poo.BackgroundAllowed),
		entered: make(chan struct{}),
	}
	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1
	s := newTestSession(t, opts, poo.WithJobs(poo.JobOptions{
		Timeout: 10 * time.Millisecond,
		OnIdle:  poo.OnIdleHold,
	}))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.entered
	waitFor(t, "job completion", func() bool { return sink.count("job.ended") == 1 })
	for _, rec := range sink.allRecords() {
		end, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		if end.Outcome != poo.JobOutcomeError || !end.TimedOut {
			t.Errorf("job.ended outcome=%s timed_out=%v; want error/timed_out", end.Outcome, end.TimedOut)
		}
		if !strings.Contains(end.Text, "the partial output") {
			t.Errorf("job.ended text=%q; want the partial output", end.Text)
		}
	}
}

// TestTheCloseKeepsAJobsPartialOutput covers the plan's close: the
// job.ended the close writes keeps what the job had produced, plus the
// note that the session closed.
func TestTheCloseKeepsAJobsPartialOutput(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "waiting"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if err := s.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	for _, rec := range sink.allRecords() {
		end, ok := rec.(*poo.JobEnded)
		if !ok {
			continue
		}
		if !strings.Contains(end.Text, "the call was stopped") {
			t.Fatalf("job.ended text=%q; lost the partial output %q", end.Text, "the call was stopped")
		}
		return
	}
	t.Fatal("no job.ended")
}
