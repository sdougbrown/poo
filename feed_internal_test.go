package poo

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestTheFeedBackoffDoublesUpToACap covers the restart rule: the wait
// doubles from one second, stops at a minute, and an attempt that ran a
// minute resets it.
func TestTheFeedBackoffDoublesUpToACap(t *testing.T) {
	for name, tc := range map[string]struct {
		ran      time.Duration
		previous time.Duration
		want     time.Duration
	}{
		"first attempt":        {ran: 0, previous: 0, want: time.Second},
		"no previous wait":     {ran: 0, previous: -time.Second, want: time.Second},
		"doubles":              {ran: 0, previous: time.Second, want: 2 * time.Second},
		"caps":                 {ran: 0, previous: 40 * time.Second, want: feedMaxRestartDelay},
		"stays at the cap":     {ran: 0, previous: feedMaxRestartDelay, want: feedMaxRestartDelay},
		"a long attempt reset": {ran: feedLongAttempt, previous: feedMaxRestartDelay, want: feedFirstRestartDelay},
		"past the reset":       {ran: 2 * feedLongAttempt, previous: 20 * time.Second, want: feedFirstRestartDelay},
	} {
		if got := feedRestartDelay(tc.ran, tc.previous); got != tc.want {
			t.Errorf("%s: the wait is %s, want %s", name, got, tc.want)
		}
	}
}

// refusedDeliverySink accepts every record but an input's delivery.
type refusedDeliverySink struct{}

// Append refuses the delivery's record.
func (refusedDeliverySink) Append(_ context.Context, rec Record) error {
	if rec.Meta().Record == "input.delivered" {
		return errors.New("the store refused the delivery")
	}
	return nil
}

// Flush does nothing.
func (refusedDeliverySink) Flush(context.Context) error { return nil }

// silentTransport is a transport a session's feed never calls.
type silentTransport struct{}

// Stream fails the request it is never given.
func (silentTransport) Stream(context.Context, ChatRequest) (StreamReader, Rendered, error) {
	return nil, Rendered{}, errors.New("no transport")
}

// Fork fails the fork it is never given.
func (silentTransport) Fork(context.Context, Rendered, []Message, int) (StreamReader, error) {
	return nil, errors.New("no transport")
}

// TestARefusedDeliveryWriteBreaksTheFeeds covers the delivery path's
// break: a store that refuses an input's delivery record breaks the
// session there too, so the feeds stop with it. The break is taken
// through the delivery's own write, so no later record write can break
// the session for it.
func TestARefusedDeliveryWriteBreaksTheFeeds(t *testing.T) {
	started := make(chan struct{})
	s, err := New(Options{
		SessionID:    "s1",
		Model:        ModelInfo{ModelID: "test-model", ContextLimit: 4096},
		Transport:    silentTransport{},
		Sink:         refusedDeliverySink{},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}, WithFeeds(FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, _ FeedIO) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	<-started

	// The run's delivery is the store's to refuse, and the session breaks
	// at that write.
	run := &runState{id: "r1"}
	if err := s.writeDelivery(context.Background(), run, &InputDeliveredRecord{
		RecordMeta: NewRecordMeta("input.delivered"),
		SessionID:  "s1",
		InputID:    "s1-ji1",
		Mode:       InputModeFeed,
	}); err == nil {
		t.Fatal("the refused delivery returned no error")
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}

	// The feed stops with the session that broke under it.
	s.mu.Lock()
	f := s.feeds["inbox"]
	s.mu.Unlock()
	if f == nil {
		t.Fatal("the session holds no inbox feed")
	}
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the feed's supervisor is still running after the store refused a delivery")
	}
	for _, job := range s.Jobs() {
		if job.Tool == FeedToolName && job.Status != JobHeld {
			t.Errorf("the session lists the broken feed as %v, want no attempt of it", job)
		}
	}
}
