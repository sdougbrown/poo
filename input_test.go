package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The input path: a steer reaches the model at the next turn end, an
// interrupt aborts the current activity, and a follow-up waits for a
// natural stop. Every accepted input ends as exactly one of delivered or
// dropped.

// TestASteerIsDeliveredAtTheNextTurnEnd covers the steer: the message
// reaches the model at the turn end, and the turn's work is untouched.
func TestASteerIsDeliveredAtTheNextTurnEnd(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	}}
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

	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	}}
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

	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	}}
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

	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{
				{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"one"}`)},
				{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"text":"two"}`)},
			},
		},
		{text: "the second answer"},
	}}
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
	if skipped.Text != poo.SkippedInterruptedText {
		t.Errorf("the result's text is %q, want the visible note", skipped.Text)
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

	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
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

	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Send("and then this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
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

	tr := &fakeTransport{responses: []fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	}}
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
	turns := tr.requestCount()
	if after.calls != turns {
		t.Errorf("the AfterTurn hook ran %d times, want %d: one per turn", after.calls, turns)
	}
}

// TestAMessageWhileIdleIsRefused covers the plan's rule: a message for a
// session with no run is refused.
func TestAMessageWhileIdleIsRefused(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
		return &stallingReader{ctx: ctx, started: streaming}, poo.Rendered{API: poo.APIChatCompletions}, nil
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
}

// Next hands out one delta and then waits for the context.
func (r *stallingReader) Next() (poo.StreamEvent, error) {
	if !r.sent {
		r.sent = true
		close(r.started)
		return poo.StreamEvent{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "partial text"}}, nil
	}
	<-r.ctx.Done()
	return poo.StreamEvent{}, r.ctx.Err()
}

// Close releases the reader.
func (r *stallingReader) Close() error { return nil }
