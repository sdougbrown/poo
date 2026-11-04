package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The feed's lifecycle: the session runs the feeds it was built with,
// lists them, stops them, restarts them after a failure and ends every
// attempt exactly once.

// feedEnd returns the nth feed.ended record a log holds, counting from
// one, or nil when the log holds fewer.
func feedEnd(records []poo.Record, n int) *poo.FeedEndedRecord {
	var ends []*poo.FeedEndedRecord
	for _, rec := range records {
		if ended, ok := rec.(*poo.FeedEndedRecord); ok {
			ends = append(ends, ended)
		}
	}
	if len(ends) < n {
		return nil
	}
	return ends[n-1]
}

// TestAFeedDeliversItsInputs covers the run of a feed: the session starts
// it, the runner emits an input, and the input wakes a run and reaches
// the model.
func TestAFeedDeliversItsInputs(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			if err := io.Emit("alice", "the message"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))

	waitFor(t, "the woken run to settle", func() bool {
		return sink.Settled() != nil
	})
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the log holds %d feed.started records, want 1", n)
	}
	if started := sink.count("feed.ended"); started != 0 {
		t.Errorf("the log holds %d feed.ended records for a feed that is still running", started)
	}

	// The run the feed woke says so, and names the feed.
	var run *poo.RunStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStartedRecord); ok {
			run = r
		}
	}
	if run == nil {
		t.Fatal("the log holds no run.started record")
	}
	if run.Trigger != poo.TriggerFeed {
		t.Errorf("the run's trigger is %q, want feed", run.Trigger)
	}
	if len(run.Feeds) != 1 || run.Feeds[0] != "inbox" {
		t.Errorf("the run names the feeds %v, want [inbox]", run.Feeds)
	}

	// The model read the input under the feed's origin.
	text, origin, ok := deliveredMessage(sink)
	if !ok {
		t.Fatal("the feed's input did not reach the model's view")
	}
	if want := "[feed inbox · from \"alice\": the message]"; text != want {
		t.Errorf("the delivered message is %q, want %q", text, want)
	}
	if origin != poo.FeedOrigin("inbox") {
		t.Errorf("the delivered message's origin is %q, want %q", origin, poo.FeedOrigin("inbox"))
	}
}

// TestARunningFeedIsListedWithTheJobs covers the listing: a feed that is
// running is a job-shaped entry, under its origin and the feed tool, and
// the jobs tool names it.
func TestARunningFeedIsListedWithTheJobs(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_list", Name: "jobs", Arguments: json.RawMessage(`{}`)}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(
		poo.FeedSpec{
			Name: "inbox",
			Run: func(ctx context.Context, io poo.FeedIO) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
	))
	waitFor(t, "the feed to start", func() bool {
		return sink.count("feed.started") == 1
	})
	jobs := s.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("the session lists %d jobs, want 1: %v", len(jobs), jobs)
	}
	got := jobs[0]
	if got.JobID != "feed:inbox" {
		t.Errorf("the listed id is %q, want feed:inbox", got.JobID)
	}
	if got.Tool != poo.FeedToolName {
		t.Errorf("the listed tool is %q, want %q", got.Tool, poo.FeedToolName)
	}
	if got.Status != poo.JobRunning {
		t.Errorf("the listed status is %q, want %q", got.Status, poo.JobRunning)
	}
	if got.StartedAt == "" {
		t.Error("the listed feed has no start time")
	}

	// The model reads the same list through the jobs tool.
	runSession(t, s, "list the jobs")
	var listing *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == poo.JobsToolName {
			listing = r
		}
	}
	if listing == nil {
		t.Fatal("the jobs tool never ran")
	}
	if !strings.Contains(listing.Text, "feed:inbox") {
		t.Errorf("the jobs tool does not name the feed: %q", listing.Text)
	}
}

// TestCancelJobStopsAFeed covers the stop: cancel_job ends the attempt as
// cancelled, and a feed that asked to be restarted is not.
func TestCancelJobStopsAFeed(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	waitFor(t, "the feed to start", func() bool {
		return sink.count("feed.started") == 1
	})
	if err := s.CancelJob("feed:inbox"); err != nil {
		t.Fatalf("cancelling the feed: %v", err)
	}
	waitFor(t, "the feed to end", func() bool {
		return sink.count("feed.ended") == 1
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedCancelled {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedCancelled)
	}
	if ended.RestartInMS != 0 {
		t.Errorf("the cancelled attempt schedules a restart in %dms, want none", ended.RestartInMS)
	}
	// The end record is written before the attempt's supervisor returns,
	// so no attempt can follow it.
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	if jobs := s.Jobs(); len(jobs) != 0 {
		t.Errorf("the session still lists the cancelled feed: %v", jobs)
	}
	// The feed is stopped, so a second cancel of it names no job, and
	// neither does one that names a feed the session never had.
	if err := s.CancelJob("feed:inbox"); !errors.Is(err, poo.ErrUnknownJob) {
		t.Errorf("cancelling the stopped feed returned %v, want ErrUnknownJob", err)
	}
	if err := s.CancelJob("feed:nobody"); !errors.Is(err, poo.ErrUnknownJob) {
		t.Errorf("cancelling an unknown feed returned %v, want ErrUnknownJob", err)
	}
}

// TestAFailingFeedEndsOnceAndDoesNotRestart covers a runner that returns
// an error without asking to be restarted: the attempt fails, the log
// says why, and no second attempt follows.
func TestAFailingFeedEndsOnceAndDoesNotRestart(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			return errors.New("the feed broke")
		},
	}))
	waitFor(t, "the feed to end", func() bool {
		return sink.count("feed.ended") == 1
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedFailed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if !strings.Contains(ended.Error, "the feed broke") {
		t.Errorf("the attempt's error is %q, want the runner's", ended.Error)
	}
	if ended.RestartInMS != 0 {
		t.Errorf("the failed attempt schedules a restart in %dms, want none", ended.RestartInMS)
	}
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	if jobs := s.Jobs(); len(jobs) != 0 {
		t.Errorf("the session still lists the failed feed: %v", jobs)
	}
}

// TestARestartingFeedWaitsForTheBackoff covers the restart setting: after
// a failed attempt the feed starts again once the backoff has passed, and
// the end record names the wait.
func TestARestartingFeedWaitsForTheBackoff(t *testing.T) {
	defer poo.SetFeedRestartDelay(func(ran, prev time.Duration) time.Duration {
		return time.Millisecond
	})()
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	var attempts atomic.Int32
	newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			if attempts.Add(1) == 1 {
				return errors.New("the first attempt failed")
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	waitFor(t, "the second attempt", func() bool {
		return sink.count("feed.started") == 2
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended == nil {
		t.Fatal("the log holds no feed.ended record for the first attempt")
	}
	if ended.Outcome != poo.FeedFailed {
		t.Errorf("the first attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if ended.RestartInMS == 0 {
		t.Error("the first attempt's end does not name the wait before the restart")
	}
}

// TestACancelDuringTheBackoffStopsTheFeed covers the case the restart
// asks about: a cancel while the feed waits for its next attempt ends the
// feed with no new start.
func TestACancelDuringTheBackoffStopsTheFeed(t *testing.T) {
	defer poo.SetFeedRestartDelay(func(ran, prev time.Duration) time.Duration {
		return time.Hour
	})()
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			return errors.New("the feed broke")
		},
	}))
	waitFor(t, "the attempt to end", func() bool {
		return sink.count("feed.ended") == 1
	})
	if jobs := s.Jobs(); len(jobs) != 1 || jobs[0].Status != poo.JobRestarting {
		t.Fatalf("the waiting feed is listed as %v, want one restarting entry", jobs)
	}
	if err := s.CancelJob("feed:inbox"); err != nil {
		t.Fatalf("cancelling the feed: %v", err)
	}
	waitFor(t, "the feed to stop", func() bool {
		return len(s.Jobs()) == 0
	})
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	if n := sink.count("feed.ended"); n != 1 {
		t.Errorf("the log holds %d feed.ended records, want 1", n)
	}
}

// TestTheCloseEndsAFeedOnce covers the close: a running attempt ends as
// session_closed, exactly once, even though the runner returns as the
// close cancels it.
func TestTheCloseEndsAFeedOnce(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	waitFor(t, "the feed to start", func() bool {
		return sink.count("feed.started") == 1
	})
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if n := sink.count("feed.ended"); n != 1 {
		t.Fatalf("the close wrote %d feed.ended records, want 1", n)
	}
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedSessionClosed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedSessionClosed)
	}
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
}

// TestAPanickingFeedEndsFailed covers a runner that panics: the attempt
// fails, and the process stays up.
func TestAPanickingFeedEndsFailed(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			panic("the feed exploded")
		},
	}))
	waitFor(t, "the feed to end", func() bool {
		return sink.count("feed.ended") == 1
	})
	ended := feedEnd(sink.allRecords(), 1)
	if ended.Outcome != poo.FeedFailed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if !strings.Contains(ended.Error, "the feed exploded") {
		t.Errorf("the attempt's error is %q, want the panic's text", ended.Error)
	}
}

// TestAFeedsHoldOverridesTheSessionsWake covers the per-feed idle policy:
// a feed that holds its input keeps a session that wakes idle results
// from starting a run.
func TestAFeedsHoldOverridesTheSessionsWake(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleWake}
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name:   "inbox",
		OnIdle: poo.OnIdleHold,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			if err := io.Emit("", "the message"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	waitFor(t, "the input to be held", func() bool {
		return sink.count("input.held") == 1
	})
	if s.Running() {
		t.Fatal("the session woke a run, want the feed's input held")
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
}

// TestAFeedsWakeOverridesTheSessionsHold covers the other direction: a
// feed that wakes its input starts a run in a session that holds its idle
// results.
func TestAFeedsWakeOverridesTheSessionsHold(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name:   "inbox",
		OnIdle: poo.OnIdleWake,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			if err := io.Emit("", "the message"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	waitFor(t, "the woken run to settle", func() bool {
		return sink.Settled() != nil
	})
	if n := sink.count("input.held"); n != 0 {
		t.Errorf("the log holds %d input.held records, want none", n)
	}
	var run *poo.RunStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStartedRecord); ok {
			run = r
		}
	}
	if run == nil || run.Trigger != poo.TriggerFeed {
		t.Fatalf("the run's trigger is %v, want feed", run)
	}
}

// TestAFeedOnlySessionOffersTheJobTools covers the registration: a
// session with no tool of its own still offers jobs and cancel_job when
// it runs a feed, because the feed is listed and stopped through them.
func TestAFeedOnlySessionOffersTheJobTools(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	// The session has no tools of its own, so the job tools are the only
	// ones it can offer.
	opts.Tools = nil
	opts.ToolOrder = nil
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	runSession(t, s, "do the thing")
	reqs := tr.Requests()
	if len(reqs) == 0 {
		t.Fatal("the transport got no request")
	}
	names := map[string]bool{}
	for _, spec := range reqs[0].Tools {
		names[spec.Name] = true
	}
	if !names[poo.JobsToolName] {
		t.Error("the model was not offered the jobs tool")
	}
	if !names[poo.CancelJobToolName] {
		t.Error("the model was not offered the cancel_job tool")
	}
}

// TestTheRestoreContinuesTheFeedAttempts covers the attempt counter: a
// restored session's first attempt of a feed continues the numbers the
// records hold, so the same log never names one attempt twice. A feed the
// records never name starts at its first attempt.
func TestTheRestoreContinuesTheFeedAttempts(t *testing.T) {
	started := func(feed string, attempt int, at string) *poo.FeedStartedRecord {
		return &poo.FeedStartedRecord{
			RecordMeta: poo.NewRecordMetaFrom("feed.started", poo.FeedOrigin(feed)),
			SessionID:  "s1",
			Feed:       feed,
			Attempt:    attempt,
			StartedAt:  at,
		}
	}
	ended := func(feed string, attempt int, at string) *poo.FeedEndedRecord {
		return &poo.FeedEndedRecord{
			RecordMeta: poo.NewRecordMetaFrom("feed.ended", poo.FeedOrigin(feed)),
			SessionID:  "s1",
			Feed:       feed,
			Attempt:    attempt,
			Outcome:    poo.FeedFailed,
			EndedAt:    at,
		}
	}
	records := []poo.Record{
		header("s1"),
		// The inbox ran two attempts before the process stopped, and the
		// digest one.
		started("inbox", 1, "2026-01-01T00:00:00Z"),
		ended("inbox", 1, "2026-01-01T00:00:01Z"),
		started("inbox", 2, "2026-01-01T00:00:02Z"),
		ended("inbox", 2, "2026-01-01T00:00:03Z"),
		started("digest", 1, "2026-01-01T00:00:04Z"),
		ended("digest", 1, "2026-01-01T00:00:05Z"),
	}
	hold := func(ctx context.Context, io poo.FeedIO) error {
		<-ctx.Done()
		return ctx.Err()
	}
	sink := &recordingSink{}
	opts := restoreOptions(t, scripted(nil), sink)
	restored, _, err := poo.Restore(opts, records, poo.WithFeeds(
		poo.FeedSpec{Name: "inbox", Run: hold},
		// The audit is a feed the records never name.
		poo.FeedSpec{Name: "audit", Run: hold},
	))
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	waitFor(t, "both feeds to start", func() bool {
		return sink.count("feed.started") == 2
	})
	attempts := map[string]int{}
	for _, rec := range sink.allRecords() {
		if s, ok := rec.(*poo.FeedStartedRecord); ok {
			attempts[s.Feed] = s.Attempt
		}
	}
	if got := attempts["inbox"]; got != 3 {
		t.Errorf("the restored inbox started attempt %d, want 3", got)
	}
	if got := attempts["audit"]; got != 1 {
		t.Errorf("the restored audit started attempt %d, want 1", got)
	}
}

// TestTheRestoreStartsFeedsAfterThePendingInputs covers the order the
// restore keeps: the inputs it queues are on the queue before any feed
// runs, so a feed's first input joins them.
func TestTheRestoreStartsFeedsAfterThePendingInputs(t *testing.T) {
	pending := &poo.InputQueuedRecord{
		RecordMeta: poo.NewRecordMetaFrom("input.queued", poo.FeedOrigin("digest")),
		SessionID:  "s1",
		InputID:    "s1-ji1",
		Mode:       poo.InputModeFeed,
		Text:       "[feed digest: the first]",
		QueuedAt:   "2026-01-01T00:00:00Z",
	}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := restoreOptions(t, tr, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	restored, _, err := poo.Restore(opts, []poo.Record{header("s1"), pending}, poo.WithFeeds(poo.FeedSpec{
		Name:   "inbox",
		OnIdle: poo.OnIdleHold,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			if err := io.Emit("", "the second"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	if err != nil {
		t.Fatalf("restoring the session: %v", err)
	}
	t.Cleanup(func() { restored.Close() })
	waitFor(t, "both inputs to be held", func() bool {
		return sink.count("input.held") == 2
	})
	var held []*poo.InputHeldRecord
	for _, rec := range sink.allRecords() {
		if h, ok := rec.(*poo.InputHeldRecord); ok {
			held = append(held, h)
		}
	}
	if len(held) != 2 {
		t.Fatalf("the log holds %d input.held records, want 2", len(held))
	}
	// The restore's own hold pass names the restored input alone: the
	// feed had not run yet.
	if want := "feed:digest"; len(held[0].Jobs) != 1 || held[0].Jobs[0] != want {
		t.Errorf("the restore held %v, want [%s]", held[0].Jobs, want)
	}
	// The feed's first input joined it.
	if len(held[1].Jobs) != 2 || held[1].Jobs[0] != "feed:digest" || held[1].Jobs[1] != "feed:inbox" {
		t.Errorf("the feed's hold names %v, want [feed:digest feed:inbox]", held[1].Jobs)
	}
}

// TestTheCloseIsBoundedWithAStuckFeed covers the close's bound: the
// teardown is one deadline for the whole wait, so a runner that ignores
// its context never holds the close up, and its attempt is recorded as
// session_closed exactly once.
func TestTheCloseIsBoundedWithAStuckFeed(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	gate := make(chan struct{})
	started := make(chan struct{})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			close(started)
			<-gate
			return nil
		},
	}))
	<-started
	poo.SetRecordWriteBudget(s, 50*time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		close(gate)
		if err != nil {
			t.Errorf("closing: %v", err)
		}
	case <-time.After(6 * time.Second):
		close(gate)
		<-done
		t.Fatal("Close still blocked after 6s with a stuck feed; the teardown bound must cover the wait")
	}
	if n := sink.count("feed.ended"); n != 1 {
		t.Fatalf("the log holds %d feed.ended records, want 1", n)
	}
	if ended := feedEnd(sink.allRecords(), 1); ended.Outcome != poo.FeedSessionClosed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedSessionClosed)
	}
}

// TestARefusedFeedInputBreaksTheFeed covers the store break: a store that
// refuses a feed's input stops the feed, because a feed feeds the store
// the session writes to, and the feed is never restarted.
func TestARefusedFeedInputBreaksTheFeed(t *testing.T) {
	// The wait is short, so a restart would show up at once.
	defer poo.SetFeedRestartDelay(func(ran, prev time.Duration) time.Duration {
		return time.Millisecond
	})()
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &failingSink{failRecord: "input.queued", failAfter: 1}
	// The feed emits twice: the first input is accepted, and the second
	// one's acceptance is the record the store refuses.
	type refused struct {
		err     error
		ctxLive bool
	}
	reported := make(chan refused, 1)
	var attempts atomic.Int32
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		OnIdle:  poo.OnIdleHold,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			attempts.Add(1)
			if err := io.Emit("", "the first"); err != nil {
				reported <- refused{err: err, ctxLive: ctx.Err() == nil}
				return err
			}
			err := io.Emit("", "the second")
			// The session breaks the feed with the refused write, so the
			// runner's context has ended by the time the emit returns.
			reported <- refused{err: err, ctxLive: ctx.Err() == nil}
			return err
		},
	}))
	got := <-reported
	if got.err == nil {
		t.Fatal("the emit whose acceptance the store refused returned no error")
	}
	if errors.Is(got.err, poo.ErrSessionClosed) {
		t.Errorf("the refused emit returned %v, want the store's error", got.err)
	}
	if got.ctxLive {
		t.Error("the runner's context is still live after the store refused its input")
	}
	if !s.Broken() {
		t.Error("the session is not broken")
	}
	// A feed the store broke is not restarted: a restart would start its
	// next attempt within the wait the test's seam gives it.
	time.Sleep(100 * time.Millisecond)
	if n := attempts.Load(); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the log holds %d feed.started records, want 1", n)
	}
	// The broken store cannot record the feed's end, so the feed's own
	// status is how the session says the attempt is over.
	for _, job := range s.Jobs() {
		if job.Tool == poo.FeedToolName && job.Status != poo.JobHeld {
			t.Errorf("the session lists the broken feed as %v, want no attempt of it", job)
		}
	}
}

// TestAnEmitAfterTheAttemptEndedIsRefused covers the attempt's bound on
// its own emits: a runner that emits after its attempt ended is refused,
// so a delivery never joins a feed the session has ended.
func TestAnEmitAfterTheAttemptEndedIsRefused(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	emits := make(chan func(from, text string) error, 1)
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			emits <- io.Emit
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	emit := <-emits
	if err := s.CancelJob("feed:inbox"); err != nil {
		t.Fatalf("cancelling the feed: %v", err)
	}
	waitFor(t, "the attempt to end", func() bool {
		return sink.count("feed.ended") == 1
	})
	err := emit("alice", "the late message")
	if !errors.Is(err, poo.ErrSessionClosed) {
		t.Errorf("the late emit returned %v, want ErrSessionClosed", err)
	}
	if n := sink.count("input.queued"); n != 0 {
		t.Errorf("the log holds %d input.queued records, want none", n)
	}
	if n := sink.count("input.held"); n != 0 {
		t.Errorf("the log holds %d input.held records, want none", n)
	}
}

// TestTheCloseDuringTheRestartWait covers the close's other entry: a feed
// that waits for its next attempt is stopped by the close, and the wait
// that never became an attempt is not recorded as one.
func TestTheCloseDuringTheRestartWait(t *testing.T) {
	defer poo.SetFeedRestartDelay(func(ran, prev time.Duration) time.Duration {
		return time.Hour
	})()
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name:    "inbox",
		Restart: true,
		Run: func(ctx context.Context, io poo.FeedIO) error {
			return errors.New("the feed broke")
		},
	}))
	waitFor(t, "the feed to wait for its next attempt", func() bool {
		return sink.count("feed.ended") == 1
	})
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}
	if n := sink.count("feed.ended"); n != 1 {
		t.Errorf("the log holds %d feed.ended records, want 1", n)
	}
	if ended := feedEnd(sink.allRecords(), 1); ended.Outcome != poo.FeedFailed {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedFailed)
	}
	if n := sink.count("feed.started"); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	if jobs := s.Jobs(); len(jobs) != 0 {
		t.Errorf("the closed session still lists the feed: %v", jobs)
	}
}

// TestAFeedSpecIsValidatedOnConstruction covers the construction's check:
// a feed the session cannot run fails the build, and the error names the
// feed.
func TestAFeedSpecIsValidatedOnConstruction(t *testing.T) {
	run := func(ctx context.Context, io poo.FeedIO) error { return nil }
	for name, tc := range map[string]struct {
		feeds []poo.FeedSpec
		want  string
	}{
		"bad name": {
			feeds: []poo.FeedSpec{{Name: "In Box", Run: run}},
			want:  "In Box",
		},
		"duplicate name": {
			feeds: []poo.FeedSpec{{Name: "inbox", Run: run}, {Name: "inbox", Run: run}},
			want:  "inbox",
		},
		"no runner": {
			feeds: []poo.FeedSpec{{Name: "inbox"}},
			want:  "inbox",
		},
		"unknown idle policy": {
			feeds: []poo.FeedSpec{{Name: "inbox", Run: run, OnIdle: poo.OnIdle("sometimes")}},
			want:  "inbox",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			sink := &recordingSink{}
			_, err := poo.New(testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(tc.feeds...))
			if err == nil {
				t.Fatal("the session was built with an invalid feed")
			}
			var init *poo.ErrSessionInit
			if !asSessionInit(err, &init) {
				t.Fatalf("the error is %T, want an ErrSessionInit", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error %q does not name the feed %q", err, tc.want)
			}
		})
	}
}
