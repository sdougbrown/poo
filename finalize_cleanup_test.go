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

// slowSettleFlushSink makes the settle record take most of the cleanup
// time and the flush take a short time after it. The flush checks its
// context, so a flush context that the settle write starved fails.
type slowSettleFlushSink struct {
	*recordingSink
	settleMargin time.Duration
	flushDelay   time.Duration
	flushOnce    sync.Once
	mu           sync.Mutex
	flushed      bool
}

// Append holds the settle record until the last part of its own write
// budget. The cleanup context that the old code shared with the flush is
// left with only settleMargin, while the fresh flush budget is full.
func (s *slowSettleFlushSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "run.settled" {
		if deadline, ok := ctx.Deadline(); ok {
			if wait := time.Until(deadline) - s.settleMargin; wait > 0 {
				time.Sleep(wait)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.recordingSink.Append(ctx, rec)
}

// Flush takes a short time and records whether it completed.
func (s *slowSettleFlushSink) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.flushOnce.Do(func() { time.Sleep(s.flushDelay) })
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.flushed = true
	s.mu.Unlock()
	return nil
}

// flushSucceeded reports whether the flush completed.
func (s *slowSettleFlushSink) flushSucceeded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushed
}

// TestASlowSettleDoesNotStarveTheFlush covers the cleanup contexts: the
// settle write has its own record budget, so a short flush after a slow
// settle must get a fresh budget instead of the leftovers of a shared
// cleanup deadline.
func TestASlowSettleDoesNotStarveTheFlush(t *testing.T) {
	sink := &slowSettleFlushSink{
		recordingSink: &recordingSink{},
		settleMargin:  200 * time.Millisecond,
		flushDelay:    300 * time.Millisecond,
	}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	poo.SetRecordWriteBudget(s, 600*time.Millisecond)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q (%s), want end_turn: the flush was starved by the settle write",
			res.Reason, res.Detail.Text)
	}
	if !sink.flushSucceeded() {
		t.Error("the flush did not complete after the slow settle")
	}
	if s.Broken() {
		t.Error("the session broke because the flush lost its budget to the settle write")
	}
}

// blockedFlushSink blocks the first Flush and ignores its context.
type blockedFlushSink struct {
	*recordingSink
	release chan struct{}
	once    sync.Once
}

// Append records immediately.
func (s *blockedFlushSink) Append(ctx context.Context, rec poo.Record) error {
	return s.recordingSink.Append(ctx, rec)
}

// Flush blocks until the test releases it.
func (s *blockedFlushSink) Flush(ctx context.Context) error {
	s.once.Do(func() { <-s.release })
	return nil
}

// TestAFinalizationFlushStallIsAStoreErrorNotACloseStall covers the
// finalization's flush error: a run's flush that stalls is a store
// error, and its text names the flush, not the close.
func TestAFinalizationFlushStallIsAStoreErrorNotACloseStall(t *testing.T) {
	sink := &blockedFlushSink{
		recordingSink: &recordingSink{},
		release:       make(chan struct{}),
	}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Fatalf("the run settled %q, want store_error", res.Reason)
	}
	if !strings.Contains(res.Detail.Text, "flush") {
		t.Errorf("the settle text is %q, want the flush failure", res.Detail.Text)
	}
	if strings.Contains(res.Detail.Text, "close") {
		t.Errorf("the settle text is %q, want no close-stall wording", res.Detail.Text)
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
	close(sink.release)
}

// TestACloseFlushStallIsErrCloseStalled covers the close's flush error:
// the close wraps the stalled flush with ErrCloseStalled, so a caller's
// errors.Is contract is unchanged.
func TestACloseFlushStallIsErrCloseStalled(t *testing.T) {
	sink := &blockedFlushSink{
		recordingSink: &recordingSink{},
		release:       make(chan struct{}),
	}
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, sink))
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if !errors.Is(err, poo.ErrCloseStalled) {
			t.Fatalf("the close returned %v, want ErrCloseStalled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the close did not finish while the flush was blocked")
	}
	close(sink.release)
}
