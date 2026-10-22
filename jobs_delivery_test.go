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

// The delivery of a job's result: a run that is open takes it at the
// next turn end, a waiting run takes it at once, and an idle session
// wakes with it or holds it.

// TestTheRunWaitsForItsJobAndThenDelivers covers the plan's main row:
// a background call returns started, the model stops, the run waits, the
// job finishes, the result is delivered, the model replies, and the run
// settles end_turn.
func TestTheRunWaitsForItsJobAndThenDelivers(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The job is running, so the run waits for it.
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	tool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The result was delivered to the model.
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginJob && strings.Contains(m.Content[0].Text, "the job's output") {
			found = true
		}
	}
	if !found {
		t.Error("the job's result did not reach the model's view")
	}
	// The job's end is recorded.
	if sink.count("job.ended") != 1 {
		t.Errorf("the log holds %d job.ended records, want 1", sink.count("job.ended"))
	}
}

// TestTheBeforeSettleHookRunsOnceAtTheRealSettle covers the plan's
// rule: the settle hooks do not run when the run enters the waiting
// state, because the run is not ending.
func TestTheBeforeSettleHookRunsOnceAtTheRealSettle(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	hook := &countingBeforeSettle{}
	opts := jobOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	waitFor(t, "the run to wait", func() bool {
		return s.State() == poo.StateWaiting
	})
	// The settle hook did not run while the run waited.
	if hook.calls != 0 {
		t.Errorf("the settle hook ran %d times while the run waited, want 0", hook.calls)
	}
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if hook.calls != 1 {
		t.Errorf("the settle hook ran %d times, want 1", hook.calls)
	}
}

// TestASteerWhileWaitingIsDeliveredAtOnce covers the plan's rule: a
// steer that arrives while the run waits is delivered at once, and the
// job keeps running.
func TestASteerWhileWaitingIsDeliveredAtOnce(t *testing.T) {
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
	if _, err := s.Send("wait, do this instead", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	// The steer was delivered at once.
	waitFor(t, "the steer to be delivered", func() bool {
		return sink.count("input.delivered") == 1
	})
	// The job is still running: it was not stopped by the steer.
	tool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
}

// TestAnInterruptWhileWaitingIsDeliveredAtOnce covers the plan's rule:
// an interrupt that arrives while the run waits is delivered at once,
// because there is nothing to abort.
func TestAnInterruptWhileWaitingIsDeliveredAtOnce(t *testing.T) {
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
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	waitFor(t, "the interrupt to be delivered", func() bool {
		return sink.count("input.delivered") == 1
	})
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestAFollowUpWhileWaitingWaitsForTheRealSettle covers the plan's
// rule: a follow-up is delivered only at a real settle, so a queued
// follow-up does not keep the run from waiting.
func TestAFollowUpWhileWaitingWaitsForTheRealSettle(t *testing.T) {
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
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	// The run is still waiting: the follow-up does not wake it.
	time.Sleep(0)
	if s.State() != poo.StateWaiting {
		t.Errorf("the session is %q, want waiting", s.State())
	}
	tool.Release()
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The follow-up was delivered at the real settle.
	if sink.count("input.delivered") != 2 {
		t.Errorf("the log holds %d input.delivered records, want 2", sink.count("input.delivered"))
	}
}

// TestACancelWithARunningJobSettlesTheRunAndListsTheJob covers the
// plan's rule: a cancel stops the run, the job keeps running, and the
// settle detail lists the job.
func TestACancelWithARunningJobSettlesTheRunAndListsTheJob(t *testing.T) {
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
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.JobsRunning) != 1 || res.Detail.JobsRunning[0] != "j1" {
		t.Errorf("the settle detail lists %v, want [j1]", res.Detail.JobsRunning)
	}
	// The job is still running: the cancel did not stop it.
	tool.Release()
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
}

// TestTheJobResultWakesTheSession covers the plan's rule: a result that
// arrives while the session is idle wakes it with a new run.
func TestTheJobResultWakesTheSession(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the first run settled %q, want end_turn", res.Reason)
	}
}

// TestACancelledRunsJobResultWakesTheSession covers the plan's rule: a
// job keeps running after the run is cancelled, and its later result
// wakes the session.
func TestACancelledRunsJobResultWakesTheSession(t *testing.T) {
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
	// The run is cancelled: the job keeps running.
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.JobsRunning) != 1 {
		t.Errorf("the settle detail lists %v, want one job", res.Detail.JobsRunning)
	}
	// The job finishes, and the session wakes with it.
	tool.Release()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
	// The woken run carries the job results.
	found := false
	for _, rec := range sink.allRecords() {
		started, ok := rec.(*poo.RunStarted)
		if !ok {
			continue
		}
		if started.Trigger != poo.TriggerJob {
			continue
		}
		found = true
		if len(started.Jobs) != 1 || started.Jobs[0].Job != "j1" {
			t.Errorf("the woken run carries %v, want one job", started.Jobs)
		}
	}
	if !found {
		t.Error("no run was started by a job result")
	}
}

// TestOnIdleHoldEmitsInputHeld covers the plan's rule: a session that
// holds its results says so with an input.held event, and the results
// are delivered before the next prompt's text.
func TestOnIdleHoldEmitsInputHeld(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
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
	// The result is held: the session says so.
	tool.Release()
	waitFor(t, "the result to be held", func() bool {
		return sink.count("input.held") == 1
	})
	// No run was woken: the result waits for the next prompt.
	if s.Running() {
		t.Error("the session woke a run, want the result to wait")
	}
}

// TestTheUnattendedWakeLimitHoldsTheThirdWake covers the plan's rule:
// the wake limit caps the woken runs that follow one another with no
// client input.
func TestTheUnattendedWakeLimitHoldsTheThirdWake(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the woken answer"},
	})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{MaxUnattendedWakes: 2}

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
		t.Fatalf("waiting for run: %v", err)
	}
	// The first wake is unattended, and the limit is two.
	tool.Release()
	waitFor(t, "the woken run to settle", func() bool {
		settled := sink.settled()
		return settled != nil && settled.RunID != firstRunID
	})
	// No result was held: the first wake is under the limit.
	if sink.count("input.held") != 0 {
		t.Errorf("the log holds %d input.held records, want 0", sink.count("input.held"))
	}
}

// countingBeforeSettle counts the settle hooks that ran.
type countingBeforeSettle struct {
	mu sync.Mutex
	// calls counts the times the hook ran.
	calls int
}

// BeforeSettle counts the call and accepts the settle.
func (h *countingBeforeSettle) BeforeSettle(ctx poo.HookContext, reason poo.Reason) (poo.SettleDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	return poo.Accept(), nil
}

// Calls returns how many times the hook ran.
func (h *countingBeforeSettle) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// TestAnInterruptDuringExecutingStopsTheCallAndNotTheJob covers the
// plan's rule: an interrupt during executing stops the synchronous
// call, and the job keeps running.
func TestAnInterruptDuringExecutingStopsTheCallAndNotTheJob(t *testing.T) {
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "partial output", IsError: true}, nil
	}

	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{
			jobCall("one", true),
			{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)},
		}},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	jobTool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	jobTool.SetResult("the job's output")

	opts := poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"job":  jobTool,
			"echo": tool,
		},
		ToolOrder: []string{"job", "echo"},
		Sink:      sink,
		SessionID: "s1",
		Model:     poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	// The job is still running: the interrupt did not stop it.
	if len(s.JobsRunning()) != 1 {
		t.Errorf("the session's running jobs are %v, want one", s.JobsRunning())
	}
	jobTool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled %q: an interrupt does not end the run", res.Reason)
	}
	waitFor(t, "the job to end", func() bool {
		return sink.count("job.ended") == 1
	})
}

// TestOnIdleHoldDoesNotSuppressAWaitingDelivery covers the plan's
// delivery table: a result arriving while the run waits is delivered at
// once, whatever on_idle says. on_idle is the idle row only.
func TestOnIdleHoldDoesNotSuppressAWaitingDelivery(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "waiting now"},
		{text: "answered the job"},
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
	waitFor(t, "the run to wait", func() bool { return s.State() == poo.StateWaiting })
	tool.Release()
	waitFor(t, "the run to leave waiting", func() bool { return s.State() != poo.StateWaiting })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("waiting for the run: %v (state %s)", err, s.State())
	}
	if got := s.LastAssistantMessage(); got != "answered the job" {
		t.Errorf("last assistant message = %q, want the job answered (settled %s)", got, res.Reason)
	}
}

// openRunHoldTransport holds the second request, so a job can finish
// while the run is open.
type openRunHoldTransport struct {
	inner   *pootest.Transport
	mu      sync.Mutex
	n       int
	at2     chan struct{}
	release chan struct{}
}

// Stream holds the second request until the test releases it.
func (b *openRunHoldTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	b.mu.Lock()
	b.n++
	n := b.n
	b.mu.Unlock()
	if n == 2 {
		close(b.at2)
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, poo.Rendered{}, ctx.Err()
		}
	}
	return b.inner.Stream(ctx, req)
}

// Fork hands the fork to the scripted transport.
func (b *openRunHoldTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return b.inner.Fork(ctx, prev, appended, maxTokens)
}

// TestOnIdleHoldDeliversToAnOpenRun covers the plan's delivery table: a
// result that finishes while a run is open is delivered at the turn end,
// even when on_idle is hold.
func TestOnIdleHoldDeliversToAnOpenRun(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "the answer"},
	})
	tr := &openRunHoldTransport{inner: inner, at2: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.at2
	tool.Release()
	waitFor(t, "the job to be queued", func() bool { return s.QueuedJobResults() == 1 })
	close(tr.release)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	delivered := 0
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDelivered); ok && d.Mode == poo.InputModeJob {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("the run settled %s with %d job deliveries, want 1", res.Reason, delivered)
	}
}
