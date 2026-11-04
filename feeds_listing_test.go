package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The feed's listing: a feed, and the inputs it emitted, are one entry in
// the list the session and the model read.

// TestARunningFeedIsListedOnceWithItsHeldInput covers the list's
// identity: a running feed and the input it emitted are one entry, under
// the feed's origin, because the running feed is the row that says what
// the feed is doing.
func TestARunningFeedIsListedOnceWithItsHeldInput(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Jobs = poo.JobOptions{OnIdle: poo.OnIdleHold}
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
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

	jobs := s.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("the session lists %d jobs, want 1: %v", len(jobs), jobs)
	}
	if jobs[0].JobID != "feed:inbox" || jobs[0].Status != poo.JobRunning {
		t.Errorf("the listed job is %v, want the running feed:inbox", jobs[0])
	}
}

// TestTheJobsToolListsARunningFeedOnce covers the model's view of the
// same list: an input the feed emitted while a run was open is not listed
// a second time beside the feed that emitted it.
func TestTheJobsToolListsARunningFeedOnce(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_echo", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{calls: []poo.ToolCall{{CallID: "call_list", Name: "jobs", Arguments: json.RawMessage(`{}`)}}},
		{text: "the answer"},
	})
	tr := &openRunHoldTransport{inner: inner, at2: make(chan struct{}), release: make(chan struct{})}
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
	if _, err := s.Prompt(context.Background(), "list the jobs"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.at2
	// The run is open, so the input stays on the queue while the model's
	// next turn lists the jobs.
	if err := emit("", "the message"); err != nil {
		t.Fatalf("emitting the feed's input: %v", err)
	}
	waitFor(t, "the input to be queued", func() bool {
		return s.QueuedJobResults() == 1
	})
	close(tr.release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	var listing *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == poo.JobsToolName {
			listing = r
		}
	}
	if listing == nil {
		t.Fatal("the jobs tool never ran")
	}
	if n := strings.Count(listing.Text, "feed:inbox"); n != 1 {
		t.Errorf("the jobs tool names the feed %d times, want once: %q", n, listing.Text)
	}
}

// TestAQueuedResultIsNotListedAsHeld covers the model's list's other
// half: a result that is queued for this turn end is not held, so the
// jobs tool does not list it as waiting for the next run.
func TestAQueuedResultIsNotListedAsHeld(t *testing.T) {
	inner := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "call_echo", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}}},
		{calls: []poo.ToolCall{{CallID: "call_list", Name: "jobs", Arguments: json.RawMessage(`{}`)}}},
		{text: "the answer"},
	})
	tr := &openRunHoldTransport{inner: inner, at2: make(chan struct{}), release: make(chan struct{})}
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(poo.FeedSpec{
		Name: "inbox",
		Run: func(ctx context.Context, io poo.FeedIO) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}))
	if _, err := s.Prompt(context.Background(), "list the jobs"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tr.at2
	s.QueueJobResult(poo.JobResult{
		JobID:   "j9",
		Tool:    "job",
		Text:    "the job's output",
		Outcome: poo.OutcomeOK,
	})
	waitFor(t, "the result to be queued", func() bool {
		return s.QueuedJobResults() == 1
	})
	close(tr.release)
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	var listing *poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == poo.JobsToolName {
			listing = r
		}
	}
	if listing == nil {
		t.Fatal("the jobs tool never ran")
	}
	if strings.Contains(listing.Text, "j9") {
		t.Errorf("the jobs tool lists a result that the turn end delivers: %q", listing.Text)
	}
}
