package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The failure table is normative: most failures are not terminal, and
// the ones that are end the run with exactly one settle record.

// failingSink is a record sink that can be told to fail.
type failingSink struct {
	mu sync.Mutex
	// records holds the records that were accepted.
	records []poo.Record
	// failAt is the number of the append that fails. Zero means every
	// append succeeds.
	failAt int
	// failRecord is the name of the record that fails. Empty means no
	// record fails by name.
	failRecord string
	// failAfter is how many records of that name are accepted before
	// one fails. Zero means the first one fails.
	failAfter int
	// appends counts every append, including the failed one.
	appends int
	// named counts the records of the failing name that were seen.
	named int
	// err is the failure the sink returns.
	err error
	// blockUntilContextEnd makes every write wait for its context.
	blockUntilContextEnd bool
}

// Append keeps one record, unless it is told to fail.
func (s *failingSink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	s.appends++
	failAt := s.failAt
	block := s.blockUntilContextEnd
	s.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	if failAt != 0 && s.appends == failAt {
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return errors.New("the store refused the record")
	}
	if s.failRecord != "" && rec.Meta().Record == s.failRecord {
		s.mu.Lock()
		s.named++
		seen := s.named
		err := s.err
		s.mu.Unlock()
		if seen > s.failAfter {
			if err != nil {
				return err
			}
			return errors.New("the store refused the record")
		}
	}
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()
	return nil
}

// Flush does nothing.
func (s *failingSink) Flush(ctx context.Context) error { return nil }

// count returns how many records of a name the sink kept.
func (s *failingSink) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// TestAHookErrorSettlesTheRunOnce covers the hook row of the failure
// table: a hook that fails settles the run, and the run settles once.
func TestAHookErrorSettlesTheRunOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hook := &failingAfterTurn{}

	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonHookError {
		t.Errorf("the run settled %q, want hook_error", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestAHookDeadlineSettlesTheRunOnce covers the deadline row: a hook
// that misses its deadline settles the run, and it settles once.
func TestAHookDeadlineSettlesTheRunOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	hook := &stallingAfterTurn{}

	opts := testOptions(t, tr, tool, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	opts.HookTimeout = 50 * time.Millisecond

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonHookError {
		t.Errorf("the run settled %q, want hook_error", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestAToolFatalSettlesTheRunOnce covers the tool row: an error that
// wraps `ErrFatal` settles the run.
func TestAToolFatalSettlesTheRunOnce(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("the working directory is gone")
	}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonToolFatal {
		t.Errorf("the run settled %q, want tool_fatal", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestATransportErrorSettlesTheRunOnce covers the transport row.
func TestATransportErrorSettlesTheRunOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{
		err: &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "the endpoint is down"},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestASinkFailureSettlesTheRunOnceAndBreaksTheSession covers the
// store row: a sink that fails settles the run as store_error, and the
// session becomes broken.
func TestASinkFailureSettlesTheRunOnceAndBreaksTheSession(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	// The sink refuses the third write, which is the model's message.
	sink := &failingSink{failAt: 3}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken after a store failure")
	}
	// A broken session refuses a later prompt.
	if _, err := s.Prompt(context.Background(), "again"); err == nil {
		t.Error("the broken session accepted a prompt")
	}
}

// TestACancelDuringAToolSettlesTheRunOnce covers the cancel row.
func TestACancelDuringAToolSettlesTheRunOnce(t *testing.T) {
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "interrupted", IsError: true}, nil
	}
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
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
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
	// The tool got a result, so the history stays valid.
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

// TestARecoverableToolErrorLeavesTheRunRunning covers the recoverable
// row: an ordinary tool error goes back to the model, and the run
// continues.
func TestARecoverableToolErrorLeavesTheRunRunning(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{Text: "the command failed", IsError: true}, nil
	}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn: a recoverable error does not end the run", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
}

// TestAConcurrentCancelAndStoreFailureArbitrateByPrecedence covers the
// plan's arbitration rule: a cancel beats a store failure, and the
// session is still broken.
func TestAConcurrentCancelAndStoreFailureArbitrateByPrecedence(t *testing.T) {
	tr := newStallingTransport()
	tool := &echoTool{result: "echoed"}
	// The sink refuses the settle record, which is written after the
	// run's own records.
	sink := &failingSink{failRecord: "run.settled"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if !s.Broken() {
		t.Error("the session is not broken after a store failure")
	}
}

// TestAStuckSinkStillLetsTheWaitersWake covers the plan's bound: a sink
// that blocks until its context ends does not hang the run.
func TestAStuckSinkStillLetsTheWaitersWake(t *testing.T) {
	defer poo.SetRecordWriteBudget(50 * time.Millisecond)()

	tr := scripted([]fakeResponse{{text: "the answer"}})
	tool := &echoTool{result: "echoed"}
	sink := &failingSink{blockUntilContextEnd: true}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Wait(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the waiters did not wake")
	}
}

// TestTheConstructionRollsBack covers the plan's construction row: a
// `SessionStart` error returns `ErrSessionInit`, the observers are
// stopped, and nothing is written.
func TestTheConstructionRollsBack(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	observer := &countingObserver{}

	opts := testOptions(t, tr, tool, sink)
	opts.SessionStart = []poo.SessionStartHook{&failingSessionStart{}}
	opts.Observers = []poo.Observer{observer}

	_, err := poo.New(opts)
	if err == nil {
		t.Fatal("the session was built with a failing session_start hook")
	}
	var init *poo.ErrSessionInit
	if !asSessionInit(err, &init) {
		t.Fatalf("the error is %T, want an ErrSessionInit", err)
	}
	// Nothing was written.
	if len(sink.records) != 0 {
		t.Errorf("the log holds %d records, want none", len(sink.records))
	}
	// No run was settled: the construction failed before there was a
	// run to settle.
	if sink.count("run.settled") != 0 {
		t.Errorf("the log holds %d run.settled records, want none", sink.count("run.settled"))
	}
	// The observers were stopped, so nothing arrives after the failure.
	before := observer.eventCount()
	time.Sleep(50 * time.Millisecond)
	if after := observer.eventCount(); after != before {
		t.Errorf("the observer saw %d events after the construction failed, want none", after-before)
	}
}

// TestTheSessionStartHookAppendsToTheHistory covers the other side: a
// hook that appends puts its message in the model's view, with its own
// origin.
func TestTheSessionStartHookAppendsToTheHistory(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.SessionStart = []poo.SessionStartHook{&appendingSessionStart{}}

	s := newTestSession(t, opts)
	runSession(t, s, "do the thing")

	found := false
	for _, m := range sink.messages() {
		if m.Origin == poo.HookOrigin("appendingSessionStart") {
			found = true
		}
	}
	if !found {
		t.Error("the session_start hook's message did not reach the model's view")
	}
}

// TestEveryIssuedCallGetsAResult covers the plan's rule: a call that
// was issued but has no result gets a synthetic one, so the history
// stays valid for the next run.
func TestEveryIssuedCallGetsAResult(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		// The call waits for the run's context, so the finalization
		// abandons it.
		<-ctx.Done()
		return poo.ToolResult{Text: "interrupted", IsError: true}, nil
	}
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	// The run is cancelled, which abandons the call.
	s.Cancel("cancel:test")
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	// The call got a result.
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

// TestALateCompletionDoesNotMutateTheSettledRun covers the plan's rule:
// a completion that arrives after the commit point is dropped.
func TestALateCompletionDoesNotMutateTheSettledRun(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		return poo.ToolResult{Text: "the late result"}, nil
	}
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	// The settle record says cancelled, and the late result does not
	// change it.
	close(release)
	time.Sleep(50 * time.Millisecond)
	settle := sink.settled()
	if settle == nil {
		t.Fatal("the log holds no settle record")
	}
	if settle.Reason != poo.ReasonCancelled {
		t.Errorf("the settle record says %q, want cancelled", settle.Reason)
	}
}

// TestTheBusyFlagClearsAfterTheRun covers the plan's rule: the busy
// flag clears when the run settles, so a new prompt is accepted.
func TestTheBusyFlagClearsAfterTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	})
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, &recordingSink{}))
	runSession(t, s, "do the thing")

	if _, err := s.Prompt(context.Background(), "again"); err != nil {
		t.Fatalf("the session refused a prompt after the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// TestClosingTheSessionCancelsTheRun covers the plan's close ordering:
// an active run is cancelled when the session closes.
func TestClosingTheSessionCancelsTheRun(t *testing.T) {
	tr := newStallingTransport()
	tool := &echoTool{result: "echoed"}
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.streaming
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	settle := sink.settled()
	if settle == nil {
		t.Fatal("the log holds no settle record")
	}
	if settle.Reason != poo.ReasonCancelled {
		t.Errorf("the settle record says %q, want cancelled", settle.Reason)
	}
	if settle.Detail.Source != "cancel:close" {
		t.Errorf("the settle source is %q, want cancel:close", settle.Detail.Source)
	}
}

// failingAfterTurn is an AfterTurn hook that always fails.
type failingAfterTurn struct{}

// AfterTurn fails.
func (h *failingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	return poo.TurnDecision{}, errors.New("the hook failed")
}

// stallingAfterTurn is an AfterTurn hook that never returns.
type stallingAfterTurn struct{}

// AfterTurn waits for its context to end.
func (h *stallingAfterTurn) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	<-ctx.Done()
	return poo.TurnDecision{}, ctx.Err()
}

// failingSessionStart is a session-start hook that always fails.
type failingSessionStart struct{}

// SessionStart fails.
func (h *failingSessionStart) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{}, errors.New("the hook failed")
}

// appendingSessionStart is a session-start hook that appends a message.
type appendingSessionStart struct{}

// SessionStart appends a message.
func (h *appendingSessionStart) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	return poo.StartDecision{
		Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "the session's own note")},
	}, nil
}

// countingObserver is an observer that counts the events it sees.
//
// It also reports whether the session stopped it, so a test can check
// the construction rollback.
type countingObserver struct {
	mu sync.Mutex
	// events counts the events the observer saw.
	events int
}

// Observe counts the event.
func (o *countingObserver) Observe(ev poo.Event) error {
	o.mu.Lock()
	o.events++
	o.mu.Unlock()
	return nil
}

// eventCount returns how many events the observer saw.
func (o *countingObserver) eventCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.events
}

// TestABeforeToolHookErrorSettlesTheRun covers the hook row of the
// failure table for the before-tool slot: a hook that fails settles the
// run, and the run settles once.
func TestABeforeToolHookErrorSettlesTheRun(t *testing.T) {
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.BeforeTool = []poo.BeforeToolHook{&failingBeforeTool{}}

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonHookError {
		t.Errorf("the run settled %q, want hook_error", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// failingBeforeTool is a before-tool hook that always fails.
type failingBeforeTool struct{}

// BeforeTool fails.
func (h *failingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	return poo.ToolDecision{}, errors.New("the hook failed")
}

// TestACallThatFinishesAsTheRunSettlesGetsOneResult covers the race:
// a call that finishes at the same moment the run settles gets exactly
// one result, never two.
func TestACallThatFinishesAsTheRunSettlesGetsOneResult(t *testing.T) {
	// The test runs the race many times, because the two goroutines
	// have to meet for the bug to show.
	for i := 0; i < 50; i++ {
		release := make(chan struct{})
		started := make(chan struct{})
		tool := &echoTool{result: "echoed"}
		tool.call = func(ctx context.Context) (poo.ToolResult, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return poo.ToolResult{Text: "the result"}, nil
		}
		tr := scripted([]fakeResponse{
			{
				calls: []poo.ToolCall{{
					ID:        "call_1",
					Name:      "echo",
					Arguments: []byte(`{"text":"hello"}`),
				}},
			},
			{text: "done"},
		})
		sink := &recordingSink{}

		s := newTestSession(t, testOptions(t, tr, tool, sink))
		if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
			t.Fatalf("starting the run: %v", err)
		}
		<-started
		// The run is cancelled and the call is released at the same
		// moment.
		go func() {
			close(release)
		}()
		s.Cancel("cancel:test")
		if _, err := s.Wait(context.Background()); err != nil {
			t.Fatalf("waiting for the run: %v", err)
		}

		if n := sink.count("tool.result"); n != 1 {
			t.Fatalf("the log holds %d tool.result records, want exactly 1", n)
		}
	}
}

// flushFailingSink takes every record and fails the flush.
type flushFailingSink struct {
	recordingSink
}

// Flush fails.
func (s *flushFailingSink) Flush(ctx context.Context) error {
	return errors.New("the disk is full")
}

// TestAFailingFlushAfterTheSettleIsReported covers a sink that cannot
// flush: the run reports the store failure, the session is broken, and
// the sink holds exactly one run.settled record.
func TestAFailingFlushAfterTheSettleIsReported(t *testing.T) {
	tr := scripted([]fakeResponse{{
		text:  "all done",
		usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 2},
	}})
	sink := &flushFailingSink{}

	s := newTestSession(t, testOptions(t, tr, &echoTool{result: "echoed"}, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if !strings.Contains(res.Detail.Text, "the disk is full") {
		t.Errorf("the settle text is %q, want the flush error", res.Detail.Text)
	}
	if !s.Broken() {
		t.Error("the session is not broken after a failed flush")
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the sink holds %d run.settled records, want 1", sink.count("run.settled"))
	}
}

// TestAHandleNamesOneCallForTheLifeOfTheSession covers the plan's
// session-monotonic handles: the second turn's call does not reuse the
// first turn's handle, and the second run's does not reuse either.
func TestAHandleNamesOneCallForTheLifeOfTheSession(t *testing.T) {
	call := poo.ToolCall{ID: "call", Name: "echo", Arguments: []byte(`{}`)}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{call}},
		{calls: []poo.ToolCall{call}},
		{text: "done"},
		{calls: []poo.ToolCall{call}},
		{text: "done"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{result: "echoed"}, sink))
	runSession(t, s, "first")
	runSession(t, s, "second")

	var got []string
	for _, rec := range sink.allRecords() {
		if c, ok := rec.(*poo.ToolCallRecord); ok {
			got = append(got, c.Handle)
		}
	}
	want := []string{"r1", "r2", "r3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the calls' handles are %v, want %v", got, want)
	}
}

// TestACandidateRaisedInTheBatchIsListedOnce covers detail.losing: it
// holds the other candidates, so a lone tool_fatal has no losers and the
// winner is never among them.
func TestACandidateRaisedInTheBatchIsListedOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("the working directory is gone")
	}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonToolFatal {
		t.Fatalf("the run settled %q, want tool_fatal", res.Reason)
	}
	if len(res.Detail.Losing) != 0 {
		t.Errorf("detail.losing is %v, want none", res.Detail.Losing)
	}
}

// TestALosingCandidateIsListedOnce covers detail.losing for a run that
// holds a candidate raised in the batch and a later one: the
// loser appears once.
func TestALosingCandidateIsListedOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"hello"}`)}},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("the working directory is gone")
	}
	opts := testOptions(t, tr, tool, sink)
	opts.MaxTurns = 1

	s := newTestSession(t, opts)
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonToolFatal {
		t.Fatalf("the run settled %q, want tool_fatal", res.Reason)
	}
	if got := res.Detail.Losing; len(got) != 1 || got[0] != string(poo.ReasonMaxTurns) {
		t.Errorf("detail.losing is %v, want [max_turns]", got)
	}
}

// TestAFatalToolErrorSkipsTheRestOfTheBatch covers the plan's rule that
// a fatal error ends the batch: the calls after it do not start, and
// they get a visible skipped result so the history stays valid.
func TestAFatalToolErrorSkipsTheRestOfTheBatch(t *testing.T) {
	tr := scripted([]fakeResponse{{
		calls: []poo.ToolCall{
			{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"one"}`)},
			{ID: "call_2", Name: "echo", Arguments: []byte(`{"text":"two"}`)},
		},
	}})
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed", class: poo.ClassSideEffecting}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("the working directory is gone")
	}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	res := runSession(t, s, "do the thing")

	if res.Reason != poo.ReasonToolFatal {
		t.Fatalf("the run settled %q, want tool_fatal", res.Reason)
	}
	if n := len(tool.Calls()); n != 1 {
		t.Errorf("the tool ran %d times, want 1: the fatal error ends the batch", n)
	}
	outcomes := map[string]poo.Outcome{}
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			outcomes[r.Handle] = r.Outcome
		}
	}
	if outcomes["r1"] != poo.OutcomeError || outcomes["r2"] != poo.OutcomeSkipped {
		t.Errorf("the outcomes are %v, want r1 error and r2 skipped", outcomes)
	}
}

// lateAnswerTransport holds the stream until the test releases it, then
// answers with text and one tool call. It is the shape of a response
// that arrives after the run has already settled.
type lateAnswerTransport struct {
	// entered is closed when the request reaches the transport.
	entered chan struct{}
	// release is closed to let the stream be handed out.
	release chan struct{}
	// once guards the entered channel.
	once sync.Once
}

// Stream waits for the release.
func (t *lateAnswerTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.once.Do(func() { close(t.entered) })
	<-t.release
	r := fakeResponse{text: "late answer", calls: []poo.ToolCall{{
		ID:        "call_1",
		Name:      "echo",
		Arguments: []byte(`{"text":"late"}`),
	}}}
	return &fakeReader{events: r.events()}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake.
func (t *lateAnswerTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// TestALateResponseAfterTheSettleDoesNotRecord covers the plan's rule
// that a run-owned completion arriving after the commit point never
// mutates a settled run: a provider response that arrives after the
// settle must not append a message, run a tool, or write a record.
func TestALateResponseAfterTheSettleDoesNotRecord(t *testing.T) {
	tr := &lateAnswerTransport{entered: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}
	opts := testOptions(t, tr, tool, sink)
	opts.MaxDuration = 30 * time.Millisecond

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.entered
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonBudgetExhausted {
		t.Fatalf("the run settled %q, want budget_exhausted", res.Reason)
	}
	recordsBefore := len(sink.allRecords())
	eventsBefore := len(s.Events())

	close(tr.release)
	time.Sleep(200 * time.Millisecond)

	if got := len(sink.allRecords()); got != recordsBefore {
		t.Errorf("the late response wrote %d records, want none", got-recordsBefore)
	}
	if n := len(tool.Calls()); n != 0 {
		t.Errorf("the tool ran %d times after the run settled", n)
	}
	if got := s.State(); got != poo.StateSettled {
		t.Errorf("the session is %q, want settled", got)
	}
	if got := len(s.Events()); got != eventsBefore {
		t.Errorf("the late response emitted %d events, want none", got-eventsBefore)
	}
}

// refusingDeliverySink refuses the follow-up message record and the
// delivered record, and accepts everything else.
type refusingDeliverySink struct {
	*recordingSink
}

// Append refuses the two records a follow-up's delivery writes.
func (s *refusingDeliverySink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && m.Origin == poo.OriginFollowUp {
		return errors.New("the store refused the follow-up record")
	}
	if _, ok := rec.(*poo.InputDelivered); ok {
		return errors.New("the store refused the delivered record")
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestTheWinnerIsNotListedAsItsOwnLoser covers detail.losing: it holds
// the other candidates, so a winner that shares its reason with a
// duplicate is listed neither as a loser nor twice.
func TestTheWinnerIsNotListedAsItsOwnLoser(t *testing.T) {
	tr := scripted([]fakeResponse{
		{text: "the first answer", calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "the second answer"},
	})
	sink := &refusingDeliverySink{recordingSink: &recordingSink{}}
	tool := &echoTool{result: "echoed"}
	hold := newNoteHold(tool)
	opts := testOptions(t, tr, tool, sink)
	opts.MaxTurns = 3

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	hold.wait(t)
	if _, err := s.Send("and this", poo.FollowUp); err != nil {
		t.Fatalf("sending the follow-up: %v", err)
	}
	hold.finish()
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Fatalf("the run settled %q, want store_error", res.Reason)
	}
	for _, losing := range res.Detail.Losing {
		if losing == string(res.Reason) {
			t.Errorf("the winner %q is listed in its own detail.losing %v", res.Reason, res.Detail.Losing)
		}
	}
}

// finalizingContextTransport holds the request until its context ends,
// and then returns the context's error. It is the shape of a provider
// request the finalization cancels.
type finalizingContextTransport struct {
	// entered is closed when the request reaches the transport.
	entered chan struct{}
	// once guards the entered channel.
	once sync.Once
}

// Stream waits for the request's context to end.
func (t *finalizingContextTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.once.Do(func() { close(t.entered) })
	<-ctx.Done()
	return nil, poo.Rendered{API: poo.APIChatCompletions}, ctx.Err()
}

// Fork is not implemented by the fake.
func (t *finalizingContextTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// TestTheFinalizesOwnCancelIsNotACandidate covers the plan's rule that
// finalizing never manufactures a cancelled run: the timer settles a run
// whose request is cancelled by the finalization itself.
func TestTheFinalizesOwnCancelIsNotACandidate(t *testing.T) {
	tr := &finalizingContextTransport{entered: make(chan struct{})}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "echoed"}, sink)
	opts.MaxDuration = 30 * time.Millisecond

	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.entered
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason == poo.ReasonCancelled {
		t.Errorf("the run settled cancelled: the finalization's own cancel became a candidate")
	}
	for _, losing := range res.Detail.Losing {
		if losing == string(poo.ReasonCancelled) {
			t.Errorf("the finalize's own cancel is a losing candidate: %v", res.Detail.Losing)
		}
	}
}

// TestAFailedRunStartStillSettles covers the failure table's RecordSink
// row at the run's first write: the run still goes through finalize, so
// it emits run.settled and takes the store_error exit code.
func TestAFailedRunStartStillSettles(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "hi"}})
	tool := &echoTool{result: "ok"}
	sink := &failingSink{failAt: 1}
	opts := testOptions(t, tr, tool, sink)
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("reason = %q, want store_error", res.Reason)
	}
	settled := 0
	for _, ev := range s.Events() {
		if ev.Name == poo.EventRunSettled {
			settled++
		}
	}
	if settled != 1 {
		t.Errorf("the session emitted %d run.settled events, want 1", settled)
	}
	codes := s.SettleCodes()
	if len(codes) != 1 || codes[0] != 23 {
		t.Errorf("settle codes = %v, want [23]", codes)
	}
}

// TestAUsageWriteFailureIsTerminal covers the failure table's RecordSink
// row: a refused usage record is a store_error candidate and breaks the
// session, like every other record.
func TestAUsageWriteFailureIsTerminal(t *testing.T) {
	sink := &failingSink{failRecord: "usage"}
	tr := scripted([]fakeResponse{{
		text:  "done",
		usage: &poo.UsageReport{InputTokens: 100, OutputTokens: 20},
	}})
	s := newTestSession(t, poo.Options{
		Transport:    tr,
		Sink:         sink,
		ContextLimit: 32768,
	})
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonStoreError || !s.Broken() {
		t.Fatalf("usage append failed: reason=%s broken=%v; want store_error and broken", res.Reason, s.Broken())
	}
}
