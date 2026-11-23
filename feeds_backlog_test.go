package poo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// A feed's backlog cap: with on_idle hold and no run to take them, a
// chatty feed's undelivered inputs are capped per feed. Past the cap,
// the oldest undelivered input is dropped with an input.dropped record,
// and the next input names the drops.

// blockingFeedRunner is a feed's runner that runs until the session
// closes.
func blockingFeedRunner(ctx context.Context, io poo.FeedIO) error {
	<-ctx.Done()
	return ctx.Err()
}

// backlogFeedSpec declares a feed with a backlog cap.
func backlogFeedSpec(name string, maxPending int) poo.FeedSpec {
	return poo.FeedSpec{Name: name, Run: blockingFeedRunner, MaxPending: maxPending}
}

// TestAFeedBacklogDropsItsOldestInput covers the cap: a feed whose
// undelivered inputs are at the cap drops its oldest input when the
// next one is accepted, and the accepted input names the drop.
func TestAFeedBacklogDropsItsOldestInput(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.Feeds = []poo.FeedSpec{backlogFeedSpec("inbox", 2)}
	s := newTestSession(t, opts)

	for _, text := range []string{"one", "two", "three"} {
		if err := poo.QueueFeedInput(s, "inbox", "", text); err != nil {
			t.Fatalf("queueing %q: %v", text, err)
		}
	}
	waitFor(t, "the oldest input to be dropped", func() bool {
		return sink.count("input.dropped") == 1
	})

	// The dropped input is the oldest one, and the drop names the
	// backlog as the reason.
	var dropped *poo.InputDroppedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.InputDroppedRecord); ok {
			dropped = r
		}
	}
	if dropped == nil {
		t.Fatal("the log holds no input.dropped record")
	}
	if dropped.Reason != poo.ReasonBacklog {
		t.Errorf("the drop's reason is %q, want %q", dropped.Reason, poo.ReasonBacklog)
	}

	// The queued texts: the input that made room names the drop, and
	// the one before it does not.
	// The queued texts: the input that made room names the drop, and
	// the one before it does not. The dropped input is the oldest one.
	type queuedInput struct {
		id   string
		text string
	}
	var inputs []queuedInput
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.InputQueuedRecord)
		if !ok || r.Mode != poo.InputModeFeed {
			continue
		}
		inputs = append(inputs, queuedInput{id: r.InputID, text: r.Text})
	}
	if len(inputs) != 3 {
		t.Fatalf("the log holds %d queued feed inputs, want 3", len(inputs))
	}
	if strings.Contains(inputs[1].text, "dropped") {
		t.Errorf("the input before the drop names it: %q", inputs[1].text)
	}
	want := `<feed name="inbox">three</feed>` + "\n" + "[poo: 1 earlier input from this feed was dropped unread]"
	if inputs[2].text != want {
		t.Errorf("the input that made room is %q, want %q", inputs[2].text, want)
	}
	if dropped.InputID != inputs[0].id {
		t.Errorf("the drop names input %q, want the oldest one %q", dropped.InputID, inputs[0].id)
	}

	// A restore reads the drop as the input's fate: the two undelivered
	// inputs are pending, and the dropped one is not.
	pending := poo.PendingJobResults(sink.allRecords())
	if len(pending) != 2 {
		t.Fatalf("the log holds %d pending inputs, want 2", len(pending))
	}
	for _, p := range pending {
		if strings.Contains(p.Text, ">one<") {
			t.Errorf("the dropped input is still pending: %q", p.Text)
		}
	}

	// The next prompt reads the held inputs, and the note rides the
	// delivered message.
	if _, err := s.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	_, origin, ok := deliveredMessage(sink)
	if !ok {
		t.Fatal("the feed's input did not reach the model's view")
	}
	if origin != poo.FeedOrigin("inbox") {
		t.Errorf("the delivered message's origin is %q, want %q", origin, poo.FeedOrigin("inbox"))
	}
	// The input that made room carries the note to the model; the one
	// before it does not.
	noted := false
	for _, m := range sink.messages() {
		if m.Origin != poo.FeedOrigin("inbox") || len(m.Content) == 0 {
			continue
		}
		got := m.Content[0].Text
		switch {
		case strings.Contains(got, "three") && strings.Contains(got, "dropped unread"):
			noted = true
		case strings.Contains(got, "two") && strings.Contains(got, "dropped"):
			t.Errorf("the message before the drop names it: %q", got)
		}
	}
	if !noted {
		t.Error("the model read no message that names the drop")
	}
}

// TestAFeedWithoutASpecTakesTheDefaultBacklog covers the default: a
// feed that sets no cap holds DefaultFeedBacklog undelivered inputs,
// and the next one drops the oldest.
func TestAFeedWithoutASpecTakesTheDefaultBacklog(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s := newTestSession(t, opts)

	for i := 0; i < poo.DefaultFeedBacklog+1; i++ {
		if err := poo.QueueFeedInput(s, "inbox", "", "the message"); err != nil {
			t.Fatalf("queueing the input: %v", err)
		}
	}
	waitFor(t, "the oldest input to be dropped", func() bool {
		return sink.count("input.dropped") == 1
	})
	if pending := poo.PendingJobResults(sink.allRecords()); len(pending) != poo.DefaultFeedBacklog {
		t.Errorf("the log holds %d pending inputs, want %d", len(pending), poo.DefaultFeedBacklog)
	}
}
