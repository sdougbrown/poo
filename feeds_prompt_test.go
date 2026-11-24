package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/decide"
)

// The filter prompt: the question the model sets on a feed instance, which
// replaces the manifest's question while it is set.

// thePromptedFeed is a feed a subscription starts and a prompt screens.
func thePromptedFeed(run poo.FeedRunner) poo.FeedSpec {
	return poo.FeedSpec{
		Name:        "audit",
		Description: "Events worth watching for a while.",
		Optional:    true,
		Run:         run,
	}
}

// promptAnswer is the decider a session needs before it may take a filter
// prompt. The answer itself is the message's delivery, which these tests do
// not read: the config layer is where a prompt's question reaches a record.
func promptAnswer(context.Context, any, string, []decide.Question) ([]decide.Answer, error) {
	return nil, decide.ErrDeferred
}

// subscribePromptCall is a subscribe_feed call that names a prompt, the
// empty string being the clear.
func subscribePromptCall(callID, feed, question string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{"feed": feed, "prompt": question})
	return poo.ToolCall{CallID: callID, Name: poo.SubscribeFeedToolName, Arguments: raw}
}

// logBuffer is a writer a test can read while the session's log dispatcher
// writes it.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

// Write appends one log line.
func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// String returns what the log holds.
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// TestASubscribeThatOverridesTheManifestsQuestionSaysSo covers the
// visibility the override gets: a call that sets or clears a prompt on a
// feed whose manifest configured a question says in its result that the
// operator's screening policy does or does not apply, and the session's log
// carries the same warning. A feed whose manifest configured no question
// takes the same calls without a notice.
func TestASubscribeThatOverridesTheManifestsQuestionSaysSo(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribePromptCall("c1", "audit", "Only the tests.")}},
		{text: "the answer"},
		{calls: []poo.ToolCall{subscribePromptCall("c2", "audit", "")}},
		{text: "the answer"},
		{calls: []poo.ToolCall{subscribePromptCall("c3", "scan", "Only the tests.")}},
		{text: "the answer"},
		{calls: []poo.ToolCall{subscribePromptCall("c4", "scan", "")}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	logs := &logBuffer{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Logger = slog.New(slog.NewTextHandler(logs, nil))
	audit := thePromptedFeed(waitingFeed)
	audit.FilterQuestion = "Does the operator want this?"
	scan := thePromptedFeed(waitingFeed)
	scan.Name = "scan"
	s := newTestSession(t, opts,
		poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)),
		poo.WithFeeds(audit, scan),
	)

	for _, prompt := range []string{
		"screen the feed for me",
		"stop screening it",
		"screen the other feed",
		"stop screening that one",
	} {
		runSession(t, s, prompt)
	}

	results := toolResults(sink, poo.SubscribeFeedToolName)
	if len(results) != 4 {
		t.Fatalf("the session recorded %d subscribe results, want 4", len(results))
	}
	// The first call replaced the operator's question.
	if !strings.Contains(results[0].Text, "manifest's filter question was replaced") {
		t.Errorf("the setting call says %q, want it to name the manifest's question it replaced", results[0].Text)
	}
	if !strings.Contains(results[0].Text, "operator's screening policy does not apply") {
		t.Errorf("the setting call says %q, want it to say the operator's policy does not apply", results[0].Text)
	}
	// The second restored it.
	if !strings.Contains(results[1].Text, "manifest's filter question is restored") {
		t.Errorf("the clearing call says %q, want it to say the manifest's question is restored", results[1].Text)
	}
	// The unfiltered feed's calls carry no notice either way.
	for _, result := range results[2:] {
		if strings.Contains(result.Text, "manifest's filter question") || strings.Contains(result.Text, "operator's screening policy") {
			t.Errorf("the call on an unfiltered feed says %q, want no override notice", result.Text)
		}
	}
	// The operator's log carries the override, on the filtered feed alone.
	waitFor(t, "the override warning in the log", func() bool {
		return strings.Count(logs.String(), "overrides the feed's manifest filter question") == 2
	})
}

// theFilteredFeed is a feed whose manifest screens its records through its
// own process, so no prompt has a place on it.
func theFilteredFeed(run poo.FeedRunner) poo.FeedSpec {
	spec := thePromptedFeed(run)
	spec.ScriptFilter = true
	return spec
}

// promptRefusal reports whether the error is a refusal the model may
// correct.
func promptRefusal(err error) bool {
	var refusal *poo.ErrInvalidArgs
	return errors.As(err, &refusal)
}

// promptSeen is one reading of an instance's filter prompt.
type promptSeen struct {
	prompt poo.FeedFilterPrompt
	set    bool
}

// reportingFeed reports the instance's filter prompt each time the test
// asks, and waits to be stopped.
//
// The getter is the seam the config layer's filter reads for each record, so
// what this runner sees is what a record would be asked about.
func reportingFeed(ask <-chan struct{}, seen chan<- promptSeen) poo.FeedRunner {
	return func(ctx context.Context, io poo.FeedIO) error {
		for {
			select {
			case <-ask:
				prompt, set := io.FilterPrompt()
				seen <- promptSeen{prompt: prompt, set: set}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// TestTheFilterPromptBelongsToTheInstance covers the state: the prompt a
// call sets is the instance's, a later call replaces it without restarting
// the feed, a re-arm keeps it, and the empty prompt clears it.
func TestTheFilterPromptBelongsToTheInstance(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	ask := make(chan struct{})
	seen := make(chan promptSeen, 1)
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink),
		poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)),
		poo.WithFeeds(thePromptedFeed(reportingFeed(ask, seen))),
	)
	read := func() promptSeen {
		t.Helper()
		ask <- struct{}{}
		return <-seen
	}

	if _, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{Question: "Only the tests."}); err != nil || !started {
		t.Fatalf("the first subscription is %v, %v; want a started instance", started, err)
	}
	if got := read(); !got.set || got.prompt.Question != "Only the tests." {
		t.Errorf("the instance's prompt is %+v, %v; want the one the call set", got.prompt, got.set)
	}

	// A call for the running instance replaces the prompt, and the change
	// is the instance's: the attempt that reads it is the one already
	// running.
	if _, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{Question: "Only the failures.", MinNoProbability: 0.8}); err != nil || started {
		t.Fatalf("the second subscription is %v, %v; want the instance that is already there", started, err)
	}
	if got := read(); !got.set || got.prompt.Question != "Only the failures." || got.prompt.MinNoProbability != 0.8 {
		t.Errorf("the replaced prompt is %+v, %v; want the one the second call set", got.prompt, got.set)
	}
	if starts := feedStarts(sink); len(starts) != 1 {
		t.Errorf("the session started %d attempts, want the one the first call started", len(starts))
	}

	// A re-arm runs the same instance, so the prompt it carries is the
	// one the instance was left with.
	origin := feedStarts(sink)[0].Meta().Origin
	if err := s.CancelJob(string(origin)); err != nil {
		t.Fatalf("stopping the instance: %v", err)
	}
	waitFor(t, "the instance's attempt to end", func() bool { return len(feedEnds(sink)) == 1 })
	if _, started, err := poo.SubscribeFeed(s, "audit", nil); err != nil || !started {
		t.Fatalf("the re-arm is %v, %v; want the stopped instance started again", started, err)
	}
	if got := read(); !got.set || got.prompt.Question != "Only the failures." {
		t.Errorf("the re-armed prompt is %+v, %v; want the prompt the instance kept", got.prompt, got.set)
	}

	// The empty prompt clears it, and the manifest's question is what
	// applies again.
	if _, _, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{}); err != nil {
		t.Fatalf("clearing the prompt: %v", err)
	}
	if got := read(); got.set {
		t.Errorf("the cleared instance still has the prompt %+v", got.prompt)
	}
}

// TestTheFilterPromptIsRefusedWhenItCannotScreen covers the refusals: a
// prompt is put to a decision model on each record, so a session without one
// refuses it, and a feed that screens its own records refuses it too.
func TestTheFilterPromptIsRefusedWhenItCannotScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		spec    poo.FeedSpec
		decider bool
		want    string
	}{
		{
			name: "no decision model",
			spec: thePromptedFeed(waitingFeed),
			want: "no decision model",
		},
		{
			name:    "a feed with a filter of its own",
			spec:    theFilteredFeed(waitingFeed),
			decider: true,
			want:    "runs a filter of its own",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			sink := &recordingSink{}
			opts := testOptions(t, tr, &echoTool{}, sink)
			fns := []poo.Option{poo.WithFeeds(tc.spec)}
			if tc.decider {
				fns = append(fns, poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)))
			}
			s := newTestSession(t, opts, fns...)

			_, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{Question: "Only the tests."})
			if err == nil {
				t.Fatal("the call set a prompt the feed cannot be asked about")
			}
			if started {
				t.Error("the refused call started the feed")
			}
			if !promptRefusal(err) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %q, want an invalid-arguments refusal naming %q", err, tc.want)
			}
			// The refusal is the whole of the call: the feed the model
			// believes it started must not be running.
			if starts := feedStarts(sink); len(starts) != 0 {
				t.Errorf("the session started %d feeds, want none", len(starts))
			}
		})
	}
}

// TestTheFilterPromptHoldsTheCallsOwnValues covers the prompt's own shape:
// a blank question is no clear, a probability without a prompt belongs to
// nothing, and a probability outside the range is refused.
func TestTheFilterPromptHoldsTheCallsOwnValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt poo.FeedFilterPrompt
		want   string
	}{
		{
			name:   "a blank question",
			prompt: poo.FeedFilterPrompt{Question: "   "},
			want:   "blank",
		},
		{
			name:   "a probability with no prompt",
			prompt: poo.FeedFilterPrompt{MinNoProbability: 0.7},
			want:   "belongs to a filter prompt",
		},
		{
			name:   "a probability at the bound",
			prompt: poo.FeedFilterPrompt{Question: "Only the tests.", MinNoProbability: 0.5},
			want:   "greater than 0.5 and at most 1",
		},
		{
			name:   "a probability past the bound",
			prompt: poo.FeedFilterPrompt{Question: "Only the tests.", MinNoProbability: 1.5},
			want:   "greater than 0.5 and at most 1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}),
				poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)),
				poo.WithFeeds(thePromptedFeed(waitingFeed)),
			)
			prompt := tc.prompt
			_, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &prompt)
			if err == nil {
				t.Fatal("the call set a prompt it cannot hold")
			}
			if started {
				t.Error("the refused call started the feed")
			}
			if !promptRefusal(err) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is %q, want an invalid-arguments refusal naming %q", err, tc.want)
			}
		})
	}
}

// TestClearingTheFilterPromptIsNeverRefused covers the clear path's
// ordering: an empty prompt clears the instance's question, and the clear
// is never refused, whatever the feed or the session. A feed that screens
// its own records takes the clear, and a session without a decision model
// takes it too, because the clear restores the manifest's question.
func TestClearingTheFilterPromptIsNeverRefused(t *testing.T) {
	for _, tc := range []struct {
		name    string
		spec    poo.FeedSpec
		decider bool
	}{
		{
			name:    "a feed with a filter of its own",
			spec:    theFilteredFeed(waitingFeed),
			decider: true,
		},
		{
			name: "no decision model",
			spec: thePromptedFeed(waitingFeed),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
			fns := []poo.Option{poo.WithFeeds(tc.spec)}
			if tc.decider {
				fns = append(fns, poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)))
			}
			s := newTestSession(t, opts, fns...)

			// The empty prompt clears the instance's question, and the
			// clear is never refused.
			_, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{})
			if err != nil {
				t.Fatalf("the clear was refused: %v", err)
			}
			if !started {
				t.Error("the clear did not start the feed")
			}
		})
	}
}

// TestTheFilterPromptTakesTheUpperBound covers the range's upper bound:
// a min_no_probability of 1 is the most a filter prompt takes, and it is
// accepted, not refused.
func TestTheFilterPromptTakesTheUpperBound(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}),
		poo.WithDecider(poo.ModelInfo{}, decide.DeciderFunc(promptAnswer)),
		poo.WithFeeds(thePromptedFeed(waitingFeed)),
	)
	// 1 is the most a filter prompt takes, and it is accepted.
	_, started, err := poo.SubscribeFeedWithPrompt(s, "audit", nil, &poo.FeedFilterPrompt{Question: "Only the tests.", MinNoProbability: 1})
	if err != nil {
		t.Fatalf("the call set a prompt at the upper bound, which is %v; want it accepted", err)
	}
	if !started {
		t.Error("the accepted call did not start the feed")
	}
}
