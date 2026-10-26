package poo_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: the compaction is claimed against the
// seal, an interrupt re-enters the gate, and a refused record closes it.

// TestInterruptDiscardsASuccessfulCompaction covers the rule that an
// interrupted compaction's successful return is discarded.
func TestInterruptDiscardsASuccessfulCompaction(t *testing.T) {
	entered := make(chan struct{})
	var used atomic.Bool
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &fix2Compactor{
		should: func(v poo.View, b poo.Budget) bool { return b.Turns == 1 && !used.Load() },
		compact: func(ctx context.Context, _ poo.View, _ poo.ForkFunc) (poo.Compaction, error) {
			used.Store(true)
			close(entered)
			<-ctx.Done()
			return poo.Compaction{Strategy: "interrupted", End: &poo.EndRun{Handoff: "must be discarded"}}, nil
		},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := s.Send("interrupt", poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != poo.ReasonEndTurn || sink.count("compaction") != 0 {
		t.Fatalf("interrupt applied compaction: reason=%s, compactions=%d, dropped=%v", res.Reason, sink.count("compaction"), res.Detail.UndeliveredInput)
	}
}

// TestInterruptDuringACompactionRechecksTheGate covers the rule that an
// interrupted gate re-enters the full gate, budget check included.
func TestInterruptDuringACompactionRechecksTheGate(t *testing.T) {
	entered := make(chan struct{})
	var used atomic.Bool
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Model.ContextLimit = 10000
	opts.Compactor = &fix2Compactor{
		should: func(v poo.View, b poo.Budget) bool { return b.Turns == 1 && !used.Load() },
		compact: func(ctx context.Context, _ poo.View, _ poo.ForkFunc) (poo.Compaction, error) {
			used.Store(true)
			close(entered)
			<-ctx.Done()
			return poo.Compaction{}, ctx.Err()
		},
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := s.Send(strings.Repeat("x", 100000), poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Requests()) != 1 {
		t.Fatalf("over-budget interrupt opened request: requests=%d, max_tokens=%d, settled=%s", len(tr.Requests()), tr.Requests()[1].MaxTokens, res.Reason)
	}
}

// fix2CompactionSink holds the compaction record until the test releases
// it.
type fix2CompactionSink struct {
	*recordingSink
	entered, release chan struct{}
}

func (s *fix2CompactionSink) Append(ctx context.Context, r poo.Record) error {
	if r.Meta().Record == "compaction" {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return s.recordingSink.Append(ctx, r)
	}
	return s.recordingSink.Append(ctx, r)
}

// TestCompactionWriteRespectsTheSeal covers the rule that a compaction
// is claimed against the seal like a call record.
func TestCompactionWriteRespectsTheSeal(t *testing.T) {
	inner := &recordingSink{}
	sink := &fix2CompactionSink{recordingSink: inner, entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	opts := testOptions(t, tr, &echoTool{result: "output"}, inner)
	opts.Sink = sink
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return fix2Summary(), nil },
	}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	// The cancel waits for the compaction's claim, so the test releases
	// the store while the cancel is under way.
	cancelled := make(chan struct{})
	go func() {
		s.Cancel("fix2")
		close(cancelled)
	}()
	close(sink.release)
	<-cancelled
	seenSettle := false
	for _, r := range sink.allRecords() {
		if r.Meta().Record == "run.settled" {
			seenSettle = true
		}
		if seenSettle && r.Meta().Record == "compaction" {
			t.Fatal("record order: run.settled then compaction (sink honored its uncancelled context)")
		}
	}
}

// fix2FailCompactionSink refuses the compaction record.
type fix2FailCompactionSink struct{ recordingSink }

func (s *fix2FailCompactionSink) Append(ctx context.Context, r poo.Record) error {
	if r.Meta().Record == "compaction" {
		return errors.New("injected compaction write failure")
	}
	return s.recordingSink.Append(ctx, r)
}

// TestCompactionStoreErrorPreventsRequest covers the rule that a failed
// compaction record closes the gate.
func TestCompactionStoreErrorPreventsRequest(t *testing.T) {
	tr := scripted([]fakeResponse{fix2Turn(), {text: "done"}})
	sink := &fix2FailCompactionSink{}
	opts := testOptions(t, tr, &echoTool{result: "output"}, sink)
	opts.Compactor = &compactionCompactor{
		should:     afterTurns(1),
		compaction: func(poo.View) (poo.Compaction, error) { return fix2Summary(), nil },
	}
	s := newTestSession(t, opts)
	res := runSession(t, s, "start")
	if len(tr.Requests()) != 1 {
		t.Fatalf("pending store_error did not close gate: requests=%d settled=%s", len(tr.Requests()), res.Reason)
	}
}
