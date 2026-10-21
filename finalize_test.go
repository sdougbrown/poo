package poo_test

import (
	"context"
	"errors"
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
	// appends counts every append, including the failed one.
	appends int
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
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return errors.New("the store refused the record")
	}
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()
	return nil
}

// Flush does nothing.
func (s *failingSink) Flush(ctx context.Context) error { return nil }

// TestAHookErrorSettlesTheRunOnce covers the hook row of the failure
// table: a hook that fails settles the run, and the run settles once.
func TestAHookErrorSettlesTheRunOnce(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	}}
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
	tr := &fakeTransport{responses: []fakeResponse{{
		err: &poo.ProviderError{Kind: poo.ProviderKindServer, Message: "the endpoint is down"},
	}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
		{text: "the second answer"},
	}}
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

	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{{text: "the answer"}}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	}}
	tool := &echoTool{result: "echoed"}
	tool.call = func(ctx context.Context) (poo.ToolResult, error) {
		// The call never returns: the run settles underneath it.
		select {}
	}
	sink := &recordingSink{}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	// The run is cancelled, which abandons the call.
	time.Sleep(50 * time.Millisecond)
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{text: "the first answer"},
		{text: "the second answer"},
	}}
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
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	}}
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
