package poo_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The rest of the jobs: two results that finish while the session is
// idle, the unattended-wake limit, a mixed batch, and the failures a
// job's end can have.

// TestTwoResultsFinishingWhileIdleStartOneWokenRun covers the plan's
// rule: two results that finish while the session is idle start one
// woken run that carries both.
func TestTwoResultsFinishingWhileIdleStartOneWokenRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			{ID: "call_1", Name: "job", Arguments: json.RawMessage(`{"text":"one"}`)},
			{ID: "call_2", Name: "job", Arguments: json.RawMessage(`{"text":"two"}`)},
		}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The two jobs are running, and the run waits for them.
	<-tool.Started()
	waitFor(t, "the jobs to start", func() bool {
		return tool.Calls() == 2
	})
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	// The run is cancelled, and the jobs keep running.
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if s.Running() {
		t.Fatal("the session is still running after the run settled")
	}

	// Both jobs finish while the session is idle. The seam holds the
	// wake, so the second result is queued before the wake collects
	// the results.
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeWake: func() {
			once.Do(func() {
				close(reached)
				<-release
			})
		},
	})
	defer restore()

	tool.ReleaseAll()
	<-reached
	waitFor(t, "the jobs to end", func() bool {
		return sink.count("job.ended") == 2
	})
	close(release)
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})

	// Exactly one woken run started, and it carries both results.
	woken := 0
	for _, rec := range sink.allRecords() {
		started, ok := rec.(*poo.RunStarted)
		if !ok || started.Trigger != poo.TriggerJob {
			continue
		}
		woken++
		if len(started.Jobs) != 2 {
			t.Errorf("the woken run carries %d results, want 2", len(started.Jobs))
		}
	}
	if woken != 1 {
		t.Errorf("the log holds %d woken runs, want 1", woken)
	}
}

// TestTheWakeLimitHoldsTheResultsPastTheLimit covers the plan's rule:
// the third unattended wake in a row is held with input.held and the
// reason wake_limit.
//
// Every run starts one job, and the test releases the job after the
// run that started it has settled, so the wakes follow one another with
// no client input in between.
func TestTheWakeLimitHoldsTheResultsPastTheLimit(t *testing.T) {
	s, sink, tool := wakeLimitSession(t, 2)
	_ = tool
	// The first two wakes are under the limit.
	releaseJobAndWait(t, s, sink, tool)
	releaseJobAndWait(t, s, sink, tool)
	if got := wokenRuns(sink); got != 2 {
		t.Errorf("the log holds %d woken runs, want 2", got)
	}
	if sink.count("input.held") != 0 {
		t.Errorf("the log holds %d input.held records, want 0", sink.count("input.held"))
	}
	// The third wake is past the limit: the result is held.
	releaseJobAndWait(t, s, sink, tool)
	if sink.count("input.held") != 1 {
		t.Errorf("the log holds %d input.held records, want 1", sink.count("input.held"))
	}
	var held *poo.InputHeld
	for _, rec := range sink.allRecords() {
		if h, ok := rec.(*poo.InputHeld); ok {
			held = h
		}
	}
	if held == nil {
		t.Fatal("the log holds no input.held record")
	}
	if held.Reason != "wake_limit" {
		t.Errorf("the held reason is %q, want wake_limit", held.Reason)
	}
	_ = s
}

// TestAClientMessageResetsTheWakeCount covers the plan's rule: any
// client input resets the count of the unattended wakes.
func TestAClientMessageResetsTheWakeCount(t *testing.T) {
	s, sink, tool := wakeLimitSession(t, 1)
	_ = tool
	// The first wake is under the limit of one.
	releaseJobAndWait(t, s, sink, tool)
	if got := wokenRuns(sink); got != 1 {
		t.Errorf("the log holds %d woken runs, want 1", got)
	}
	if sink.count("input.held") != 0 {
		t.Errorf("the log holds %d input.held records, want 0", sink.count("input.held"))
	}
	// A client message resets the count, so the next wake is under the
	// limit again. The run the client starts delivers the held result
	// before its own text.
	if _, err := s.Prompt(context.Background(), "and this"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	delivered := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginJob {
			delivered = true
		}
	}
	if !delivered {
		t.Error("the held result did not reach the model's view")
	}
	// The count was reset, so one more wake is allowed before the
	// next hold.
	woken := wokenRuns(sink)
	releaseJobAndWait(t, s, sink, tool)
	if got := wokenRuns(sink) - woken; got != 1 {
		t.Errorf("the client message allowed %d more wakes, want 1", got)
	}
}

// releaseJobAndWait releases the session's job and waits for the
// session to act on it.
//
// The job is released after the run that started it has settled, so the
// result arrives while the session is idle and wakes it.
func releaseJobAndWait(t *testing.T, s *poo.Session, sink *recordingSink, tool *pootest.JobTool) {
	t.Helper()
	before := sink.count("job.ended")
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") > before
	})
	// The woken run settles on its own.
	waitFor(t, "the session to settle", func() bool {
		return !s.Running()
	})
}

// wokenRuns returns how many runs a job result started.
func wokenRuns(sink *recordingSink) int {
	n := 0
	for _, rec := range sink.allRecords() {
		started, ok := rec.(*poo.RunStarted)
		if !ok || started.Trigger != poo.TriggerJob {
			continue
		}
		n++
	}
	return n
}

// wakeLimitSession builds a session whose every run starts one job.
//
// The turn ceiling of one settles each run right after the job starts,
// so the session is idle with a running job, and the job's result
// wakes it.
func wakeLimitSession(t *testing.T, limit int) (*poo.Session, *recordingSink, *pootest.JobTool) {
	t.Helper()
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "job",
			Arguments: json.RawMessage(`{"text":"one"}`),
		}}},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{MaxUnattendedWakes: limit}
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	// The first run starts the job and settles: the turn ceiling is
	// one, so the run settles right after the batch.
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-tool.Started()
	return s, sink, tool
}

// TestAJobThatFailsFatallyWhileIdleWakesARunThatSettles covers the
// plan's rule: a job that fails fatally while the session is idle
// wakes a run that settles tool_fatal with no request.
func TestAJobThatFailsFatallyWhileIdleWakesARunThatSettles(t *testing.T) {
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
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The job fails fatally while the session is idle, and the woken
	// run settles tool_fatal.
	tool.Release()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
	settled := sink.settled()
	if settled.Reason != poo.ReasonToolFatal {
		t.Errorf("the woken run settled %q, want tool_fatal", settled.Reason)
	}
	// The woken run made no request: the fatal result settled it
	// before the gate.
	requests := 0
	for _, ev := range s.Events() {
		if ev.Name != poo.EventTurnStarted {
			continue
		}
		if ev.Payload["run_id"] == settled.RunID {
			requests++
		}
	}
	if requests != 0 {
		t.Errorf("the woken run made %d requests, want 0", requests)
	}
}

// TestASinkFailureOnTheJobEndBreaksTheSession covers the plan's rule: a
// sink failure while the job's end is written breaks the session, and
// no wake happens.
func TestASinkFailureOnTheJobEndBreaksTheSession(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	broken := &failingSink{failRecord: "job.ended"}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, broken))
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
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

	// The job's end is written to a sink that refuses it.
	tool.Release()
	waitFor(t, "the session to break", func() bool {
		return s.Broken()
	})
	// The session is broken, and no wake happened.
	starts := 0
	for _, rec := range broken.records {
		if _, ok := rec.(*poo.RunStarted); ok {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("the log holds %d run.started records, want 1", starts)
	}
	_ = firstRunID
}

// TestTheHeldResultIsDeliveredBeforeTheNextPrompt covers the plan's
// rule: a held result is delivered at the start of the next run, before
// the prompt's text.
func TestTheHeldResultIsDeliveredBeforeTheNextPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}

	s := newTestSession(t, opts)
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
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
	// The result is held, and the session says so.
	tool.Release()
	waitFor(t, "the result to be held", func() bool {
		return sink.count("input.held") == 1
	})
	// The next prompt delivers the held result before its own text.
	secondRunID, err := s.Prompt(context.Background(), "and this")
	if err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The held result's message came before the prompt's message.
	var jobAt, promptAt = -1, -1
	for i, m := range sink.messages() {
		if m.RunID != secondRunID {
			continue
		}
		if m.Origin == poo.OriginJob && jobAt < 0 {
			jobAt = i
		}
		if m.Origin == poo.OriginUser && promptAt < 0 {
			promptAt = i
		}
	}
	if jobAt < 0 {
		t.Fatal("the held result did not reach the model's view")
	}
	if promptAt < 0 {
		t.Fatal("the prompt's message did not reach the model's view")
	}
	if jobAt > promptAt {
		t.Error("the held result came after the prompt's text")
	}
	_ = firstRunID
}

// TestTheJobLimitIsPerSession covers the plan's rule: the limit is per
// session, so two runs' jobs share it.
func TestTheJobLimitIsPerSession(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{MaxRunning: 1}
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The first run's job is still running, so the second run's job has
	// no slot.
	tr2 := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("two", true)}},
		{text: "the answer"},
	})
	sink2 := &recordingSink{}
	tool2 := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool2.SetResult("the job's output")
	opts2 := jobOptions(t, tr2, tool2, sink2)
	opts2.Jobs = poo.JobOptions{MaxRunning: 1}
	opts2.MaxTurns = 1

	s2 := newTestSession(t, opts2)
	if _, err := s2.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s2.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The second session has its own limit, so its job started.
	if sink2.count("job.started") != 1 {
		t.Errorf("the second session's log holds %d job.started records, want 1", sink2.count("job.started"))
	}
	// The first session's job is still running, and the session counts
	// it.
	running := s.JobsRunning()
	if len(running) != 1 || running[0] != "j1" {
		t.Errorf("the session's running jobs are %v, want [j1]", running)
	}
}

// TestAMixedBatchRunsTheJobAndTheCall covers the plan's rule: a batch
// with a job and an ordinary call runs both, and the ordinary call does
// not wait for the job.
func TestAMixedBatchRunsTheJobAndTheCall(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			jobCall("one", true),
			{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
		}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	jobTool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	jobTool.SetResult("the job's output")
	echo := &echoTool{result: "echoed"}

	opts := poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"job":  jobTool,
			"echo": echo,
		},
		ToolOrder:    []string{"job", "echo"},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The ordinary call ran without waiting for the job.
	waitFor(t, "the echo to run", func() bool {
		return len(echo.Calls()) == 1
	})
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
	jobTool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestTheJobsOptionsMatchTheSettings covers the plan's rule: WithJobs
// and the jobs settings produce the same behavior.
func TestTheJobsOptionsMatchTheSettings(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{MaxRunning: 2, Timeout: time.Minute}

	s := newTestSession(t, opts, poo.WithJobs(poo.JobOptions{MaxRunning: 2, Timeout: time.Minute}))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
}
