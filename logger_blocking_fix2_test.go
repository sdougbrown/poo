package poo_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// The fix-pass-2 reproducers: the logger never blocks a core path.

// fix2BlockingHandler blocks in Handle until it is released.
type fix2BlockingHandler struct {
	entered, release chan struct{}
	once             sync.Once
}

func (h *fix2BlockingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *fix2BlockingHandler) Handle(context.Context, slog.Record) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}
func (h *fix2BlockingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *fix2BlockingHandler) WithGroup(string) slog.Handler      { return h }

// TestLoggerDoesNotBlockClose covers the rule that a blocking slog
// handler never holds a core control path.
func TestLoggerDoesNotBlockClose(t *testing.T) {
	h := &fix2BlockingHandler{entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &lateCandidateSink{recordingSink: &recordingSink{}, entered: make(chan struct{}), release: make(chan struct{})}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Logger = slog.New(h)
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	<-h.entered
	close(sink.release)
	_, _ = s.Wait(context.Background())
	blocked := false
	select {
	case <-closed:
	case <-time.After(100 * time.Millisecond):
		blocked = true
	}
	close(h.release)
	<-closed
	if blocked {
		t.Fatal("Close blocked on synchronous Logger after run settled (handler released by test)")
	}
}
