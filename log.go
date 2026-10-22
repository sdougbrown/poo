package poo

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// discardLogger drops every record. It is what a session without a
// logger uses, so the loop never checks for nil at a log site.
var discardLogger = slog.New(slog.DiscardHandler)

// logQueueSize is how many diagnostics may wait for the handler.
//
// The queue is bounded, and a full queue drops the record instead of
// blocking the core. The count of dropped records is kept, so a consumer
// can tell that its handler could not keep up.
const logQueueSize = 256

// logDispatcher hands the diagnostics to a handler on its own goroutine.
//
// A slog call runs the handler synchronously, and a handler that blocks
// would hold the loop, the finalization or Close. The dispatcher keeps
// every core path non-blocking: Handle only enqueues, and the handler
// runs off the control path. The queue is bounded, and a record that
// does not fit is counted as dropped.
type logDispatcher struct {
	// inner is the consumer's handler.
	inner slog.Handler
	// queue holds the records waiting for the handler.
	queue chan logItem
	// dropped counts the records the queue could not take.
	dropped atomic.Int64
	// stopCh asks the worker to drain and end.
	stopCh chan struct{}
	// once guards stop.
	once sync.Once
}

// logItem is one queued diagnostic.
type logItem struct {
	rec    slog.Record
	attrs  []slog.Attr
	groups []string
}

// newLogDispatcher starts a dispatcher around a handler.
func newLogDispatcher(inner slog.Handler) *logDispatcher {
	d := &logDispatcher{
		inner:  inner,
		queue:  make(chan logItem, logQueueSize),
		stopCh: make(chan struct{}),
	}
	go d.run()
	return d
}

// run hands the queued records to the handler.
//
// A handler that blocks holds only this goroutine. The stop signal drains
// what is already queued and ends; Close never waits for it, so a
// handler that never returns cannot hold the session's teardown up.
func (d *logDispatcher) run() {
	for {
		select {
		case item := <-d.queue:
			d.write(item)
		case <-d.stopCh:
			for {
				select {
				case item := <-d.queue:
					d.write(item)
				default:
					return
				}
			}
		}
	}
}

// write runs the handler for one queued record.
func (d *logDispatcher) write(item logItem) {
	h := d.inner
	for _, name := range item.groups {
		h = h.WithGroup(name)
	}
	if len(item.attrs) > 0 {
		h = h.WithAttrs(item.attrs)
	}
	_ = h.Handle(context.Background(), item.rec)
}

// stop asks the dispatcher to drain and end.
//
// It does not wait: a consumer's handler is not on any core path, and a
// handler that never returns must not hold Close up.
func (d *logDispatcher) stop() {
	d.once.Do(func() { close(d.stopCh) })
}

// droppedCount returns how many records the queue could not take.
func (d *logDispatcher) droppedCount() int64 { return d.dropped.Load() }

// handler returns the slog.Handler the session's logger uses.
func (d *logDispatcher) handler() slog.Handler { return &logHandler{d: d} }

// logHandler enqueues records instead of calling the consumer's handler.
type logHandler struct {
	// d is the dispatcher.
	d *logDispatcher
	// attrs are the attributes attached to this logger.
	attrs []slog.Attr
	// groups are the groups opened on this logger.
	groups []string
}

// Enabled takes every level: the consumer's handler decides what to
// write, off the core's path.
func (h *logHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle enqueues one record.
func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	item := logItem{
		rec:    r.Clone(),
		attrs:  h.attrs,
		groups: h.groups,
	}
	select {
	case h.d.queue <- item:
	default:
		h.d.dropped.Add(1)
	}
	return nil
}

// WithAttrs returns a handler with the attributes attached.
func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &logHandler{d: h.d, attrs: next, groups: h.groups}
}

// WithGroup returns a handler with the group opened.
func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := append(append([]string(nil), h.groups...), name)
	return &logHandler{d: h.d, attrs: h.attrs, groups: next}
}

// logger returns the session's logger, or the no-op logger.
func (s *Session) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return discardLogger
}

// logWarn writes one diagnostic with the session's id attached.
//
// The call is outside the session lock and its result is ignored: a
// logger never changes a record, an event or an outcome. The dispatch is
// non-blocking, so a consumer's slow handler never holds the loop up.
func (s *Session) logWarn(msg string, attrs ...slog.Attr) {
	s.logger().LogAttrs(context.Background(), slog.LevelWarn, msg,
		append([]slog.Attr{slog.String("session_id", s.opts.SessionID)}, attrs...)...)
}

// logLateCandidate records a candidate that arrived after the commit
// point. The candidate's detail text is not logged: it may carry a tool
// result or a hook's text.
func (s *Session) logLateCandidate(runID string, c candidate) {
	s.logWarn("finalize candidate after commit point",
		slog.String("run_id", runID),
		slog.String("reason", string(c.reason)),
		slog.String("source", c.detail.Source))
}

// logLateToolResult records a tool result that arrived after the commit
// point. The result's text is not logged.
func (s *Session) logLateToolResult(runID, handle, tool string, turn int) {
	s.logWarn("tool result after commit point",
		slog.String("run_id", runID),
		slog.String("handle", handle),
		slog.String("tool", tool),
		slog.Int("turn", turn))
}

// logLateHookDecision records a hook decision that arrived after the
// commit point. The decision's text is not logged.
func (s *Session) logLateHookDecision(runID, handle, tool string, turn int) {
	s.logWarn("hook decision after commit point",
		slog.String("run_id", runID),
		slog.String("handle", handle),
		slog.String("tool", tool),
		slog.Int("turn", turn))
}

// logLateProviderResponse records a provider response that arrived after
// the commit point. It is one line per response, never one per delta.
func (s *Session) logLateProviderResponse(runID string, turn int) {
	s.logWarn("provider response after commit point",
		slog.String("run_id", runID),
		slog.Int("turn", turn))
}

// logLateHookPoint records a start, turn or settle hook decision that
// arrived after its activity ended. The decision's text is not logged.
func (s *Session) logLateHookPoint(runID, point, hook string, turn int) {
	s.logWarn("hook decision after the activity ended",
		slog.String("run_id", runID),
		slog.String("point", point),
		slog.String("hook", hook),
		slog.Int("turn", turn))
}
