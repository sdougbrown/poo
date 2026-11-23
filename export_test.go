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
// The interval is the one second; a test that waits for a
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
// The buffer is the 1024; a test that fills it shortens it.
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

// QueueFeedInput queues one feed's input for the model.
//
// The option that installs a feed is the supported entry, so a test
// reaches the acceptance through this. The input belongs to no attempt,
// so no attempt bounds it.
func QueueFeedInput(s *Session, feed, from, text string) error {
	return s.queueFeedInput(feedInput{instance: feed, from: from, text: text})
}

// QueueFeedImageInput queues one feed's input that carries an image,
// admitting the image the way a feed's emit does.
func QueueFeedImageInput(s *Session, feed, from, text string, image *FeedImage) error {
	attachments, err := s.feedImageAttachments(feed, image)
	if err != nil {
		return err
	}
	return s.queueFeedInput(feedInput{instance: feed, from: from, text: text, attachments: attachments})
}

// SetFeedRestartDelay replaces how long the session's feeds wait before
// their next attempt, and returns a function that restores the rule.
//
// The delay is per session, so a test never races another session's feed
// supervisor.
func SetFeedRestartDelay(s *Session, fn func(ran, prev time.Duration) time.Duration) func() {
	previous := s.feedDelay
	s.feedDelay = fn
	return func() { s.feedDelay = previous }
}

// RequeueAtFront puts a queued input back at the front of the session's
// job queue, the way an abandoned delivery does. The queue is in
// completion order, so a test reaches the reordered queue the backlog
// cap must read through this.
func RequeueAtFront(s *Session, inputID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.jobQueue {
		if item.result.id != inputID {
			continue
		}
		s.jobQueue = append(s.jobQueue[:i], s.jobQueue[i+1:]...)
		s.jobQueue = append([]jobQueueItem{item}, s.jobQueue...)
		return
	}
}

// SubscribeFeed starts one instance of an optional feed, the way the
// subscribe_feed tool does, and reports the instance and whether this call
// started it.
func SubscribeFeed(s *Session, feed string, scope map[string]string) (string, bool, error) {
	return s.subscribeFeed(feed, scope, nil, false)
}

// SubscribeFeedWithPrompt starts one instance of an optional feed and sets
// the filter prompt the call names, the way the subscribe_feed tool does.
func SubscribeFeedWithPrompt(s *Session, feed string, scope map[string]string, prompt *FeedFilterPrompt) (string, bool, error) {
	hasMinNoProbability := prompt != nil && prompt.MinNoProbability != 0
	return s.subscribeFeed(feed, scope, prompt, hasMinNoProbability)
}
