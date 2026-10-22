package poo_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// TestACallWhoseRecordFailsStartsNoEffect covers the failure table: a
// tool.call record the store refuses is terminal, so the tool never runs.
func TestACallWhoseRecordFailsStartsNoEffect(t *testing.T) {
	sink := &failingSink{failRecord: "tool.call"}
	tool := &echoTool{result: "effect executed"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if got := len(tool.Calls()); got != 0 {
		t.Fatalf("the tool ran %d time(s) after its tool.call record was refused, want 0", got)
	}
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
}

// blockingRecordSink holds one record until the test releases it.
type blockingRecordSink struct {
	recordingSink
	kind    string
	fail    bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the named record.
func (s *blockingRecordSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == s.kind {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
		if s.fail {
			return errors.New("injected refusal")
		}
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestACancelDuringTheCallRecordStillGetsAResult covers the rule that
// every call gets exactly one result: a call whose tool.call write is in
// flight when the run settles is registered first, so the finalization
// settles it.
func TestACancelDuringTheCallRecordStillGetsAResult(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprint(background), func(t *testing.T) {
			sink := &blockingRecordSink{
				kind:    "tool.call",
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{jobCall("one", background)}},
				{text: "done"},
			})
			s := newTestSession(t, jobOptions(t, tr, tool, sink))
			id, err := s.Prompt(context.Background(), "start")
			if err != nil {
				t.Fatal(err)
			}
			<-sink.entered
			// The record write stays in flight while the finalization
			// starts. It is released shortly after, so the call record can
			// land before the settle only if the finalization waits for
			// the write.
			go func() {
				time.Sleep(50 * time.Millisecond)
				close(sink.release)
			}()
			s.Cancel("probe")
			if _, err := s.WaitRun(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the late turn end", func() bool {
				for _, e := range s.Events() {
					if e.Name == poo.EventTurnEnded {
						return true
					}
				}
				return false
			})
			call, result, settled := -1, -1, -1
			for i, name := range sink.names() {
				switch name {
				case "tool.call":
					call = i
				case "tool.result":
					result = i
				case "run.settled":
					settled = i
				}
			}
			if call < 0 || !(call < result && result < settled) {
				t.Fatalf("records are %v; want tool.call before tool.result before run.settled", sink.names())
			}
			if got := sink.count("tool.result"); got != 1 {
				t.Fatalf("the recorded call has %d result(s) after cancellation, want 1; records: %v", got, sink.names())
			}
			if got := tool.Calls(); got != 0 {
				t.Errorf("the tool ran %d time(s) after the run settled, want 0", got)
			}
		})
	}
}

// TestATruncatedInvalidBackgroundIsSkipped covers the truncation
// completeness rule: a call whose arguments parse but whose reserved
// background argument fails validation did not pass preparation, so in
// a truncated response it is skipped rather than counted as processed.
func TestATruncatedInvalidBackgroundIsSkipped(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tr := scripted([]fakeResponse{
		{
			finish: poo.FinishLength,
			calls:  []poo.ToolCall{{ID: "c", Name: "job", Arguments: []byte(`{"text":"one","background":null}`)}},
		},
		{text: "done"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, &recordingSink{}))
	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonOutputTruncated || res.Turns != 1 {
		t.Fatalf("the truncated call settled %q after %d turn(s), want output_truncated after 1", res.Reason, res.Turns)
	}
	if got := tool.Calls(); got != 0 {
		t.Errorf("the invalid call ran %d time(s), want 0", got)
	}
}

// dispatchBarrier is an echo tool whose Spec blocks once, so a test can
// place a call between the winding-down check and the write of its
// tool.call record.
type dispatchBarrier struct {
	echoTool
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Spec blocks the call the first time it is asked for after the hook
// armed the barrier.
func (b *dispatchBarrier) Spec() poo.ToolSpec {
	if b.armed.Load() {
		b.once.Do(func() {
			close(b.entered)
			<-b.release
		})
	}
	return b.echoTool.Spec()
}

// armDispatch arms the barrier when the call reaches the before-tool
// hooks.
type armDispatch struct{ b *dispatchBarrier }

// BeforeTool arms the barrier and allows the call.
func (h armDispatch) BeforeTool(poo.HookContext, poo.ToolCallInfo) (poo.ToolDecision, error) {
	h.b.armed.Store(true)
	return poo.Allow(), nil
}

// TestALateDispatchWritesNoRecordAfterTheSettle covers the claim rule: a
// call that passes the winding-down check and is then descheduled until
// after the run settles is owned by the finalization, which writes its
// tool.call record and its result before run.settled.
func TestALateDispatchWritesNoRecordAfterTheSettle(t *testing.T) {
	sink := &recordingSink{}
	tool := &dispatchBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{ID: "c", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, &tool.echoTool, sink)
	opts.Tools["echo"] = tool
	opts.BeforeTool = []poo.BeforeToolHook{armDispatch{tool}}
	s := newTestSession(t, opts)
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	<-tool.entered

	s.Cancel("probe")
	if _, err := s.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(tool.release)
	// The turn ends after the dispatch returns, so it is the point at
	// which a record written by the late dispatch would have landed.
	waitFor(t, "the late turn end", func() bool {
		for _, e := range s.Events() {
			if e.Name == poo.EventTurnEnded {
				return true
			}
		}
		return false
	})

	call, result, settled := -1, -1, -1
	names := sink.names()
	for i, name := range names {
		switch name {
		case "tool.call":
			call = i
		case "tool.result":
			result = i
		case "run.settled":
			settled = i
		}
	}
	if call < 0 || !(call < result && result < settled) {
		t.Fatalf("records are %v; want tool.call before tool.result before run.settled", names)
	}
	if got := sink.count("tool.call"); got != 1 {
		t.Errorf("the log holds %d tool.call records, want 1", got)
	}
	if got := sink.count("tool.result"); got != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", got)
	}
	if got := len(tool.Calls()); got != 0 {
		t.Errorf("the tool ran %d time(s) after the run settled, want 0", got)
	}
}

// skippedCallSink holds the second call's tool.call record until the test
// releases it.
type skippedCallSink struct {
	recordingSink
	handle  string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the named call's record.
func (s *skippedCallSink) Append(ctx context.Context, rec poo.Record) error {
	call, ok := rec.(*poo.ToolCallRecord)
	if !ok || call.Handle != s.handle {
		return s.recordingSink.Append(ctx, rec)
	}
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.recordingSink.Append(ctx, rec)
}

// TestSkippedCallRecordsPrecedeTheSettle covers the claim rule for a
// skipped call: a call that never starts is work the finalization owns,
// so its tool.call and tool.result land before run.settled.
//
// The interleaving is pinned: the seal takes the second call before the
// batch's skip path is allowed to claim it, so the finalization is the
// one that writes its records. Without the batch's registration the call
// would escape both claims.
func TestSkippedCallRecordsPrecedeTheSettle(t *testing.T) {
	sink := &skippedCallSink{handle: "r2", entered: make(chan struct{}), release: make(chan struct{})}
	started := make(chan struct{})
	tool := &echoTool{class: poo.ClassSideEffecting, call: func(ctx context.Context) (poo.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return poo.ToolResult{Text: "partial"}, ctx.Err()
	}}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{
		{ID: "a", Name: "echo", Arguments: []byte(`{"text":"one"}`)},
		{ID: "b", Name: "echo", Arguments: []byte(`{"text":"two"}`)},
	}}})
	s := newTestSession(t, testOptions(t, tr, tool, sink))
	sealed := make(chan struct{})
	skipReached := make(chan struct{})
	var once sync.Once
	restore := s.SetSealRaceHooks(&poo.SealRaceHooks{
		AfterSeal: func() { close(sealed) },
		BeforeSkipCalls: func() {
			once.Do(func() { close(skipReached) })
			<-sealed
		},
	})
	defer restore()
	id, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancelled := make(chan struct{})
	go func() {
		s.Cancel("probe")
		close(cancelled)
	}()
	// The batch reaches the skip path after the first call's result, and
	// the seal has taken the second call by then.
	<-skipReached
	<-sealed
	waitFor(t, "the sealed call's tool.call record", func() bool {
		select {
		case <-sink.entered:
			return true
		default:
			return false
		}
	})
	// The run must not settle while a skipped call's record is being
	// written: the record is tracked work.
	if got := sink.count("run.settled"); got != 0 {
		t.Fatalf("the run settled while a skipped call's tool.call record was still being written")
	}
	close(sink.release)
	if _, err := s.WaitRun(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	<-cancelled
	waitFor(t, "the skipped call's result", func() bool { return sink.count("tool.result") == 2 })

	names := sink.names()
	lastResult, settledAt := -1, -1
	for i, name := range names {
		switch name {
		case "tool.result":
			lastResult = i
		case "run.settled":
			settledAt = i
		}
	}
	if lastResult < 0 || lastResult > settledAt {
		t.Fatalf("records are %v; want every tool.result before run.settled", names)
	}
	if got := sink.count("tool.call"); got != 2 {
		t.Errorf("the log holds %d tool.call records, want 2", got)
	}
}
