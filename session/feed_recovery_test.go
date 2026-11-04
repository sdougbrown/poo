package session

import (
	"context"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// feedStarted is a feed's attempt that started and did not end.
func feedStarted(feed string, attempt int, at string) *poo.FeedStartedRecord {
	return &poo.FeedStartedRecord{
		RecordMeta: poo.NewRecordMetaFrom("feed.started", poo.FeedOrigin(feed)),
		SessionID:  "s1",
		Feed:       feed,
		Attempt:    attempt,
		StartedAt:  at,
	}
}

// feedEnded is a feed's attempt that ended.
func feedEnded(feed string, attempt int, outcome poo.FeedOutcome, at string) *poo.FeedEndedRecord {
	return &poo.FeedEndedRecord{
		RecordMeta: poo.NewRecordMetaFrom("feed.ended", poo.FeedOrigin(feed)),
		SessionID:  "s1",
		Feed:       feed,
		Attempt:    attempt,
		Outcome:    outcome,
		EndedAt:    at,
	}
}

// TestAnUnfinishedFeedAttemptIsInterrupted covers the recovery's pairing:
// a start with no end is marked interrupted, and a start the log already
// ended is left alone. The recovery never runs a feed again. The records
// it appends come in the documented order: the jobs, then the feeds, then
// the inputs.
func TestAnUnfinishedFeedAttemptIsInterrupted(t *testing.T) {
	dir, store := newTestStore(t)
	ctx := context.Background()

	crash := []poo.Record{
		createdRecord("s1"),
		// The inbox has a closed attempt and an open one.
		feedStarted("inbox", 1, "2026-10-20T00:00:00Z"),
		feedEnded("inbox", 1, poo.FeedCancelled, "2026-10-20T00:00:01Z"),
		feedStarted("inbox", 2, "2026-10-20T00:00:02Z"),
		// The digest's only attempt ended before the process stopped.
		feedStarted("digest", 1, "2026-10-20T00:00:03Z"),
		feedEnded("digest", 1, poo.FeedFailed, "2026-10-20T00:00:04Z"),
		// The job never finished, and the client's input never reached
		// the model.
		jobStarted("j1", "r1", "bash", "r1"),
		inputQueued("in1", poo.Steer),
	}
	writeSession(t, dir, "s1", crash...)

	loaded, err := store.Load(ctx, "s1")
	if err != nil {
		t.Fatalf("loading the session: %v", err)
	}
	appended := loaded.Records[len(crash):]
	names := recordNames(appended)
	want := []string{"job.ended", "feed.ended", "input.dropped"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("the recovery appended %v, want %v", names, want)
	}

	ended := appended[1].(*poo.FeedEndedRecord)
	if ended.Feed != "inbox" || ended.Attempt != 2 {
		t.Errorf("the recovered end names %s attempt %d, want inbox attempt 2", ended.Feed, ended.Attempt)
	}
	if ended.Outcome != poo.FeedInterrupted {
		t.Errorf("the attempt ended %q, want %q", ended.Outcome, poo.FeedInterrupted)
	}
	if ended.Origin != poo.FeedOrigin("inbox") {
		t.Errorf("the recovered end's origin is %q, want %q", ended.Origin, poo.FeedOrigin("inbox"))
	}
	// The attempt the log ended keeps its own end: the recovery adds one
	// record, for the attempt that has none.
	if n := countRecord(loaded.Records, "feed.ended"); n != 3 {
		t.Errorf("the log holds %d feed.ended records, want 3", n)
	}
}

// countRecord returns how many records of a name the log holds.
func countRecord(recs []poo.Record, name string) int {
	n := 0
	for _, rec := range recs {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}
