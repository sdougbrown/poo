package poo

import "time"

// SetRecordWriteBudget changes how long one record write may take on
// one session.
//
// The budget is per session, so a test never races another session's
// run goroutine.
func SetRecordWriteBudget(s *Session, d time.Duration) {
	s.recordWriteBudget = d
}

// SetObserverDropInterval changes how often the overflow records are
// written, and returns a function that restores the previous interval.
//
// The interval is the plan's one second; a test that waits for a
// coalesced record shortens it.
func SetObserverDropInterval(d time.Duration) func() {
	prev := observerDropInterval
	observerDropInterval = d
	return func() { observerDropInterval = prev }
}

// SetObserverBuffer changes how many events an observer may fall
// behind before the bus drops them for that observer, and returns a
// function that restores the previous buffer.
//
// The buffer is the plan's 1024; a test that fills it shortens it.
func SetObserverBuffer(n int) func() {
	prev := observerBuffer
	observerBuffer = n
	return func() { observerBuffer = prev }
}

// LogDropped returns how many diagnostics the session's non-blocking
// logger dropped because its queue was full.
func LogDropped(s *Session) int64 {
	if s.logs == nil {
		return 0
	}
	return s.logs.droppedCount()
}

// SessionView returns the model's history as a read-only view.
func SessionView(s *Session) View { return s.view() }

// Messages returns the model's history, in the order the model reads
// it.
func Messages(s *Session) []Message { return s.view().Messages() }

// TurnSpanMessages returns the messages one session turn's span covers.
//
// The second result reports whether the session has a span for the turn.
func TurnSpanMessages(s *Session, turn int) (msgs []Message, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	span, ok := s.spanForTurnLocked(turn)
	if !ok {
		return nil, false
	}
	return append([]Message(nil), s.history[span.start:span.end]...), true
}

// SessionProfile returns the profile the session was built with.
func SessionProfile(s *Session) string { return s.opts.Profile }

// SessionChatModel returns the model the session talks to.
func SessionChatModel(s *Session) ModelInfo { return s.opts.Model }
