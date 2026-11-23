package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/config"
	"github.com/sdougbrown/poo/decide"
	"github.com/sdougbrown/poo/pootest"
)

// The agent filter prompt: the question the model sets on a feed instance
// with subscribe_feed, which replaces the manifest's question while it is
// set.

// promptDecider answers the feed's filter question from the test's table,
// and keeps the questions it was asked, in order.
type promptDecider struct {
	mu     sync.Mutex
	answer func(question, text string) float64
	asked  []string
	// undecided makes every call an answer the filter cannot use, which is
	// how a decider that fails or declines looks to it.
	undecided bool
}

// Decide answers one filter's question about one message.
func (d *promptDecider) Decide(_ context.Context, state any, _ string, qs []decide.Question) ([]decide.Answer, error) {
	fields, ok := state.(map[string]string)
	if !ok || len(qs) != 1 {
		return nil, decide.ErrDeferred
	}
	d.mu.Lock()
	d.asked = append(d.asked, qs[0].Instructions)
	d.mu.Unlock()
	if d.undecided {
		return nil, decide.ErrDeferred
	}
	no := d.answer(qs[0].Instructions, fields["text"])
	return []decide.Answer{{
		ID:            qs[0].ID,
		Probabilities: map[string]float64{"no": no, "yes": 1 - no},
	}}, nil
}

// questions returns the questions the decider was asked, in order.
func (d *promptDecider) questions() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.asked...)
}

// promptFeedSession builds a session whose one optional feed is the
// manifest entry given, with the decision model the test hands over, and
// returns it with its sink.
//
// The feed is optional, so it holds its inputs by default: every run of the
// session comes from the test's own prompt, and the turns the test scripts
// are the ones it reads.
func promptFeedSession(t *testing.T, name string, entry map[string]any, decider decide.Decider, turns ...pootest.Turn) (*poo.Session, *pootest.Sink) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"version": 1, "feeds": map[string]any{name: entry}})
	if err != nil {
		t.Fatalf("encoding the manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	settings, err := config.Parse([]byte(theFeedsSettings(path)), config.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	runtime := build(t, settings, config.Overrides{})
	runtime.Options.Transport = pootest.NewTransport(turns...)
	sink := pootest.NewSink()
	runtime.Options.Sink = sink
	runtime.Options.SessionDir = t.TempDir()
	sess, err := poo.New(runtime.Options, poo.WithDecider(poo.ModelInfo{}, decider))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, sink
}

// subscribePromptTurn is one scripted turn that subscribes to a feed with
// the arguments given beside the feed's name.
func subscribePromptTurn(name string, args map[string]any) pootest.Turn {
	call := map[string]any{"feed": name}
	for key, value := range args {
		call[key] = value
	}
	raw, _ := json.Marshal(call)
	return pootest.Turn{Calls: []pootest.Call{{Name: poo.SubscribeFeedToolName, Arguments: raw, ID: "c1"}}}
}

// ackTurn is the turn that ends a run after the tool call.
var ackTurn = pootest.Turn{Text: "ack"}

// promptRun runs one turn of the session and waits for it to end.
func promptRun(t *testing.T, sess *poo.Session, text string) {
	t.Helper()
	if _, err := sess.Prompt(context.Background(), text); err != nil {
		t.Fatalf("prompting %q: %v", text, err)
	}
	if _, err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// stopFeed stops the feed the session lists and waits for its attempt to
// end, so a later subscription re-arms the instance instead of reaching the
// one that is running.
func stopFeed(t *testing.T, sess *poo.Session, sink *pootest.Sink, ended int) {
	t.Helper()
	id := ""
	for _, job := range sess.Jobs() {
		if job.Tool == poo.FeedToolName {
			id = job.JobID
		}
	}
	if id == "" {
		t.Fatalf("the session lists no feed to stop: %+v", sess.Jobs())
	}
	if err := sess.CancelJob(id); err != nil {
		t.Fatalf("stopping the feed %q: %v", id, err)
	}
	waitUntil(t, "the feed's attempt to end", func() bool { return feedEndCount(sink) >= ended })
}

// feedEndCount returns how many feed attempts the sink recorded an end for.
func feedEndCount(sink *pootest.Sink) int {
	n := 0
	for _, rec := range sink.Records() {
		if _, ok := rec.(*poo.FeedEndedRecord); ok {
			n++
		}
	}
	return n
}

// queuedFeedTexts returns the texts the session accepted from its feeds, in
// order.
func queuedFeedTexts(sink *pootest.Sink) []string {
	out := make([]string, 0, 4)
	for _, input := range queuedFeedInputs(sink) {
		out = append(out, input.Text)
	}
	return out
}

// subscribeResults returns the results the session recorded for the
// subscribe_feed tool, in order.
func subscribeResults(sink *pootest.Sink) []*poo.ToolResultRecord {
	var out []*poo.ToolResultRecord
	for _, rec := range sink.Records() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == poo.SubscribeFeedToolName {
			out = append(out, r)
		}
	}
	return out
}

// The questions the prompt tests put to the decider.
const (
	theManifestQuestion = "Does the manifest want this message?"
	theFirstOnly        = "Only deliver the first message."
	theSecondOnly       = "Only deliver the second message."
)

// TestTheFilterPromptReplacesTheManifestsQuestion covers the prompt's
// lifecycle against a feed whose manifest asks a question: the model's
// question is the one the decider is asked, a later call replaces it, and
// the empty prompt restores the manifest's question.
func TestTheFilterPromptReplacesTheManifestsQuestion(t *testing.T) {
	decider := &promptDecider{answer: func(question, text string) float64 {
		switch question {
		case theFirstOnly:
			if text == "the first message" {
				return 0.01
			}
			return 0.99
		case theSecondOnly:
			if text == "the second message" {
				return 0.01
			}
			return 0.99
		case theManifestQuestion:
			return 0.99
		}
		t.Errorf("the filter put the question %q to the decider", question)
		return 0.99
	}}
	sess, sink := promptFeedSession(t, "chat", map[string]any{
		"command":  []string{feedScript(t, "chat")},
		"optional": true,
		"filter":   map[string]any{"question": theManifestQuestion, "min_no_probability": 0.9},
	}, decider,
		// The manifest's question would drop both messages, so whatever
		// arrives is the prompt's doing.
		subscribePromptTurn("chat", map[string]any{"prompt": theFirstOnly}), ackTurn,
		subscribePromptTurn("chat", map[string]any{"prompt": theSecondOnly}), ackTurn,
		subscribePromptTurn("chat", map[string]any{"prompt": ""}), ackTurn,
	)

	promptRun(t, sess, "watch it for the tests")
	waitUntil(t, "the first two records to be asked about", func() bool { return decisionCount(sink) == 2 })
	if got := queuedFeedTexts(sink); len(got) != 1 || !strings.Contains(got[0], "the first message") {
		t.Fatalf("the prompt delivered %v, want the first message alone", got)
	}

	// A second call replaces the prompt, and the records the re-armed
	// instance emits follow the new question.
	stopFeed(t, sess, sink, 1)
	promptRun(t, sess, "watch it for the failures")
	waitUntil(t, "the re-armed feed's records to be asked about", func() bool { return decisionCount(sink) == 4 })
	if got := queuedFeedTexts(sink); len(got) != 2 || !strings.Contains(got[1], "the second message") {
		t.Fatalf("the replaced prompt delivered %v, want the second message beside the first", got)
	}

	// The empty prompt clears the model's question, and the manifest's is
	// the one that applies again.
	stopFeed(t, sess, sink, 2)
	promptRun(t, sess, "stop screening for the tests")
	waitUntil(t, "the cleared feed's records to be asked about", func() bool { return decisionCount(sink) == 6 })

	want := []string{
		theFirstOnly, theFirstOnly,
		theSecondOnly, theSecondOnly,
		theManifestQuestion, theManifestQuestion,
	}
	got := decider.questions()
	if len(got) != len(want) {
		t.Fatalf("the decider was asked %d questions (%q), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("question %d is %q, want %q", i, got[i], want[i])
		}
	}
	if queued := queuedFeedTexts(sink); len(queued) != 2 {
		t.Errorf("the manifest's question delivered %d messages, want only the two the prompt let through: %v", len(queued), queued)
	}
	// The call's result says what the prompt did, so the model knows which
	// question is screening its feed.
	results := subscribeResults(sink)
	if len(results) != 3 {
		t.Fatalf("the session recorded %d subscribe results, want 3", len(results))
	}
	if !strings.Contains(results[0].Text, "The filter prompt is set") {
		t.Errorf("the setting call says %q, want it to report the prompt", results[0].Text)
	}
	if !strings.Contains(results[2].Text, "The filter prompt is cleared") {
		t.Errorf("the clearing call says %q, want it to report the clear", results[2].Text)
	}
}

// TestAPlainSubscriptionKeepsTheFilterPrompt covers the absent argument: a
// subscription that names no prompt leaves the instance's own alone, so a
// re-armed feed still asks the question the model set.
func TestAPlainSubscriptionKeepsTheFilterPrompt(t *testing.T) {
	decider := &promptDecider{answer: func(question, text string) float64 {
		if question != theFirstOnly {
			t.Errorf("the filter put the question %q to the decider, want the prompt the instance kept", question)
		}
		if text == "the first message" {
			return 0.01
		}
		return 0.99
	}}
	// The manifest asks no question, so the prompt is the only thing that
	// screens the feed's records.
	sess, sink := promptFeedSession(t, "chat", map[string]any{
		"command":  []string{feedScript(t, "chat")},
		"optional": true,
	}, decider,
		subscribePromptTurn("chat", map[string]any{"prompt": theFirstOnly}), ackTurn,
		subscribePromptTurn("chat", nil), ackTurn,
	)

	promptRun(t, sess, "watch it for the tests")
	waitUntil(t, "the first two records to be asked about", func() bool { return decisionCount(sink) == 2 })

	stopFeed(t, sess, sink, 1)
	promptRun(t, sess, "watch it again")
	waitUntil(t, "the re-armed feed's records to be asked about", func() bool { return decisionCount(sink) == 4 })
	if queued := queuedFeedTexts(sink); len(queued) != 2 {
		t.Fatalf("the re-armed feed delivered %v, want the first message twice", queued)
	}
	if got := decider.questions(); len(got) != 4 {
		t.Errorf("the decider was asked %d questions (%q), want four under the kept prompt", len(got), got)
	}
}

// TestTheFilterPromptsProbability covers the threshold's sources — the
// default of 0.6 when neither the call nor the manifest names one, the
// call's own value beside the prompt, and the manifest's value when the call
// names none — and the fail-open answer a prompt inherits.
func TestTheFilterPromptsProbability(t *testing.T) {
	const no = 0.62
	for _, tc := range []struct {
		name      string
		filter    map[string]any
		args      map[string]any
		answer    float64
		undecided bool
		want      int
	}{
		{
			// 0.62 is past the default 0.6, so the messages are dropped.
			name:   "the default",
			args:   map[string]any{"prompt": theFirstOnly},
			answer: no,
			want:   0,
		},
		{
			// 0.59 is just below the default 0.6, so the messages deliver:
			// a default that regressed to 0.5 or 0.4 would drop them.
			name:   "the default, just below the bound",
			args:   map[string]any{"prompt": theFirstOnly},
			answer: 0.59,
			want:   2,
		},
		{
			// The call's 0.7 is above the answer's 0.62, so both deliver.
			name:   "the call's value",
			args:   map[string]any{"prompt": theFirstOnly, "min_no_probability": 0.7},
			answer: no,
			want:   2,
		},
		{
			// The manifest's 0.55 is below the answer's 0.57, so both
			// drop: the default 0.6 would have delivered them.
			name:   "the manifest's value",
			filter: map[string]any{"question": theManifestQuestion, "min_no_probability": 0.55},
			args:   map[string]any{"prompt": theFirstOnly},
			answer: 0.57,
			want:   0,
		},
		{
			// A prompt fails open like the manifest's question: an answer
			// the filter cannot use delivers the message.
			name:      "an answer it cannot use",
			args:      map[string]any{"prompt": theFirstOnly},
			answer:    no,
			undecided: true,
			want:      2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, undecided := tc.answer, tc.undecided
			decider := &promptDecider{answer: func(string, string) float64 { return answer }, undecided: undecided}
			entry := map[string]any{
				"command":  []string{feedScript(t, "chat")},
				"optional": true,
			}
			if tc.filter != nil {
				entry["filter"] = tc.filter
			}
			sess, sink := promptFeedSession(t, "chat", entry, decider,
				subscribePromptTurn("chat", tc.args), ackTurn)
			promptRun(t, sess, "watch it for the tests")
			waitUntil(t, "the feed's records to be asked about", func() bool { return decisionCount(sink) == 2 })
			if queued := queuedFeedTexts(sink); len(queued) != tc.want {
				t.Errorf("the threshold delivered %v, want %d messages", queued, tc.want)
			}
			// Every arm asks the same question, so the threshold or the
			// answer explains the difference in what was delivered.
			if got := decider.questions(); len(got) != 2 || got[0] != theFirstOnly {
				t.Errorf("the decider was asked %q, want the prompt twice", got)
			}
		})
	}
}

// TestTheFilterPromptIsRefusedWhenTheFeedCannotTakeIt covers the refusals a
// built session makes: a feed whose manifest screens its own records takes
// no prompt, and a threshold outside the range is no threshold at all.
func TestTheFilterPromptIsRefusedWhenTheFeedCannotTakeIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry map[string]any
		args  map[string]any
		want  string
	}{
		{
			name: "the feed runs a filter of its own",
			entry: map[string]any{
				"command":  []string{feedScript(t, "chat")},
				"optional": true,
				"filter":   map[string]any{"command": []string{filterScript(t, "filter-echo")}},
			},
			args: map[string]any{"prompt": theFirstOnly},
			want: "runs a filter of its own",
		},
		{
			name: "the threshold is out of range",
			entry: map[string]any{
				"command":  []string{feedScript(t, "chat")},
				"optional": true,
			},
			args: map[string]any{"prompt": theFirstOnly, "min_no_probability": 0.2},
			want: "min_no_probability",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decider := &promptDecider{answer: func(string, string) float64 { return 0.01 }}
			sess, sink := promptFeedSession(t, "chat", tc.entry, decider,
				subscribePromptTurn("chat", tc.args), ackTurn)
			promptRun(t, sess, "watch it for the tests")

			results := subscribeResults(sink)
			if len(results) != 1 || !results[0].IsError {
				t.Fatalf("the call's results are %+v, want one refusal", results)
			}
			if !strings.Contains(results[0].Text, tc.want) {
				t.Errorf("the refusal is %q, want it to name %q", results[0].Text, tc.want)
			}
			// The refusal is the whole of the call: nothing started, and
			// the decider was asked nothing.
			for _, rec := range sink.Records() {
				if _, ok := rec.(*poo.FeedStartedRecord); ok {
					t.Error("the refused call started the feed")
				}
			}
			if asked := decider.questions(); len(asked) != 0 {
				t.Errorf("the refused call asked the decider %q", asked)
			}
		})
	}
}

// TestAnExplicitMinNoProbabilityWithNoPromptIsRefused covers the
// zero-value convention's edge: an explicit min_no_probability of 0 with no
// prompt is out of range, so it is refused, and the instance's prompt is
// left alone.
func TestAnExplicitMinNoProbabilityWithNoPromptIsRefused(t *testing.T) {
	decider := &promptDecider{answer: func(question, text string) float64 {
		if question != theFirstOnly {
			t.Errorf("the filter put the question %q to the decider, want the prompt the instance kept", question)
		}
		if text == "the first message" {
			return 0.01
		}
		return 0.99
	}}
	// The manifest asks no question, so the prompt is the only thing that
	// screens the feed's records: a clear would leave nothing to ask.
	sess, sink := promptFeedSession(t, "chat", map[string]any{
		"command":  []string{feedScript(t, "chat")},
		"optional": true,
	}, decider,
		subscribePromptTurn("chat", map[string]any{"prompt": theFirstOnly}), ackTurn,
		subscribePromptTurn("chat", map[string]any{"min_no_probability": 0}), ackTurn,
		subscribePromptTurn("chat", nil), ackTurn,
	)

	promptRun(t, sess, "watch it for the tests")
	waitUntil(t, "the first two records to be asked about", func() bool { return decisionCount(sink) == 2 })

	// The explicit 0 with no prompt is refused — the call's argument is
	// outside the range a min_no_probability takes, whatever the prompt —
	// and the refusal names the bound.
	promptRun(t, sess, "name the threshold")
	results := subscribeResults(sink)
	if len(results) != 2 || !results[1].IsError {
		t.Fatalf("the call's results are %+v, want the second to be a refusal", results)
	}
	if !strings.Contains(results[1].Text, "greater than 0.5") {
		t.Errorf("the refusal is %q, want it to name the bound", results[1].Text)
	}

	// The refusal is the whole of the call: the instance's prompt is left
	// alone. Re-arm and the kept prompt is the one the decider is asked.
	stopFeed(t, sess, sink, 1)
	promptRun(t, sess, "watch it again")
	waitUntil(t, "the re-armed feed's records to be asked about", func() bool { return decisionCount(sink) == 4 })
	got := decider.questions()
	if len(got) != 4 {
		t.Fatalf("the decider was asked %d questions (%q), want four under the kept prompt", len(got), got)
	}
	for _, q := range got {
		if q != theFirstOnly {
			t.Errorf("the decider was asked %q, want the prompt the instance kept", q)
		}
	}
}
