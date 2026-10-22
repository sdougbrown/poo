package poo

import (
	"context"
	"log/slog"
)

// discardLogger drops every record. It is what a session without a
// logger uses, so the loop never checks for nil at a log site.
var discardLogger = slog.New(slog.DiscardHandler)

// logger returns the session's logger, or the no-op logger.
func (s *Session) logger() *slog.Logger {
	if s.opts.Logger != nil {
		return s.opts.Logger
	}
	return discardLogger
}

// logWarn writes one diagnostic with the session's id attached.
//
// The call is outside the session lock and its result is ignored: a
// logger never changes a record, an event or an outcome.
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
