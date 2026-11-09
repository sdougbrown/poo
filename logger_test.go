package poo_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// capturedLog is one record a capture handler took.
type capturedLog struct {
	msg   string
	attrs map[string]any
}

// logCapture collects the records of a capture handler.
type logCapture struct {
	mu      sync.Mutex
	records []capturedLog
}

// captureHandler is a slog.Handler that keeps every record.
type captureHandler struct {
	capture *logCapture
	attrs   []slog.Attr
}

// newCaptureLogger returns a logger and the records it will take.
func newCaptureLogger() (*slog.Logger, *logCapture) {
	capture := &logCapture{}
	return slog.New(&captureHandler{capture: capture}), capture
}

// Enabled takes every level.
func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps the record and the attributes it carries.
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.capture.mu.Lock()
	h.capture.records = append(h.capture.records, capturedLog{msg: r.Message, attrs: attrs})
	h.capture.mu.Unlock()
	return nil
}

// WithAttrs returns a handler with the attributes attached.
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &captureHandler{capture: h.capture, attrs: next}
}

// WithGroup returns the same handler: the core does not use groups.
func (h *captureHandler) WithGroup(string) slog.Handler { return h }

// find returns the records with the given message.
func (c *logCapture) find(msg string) []capturedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedLog
	for _, r := range c.records {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

// failingOnceObserver fails its first event and takes the rest.
type failingOnceObserver struct {
	mu     sync.Mutex
	failed bool
}

// Observe fails once.
func (o *failingOnceObserver) Observe(poo.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failed {
		return nil
	}
	o.failed = true
	return errors.New("the observer is broken")
}

// HookName names the observer.
func (*failingOnceObserver) HookName() string { return "flaky" }

// TestAnObserverErrorIsLoggedOnce covers the observer row: an
// observer's error is logged with the session, the observer and the
// event it failed on, and the run carries on.
func TestAnObserverErrorIsLoggedOnce(t *testing.T) {
	logger, capture := newCaptureLogger()
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Observers = []poo.Observer{&failingOnceObserver{}}
	opts.Logger = logger
	s := newTestSession(t, opts)

	if res := runSession(t, s, "start"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	waitFor(t, "the observer error", func() bool {
		return len(capture.find("observer error")) == 1
	})
	got := capture.find("observer error")
	if len(got) != 1 {
		t.Fatalf("the logger holds %d observer errors, want 1", len(got))
	}
	if got[0].attrs["session_id"] != "s1" {
		t.Errorf("the observer error names session %v, want s1", got[0].attrs["session_id"])
	}
	if got[0].attrs["observer"] != "flaky" {
		t.Errorf("the observer error names observer %v, want flaky", got[0].attrs["observer"])
	}
	if _, ok := got[0].attrs["event"]; !ok {
		t.Error("the observer error names no event")
	}
	if err, ok := got[0].attrs["error"].(error); !ok || err.Error() != "the observer is broken" {
		t.Errorf("the observer error carries %v, want the observer's error", got[0].attrs["error"])
	}
}

// lateCandidateSink holds the settle record after the commit point, so
// the test can send a candidate that arrives while the run has
// committed but has not settled.
type lateCandidateSink struct {
	*recordingSink
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Append holds the run.settled record until the test releases it.
func (s *lateCandidateSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == "run.settled" {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestACandidateAfterTheCommitIsLogged covers the rule that a
// candidate arriving after the commit point is logged and ignored.
func TestACandidateAfterTheCommitIsLogged(t *testing.T) {
	logger, capture := newCaptureLogger()
	sink := &lateCandidateSink{
		recordingSink: &recordingSink{},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Logger = logger
	s := newTestSession(t, opts)

	runID, err := s.Prompt(context.Background(), "start")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-sink.entered
	// The commit point has passed, and the settle record is still being
	// written. The candidate is late.
	s.Cancel("late:cancel")
	close(sink.release)
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the late candidate changed the run to %q, want end_turn", res.Reason)
	}
	waitFor(t, "the late candidate", func() bool {
		return len(capture.find("finalize candidate after commit point")) == 1
	})
	got := capture.find("finalize candidate after commit point")
	if len(got) != 1 {
		t.Fatalf("the logger holds %d late candidates, want 1", len(got))
	}
	if got[0].attrs["session_id"] != "s1" || got[0].attrs["run_id"] != runID {
		t.Errorf("the late candidate names %v/%v, want s1/%s",
			got[0].attrs["session_id"], got[0].attrs["run_id"], runID)
	}
	if got[0].attrs["reason"] != string(poo.ReasonCancelled) {
		t.Errorf("the late candidate says reason %v, want cancelled", got[0].attrs["reason"])
	}
	if got[0].attrs["source"] != "late:cancel" {
		t.Errorf("the late candidate says source %v, want late:cancel", got[0].attrs["source"])
	}
}

// TestALateToolResultIsLogged covers the rule that a run-owned
// completion arriving after the commit point is dropped and logged.
func TestALateToolResultIsLogged(t *testing.T) {
	logger, capture := newCaptureLogger()
	release := make(chan struct{})
	started := make(chan struct{})
	tool := &echoTool{result: "echoed"}
	tool.call = func(context.Context) (poo.ToolResult, error) {
		close(started)
		<-release
		return poo.ToolResult{Text: "the late result"}, nil
	}
	tr := scripted([]fakeResponse{
		{
			calls: []poo.ToolCall{{
				CallID:    "call_1",
				Name:      "echo",
				Arguments: []byte(`{"text":"hello"}`),
			}},
		},
	})
	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.Logger = logger
	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)

	runID, err := s.Prompt(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-started
	s.Cancel("cancel:test")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	close(release)
	waitFor(t, "the late tool result", func() bool {
		return len(capture.find("tool result after commit point")) == 1
	})
	got := capture.find("tool result after commit point")
	if len(got) != 1 {
		t.Fatalf("the logger holds %d late results, want 1", len(got))
	}
	if got[0].attrs["session_id"] != "s1" || got[0].attrs["run_id"] != runID {
		t.Errorf("the late result names %v/%v, want s1/%s",
			got[0].attrs["session_id"], got[0].attrs["run_id"], runID)
	}
	if got[0].attrs["tool"] != "echo" {
		t.Errorf("the late result names tool %v, want echo", got[0].attrs["tool"])
	}
	if handle, _ := got[0].attrs["handle"].(string); handle == "" {
		t.Error("the late result names no handle")
	}
	if _, ok := got[0].attrs["turn"]; !ok {
		t.Error("the late result names no turn")
	}
}

// TestANilLoggerIsSafe covers the option's nil case: a session without a
// logger runs the same, and a late completion does not log or fail.
func TestANilLoggerIsSafe(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Logger = nil
	s := newTestSession(t, opts)
	if res := runSession(t, s, "start"); res.Reason != poo.ReasonEndTurn {
		t.Fatalf("the run settled %q, want end_turn", res.Reason)
	}
	// The cleanup closes the session, which also logs through the nil
	// logger.
}

// lateAfterTurnHook blocks until it is released or its activity ends.
type lateAfterTurnHook struct {
	mu      sync.Mutex
	runs    int
	entered chan struct{}
}

func (h *lateAfterTurnHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.mu.Lock()
	h.runs++
	first := h.runs == 1
	h.mu.Unlock()
	if first {
		close(h.entered)
		<-ctx.Done()
		return poo.TurnDecision{Append: []poo.Message{poo.NewTextMessage(poo.RoleUser, "late")}}, nil
	}
	return poo.TurnDecision{}, nil
}

// TestALateAfterTurnDecisionIsLogged covers the rule that a hook
// decision that arrives after its activity ended is logged.
func TestALateAfterTurnDecisionIsLogged(t *testing.T) {
	logger, capture := newCaptureLogger()
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Logger = logger
	h := &lateAfterTurnHook{entered: make(chan struct{})}
	opts.AfterTurn = []poo.AfterTurnHook{h}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-h.entered
	if _, err := s.Send("stop", poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the late hook decision", func() bool {
		return len(capture.find("hook decision after the activity ended")) == 1
	})
	got := capture.find("hook decision after the activity ended")
	if len(got) != 1 {
		t.Fatalf("the logger holds %d late hook decisions, want 1", len(got))
	}
	if got[0].attrs["point"] != "after_turn" {
		t.Errorf("the late decision names point %v, want after_turn", got[0].attrs["point"])
	}
}

// lateResponseTransport serves one response that never finishes.
type lateResponseTransport struct {
	poo.ChatTransport
	started chan struct{}
}

func (tr *lateResponseTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return &heldToolCallReader{ctx: ctx, started: tr.started}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// TestALateProviderResponseIsLoggedOnce covers the rule: a late
// provider response is dropped and logged once, never per delta.
func TestALateProviderResponseIsLoggedOnce(t *testing.T) {
	logger, capture := newCaptureLogger()
	tr := &lateResponseTransport{
		ChatTransport: scripted([]fakeResponse{{text: "done"}}),
		started:       make(chan struct{}),
	}
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Logger = logger
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-tr.started
	s.Cancel("late:response")
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the late provider response", func() bool {
		return len(capture.find("provider response after commit point")) == 1
	})
	if got := len(capture.find("provider response after commit point")); got != 1 {
		t.Fatalf("the logger holds %d late provider responses, want 1", got)
	}
}

// blockingLogHandler blocks in Handle until it is released.
type blockingLogHandler struct {
	entered, release chan struct{}
	once             sync.Once
}

func (h *blockingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockingLogHandler) Handle(context.Context, slog.Record) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}
func (h *blockingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *blockingLogHandler) WithGroup(string) slog.Handler      { return h }

// TestLoggerDoesNotBlockClose covers the rule that a blocking slog
// handler never holds a core control path.
func TestLoggerDoesNotBlockClose(t *testing.T) {
	h := &blockingLogHandler{entered: make(chan struct{}), release: make(chan struct{})}
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
