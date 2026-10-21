package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The races: a job that finishes at a boundary of the turn end or of
// the finalization is taken exactly once, and a wake that races a
// prompt starts exactly one run.
//
// Every race is tested at a named boundary, with the core's own hooks
// deciding when the boundary is reached. Nothing sleeps.

// TestAJobFinishingBeforeTheWaitingEntrySendsTheRunToTheGate covers
// the plan's race: a job that finishes between the step 2 drain and
// the waiting entry sends the run to the gate, not to waiting.
func TestAJobFinishingBeforeTheWaitingEntrySendsTheRunToTheGate(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	// The seam: the job finishes while the run is between the drain
	// and the waiting entry.
	reached := make(chan struct{})
	release := make(chan struct{})
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeWaiting: func(wait poo.WaitForResult) {
			close(reached)
			// The job finishes at the boundary: the result is
			// queued before the run looks.
			tool.Release()
			wait()
			close(release)
		},
	})
	defer restore()

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-reached

	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The result was delivered to the run, and the run never waited.
	delivered := 0
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDelivered); ok && d.Mode == poo.InputModeJob {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("the log holds %d job deliveries, want 1", delivered)
	}
	for _, state := range s.Transitions() {
		if state == poo.StateWaiting {
			t.Error("the run waited, want it to go to the gate")
		}
	}
}

// TestAJobFinishingBeforeTheFinalizeCheckAbandonsTheFinalize covers
// the plan's race: a job result that arrives before finalize step 1
// abandons a natural-only finalize, like a late steer.
//
// The job belongs to an earlier run, so the run that settles is free to
// settle naturally.
func TestAJobFinishingBeforeTheFinalizeCheckAbandonsTheFinalize(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	// The first run starts the job and is cancelled: the job keeps
	// running.
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

	// The second run settles naturally, and the seam fires at its
	// input check.
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeFinalizeCheck: func(wait poo.WaitForResult) {
			// The seam fires once: the finalize that the result
			// abandons is followed by another one.
			once.Do(func() {
				close(reached)
				// The job finishes at the boundary: the result
				// is queued before the input check looks.
				tool.Release()
				wait()
				close(release)
			})
		},
	})
	defer restore()

	secondRunID, err := s.Prompt(context.Background(), "and this")
	if err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	<-reached
	<-release

	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The result was delivered to the run that abandoned the finalize.
	delivered := false
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDelivered)
		if !ok || d.Mode != poo.InputModeJob {
			continue
		}
		if d.RunID == secondRunID {
			delivered = true
		}
	}
	if !delivered {
		t.Error("the result was not delivered to the run that abandoned the finalize")
	}
}

// TestAJobFinishingAfterTheCommitWakesAfterTheSettle covers the
// plan's race: a result that arrives after the commit point stays
// queued and wakes the session after the run has settled.
func TestAJobFinishingAfterTheCommitWakesAfterTheSettle(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	// The first run starts the job and is cancelled: the job keeps
	// running.
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

	// The second run settles, and the seam fires at its commit point.
	// The result arrives after the commit, so the settling run is not
	// reopened: the result wakes the session afterwards.
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeCommit: func(wait poo.WaitForResult) {
			once.Do(func() {
				close(reached)
				tool.Release()
				wait()
				close(release)
			})
		},
	})
	defer restore()

	if _, err := s.Prompt(context.Background(), "and this"); err != nil {
		t.Fatalf("starting the second run: %v", err)
	}
	<-reached
	<-release

	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The result was never delivered to the settling run: it woke the
	// session instead.
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDelivered)
		if !ok || d.Mode != poo.InputModeJob {
			continue
		}
		if d.RunID == firstRunID {
			t.Error("the result was delivered to the settling run")
		}
	}
	// The session woke with the result.
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
}

// TestAJobResultNeverReopensASettlingRun covers the plan's rule: a
// result that arrives while the run settles never reopens it, and a
// run with a max_turns candidate settles as max_turns.
func TestAJobResultNeverReopensASettlingRun(t *testing.T) {
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
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	// The result wakes the session, and the woken run never reopens the
	// first one.
	tool.Release()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
	settles := 0
	for _, rec := range sink.allRecords() {
		if _, ok := rec.(*poo.RunSettled); ok {
			settles++
		}
	}
	if settles != 2 {
		t.Errorf("the log holds %d settle records, want 2", settles)
	}
}

// TestAWakeRacingAPromptStartsOneRun covers the plan's race: a wake
// and a concurrent prompt race for one reservation, and exactly one run
// starts.
//
// The wake holds the reservation when the prompt arrives, so the prompt
// is refused with busy, and nothing it sent is accepted.
func TestAWakeRacingAPromptStartsOneRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	firstRunID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
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

	// The wake takes the reservation and stops at the seam.
	reached := make(chan struct{})
	release := make(chan struct{})
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeWake: func() {
			close(reached)
			<-release
		},
	})
	defer restore()

	tool.Release()
	<-reached

	// The prompt loses the race: it is refused, and nothing is
	// accepted.
	_, promptErr := s.Prompt(context.Background(), "and this")
	if promptErr != poo.ErrBusy {
		t.Errorf("the prompt failed with %v, want ErrBusy", promptErr)
	}
	close(release)

	// Exactly one run started after the first one: the wake's.
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
	starts := 0
	for _, rec := range sink.allRecords() {
		if _, ok := rec.(*poo.RunStarted); ok {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("the log holds %d run.started records, want 2", starts)
	}
}

// TestAConcurrentPromptIsRefusedWhileTheSessionIsBusy covers the
// plan's rule: a second prompt while a run is active is refused with
// busy, and nothing is accepted.
func TestAConcurrentPromptIsRefusedWhileTheSessionIsBusy(t *testing.T) {
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
	// The run is open, so the prompt is refused.
	if _, err := s.Prompt(context.Background(), "and this"); err != poo.ErrBusy {
		t.Errorf("the prompt failed with %v, want ErrBusy", err)
	}
	// Nothing was accepted: no input was queued.
	if sink.count("input.queued") != 0 {
		t.Errorf("the log holds %d input.queued records, want 0", sink.count("input.queued"))
	}
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestConcurrentStartsAtTheLimitAdmitExactlyTheLimit covers the
// plan's rule: two calls that race for the last slot admit exactly one
// of them.
func TestConcurrentStartsAtTheLimitAdmitExactlyTheLimit(t *testing.T) {
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
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The limit is one, so only one job started, and the other call
	// got the recoverable error.
	if sink.count("job.started") != 1 {
		t.Errorf("the log holds %d job.started records, want 1", sink.count("job.started"))
	}
	limit := false
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		if r.Outcome == poo.OutcomeError && strings.Contains(r.Text, "too many jobs running") {
			limit = true
		}
	}
	if !limit {
		t.Error("the model did not see the limit error")
	}
}

// TestABeforeToolDenialStartsNoJob covers the plan's rule: a refused
// call never becomes a job.
func TestABeforeToolDenialStartsNoJob(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.BeforeTool = []poo.BeforeToolHook{denyingBeforeTool{}}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if sink.count("job.started") != 0 {
		t.Errorf("the log holds %d job.started records, want 0", sink.count("job.started"))
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d times, want 0", tool.Calls())
	}
}

// TestABeforeToolResultStartsNoJob covers the plan's rule: a hook that
// answers the call itself never starts a job.
func TestABeforeToolResultStartsNoJob(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.BeforeTool = []poo.BeforeToolHook{answeringBeforeTool{}}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if sink.count("job.started") != 0 {
		t.Errorf("the log holds %d job.started records, want 0", sink.count("job.started"))
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d times, want 0", tool.Calls())
	}
}

// TestTheToolCallRecordKeepsTheMode covers the plan's rule: the
// tool.call record keeps the original arguments and says how the call
// ran.
func TestTheToolCallRecordKeepsTheMode(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	tool.Release()

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	var mode poo.CallMode
	for _, rec := range sink.allRecords() {
		call, ok := rec.(*poo.ToolCallRecord)
		if !ok {
			continue
		}
		mode = call.Mode
		if !strings.Contains(string(call.Arguments), "background") {
			t.Error("the tool.call record does not keep the original arguments")
		}
	}
	if mode != poo.CallModeJob {
		t.Errorf("the call's mode is %q, want job", mode)
	}
}

// denyingBeforeTool refuses every call.
type denyingBeforeTool struct{}

// BeforeTool refuses the call.
func (h denyingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	return poo.Deny("not this time"), nil
}

// answeringBeforeTool answers every call itself.
type answeringBeforeTool struct{}

// BeforeTool answers the call.
func (h answeringBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	return poo.ShortCircuit(poo.ToolResult{Text: "answered by the hook"}), nil
}

// unusedJSON keeps the json import honest when the file changes.
var _ = json.RawMessage(nil)

// unusedTime keeps the time import honest when the file changes.
var _ = time.Second

// blockingBeforeToolHook holds a call after its validation until the test
// lets it go.
type blockingBeforeToolHook struct {
	started chan struct{}
	release chan struct{}
}

// BeforeTool blocks until the test releases it.
func (h *blockingBeforeToolHook) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	close(h.started)
	<-h.release
	return poo.Allow(), nil
}

// TestNoJobStartsAfterTheCommitPoint covers the plan's rule: no new
// effect starts after the commit point. A cancel that commits while a
// before-tool hook runs must not let the call become a job.
func TestNoJobStartsAfterTheCommitPoint(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")
	hook := &blockingBeforeToolHook{started: make(chan struct{}), release: make(chan struct{})}

	opts := jobOptions(t, tr, tool, sink)
	opts.BeforeTool = []poo.BeforeToolHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hook.started
	go s.Cancel("cancel:test")
	waitFor(t, "the run to settle", func() bool { return !s.Running() })
	close(hook.release)

	if sink.count("job.started") != 0 {
		t.Errorf("the log holds %d job.started records after the run settled, want 0", sink.count("job.started"))
	}
	if len(s.JobsRunning()) != 0 {
		t.Errorf("a job is running for a settled run: %v", s.JobsRunning())
	}
	if tool.Calls() != 0 {
		t.Errorf("the tool ran %d times after the settle, want 0", tool.Calls())
	}
}

// blockingJobStartSink holds the job's acceptance record until the test
// releases it.
type blockingJobStartSink struct {
	entered chan struct{}
	release chan struct{}
	inner   recordingSink
}

// Append holds a job.started record.
func (s *blockingJobStartSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "job.started" {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.inner.Append(ctx, rec)
}

// Flush does nothing.
func (s *blockingJobStartSink) Flush(ctx context.Context) error { return nil }

// TestNoJobLaunchesAfterTheSettle covers the acceptance record as
// tracked work: a job whose job.started write is still in flight when
// the run settles never launches its tool.
func TestNoJobLaunchesAfterTheSettle(t *testing.T) {
	sink := &blockingJobStartSink{entered: make(chan struct{}), release: make(chan struct{})}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, sink))

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-sink.entered
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(sink.release)
	select {
	case <-tool.Started():
		t.Fatalf("the job began executing after the run settled; records: %v", sink.inner.names())
	case <-time.After(100 * time.Millisecond):
	}
}
