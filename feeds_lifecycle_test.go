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

// feedEnd returns the nth feed.ended record the sink kept, counting from
// one, or nil when the log holds fewer.
func feedEnd(sink *recordingSink, n int) *poo.FeedEndedRecord {
	var ends []*poo.FeedEndedRecord
	for _, rec := range sink.allRecords() {
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
	ended := feedEnd(sink, 1)
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
	ended := feedEnd(sink, 1)
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
	ended := feedEnd(sink, 1)
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
	ended := feedEnd(sink, 1)
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
	ended := feedEnd(sink, 1)
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
