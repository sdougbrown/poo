package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The feed's input: an in-process caller pushes a tagged message into a
// session, and the session delivers, holds, wakes on and restores it the
// way it does a job's result.

// feedInput finds the input.queued record of a feed's input.
func feedInput(records []poo.Record) *poo.InputQueuedRecord {
	for _, rec := range records {
		queued, ok := rec.(*poo.InputQueuedRecord)
		if ok && queued.Mode == poo.InputModeFeed {
			return queued
		}
	}
	return nil
}

// TestAFeedInputWakesAnIdleSession covers the wake row of the delivery
// table: a feed's input that arrives while the session is idle starts a
// run, and the run is recorded as a feed's wake.
func TestAFeedInputWakesAnIdleSession(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	if err := poo.QueueFeedInput(s, "inbox", "alice", "the message"); err != nil {
		t.Fatalf("queueing the feed's input: %v", err)
	}
	waitFor(t, "the woken run to settle", func() bool {
		return sink.Settled() != nil
	})

	// The run's record says a feed woke it, and names the feed. The jobs
	// stay the job results' column.
	var started *poo.RunStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStartedRecord); ok {
			started = r
		}
	}
	if started == nil {
		t.Fatal("the log holds no run.started record")
	}
	if started.Trigger != poo.TriggerFeed {
		t.Errorf("the run's trigger is %q, want feed", started.Trigger)
	}
	if len(started.Feeds) != 1 || started.Feeds[0] != "inbox" {
		t.Errorf("the run names the feeds %v, want [inbox]", started.Feeds)
	}
	if len(started.Jobs) != 0 {
		t.Errorf("the run names the jobs %v, want none", started.Jobs)
	}

	// The event carries the same trigger and the feed's name.
	eventFound := false
	for _, ev := range s.Events() {
		if ev.Name != poo.EventRunStarted {
			continue
		}
		if ev.Payload["trigger"] != "feed" {
			t.Errorf("the event's trigger is %v, want feed", ev.Payload["trigger"])
		}
		feeds, _ := ev.Payload["feeds"].([]string)
		if len(feeds) != 1 || feeds[0] != "inbox" {
			t.Errorf("the event names the feeds %v, want [inbox]", ev.Payload["feeds"])
		}
		eventFound = true
	}
	if !eventFound {
		t.Error("the session emitted no run.started event")
	}

	// The model read the rendered text, under the feed's origin.
	text, origin, ok := deliveredMessage(sink)
	if !ok {
		t.Fatal("the feed's input did not reach the model's view")
	}
	if origin != poo.FeedOrigin("inbox") {
		t.Errorf("the delivered message's origin is %q, want %q", origin, poo.FeedOrigin("inbox"))
	}
	if want := "[feed inbox · from \"alice\": the message]"; text != want {
		t.Errorf("the delivered message is %q, want %q", text, want)
	}
}

// TestAHeldFeedInputWaitsForTheNextPrompt covers the hold row: the
// session holds the input, says so with input.held, and the next prompt
// delivers it before its own text.
func TestAHeldFeedInputWaitsForTheNextPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s := newTestSession(t, opts)

	if err := poo.QueueFeedInput(s, "inbox", "", "the message"); err != nil {
		t.Fatalf("queueing the feed's input: %v", err)
	}
	waitFor(t, "the input to be held", func() bool {
		return sink.count("input.held") == 1
	})
	if s.Running() {
		t.Fatal("the session woke a run, want the input held")
	}
	var held *poo.InputHeldRecord
	for _, rec := range sink.allRecords() {
		if h, ok := rec.(*poo.InputHeldRecord); ok {
			held = h
		}
	}
	if held == nil {
		t.Fatal("the log holds no input.held record")
	}
	if len(held.Jobs) != 1 || held.Jobs[0] != "feed:inbox" {
		t.Errorf("the held record names %v, want [feed:inbox]", held.Jobs)
	}
	if held.Reason != poo.HeldOnIdle {
		t.Errorf("the held reason is %q, want %q", held.Reason, poo.HeldOnIdle)
	}

	// The next prompt delivers the held input before its own text.
	if _, err := s.Prompt(context.Background(), "and this"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	feedAt, promptAt := -1, -1
	for i, m := range sink.messages() {
		if m.Origin == poo.FeedOrigin("inbox") && feedAt < 0 {
			feedAt = i
			if want := "[feed inbox: the message]"; m.Content[0].Text != want {
				t.Errorf("the delivered message is %q, want %q", m.Content[0].Text, want)
			}
		}
		if m.Origin == poo.OriginUser && promptAt < 0 {
			promptAt = i
		}
	}
	if feedAt < 0 {
		t.Fatal("the held input did not reach the model's view")
	}
	if promptAt < 0 {
		t.Fatal("the prompt's message did not reach the model's view")
	}
	if feedAt > promptAt {
		t.Error("the held input came after the prompt's text")
	}
	// The delivery says which mode it delivered and where the input came
	// from.
	delivered := 0
	for _, rec := range sink.allRecords() {
		d, ok := rec.(*poo.InputDeliveredRecord)
		if !ok || d.Mode != poo.InputModeFeed {
			continue
		}
		delivered++
		if d.Origin != poo.FeedOrigin("inbox") {
			t.Errorf("the delivered record's origin is %q, want %q", d.Origin, poo.FeedOrigin("inbox"))
		}
	}
	if delivered != 1 {
		t.Errorf("the log holds %d feed deliveries, want 1", delivered)
	}
}

// TestTheWakeLimitHoldsAFeedInput covers the wake limit: an input that
// may not start a run is held with the limit's reason.
func TestTheWakeLimitHoldsAFeedInput(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{MaxUnattendedWakes: 1}
	s := newTestSession(t, opts)

	if err := poo.QueueFeedInput(s, "inbox", "", "the first"); err != nil {
		t.Fatalf("queueing the first input: %v", err)
	}
	waitFor(t, "the woken run to settle", func() bool {
		return sink.Settled() != nil
	})
	// The second input is past the limit of one unattended wake.
	if err := poo.QueueFeedInput(s, "inbox", "", "the second"); err != nil {
		t.Fatalf("queueing the second input: %v", err)
	}
	waitFor(t, "the second input to be held", func() bool {
		return sink.count("input.held") == 1
	})
	var held *poo.InputHeldRecord
	for _, rec := range sink.allRecords() {
		if h, ok := rec.(*poo.InputHeldRecord); ok {
			held = h
		}
	}
	if held == nil {
		t.Fatal("the log holds no input.held record")
	}
	if held.Reason != poo.HeldWakeLimit {
		t.Errorf("the held reason is %q, want %q", held.Reason, poo.HeldWakeLimit)
	}
}

// TestAFeedInputDuringARunIsDeliveredAtTheTurnEnd covers the open row of
// the delivery table: an input that arrives while a run is open reaches
// the model at the turn end.
func TestAFeedInputDuringARunIsDeliveredAtTheTurnEnd(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{text: "the answer"},
	})
	tr := &openRunHoldTransport{inner: inner, at2: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	if _, err := s.Prompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.at2
	if err := poo.QueueFeedInput(s, "inbox", "", "the message"); err != nil {
		t.Fatalf("queueing the feed's input: %v", err)
	}
	waitFor(t, "the input to be queued", func() bool {
		return s.QueuedJobResults() == 1
	})
	close(tr.release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	delivered := 0
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDeliveredRecord); ok && d.Mode == poo.InputModeFeed {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("the log holds %d feed deliveries, want 1", delivered)
	}
}

// TestTheRestoreDeliversAnUndeliveredFeedInputOnce covers the restore:
// an accepted feed input that never reached the model is delivered once
// after the restore, with the bytes a live delivery carries.
func TestTheRestoreDeliversAnUndeliveredFeedInputOnce(t *testing.T) {
	// The live delivery is the reference.
	liveTransport := scripted([]fakeResponse{{text: "the answer"}})
	liveSink := &recordingSink{}
	liveOpts := testOptions(t, liveTransport, &echoTool{}, liveSink)
	liveOpts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	live := newTestSession(t, liveOpts)
	if err := poo.QueueFeedInput(live, "inbox", "alice", "the message"); err != nil {
		t.Fatalf("queueing the feed's input: %v", err)
	}
	waitFor(t, "the input to be held", func() bool {
		return liveSink.count("input.held") == 1
	})
	if _, err := live.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the live run: %v", err)
	}
	if _, err := live.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the live run: %v", err)
	}
	liveText, liveOrigin, ok := deliveredMessage(liveSink)
	if !ok {
		t.Fatal("the live session delivered no message")
	}
	queued := feedInput(liveSink.allRecords())
	if queued == nil {
		t.Fatal("the live session wrote no input.queued record")
	}

	// The same records, without the delivery, are restored.
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := restoreOptions(t, tr, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	restored, _, err := poo.Restore(opts, []poo.Record{header("s1"), queued})
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	waitFor(t, "the restored input to be held", func() bool {
		return sink.count("input.held") == 1
	})
	if _, err := restored.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the restored run: %v", err)
	}
	if _, err := restored.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the restored run: %v", err)
	}
	text, origin, ok := deliveredMessage(sink)
	if !ok {
		t.Fatal("the restored session delivered no message")
	}
	if text != liveText {
		t.Errorf("the restored delivery is %q, want the live delivery %q", text, liveText)
	}
	if origin != liveOrigin {
		t.Errorf("the restored delivery's origin is %q, want %q", origin, liveOrigin)
	}
	deliveries := 0
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDeliveredRecord); ok && d.Mode == poo.InputModeFeed {
			deliveries++
		}
	}
	if deliveries != 1 {
		t.Errorf("the restored session delivered the input %d times, want once", deliveries)
	}
	// The record the restore writes names the input it delivered.
	if pending := poo.PendingJobResults(append([]poo.Record{header("s1"), queued}, sink.allRecords()...)); len(pending) != 0 {
		t.Errorf("the log holds %d pending inputs after the delivery, want none", len(pending))
	}
}

// TestTheRestoreDoesNotRedeliverADeliveredFeedInput covers the restore's
// other side: an input that reached the model stays delivered.
func TestTheRestoreDoesNotRedeliverADeliveredFeedInput(t *testing.T) {
	queued := &poo.InputQueuedRecord{
		RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")),
		SessionID:  "s1",
		InputID:    "s1-ji1",
		Mode:       poo.InputModeFeed,
		Text:       "[feed inbox: the message]",
		QueuedAt:   "2026-01-01T00:00:00Z",
	}
	delivered := &poo.InputDeliveredRecord{
		RecordMeta:  poo.NewRecordMetaFrom("input.delivered", poo.FeedOrigin("inbox")),
		SessionID:   "s1",
		RunID:       "run-1",
		InputID:     "s1-ji1",
		Mode:        poo.InputModeFeed,
		DeliveredAt: "2026-01-01T00:00:01Z",
	}
	records := []poo.Record{header("s1"), queued, delivered}
	if pending := poo.PendingJobResults(records); len(pending) != 0 {
		t.Errorf("the records hold %d pending inputs, want none", len(pending))
	}

	// The restored session holds nothing, so its next run carries the
	// prompt only.
	tr := scripted([]fakeResponse{{text: "ok"}})
	sink := &recordingSink{}
	opts := restoreOptions(t, tr, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	restored, info, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	if len(info.Jobs) != 0 {
		t.Errorf("the restore queued %d results, want none", len(info.Jobs))
	}
	if held := restored.HeldJobs(); len(held) != 0 {
		t.Errorf("the restored session holds %v, want nothing", held)
	}
}

// TestTwoPendingFeedInputsFromTheSameFeedSurviveRestore covers the
// restore's order: two undelivered inputs of one feed are delivered in
// the order they were accepted.
func TestTwoPendingFeedInputsFromTheSameFeedSurviveRestore(t *testing.T) {
	first := &poo.InputQueuedRecord{
		RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")),
		SessionID:  "s1",
		InputID:    "s1-ji1",
		Mode:       poo.InputModeFeed,
		Text:       "[feed inbox: the first]",
		QueuedAt:   "2026-01-01T00:00:00Z",
	}
	second := &poo.InputQueuedRecord{
		RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("inbox")),
		SessionID:  "s1",
		InputID:    "s1-ji2",
		Mode:       poo.InputModeFeed,
		Text:       "[feed inbox: the second]",
		QueuedAt:   "2026-01-01T00:00:01Z",
	}
	records := []poo.Record{header("s1"), first, second}
	pending := poo.PendingJobResults(records)
	if len(pending) != 2 {
		t.Fatalf("the records hold %d pending inputs, want 2", len(pending))
	}
	if pending[0].InputID != "s1-ji1" || pending[1].InputID != "s1-ji2" {
		t.Errorf("the pending inputs are %v, want both in order", pending)
	}

	tr := scripted([]fakeResponse{{text: "ok"}})
	sink := &recordingSink{}
	opts := restoreOptions(t, tr, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	restored, _, err := poo.Restore(opts, records)
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	if _, err := restored.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	if _, err := restored.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	var texts []string
	for _, m := range sink.messages() {
		if m.Origin == poo.FeedOrigin("inbox") {
			texts = append(texts, m.Content[0].Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("the model read %d feed messages, want 2", len(texts))
	}
	if texts[0] != "[feed inbox: the first]" || texts[1] != "[feed inbox: the second]" {
		t.Errorf("the model read %v, want the inputs in order", texts)
	}
}

// TestAMixedWakeNamesTheJobAndTheFeed covers the wake's two columns: a
// job's result and a feed's input that join one wake start one run, and
// the run says so with both of them in the order the model reads them.
func TestAMixedWakeNamesTheJobAndTheFeed(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	// The seam: the feed's input joins the job's result in the wake the
	// job's result reserves, so one wake takes both.
	queued := make(chan error, 1)
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		BeforeWake: func() {
			queued <- poo.QueueFeedInput(s, "inbox", "", "the message")
		},
	})
	defer restore()
	s.QueueJobResult(poo.JobResult{
		JobID:   "j1",
		Handle:  "r1",
		Tool:    "job",
		Text:    "the job's output",
		Outcome: poo.OutcomeOK,
	})
	if err := <-queued; err != nil {
		t.Fatalf("queueing the feed's input: %v", err)
	}
	waitFor(t, "the woken run to settle", func() bool {
		return sink.Settled() != nil
	})

	// The job's result decides the trigger, and each column names its
	// own results.
	var started *poo.RunStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStartedRecord); ok {
			started = r
		}
	}
	if started == nil {
		t.Fatal("the log holds no run.started record")
	}
	if started.Trigger != poo.TriggerJob {
		t.Errorf("the run's trigger is %q, want job", started.Trigger)
	}
	if len(started.Jobs) != 1 || started.Jobs[0].JobID != "j1" {
		t.Errorf("the run names the jobs %v, want [j1]", started.Jobs)
	}
	if len(started.Feeds) != 1 || started.Feeds[0] != "inbox" {
		t.Errorf("the run names the feeds %v, want [inbox]", started.Feeds)
	}

	// The model read the job's result before the feed's input.
	jobAt, feedAt := -1, -1
	for i, m := range sink.messages() {
		if m.Origin == poo.OriginJob && jobAt < 0 {
			jobAt = i
		}
		if m.Origin == poo.FeedOrigin("inbox") && feedAt < 0 {
			feedAt = i
			if want := "[feed inbox: the message]"; m.Content[0].Text != want {
				t.Errorf("the delivered message is %q, want %q", m.Content[0].Text, want)
			}
		}
	}
	if jobAt < 0 {
		t.Fatal("the job's result did not reach the model's view")
	}
	if feedAt < 0 {
		t.Fatal("the feed's input did not reach the model's view")
	}
	if jobAt > feedAt {
		t.Error("the feed's input came before the job's result")
	}
}

// TestAClosedSessionRefusesAFeedInput covers the refusal: a session that
// has closed takes no input.
func TestAClosedSessionRefusesAFeedInput(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	err := poo.QueueFeedInput(s, "inbox", "", "the message")
	if !errors.Is(err, poo.ErrSessionClosed) {
		t.Errorf("the closed session returned %v, want ErrSessionClosed", err)
	}
	if queued := feedInput(sink.allRecords()); queued != nil {
		t.Errorf("the closed session recorded the input %q, want no acceptance", queued.InputID)
	}
}

// blockedSink holds one record write until the test releases it.
type blockedSink struct {
	// inner keeps the records.
	inner *recordingSink
	// record is the name of the write that blocks.
	record string
	// at is closed when the blocked write starts.
	at chan struct{}
	// release lets the blocked write finish.
	release chan struct{}
	// once makes only the first write of the name block.
	once sync.Once
}

// Append blocks the named record, and keeps every record the inner sink
// takes.
func (s *blockedSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == s.record {
		s.once.Do(func() {
			close(s.at)
			<-s.release
		})
	}
	return s.inner.Append(ctx, rec)
}

// Flush flushes the inner sink.
func (s *blockedSink) Flush(ctx context.Context) error { return s.inner.Flush(ctx) }

// TestACloseRacingAFeedAcceptanceKeepsTheInput covers the acceptance's
// race with the close: an input whose acceptance record is being written
// when the close begins is held by the close, never lost.
func TestACloseRacingAFeedAcceptanceKeepsTheInput(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	inner := &recordingSink{}
	sink := &blockedSink{inner: inner, record: "input.queued", at: make(chan struct{}), release: make(chan struct{})}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink))

	// The acceptance is in flight, and it holds its write registration.
	accepted := make(chan error, 1)
	go func() {
		accepted <- poo.QueueFeedInput(s, "inbox", "", "the message")
	}()
	<-sink.at

	// The close begins while the acceptance record is being written. The
	// write is released only once the close has set the session closed: an
	// acceptance that finished first would wake a run, and the test would
	// cover the other race.
	closed := make(chan error, 1)
	go func() {
		closed <- s.Close()
	}()
	waitFor(t, "the close to set the session closed", func() bool {
		return errors.Is(s.WaitJobs(context.Background()), poo.ErrSessionClosed)
	})
	close(sink.release)

	if err := <-accepted; err != nil {
		t.Fatalf("the in-flight acceptance returned %v, want it held", err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("closing the session: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the close did not finish")
	}

	// The close held the accepted input, so a restore finds it.
	var held *poo.InputHeldRecord
	for _, rec := range inner.allRecords() {
		if h, ok := rec.(*poo.InputHeldRecord); ok {
			held = h
		}
	}
	if held == nil {
		t.Fatal("the close wrote no input.held record")
	}
	if len(held.Jobs) != 1 || held.Jobs[0] != "feed:inbox" {
		t.Errorf("the held record names %v, want [feed:inbox]", held.Jobs)
	}
	if held.Reason != string(poo.CauseSessionClosed) {
		t.Errorf("the held reason is %q, want %q", held.Reason, poo.CauseSessionClosed)
	}
	if pending := poo.PendingJobResults(append([]poo.Record{header("s1")}, inner.allRecords()...)); len(pending) != 1 {
		t.Errorf("the log holds %d pending inputs after the close, want 1", len(pending))
	}
}

// deliveredMessage returns the text and origin of the last feed message
// the session delivered, and reports whether it found one.
func deliveredMessage(sink *recordingSink) (string, poo.Origin, bool) {
	for _, m := range sink.messages() {
		if !poo.IsFeedOrigin(m.Origin) {
			continue
		}
		if len(m.Content) == 0 {
			continue
		}
		return m.Content[0].Text, m.Origin, true
	}
	return "", "", false
}
