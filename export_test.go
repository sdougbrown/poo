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
