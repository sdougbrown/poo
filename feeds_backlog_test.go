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
	waitForSink(t, sink, "the oldest input to be dropped", func() bool {
		return sink.count("input.dropped") >= 1
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

// TestAFeedBacklogCapsEachFeedOnItsOwn covers the per-feed origin: two
// feeds' undelivered inputs sit on one queue, and each feed's cap
// counts and drops only its own. A feed at its cap does not drop
// another feed's input, and another feed's inputs do not fill its cap.
func TestAFeedBacklogCapsEachFeedOnItsOwn(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.Feeds = []poo.FeedSpec{backlogFeedSpec("inbox", 1), backlogFeedSpec("other", 1)}
	s := newTestSession(t, opts)

	for _, input := range []struct{ feed, text string }{
		{"inbox", "a1"}, {"other", "b1"}, {"inbox", "a2"}, {"other", "b2"},
	} {
		if err := poo.QueueFeedInput(s, input.feed, "", input.text); err != nil {
			t.Fatalf("queueing %s on %s: %v", input.text, input.feed, err)
		}
	}
	waitForSink(t, sink, "both of inbox's first inputs to be dropped", func() bool {
		return sink.count("input.dropped") >= 2
	})

	// Each feed dropped its own oldest input, in its own order.
	var dropped []string
	for _, rec := range sink.allRecords() {
		r, ok := rec.(*poo.InputDroppedRecord)
		if !ok {
			continue
		}
		dropped = append(dropped, r.InputID)
	}
	if len(dropped) != 2 {
		t.Fatalf("the log holds %d input.dropped record(s), want 2", len(dropped))
	}
	pending := poo.PendingJobResults(sink.allRecords())
	if len(pending) != 2 {
		t.Fatalf("the log holds %d pending inputs, want 2", len(pending))
	}
	for _, p := range pending {
		if !strings.Contains(p.Text, "a2") && !strings.Contains(p.Text, "b2") {
			t.Errorf("a dropped or displaced input is still pending: %q", p.Text)
		}
		if p.Origin != poo.FeedOrigin("inbox") && p.Origin != poo.FeedOrigin("other") {
			t.Errorf("the pending input's origin is %q", p.Origin)
		}
	}
	// One pending input per feed: neither feed's cap consumed the
	// other's inputs.
	origins := map[poo.Origin]int{}
	for _, p := range pending {
		origins[p.Origin]++
	}
	if origins[poo.FeedOrigin("inbox")] != 1 || origins[poo.FeedOrigin("other")] != 1 {
		t.Errorf("the pending inputs' origins are %v, want one per feed", origins)
	}
}

// TestADroppedInputStaysFatedWhenTheAcceptanceFails covers the order:
// the drop's record is written before the acceptance, so a store that
// refuses the acceptance still leaves the dropped input fated — a
// restore reads the fate and never re-queues it.
func TestADroppedInputStaysFatedWhenTheAcceptanceFails(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &failingSink{failRecord: "input.queued", failAfter: 2}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.Feeds = []poo.FeedSpec{backlogFeedSpec("inbox", 2)}
	s := newTestSession(t, opts)

	if err := poo.QueueFeedInput(s, "inbox", "", "one"); err != nil {
		t.Fatalf("queueing one: %v", err)
	}
	if err := poo.QueueFeedInput(s, "inbox", "", "two"); err != nil {
		t.Fatalf("queueing two: %v", err)
	}
	if err := poo.QueueFeedInput(s, "inbox", "", "three"); err == nil {
		t.Fatal("the refused acceptance returned no error")
	}
	if got := sink.count("input.dropped"); got != 1 {
		t.Fatalf("the log holds %d input.dropped record(s), want 1", got)
	}

	// The dropped input is fated, so a restore holds only the one
	// undelivered input that was accepted.
	pending := poo.PendingJobResults(sink.records)
	if len(pending) != 1 {
		t.Fatalf("the log holds %d pending inputs, want 1", len(pending))
	}
	if !strings.Contains(pending[0].Text, "two") {
		t.Errorf("the pending input is %q, want the second one", pending[0].Text)
	}
}

// TestTheCapDropsTheOldestWhateverOrderTheQueueHolds covers the drop's
// order: an abandoned delivery goes back at the queue's front, so the
// queue is not in acceptance order, and the cap still drops the input
// the feed accepted first.
func TestTheCapDropsTheOldestWhateverOrderTheQueueHolds(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	opts.Feeds = []poo.FeedSpec{backlogFeedSpec("inbox", 2)}
	s := newTestSession(t, opts)

	for _, text := range []string{"A", "B"} {
		if err := poo.QueueFeedInput(s, "inbox", "", text); err != nil {
			t.Fatalf("queueing %s: %v", text, err)
		}
	}
	waitForSink(t, sink, "both inputs to be held", func() bool { return sink.count("input.held") >= 2 })

	// The queue reads [A, B] by acceptance order. An abandoned delivery
	// puts its item back at the front: the queue then reads [B, A].
	var queuedIDs []string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.InputQueuedRecord); ok {
			queuedIDs = append(queuedIDs, r.InputID)
		}
	}
	if len(queuedIDs) != 2 {
		t.Fatalf("the log holds %d queued inputs, want 2", len(queuedIDs))
	}
	poo.RequeueAtFront(s, queuedIDs[1])

	// C arrives at the cap. The drop names the first accepted input,
	// whatever position the queue holds it at.
	if err := poo.QueueFeedInput(s, "inbox", "", "C"); err != nil {
		t.Fatalf("queueing C: %v", err)
	}
	waitForSink(t, sink, "the cap to drop an input", func() bool { return sink.count("input.dropped") >= 1 })

	var droppedID string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.InputDroppedRecord); ok {
			droppedID = r.InputID
		}
	}
	if droppedID == "" {
		t.Fatal("the log holds no input.dropped record")
	}
	if droppedID != queuedIDs[0] {
		t.Errorf("the cap dropped input %q, want the first accepted one %q", droppedID, queuedIDs[0])
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
	waitForSink(t, sink, "the oldest input to be dropped", func() bool {
		return sink.count("input.dropped") >= 1
	})
	if pending := poo.PendingJobResults(sink.allRecords()); len(pending) != poo.DefaultFeedBacklog {
		t.Errorf("the log holds %d pending inputs, want %d", len(pending), poo.DefaultFeedBacklog)
	}
}
