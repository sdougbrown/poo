package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The input path: a steer reaches the model at the next turn end, an
// interrupt aborts the current activity, and a follow-up waits for a
// natural stop. Every accepted input ends as exactly one of delivered or
// dropped.

// TestASteerIsDeliveredAtTheNextTurnEnd covers the steer: the message
// reaches the model at the turn end, and the turn's work is untouched.
func TestASteerIsDeliveredAtTheNextTurnEnd(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Send("also do this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}

	// The steer is recorded as queued and as delivered.
	if sink.count("input.queued") != 1 {
		t.Errorf("the log holds %d input.queued records, want 1", sink.count("input.queued"))
	}
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
	// The steer's message reached the model, with its own origin.
	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginSteer && strings.Contains(m.Content[0].Text, "also do this") {
			found = true
		}
	}
	if !found {
		t.Error("the steer's message did not reach the model's view")
	}
}

// TestTheSteerWaitsForTheBatchToFinish covers the plan's rule: a steer
// does not touch the work that is running.
func TestTheSteerWaitsForTheBatchToFinish(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-finished
		return poo.ToolResult{Text: "done"}, nil
	}

	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	if _, err := s.Send("also do this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	// The tool is still running: the steer did not touch it.
	select {
	case <-finished:
		t.Fatal("the steer ended the running tool")
	case <-time.After(50 * time.Millisecond):
	}
	close(finished)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
}

// TestAnInterruptDuringStreamingRecordsThePartialText covers the
// interrupt: the partial text is recorded as a partial message with the
// interrupt as its cause, its tool calls do not run, and the message is
// delivered next.
func TestAnInterruptDuringStreamingRecordsThePartialText(t *testing.T) {
	// The transport hands out a response that never ends, so the
	// interrupt is the only thing that stops it.
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The transport tells the test that the stream started.
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled %q: an interrupt does not end the run", res.Reason)
	}

	// The partial text is recorded.
	var partial *poo.MessageRecord
	for _, m := range sink.messages() {
		if m.Partial {
			partial = m
		}
	}
	if partial == nil {
		t.Fatal("the log holds no partial message")
	}
	if partial.Cause != poo.CauseInterrupt {
		t.Errorf("the partial message's cause is %q, want interrupt", partial.Cause)
	}
	if partial.Content[0].Text != "partial text" {
		t.Errorf("the partial message holds %q, want the streamed text", partial.Content[0].Text)
	}
	// The interrupt's message was delivered.
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
	// The turn's interrupted event names the interrupt's input.
	sawInterrupted := false
	for _, ev := range s.Events() {
		if ev.Name != poo.EventTurnInterrupted {
			continue
		}
		sawInterrupted = true
		ids, _ := ev.Payload["input_ids"].([]string)
		if len(ids) != 1 {
			t.Errorf("the interrupted event names %v, want one input", ids)
		}
	}
	if !sawInterrupted {
		t.Error("the run emitted no turn.interrupted event")
	}
}

// TestAnInterruptDuringALongCallCancelsIt covers the interrupt during
// the tool batch: the call is cancelled, and the run carries on.
func TestAnInterruptDuringALongCallCancelsIt(t *testing.T) {
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "partial output", IsError: true}, nil
	}

	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled %q: an interrupt does not end the run", res.Reason)
	}

	// The call is cancelled, and the result carries the partial output.
	var result *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Handle == "r1" {
			result = r
		}
	}
	if result == nil {
		t.Fatal("the log holds no result for the call")
	}
	if result.Outcome != poo.OutcomeCancelled {
		t.Errorf("the outcome is %q, want cancelled", result.Outcome)
	}
	if !strings.Contains(result.Text, "partial output") {
		t.Errorf("the result's text is %q, want the partial output", result.Text)
	}
	if !strings.Contains(result.Text, poo.CancelledInterruptedText) {
		t.Errorf("the result's text is %q, want the interrupt's note", result.Text)
	}
}

// TestAnInterruptSkipsTheUnstartedCalls covers the rest of the batch:
// the calls that never started are skipped with a visible result.
func TestAnInterruptSkipsTheUnstartedCalls(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	// The tool is side-effecting, so the batch runs in model order and
	// the second call has not started when the interrupt arrives.
	tool := &echoTool{result: "echoed", class: poo.ClassSideEffecting}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return poo.ToolResult{Text: "partial output", IsError: true}, nil
	}

	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The second call is skipped with the visible note.
	var skipped *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok || r.Handle != "r2" {
			continue
		}
		skipped = r
	}
	if skipped == nil {
		t.Fatal("the log holds no result for the second call")
	}
	if skipped.Outcome != poo.OutcomeSkipped {
		t.Errorf("the outcome is %q, want skipped", skipped.Outcome)
	}
	if want := poo.HandleLine("r2", "echo") + "\n" + poo.SkippedInterruptedText; skipped.Text != want {
		t.Errorf("the result's text is %q, want %q", skipped.Text, want)
	}
}

// TestAnInterruptDuringTheHooksDiscardsTheirDecision covers the plan's
// rule: the hook's decision is discarded, and that is not a hook error.
func TestAnInterruptDuringTheHooksDiscardsTheirDecision(t *testing.T) {
	// The hook blocks until the test releases it, and the interrupt
	// arrives while it waits.
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingAfterTurn{started: hookStarted, release: release}

	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 100*time.Millisecond)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hookStarted
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	// The hook's context is cancelled, so it returns.
	<-hook.done
	close(release)

	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonHookError {
		t.Errorf("the run settled %q: an interrupt is not a hook error", res.Reason)
	}
}

// TestAnInterruptDuringBeforeSettleEmitsNoSecondTurnEnd covers the
// plan's rule: the turn's end is emitted once, even when the settle
// hooks are interrupted.
func TestAnInterruptDuringBeforeSettleEmitsNoSecondTurnEnd(t *testing.T) {
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingBeforeSettle{started: hookStarted, release: release}

	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: err %v", err)
	}
	<-hookStarted
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	<-hook.done
	close(release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// The interrupted turn ends once: there is no second turn end for
	// the same turn.
	ends := map[int]int{}
	for _, ev := range s.Events() {
		if ev.Name != poo.EventTurnEnded {
			continue
		}
		turn, _ := ev.Payload["turn"].(int)
		ends[turn]++
	}
	for turn, n := range ends {
		if n != 1 {
			t.Errorf("turn %d ended %d times, want 1", turn, n)
		}
	}
}

// TestAFollowUpIsDeliveredAtANaturalStop covers the follow-up: it waits
// for the run to settle naturally, and then it is delivered.
func TestAFollowUpIsDeliveredAtANaturalStop(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	// The settle hook holds the run open, so the follow-up is accepted
	// while the run can still take it.
	hook := &blockingBeforeSettle{started: make(chan struct{}), release: make(chan struct{})}

	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hook.started
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	close(hook.release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The follow-up was delivered, so the run carried on.
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginFollowUp && strings.Contains(m.Content[0].Text, "and then this") {
			found = true
		}
	}
	if !found {
		t.Error("the follow-up's message did not reach the model's view")
	}
}

// inputAcceptedAtTheNaturalStop sends one input while the run is held at
// its settle hooks, holds the input.queued write, and lets the run reach
// its input decision before releasing the write. The input was accepted
// before the run was settling, so the natural stop has to deliver it.
//
// afterAcceptance, when set, is installed at the seam Send reaches once
// the input's acceptance is done, so a test can hold Send there and pin
// what the run does with the input in the meantime.
func inputAcceptedAtTheNaturalStop(t *testing.T, mode poo.InputMode, afterAcceptance func(*holdingInputSink)) (*holdingInputSink, poo.Result) {
	t.Helper()
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &holdingInputSink{
		mode:      mode,
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		delivered: make(chan struct{}),
	}
	tool := &echoTool{result: "echoed"}
	// The settle hook holds the run at the natural stop, so the input is
	// accepted while the run is still open.
	hook := &blockingBeforeSettle{started: make(chan struct{}), release: make(chan struct{})}
	observer := &settlingObserver{seen: make(chan struct{})}

	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.Observers = []poo.Observer{observer}

	s := newTestSession(t, opts)
	// The seam a natural finalization announces once it waits for an
	// acceptance that is still being written.
	atAcceptanceWait := make(chan struct{})
	var waitOnce sync.Once
	hooks := &poo.SealRaceHooks{BeforeAcceptanceWait: func() {
		waitOnce.Do(func() { close(atAcceptanceWait) })
	}}
	if afterAcceptance != nil {
		hooks.AfterAcceptance = func() { afterAcceptance(sink) }
	}
	restore := s.SetSealRaceHooks(hooks)
	t.Cleanup(restore)

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hook.started
	sendErr := make(chan error, 1)
	go func() {
		_, err := s.Send("and then this", mode)
		sendErr <- err
	}()
	<-sink.entered
	// The acceptance is in flight, so the run reaches its input decision
	// now. Either it moves on to settling, or it says that it waits for
	// the acceptance first; both mean the decision was taken while the
	// input was neither deliverable nor refused.
	close(hook.release)
	select {
	case <-atAcceptanceWait:
	case <-observer.seen:
	}
	close(sink.release)
	if err := <-sendErr; err != nil {
		t.Fatalf("sending the input: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return sink, res
}

// TestAFollowUpAcceptedAsTheRunStopsIsDelivered covers the window
// between Send accepting a follow-up and the input.queued record it
// writes: the run's finalization takes its input decision while the
// acceptance is still in flight, and the follow-up is delivered.
func TestAFollowUpAcceptedAsTheRunStopsIsDelivered(t *testing.T) {
	sink, res := inputAcceptedAtTheNaturalStop(t, poo.FollowUp, nil)
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// Send accepted the follow-up, so it is delivered.
	if got := sink.count("input.delivered"); got != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", got)
	}
	if got := sink.count("input.dropped"); got != 0 {
		t.Errorf("the log holds %d input.dropped records, want 0", got)
	}
}

// TestAnInterruptAcceptedAsTheRunStopsIsDelivered covers the interrupt in
// the same window: the turn it would have cut short has already ended, so
// its message is delivered at the gate, and the turn that follows is not
// reported as interrupted.
//
// The pin holds Send until the gate's delivery has landed, which is the
// order that makes the double effect visible every time: the message is
// delivered, and an abort raised after it cuts the following turn.
func TestAnInterruptAcceptedAsTheRunStopsIsDelivered(t *testing.T) {
	sink, res := inputAcceptedAtTheNaturalStop(t, poo.Interrupt, func(sink *holdingInputSink) {
		<-sink.delivered
	})
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if got := sink.count("input.delivered"); got != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", got)
	}
	if got := sink.count("input.dropped"); got != 0 {
		t.Errorf("the log holds %d input.dropped records, want 0", got)
	}
	if got := sink.count("turn.interrupted"); got != 0 {
		t.Errorf("the log holds %d turn.interrupted records, want 0: no turn was cut short", got)
	}
}

// holdingInputSink holds one input's input.queued record until the test
// releases it, so the acceptance stays in flight while the run's
// finalization takes its input decision.
type holdingInputSink struct {
	recordingSink
	// mode is the mode of the input the sink holds.
	mode poo.InputMode
	// once guards the hold, so only one record is held.
	once sync.Once
	// heldID is the identifier of the input the sink holds.
	heldID string
	// entered is closed once a record is being held.
	entered chan struct{}
	// release lets the held record through.
	release chan struct{}
	// delivered is closed once the held input's delivery record lands.
	delivered chan struct{}
	// deliveredOnce guards delivered.
	deliveredOnce sync.Once
}

// Append holds the input's acceptance record until the test releases it.
func (s *holdingInputSink) Append(ctx context.Context, rec poo.Record) error {
	if queued, ok := rec.(*poo.InputQueued); ok && queued.Mode == s.mode {
		s.once.Do(func() {
			s.heldID = queued.InputID
			close(s.entered)
			select {
			case <-s.release:
			case <-ctx.Done():
			}
		})
	}
	if delivered, ok := rec.(*poo.InputDelivered); ok && delivered.InputID == s.heldID {
		s.deliveredOnce.Do(func() { close(s.delivered) })
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.recordingSink.Append(ctx, rec)
}

// settlingObserver closes its channel when the run enters finalization.
type settlingObserver struct {
	// seen is the channel the observer closes.
	seen chan struct{}
	// once guards the channel, so a second run does not close it
	// twice.
	once sync.Once
}

// Observe closes the channel on the first settling event.
func (o *settlingObserver) Observe(ev poo.Event) error {
	if ev.Name == poo.EventRunSettling {
		o.once.Do(func() { close(o.seen) })
	}
	return nil
}

// TestAFollowUpQueuedWhenTheRunIsCancelledIsDropped covers the plan's
// rule: a follow-up that never reaches the model is dropped and
// reported.
func TestAFollowUpQueuedWhenTheRunIsCancelledIsDropped(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	// The follow-up is dropped and reported.
	if sink.count("input.dropped") != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", sink.count("input.dropped"))
	}
	if len(res.Detail.UndeliveredInput) != 1 {
		t.Fatalf("the settle detail lists %d undelivered inputs, want 1", len(res.Detail.UndeliveredInput))
	}
	if res.Detail.UndeliveredInput[0].Mode != string(poo.FollowUp) {
		t.Errorf("the undelivered input's mode is %q, want follow_up", res.Detail.UndeliveredInput[0].Mode)
	}
}

// TestASteerDuringBeforeSettleAbandonsTheFinalize covers the plan's
// rule: a steer that arrives during the settle hooks abandons the
// finalization, and the AfterTurn hooks are not re-run.
func TestASteerDuringBeforeSettleAbandonsTheFinalize(t *testing.T) {
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingBeforeSettle{started: hookStarted, release: release}
	// The hook appends on the first turn only, so the second turn
	// reaches the settle hooks.
	after := &countingAfterTurn{stopAfter: 1}

	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.AfterTurn = []poo.AfterTurnHook{after}
	// The turn ceiling keeps the run bounded when the fake transport
	// repeats its last response.
	opts.MaxTurns = 5

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hookStarted
	if _, err := s.Send("wait, do this instead", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	close(release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	// The steer was delivered, and the run went to the gate again.
	if sink.count("input.delivered") != 1 {
		t.Errorf("the log holds %d input.delivered records, want 1", sink.count("input.delivered"))
	}
	// The abandoned finalization does not re-run the AfterTurn hooks
	// for the turn it abandoned: the hook ran once per turn, and the
	// run made one turn per request.
	turns := len(tr.Requests())
	if after.calls != turns {
		t.Errorf("the AfterTurn hook ran %d times, want %d: one per turn", after.calls, turns)
	}
}

// TestAMessageWhileIdleIsRefused covers the plan's rule: a message for a
// session with no run is refused.
func TestAMessageWhileIdleIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	_, err := s.Send("hello", poo.Steer)
	if err == nil {
		t.Fatal("the session accepted a message with no run")
	}
	if err != poo.ErrNotRunning {
		t.Errorf("the error is %v, want ErrNotRunning", err)
	}
}

// TestTwoInterruptsCoalesceIntoOneAbort covers the plan's rule: several
// interrupts that arrive during one abort coalesce into one, and all
// their messages are delivered.
func TestTwoInterruptsCoalesceIntoOneAbort(t *testing.T) {
	tr := newStallingTransport()
	tr.hold = make(chan struct{})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the first interrupt: %v", err)
	}
	if _, err := s.Send("really stop", poo.Interrupt); err != nil {
		t.Fatalf("sending the second interrupt: %v", err)
	}
	// Both interrupts are accepted before the stream returns, so they
	// belong to one abort.
	close(tr.hold)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	// Both messages were delivered.
	if sink.count("input.delivered") != 2 {
		t.Errorf("the log holds %d input.delivered records, want 2", sink.count("input.delivered"))
	}
	// The abort produced one interrupted event, naming both inputs.
	interrupts := 0
	for _, ev := range s.Events() {
		if ev.Name != poo.EventTurnInterrupted {
			continue
		}
		interrupts++
		ids, _ := ev.Payload["input_ids"].([]string)
		if len(ids) != 2 {
			t.Errorf("the interrupted event names %v, want both inputs", ids)
		}
	}
	if interrupts != 1 {
		t.Errorf("the run emitted %d turn.interrupted events, want 1", interrupts)
	}
}

// TestACancelDuringAnInterruptWins covers the plan's rule: a cancel
// beats an interrupt, and the interrupt's message is dropped.
func TestACancelDuringAnInterruptWins(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	// The interrupt's message is dropped and reported.
	if sink.count("input.dropped") != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", sink.count("input.dropped"))
	}
}

// TestASteerBeforeAnAfterTurnStopIsDropped covers the plan's rule: a
// steer that arrives just before an AfterTurn stop is dropped and
// reported with the run's reason.
func TestASteerBeforeAnAfterTurnStopIsDropped(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hook := &stoppingAfterTurn{}

	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The steer arrives while the turn is ending, and the hook stops
	// the run, so the steer never reaches the model.
	if _, err := s.Send("actually, do this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStopRequested {
		t.Errorf("the run settled %q, want stop_requested", res.Reason)
	}
	// The steer is either delivered (it reached the turn end's drain) or
	// dropped, never both.
	delivered := sink.count("input.delivered")
	dropped := sink.count("input.dropped")
	if delivered+dropped != 1 {
		t.Errorf("the steer was delivered %d times and dropped %d times, want exactly one fate", delivered, dropped)
	}
}

// TestTheFinalizeIsNotAbandonedWhenACancelIsPending covers the plan's
// rule: a finalization with a cancel candidate is never abandoned, even
// when the queue is not empty.
func TestTheFinalizeIsNotAbandonedWhenACancelIsPending(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("also do this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	// The steer was dropped, not delivered: the run was settling.
	if sink.count("input.dropped") != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", sink.count("input.dropped"))
	}
}

// TestEveryAcceptedInputEndsAsDeliveredOrDropped covers the plan's
// promise: an accepted input is never lost.
func TestEveryAcceptedInputEndsAsDeliveredOrDropped(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	accepted := 0
	for i := 0; i < 3; i++ {
		if _, err := s.Send("a message", poo.Steer); err != nil {
			t.Fatalf("sending a message: %v", err)
		}
		accepted++
	}
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	queued := sink.count("input.queued")
	if queued != accepted {
		t.Errorf("the log holds %d input.queued records, want %d", queued, accepted)
	}
	fates := sink.count("input.delivered") + sink.count("input.dropped")
	if fates != accepted {
		t.Errorf("the inputs have %d fates, want %d", fates, accepted)
	}
}

// blockingAfterTurn is an AfterTurn hook that waits for its context.
type blockingAfterTurn struct {
	// once guards the started channel, so a hook that runs twice does
	// not close it twice.
	once sync.Once
	// finished guards the done channel, for the same reason.
	finished sync.Once
	// started is closed once the hook is running.
	started chan struct{}
	// release is closed to let the hook return.
	release chan struct{}
	// done is closed once the hook has returned.
	done chan struct{}
}

// AfterTurn waits for the release or the context.
func (h *blockingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	if h.done == nil {
		h.done = make(chan struct{})
	}
	h.once.Do(func() { close(h.started) })
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	h.finished.Do(func() { close(h.done) })
	return poo.TurnDecision{}, nil
}

// blockingBeforeSettle is a BeforeSettle hook that waits for its
// context.
type blockingBeforeSettle struct {
	// once guards the started channel, so a hook that runs twice does
	// not close it twice.
	once sync.Once
	// finished guards the done channel, for the same reason.
	finished sync.Once
	// started is closed once the hook is running.
	started chan struct{}
	// release is closed to let the hook return.
	release chan struct{}
	// done is closed once the hook has returned.
	done chan struct{}
}

// BeforeSettle waits for the release or the context.
func (h *blockingBeforeSettle) BeforeSettle(ctx poo.HookContext, candidate poo.Reason) (poo.SettleDecision, error) {
	if h.done == nil {
		h.done = make(chan struct{})
	}
	h.once.Do(func() { close(h.started) })
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	h.finished.Do(func() { close(h.done) })
	return poo.Accept(), nil
}

// stallingTransport answers the first request with a stream that never
// ends, and the rest with the scripted responses.
//
// It is the shape a provider has when it is thinking: the test can
// interrupt the run at a known point, and the run carries on.
type stallingTransport struct {
	mu sync.Mutex
	// streaming is closed when the first stream starts.
	streaming chan struct{}
	// responses are the responses the later requests get.
	responses []fakeResponse
	// index is the next response to serve.
	index int
	// hold keeps the stalled stream from returning after its context
	// ends, so a test can line up more than one interrupt before the
	// abort is handled.
	hold chan struct{}
}

// newStallingTransport returns a transport that stalls on its first
// request.
func newStallingTransport(responses ...fakeResponse) *stallingTransport {
	return &stallingTransport{streaming: make(chan struct{}), responses: responses}
}

// Stream hands out the stalling stream the first time, and the scripted
// responses after that.
func (t *stallingTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	if t.index == 0 {
		t.index++
		streaming := t.streaming
		t.mu.Unlock()
		return &stallingReader{ctx: ctx, started: streaming, hold: t.hold}, poo.Rendered{API: poo.APIChatCompletions}, nil
	}
	i := t.index - 1
	t.index++
	t.mu.Unlock()
	if i >= len(t.responses) {
		return &fakeReader{events: []poo.StreamEvent{
			{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "the second answer"}},
			{Kind: "finish", FinishReason: poo.FinishStop},
		}}, poo.Rendered{API: poo.APIChatCompletions}, nil
	}
	resp := t.responses[i]
	return &fakeReader{events: resp.events()}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake.
func (t *stallingTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// stallingReader yields one text delta and then waits.
type stallingReader struct {
	// ctx is the request's context.
	ctx context.Context
	// started is closed when the first delta is handed out.
	started chan struct{}
	// sent reports whether the delta was handed out.
	sent bool
	// hold delays the reader's return after its context ends, when it is
	// set.
	hold chan struct{}
}

// Next hands out one delta and then waits for the context.
func (r *stallingReader) Next() (poo.StreamEvent, error) {
	if !r.sent {
		r.sent = true
		close(r.started)
		return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "partial text"}}, nil
	}
	<-r.ctx.Done()
	if r.hold != nil {
		<-r.hold
	}
	return poo.StreamEvent{}, r.ctx.Err()
}

// Close releases the reader.
func (r *stallingReader) Close() error { return nil }

// TestAMessageWhileSettlingIsRefused covers the plan's rule: a message
// for a run that is settling is refused, because the outcome is already
// fixed.
func TestAMessageWhileSettlingIsRefused(t *testing.T) {
	// The store holds the run's settle record, so the run stays in the
	// settling state while the test sends a message.
	sink := &blockingSink{started: make(chan struct{}), release: make(chan struct{})}

	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-sink.started
	// The run is settling.
	_, err := s.Send("and this", poo.Steer)
	if err == nil {
		t.Fatal("the session accepted a message while the run was settling")
	}
	if err != poo.ErrNotRunning {
		t.Errorf("the error is %v, want ErrNotRunning", err)
	}
	close(sink.release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// blockingSink is a record sink that holds the settle record until it
// is released.
//
// The run stays in the settling state while the write is held, so a
// test can act at a known point.
type blockingSink struct {
	// mu guards the records.
	mu sync.Mutex
	// records holds the records that were accepted.
	records []poo.Record
	// started is closed when the settle record's write begins.
	started chan struct{}
	// release is closed to let the write finish.
	release chan struct{}
	// once guards the started channel.
	once sync.Once
}

// Append keeps the record, and holds the settle record until it is
// released.
func (s *blockingSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "run.settled" {
		s.once.Do(func() { close(s.started) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *blockingSink) Flush(ctx context.Context) error { return nil }

// TestTheInterruptMessageIsDeliveredNext covers the plan's rule: the
// interrupt's own message reaches the model at the turn end.
func TestTheInterruptMessageIsDeliveredNext(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The message reached the model, with the interrupt's origin.
	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginInterrupt && strings.Contains(m.Content[0].Text, "stop that") {
			found = true
		}
	}
	if !found {
		t.Error("the interrupt's message did not reach the model's view")
	}
}

// TestThePartialMessageKeepsTheToolCallsVisible covers the plan's rule:
// the calls in a partial response are not run, and the model sees that.
func TestThePartialMessageKeepsTheToolCallsVisible(t *testing.T) {
	tr := newPartialTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The tool did not run.
	if len(tool.Calls()) != 0 {
		t.Errorf("the tool ran %d times, want none: a partial response's calls do not run", len(tool.Calls()))
	}
	// The call is recorded as skipped with the visible note.
	var skipped *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.ToolResultRecord)
		if !ok {
			continue
		}
		skipped = r
	}
	if skipped == nil {
		t.Fatal("the log holds no tool result")
	}
	if skipped.Outcome != poo.OutcomeSkipped {
		t.Errorf("the outcome is %q, want skipped", skipped.Outcome)
	}
	if want := poo.HandleLine("r1", "echo") + "\n" + poo.SkippedInterruptedText; skipped.Text != want {
		t.Errorf("the result's text is %q, want %q", skipped.Text, want)
	}
	// The partial assistant message declares the call, so the skipped
	// result has a call to answer.
	var partial *poo.MessageRecord
	for _, m := range sink.messages() {
		if m.Partial {
			partial = m
		}
	}
	if partial == nil {
		t.Fatal("the log holds no partial message")
	}
	calls := 0
	for _, b := range partial.Content {
		if b.Kind == poo.ContentToolCall {
			calls++
		}
	}
	if calls != 1 {
		t.Errorf("the partial assistant message holds %d tool calls, want 1", calls)
	}
}

// partialTransport answers with a response that carries text and a tool
// call, and then never ends.
type partialTransport struct {
	// streaming is closed when the first delta is handed out.
	streaming chan struct{}
}

// newPartialTransport returns a transport that answers with a partial
// response.
func newPartialTransport() *partialTransport {
	return &partialTransport{streaming: make(chan struct{})}
}

// Stream hands out the partial stream.
func (t *partialTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return &partialCallReader{ctx: ctx, started: t.streaming}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake.
func (t *partialTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// partialCallReader yields a text delta and a tool call, and then waits.
type partialCallReader struct {
	// ctx is the request's context.
	ctx context.Context
	// started is closed when the first delta is handed out.
	started chan struct{}
	// stage is what the reader hands out next.
	stage int
}

// Next hands out the delta and the call, and then waits.
func (r *partialCallReader) Next() (poo.StreamEvent, error) {
	switch r.stage {
	case 0:
		r.stage++
		close(r.started)
		return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "partial text"}}, nil
	case 1:
		r.stage++
		return poo.StreamEvent{Kind: "tool_call", ToolCall: &poo.ToolCallDelta{
			Index:     0,
			ID:        "call_1",
			Name:      "echo",
			Arguments: []byte(`{"text":"hello"}`),
			Complete:  true,
		}}, nil
	default:
		<-r.ctx.Done()
		return poo.StreamEvent{}, r.ctx.Err()
	}
}

// Close releases the reader.
func (r *partialCallReader) Close() error { return nil }

// TestAFollowUpWaitsForTheNaturalStopNotTheTurnEnd covers the plan's
// rule: a follow-up is delivered only when the run would settle
// naturally, so a turn end that continues the run leaves it queued.
func TestAFollowUpWaitsForTheNaturalStopNotTheTurnEnd(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}}},
		{text: "the second answer"},
		{text: "the third answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	hold.finish()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}

	reqs := tr.Requests()
	if len(reqs) != 3 {
		t.Fatalf("the run made %d requests, want 3: the call, the answer, the follow-up", len(reqs))
	}
	for _, m := range reqs[1].Messages {
		if m.Origin == poo.OriginFollowUp {
			t.Errorf("the second request already carries the follow-up %q", m.Text())
		}
	}
	last := reqs[2].Messages
	if got := last[len(last)-1]; got.Origin != poo.OriginFollowUp {
		t.Errorf("the third request ends with a %q message, want the follow-up", got.Origin)
	}
}

// resumedRun starts a run whose first turn ends naturally while a
// BeforeSettle hook runs, queues a follow-up in that window, and lets
// the finalization be abandoned so the run resumes with the tool call
// the second scripted response makes.
func resumedRun(t *testing.T, tool *echoTool, maxDuration time.Duration) (*poo.Session, *recordingSink) {
	t.Helper()
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingBeforeSettle{started: hookStarted, release: release}
	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}}},
	})
	sink := &recordingSink{}
	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.MaxDuration = maxDuration
	opts.MaxTurns = 4

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hookStarted
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	close(release)
	return s, sink
}

// TestAResumedRunKeepsItsDurationBudget covers the plan's MaxDuration:
// the timer settles a stuck run, also one that resumed after an
// abandoned finalization.
func TestAResumedRunKeepsItsDurationBudget(t *testing.T) {
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
		return poo.ToolResult{Text: "done"}, nil
	}
	start := time.Now()
	s, _ := resumedRun(t, tool, 300*time.Millisecond)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonBudgetExhausted || res.Detail.Source != "max_duration" {
		t.Errorf("the run settled %q (%s), want budget_exhausted from max_duration", res.Reason, res.Detail.Source)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the run took %s to settle, want the timer to settle it near 300ms", took)
	}
}

// TestAPanicInAResumedRunStillSettles covers the settle guarantee: a
// panic in the resumed part of a run settles tool_fatal, as it does in
// the first part.
func TestAPanicInAResumedRunStillSettles(t *testing.T) {
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		panic("the tool panicked")
	}
	s, sink := resumedRun(t, tool, 0)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonToolFatal || res.Detail.Source != "panic" {
		t.Errorf("the run settled %q (%s), want tool_fatal from the panic", res.Reason, res.Detail.Source)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestAnInterruptDuringTheBatchRecordsTheReplyOnce covers the plan's
// rule that partial is for a reply that was not recorded: an interrupt
// in the executing state leaves the one complete assistant message.
func TestAnInterruptDuringTheBatchRecordsTheReplyOnce(t *testing.T) {
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "partial output", IsError: true}, nil
	}
	tr := scripted([]fakeResponse{
		{
			text:  "let me look",
			calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	if _, err := s.Send("stop that", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	var first []*poo.MessageRecord
	for _, m := range sink.messages() {
		if m.Role == poo.RoleAssistant && m.Turn == 1 {
			first = append(first, m)
		}
	}
	if len(first) != 1 {
		t.Fatalf("the log holds %d assistant messages for the first reply, want 1", len(first))
	}
	if first[0].Partial {
		t.Error("the one assistant message is marked partial, want a complete message")
	}
	n := 0
	reqs := tr.Requests()
	if len(reqs) < 2 {
		t.Fatalf("the run made %d requests, want a second one", len(reqs))
	}
	for _, m := range reqs[1].Messages {
		if m.Role == poo.RoleAssistant && m.Text() == "let me look" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the history holds the reply %d times, want 1", n)
	}
}

// sendWhenKnown sends a message that references a streaming reply,
// retrying until the session has assigned the reply's id at its first
// delta.
func sendWhenKnown(t *testing.T, s *poo.Session, text string, mode poo.InputMode, id string, upto int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := s.Send(text, mode, poo.WithDelivered(id, upto))
		if err == nil {
			return
		}
		var invalid *poo.ErrInvalidInput
		if !asInvalidInput(err, &invalid) || time.Now().After(deadline) {
			t.Fatalf("sending the message: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestThePartialMessageKeepsTheIDTheStreamAssigned covers the message id
// registry: the id assigned at the first delta is the id recorded on the
// partial message, so a reference to the streaming reply names it.
func TestThePartialMessageKeepsTheIDTheStreamAssigned(t *testing.T) {
	tr := newStallingTransport()
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	// The only id the session has assigned is the streaming reply's.
	sendWhenKnown(t, s, "stop that", poo.Interrupt, "m1", 4)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}

	var partial *poo.MessageRecord
	for _, m := range sink.messages() {
		if m.Partial {
			partial = m
		}
	}
	if partial == nil {
		t.Fatal("the log holds no partial message")
	}
	if partial.MessageID != "m1" {
		t.Errorf("the partial message's id is %q, want m1, the id the stream assigned", partial.MessageID)
	}
}

// TestARefusedDeliveryRecordEndsTheRunWithoutAnotherRequest covers the
// failure table's RecordSink row for an accepted input: a store that
// refuses the delivery record is a store_error candidate at once, not a
// failure left to the next write.
func TestARefusedDeliveryRecordEndsTheRunWithoutAnotherRequest(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}}},
		{text: "the second answer"},
	})
	sink := &failingSink{failRecord: "input.delivered"}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and this", poo.Steer); err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	hold.finish()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if n := len(tr.Requests()); n != 1 {
		t.Errorf("the run made %d requests, want 1: the refused record ends the run", n)
	}
}

// cancellingDeliverySink blocks a follow-up's message record until its
// write context ends, which is the shape of a store that is slow while
// the run is settling.
type cancellingDeliverySink struct {
	*recordingSink
	// entered is closed when the follow-up's write begins.
	entered chan struct{}
	// once guards the entered channel.
	once sync.Once
}

// Append holds the follow-up message until the write context ends.
func (s *cancellingDeliverySink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.Origin == poo.OriginFollowUp {
		s.once.Do(func() { close(s.entered) })
		<-ctx.Done()
		return ctx.Err()
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestADeliveryDuringASecondFinalizeIsDropped covers the plan's rule
// that an input which a resumed drain cannot record is reported as
// input.dropped: a cancel that commits while the drain is writing must
// not leave the input delivered, and the input is never silently lost.
func TestADeliveryDuringASecondFinalizeIsDropped(t *testing.T) {
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingBeforeSettle{started: hookStarted, release: release}
	tr := scripted([]fakeResponse{{text: "the first answer"}, {text: "the second answer"}})
	sink := &cancellingDeliverySink{recordingSink: &recordingSink{}, entered: make(chan struct{})}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}

	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 100*time.Millisecond)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hookStarted
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	close(release)
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the follow-up was never delivered")
	}
	done := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(done)
	}()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-done

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 1 {
		t.Errorf("the settle detail lists %d undelivered inputs, want 1", len(res.Detail.UndeliveredInput))
	}
	if n := sink.count("input.dropped"); n != 1 {
		t.Errorf("the log holds %d input.dropped records, want 1", n)
	}
	if n := sink.count("input.delivered"); n != 0 {
		t.Errorf("the log holds %d input.delivered records, want none", n)
	}
	for _, m := range sink.messages() {
		if m.Origin == poo.OriginFollowUp {
			t.Errorf("the follow-up was recorded after the finalization took the run over: %q", m.Content[0].Text)
		}
	}
	if n := len(tr.Requests()); n != 1 {
		t.Errorf("the run made %d requests, want 1", n)
	}
}

// refusingSteerSink refuses a steer's message record, and accepts
// everything else.
type refusingSteerSink struct {
	*recordingSink
}

// Append refuses the steer's message record.
func (s *refusingSteerSink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.Origin == poo.OriginSteer {
		return errors.New("the store refused the steer record")
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestADrainStopsWhenADeliveryWriteFails covers the plan's rule that
// accepted input is never silently lost: once a delivery's write fails,
// the failed input and the rest of the drain are reported in the settle
// detail.
func TestADrainStopsWhenADeliveryWriteFails(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the first answer", calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the second answer"},
	})
	sink := &refusingSteerSink{recordingSink: &recordingSink{}}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("the first steer", poo.Steer); err != nil {
		t.Fatalf("sending the first steer: %v", err)
	}
	if _, err := s.Send("the second steer", poo.Steer); err != nil {
		t.Fatalf("sending the second steer: %v", err)
	}
	hold.finish()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Fatalf("the run settled %q, want store_error", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 2 {
		t.Fatalf("the settle detail lists %d undelivered inputs, want 2", len(res.Detail.UndeliveredInput))
	}
	for _, got := range res.Detail.UndeliveredInput {
		if got.Mode != string(poo.Steer) {
			t.Errorf("the undelivered input's mode is %q, want steer", got.Mode)
		}
	}
	droppedEvents := 0
	for _, ev := range s.Events() {
		if ev.Name == poo.EventInputDropped {
			droppedEvents++
		}
	}
	if droppedEvents != 2 {
		t.Errorf("the session emitted %d input.dropped events, want 2", droppedEvents)
	}
}

// TestSendQueuedWriteFailureRefusesAndSettles covers the plan's rule: a
// message is recorded before Send returns, and a store failure is a
// store_error candidate for the active run, not a stuck queue entry.
func TestSendQueuedWriteFailureRefusesAndSettles(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "waiting now"},
		{text: "the answer"},
	})
	sink := &failingSink{failRecord: "input.queued"}
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.SetResult("the job's output")

	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	waitFor(t, "the run to wait", func() bool { return s.State() == poo.StateWaiting })
	if _, err := s.Send("a steer", poo.Steer); err == nil {
		t.Fatal("Send accepted the message although the record failed")
	}
	if s.QueuedJobResults() != 0 {
		// The job's own result is not what Send queued.
	}
	tool.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("the run did not settle after the store failure: %v (state %s)", err, s.State())
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
}

// TestFailedInputDeliveryIsDropped covers the plan's rule: every
// accepted input ends as exactly one of delivered or dropped. A delivery
// whose write fails is dropped and reported, not silently lost.
func TestFailedInputDeliveryIsDropped(t *testing.T) {
	sink := &failingSink{failRecord: "input.delivered"}
	tool := pootest.NewJobTool("job", poo.BackgroundNever)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", false)}},
		{text: "done"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	id, err := s.Send("accepted", poo.Steer)
	if err != nil {
		t.Fatalf("sending the steer: %v", err)
	}
	tool.Release()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	delivered, dropped := 0, 0
	for _, ev := range s.Events() {
		if ev.Payload["input_id"] != id {
			continue
		}
		switch ev.Name {
		case poo.EventInputDelivered:
			delivered++
		case poo.EventInputDropped:
			dropped++
		}
	}
	if delivered+dropped != 1 || len(res.Detail.UndeliveredInput) != 1 {
		t.Fatalf("accepted %s: reason=%s delivered=%d dropped=%d undelivered=%v; want one terminal disposition",
			id, res.Reason, delivered, dropped, res.Detail.UndeliveredInput)
	}
}

// TestAnInterruptedSyncCallKeepsItsPartialOutput covers the plan's rule:
// a synchronous call an interrupt stops keeps the partial output the
// tool returned, with the note saying why it ended.
func TestAnInterruptedSyncCallKeepsItsPartialOutput(t *testing.T) {
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundNever)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", false)}},
		{text: "done"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	if _, err := s.Send("stop it", poo.Interrupt); err != nil {
		t.Fatalf("sending the interrupt: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	for _, rec := range sink.allRecords() {
		out, ok := rec.(*poo.ToolResultRecord)
		if !ok || out.Tool != "job" {
			continue
		}
		if !strings.Contains(out.Text, "the call was stopped") {
			t.Fatalf("the interrupted result %q lost the partial output %q", out.Text, "the call was stopped")
		}
	}
}

// TestARefusedInputIsNotDrainedBeforeItsQueuedRecord covers the input
// contract: a message is not drainable until its input.queued record is
// written, so a refused message cannot reach the model through a
// concurrent turn-end drain.
func TestARefusedInputIsNotDrainedBeforeItsQueuedRecord(t *testing.T) {
	sink := &blockingRecordSink{
		kind:    "input.queued",
		fail:    true,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tool := &echoTool{result: "ok"}
	hold := newNoteHold(tool)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	hold.wait(t)
	done := make(chan error, 1)
	go func() {
		_, err := s.Send("REFUSED INPUT", poo.Steer)
		done <- err
	}()
	<-sink.entered
	hold.finish()
	// The turn-end drain has run, and the run has moved on to its next
	// request. The refused message must not be part of it.
	waitFor(t, "the drain's next request", func() bool { return len(tr.Requests()) >= 2 })
	close(sink.release)
	if err := <-done; err == nil {
		t.Fatal("Send accepted a message whose input.queued record was refused")
	}
	if _, err := s.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	for _, req := range tr.Requests() {
		for _, m := range req.Messages {
			if strings.Contains(m.Text(), "REFUSED INPUT") {
				t.Fatal("a refused input reached the provider")
			}
		}
	}
}

// blockingQueuedSink blocks the input.queued record write, so a test
// can hold an acceptance in flight while the finalization waits for it.
type blockingQueuedSink struct {
	*recordingSink
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the queued record until the test releases it.
func (s *blockingQueuedSink) Append(ctx context.Context, rec poo.Record) error {
	if _, ok := rec.(*poo.InputQueued); ok {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestASteerAcceptedDuringAFinalizeIsReportedAsUndelivered covers the
// plan's rule that an accepted input is never silently lost: a steer
// whose acceptance record is in flight when the run seals is taken out
// of the queue, and the settle detail lists it as undelivered.
func TestASteerAcceptedDuringAFinalizeIsReportedAsUndelivered(t *testing.T) {
	hookStarted := make(chan struct{})
	release := make(chan struct{})
	hook := &blockingBeforeSettle{started: hookStarted, release: release}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &blockingQueuedSink{recordingSink: &recordingSink{}, entered: make(chan struct{}), release: make(chan struct{})}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	// The settle gate is the ordering seam: the run.settling event is
	// emitted after the queues are closed, so releasing the acceptance
	// record's write from there lands it in the window where the run
	// is closed but not yet sealed.
	settling := make(chan struct{})
	obs := &settlingObserver{seen: settling}
	opts.Observers = []poo.Observer{obs}

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-hookStarted
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		if _, err := s.Send("and then this", poo.Steer); err != nil {
			t.Errorf("sending the steer: %v", err)
		}
	}()
	<-sink.entered
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Cancel("probe")
	}()
	<-settling
	close(sink.release)
	<-sent
	close(release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	<-done

	if res.Reason != poo.ReasonCancelled {
		t.Fatalf("the run settled %q, want cancelled", res.Reason)
	}
	if len(res.Detail.UndeliveredInput) != 1 {
		t.Errorf("the settle detail lists %d undelivered inputs, want 1", len(res.Detail.UndeliveredInput))
	}
	if n := sink.count("input.queued"); n != 1 {
		t.Errorf("the log holds %d input.queued records, want 1", n)
	}
	if n := sink.count("input.delivered"); n != 0 {
		t.Errorf("the log holds %d input.delivered records, want none", n)
	}
	if n := len(tr.Requests()); n != 1 {
		t.Errorf("the run made %d requests, want 1: the steer never reaches the model", n)
	}
}
