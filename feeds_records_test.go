package poo_test

import (
	"encoding/json"
	neturl "net/url"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

// A subscription and an unsubscribe are on the log, with the instance,
// the scope and the prompt, so a durable session knows which instances to
// take on again.

// feedSubscribedRecords returns the feed.subscribed records a session
// wrote, in order.
func feedSubscribedRecords(sink *recordingSink) []*poo.FeedSubscribedRecord {
	var out []*poo.FeedSubscribedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.FeedSubscribedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// feedUnsubscribedRecords returns the feed.unsubscribed records a session
// wrote, in order.
func feedUnsubscribedRecords(sink *recordingSink) []*poo.FeedUnsubscribedRecord {
	var out []*poo.FeedUnsubscribedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.FeedUnsubscribedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// subscribeWithPromptCall is a subscribe_feed call that names the scope
// and the filter prompt in one call.
func subscribeWithPromptCall(callID, feed, target, question string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{
		"feed":   feed,
		"scope":  map[string]string{"url": target},
		"prompt": question,
	})
	return poo.ToolCall{CallID: callID, Name: poo.SubscribeFeedToolName, Arguments: raw}
}

// TestASubscriptionIsRecordedWithItsInstanceScopeAndPrompt covers the
// record a subscription writes: the feed's name, the instance's label, the
// canonical scope and the question the instance screens its records with.
func TestASubscriptionIsRecordedWithItsInstanceScopeAndPrompt(t *testing.T) {
	const target = "https://example.test/repo"
	const question = "Is this about the change?"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeWithPromptCall("c1", "git-feedback", target, question)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink),
		poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)),
		poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")),
	)
	runSession(t, s, "subscribe to the feed")
	waitForSink(t, sink, "the subscription's record", func() bool {
		return sink.count("feed.subscribed") == 1
	})

	recs := feedSubscribedRecords(sink)
	if len(recs) != 1 {
		t.Fatalf("the log holds %d feed.subscribed records, want 1", len(recs))
	}
	rec := recs[0]
	if rec.Feed != "git-feedback" {
		t.Errorf("the record names the feed %q, want git-feedback", rec.Feed)
	}
	// The instance the record names is the one the feed's records and its
	// inputs carry, so a reader joins the two.
	label, ok := poo.FeedInstance(rec.Meta().Origin)
	if !ok {
		t.Fatalf("the record's origin %q names no feed instance", rec.Meta().Origin)
	}
	if rec.Instance != label {
		t.Errorf("the record names the instance %q, and its origin names %q", rec.Instance, label)
	}
	values, err := neturl.ParseQuery(rec.Scope)
	if err != nil {
		t.Fatalf("the record's scope %q is not a query string: %v", rec.Scope, err)
	}
	if values.Get("url") != target {
		t.Errorf("the record's scope carries the url %q, want %q", values.Get("url"), target)
	}
	if rec.Prompt != question {
		t.Errorf("the record carries the prompt %q, want %q", rec.Prompt, question)
	}
}

// TestAnUnsubscribeIsRecorded covers the other side: cancelling an
// instance writes the record that says the session let it go.
func TestAnUnsubscribeIsRecorded(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink),
		poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")),
	)
	runSession(t, s, "subscribe to the feed")
	// The instance's first attempt runs on its own goroutine, so the test
	// waits for it: a cancel that beats the attempt leaves no attempt
	// record to end.
	waitRecords(t, sink, "feed.subscribed", 1)
	waitRecords(t, sink, "feed.started", 1)
	subscribed := feedSubscribedRecords(sink)[0]

	// The instance's origin is the id the job list names it by, and that
	// is what cancel_job takes.
	if err := s.CancelJob(string(poo.FeedOrigin(subscribed.Instance))); err != nil {
		t.Fatalf("cancelling the feed: %v", err)
	}
	waitForSink(t, sink, "the unsubscribe's record", func() bool {
		return sink.count("feed.unsubscribed") == 1 && sink.count("feed.ended") >= 1
	})

	recs := feedUnsubscribedRecords(sink)
	if len(recs) != 1 {
		t.Fatalf("the log holds %d feed.unsubscribed records, want 1", len(recs))
	}
	rec := recs[0]
	if rec.Feed != subscribed.Feed {
		t.Errorf("the unsubscribe names the feed %q, want %q", rec.Feed, subscribed.Feed)
	}
	if rec.Instance != subscribed.Instance {
		t.Errorf("the unsubscribe names the instance %q, want %q", rec.Instance, subscribed.Instance)
	}
	// The unsubscribe precedes the attempt's end, so a reader of the log
	// sees the session let the instance go before it stopped.
	unsubscribed, ended := -1, -1
	for i, name := range sink.Names() {
		switch name {
		case "feed.unsubscribed":
			unsubscribed = i
		case "feed.ended":
			ended = i
		}
	}
	if unsubscribed < 0 || !(unsubscribed < ended) {
		t.Errorf("the records are %v, want feed.unsubscribed before feed.ended", sink.Names())
	}
}
