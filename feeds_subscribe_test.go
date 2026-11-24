package poo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The optional feed: a feed the session loads and does not start, and the
// subscribe_feed tool that starts one instance of it with the scope the
// manifest declares.

// theOptionalFeedSpec is a feed a subscription starts: its command is the
// runner the test hands over, and it declares one scope field.
func theOptionalFeedSpec(run poo.FeedRunner, onIdle poo.OnIdle) poo.FeedSpec {
	return poo.FeedSpec{
		Name:        "git-feedback",
		Description: "Feedback on one repository's pull requests.",
		Optional:    true,
		OnIdle:      onIdle,
		Scope:       []poo.FeedScopeField{{Name: "url", Type: "string", Pattern: `^https://.*$`}},
		Run:         run,
	}
}

// waitingFeed emits nothing and waits to be stopped, so a test can watch a
// subscription's lifecycle without an input on the queue.
func waitingFeed(ctx context.Context, io poo.FeedIO) error {
	<-ctx.Done()
	return ctx.Err()
}

// subscribeCall is a tool call that subscribes to a feed with a scope.
func subscribeCall(callID, feed string, scope map[string]string) poo.ToolCall {
	args := map[string]any{"feed": feed}
	if scope != nil {
		args["scope"] = scope
	}
	raw, _ := json.Marshal(args)
	return poo.ToolCall{CallID: callID, Name: poo.SubscribeFeedToolName, Arguments: raw}
}

// toolResults returns the results a session recorded for a tool, in order.
func toolResults(sink *recordingSink, tool string) []*poo.ToolResultRecord {
	var out []*poo.ToolResultRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok && r.Tool == tool {
			out = append(out, r)
		}
	}
	return out
}

// feedStarts returns the feed.started records a session wrote, in order.
func feedStarts(sink *recordingSink) []*poo.FeedStartedRecord {
	var out []*poo.FeedStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.FeedStartedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// feedEnds returns the feed.ended records a session wrote, in order.
func feedEnds(sink *recordingSink) []*poo.FeedEndedRecord {
	var out []*poo.FeedEndedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.FeedEndedRecord); ok {
			out = append(out, r)
		}
	}
	return out
}

// TestAnOptionalFeedWaitsForASubscription covers the rule the manifest
// turns on: a feed declared optional is loaded, named to the model with
// the values it watches, and never started by the session itself.
func TestAnOptionalFeedWaitsForASubscription(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	// The raw prompt is the whole prompt, so the sections render only
	// without one.
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SystemPrompt = ""
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")))

	// The session started nothing, and running a turn does not start it.
	runSession(t, s, "the prompt")
	if starts := feedStarts(sink); len(starts) != 0 {
		t.Fatalf("the session started %d feeds, want none: %+v", len(starts), starts[0])
	}

	// The prompt names the feed and the values a subscription supplies,
	// so the model knows what it may ask for.
	prompt := systemPromptOf(t, tr.Requests()[0])
	if !strings.Contains(prompt, poo.SubscribeFeedToolName) {
		t.Fatalf("the prompt says nothing about %s:\n%s", poo.SubscribeFeedToolName, prompt)
	}
	if !strings.Contains(prompt, "git-feedback: Feedback on one repository's pull requests.") {
		t.Errorf("the prompt does not describe the optional feed:\n%s", prompt)
	}
	if !strings.Contains(prompt, `Its scope is url (^https://.*$)`) {
		t.Errorf("the prompt does not name the scope field:\n%s", prompt)
	}
	// The feed does not run, so the section that lists the feeds a
	// session runs does not name it either.
	if strings.Contains(prompt, "Feeds this session runs") {
		t.Errorf("the prompt lists the optional feed as one the session runs:\n%s", prompt)
	}
}

// TestTheSubscribeToolIsOfferedOnlyWithAnOptionalFeed covers the offer: a
// session that loads only feeds it starts itself gives the model no way to
// start one.
func TestTheSubscribeToolIsOfferedOnlyWithAnOptionalFeed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spec  poo.FeedSpec
		wants bool
	}{
		{
			name:  "a feed the session starts itself",
			spec:  poo.FeedSpec{Name: "inbox", Run: waitingFeed},
			wants: false,
		},
		{
			name:  "a feed a subscription starts",
			spec:  theOptionalFeedSpec(waitingFeed, ""),
			wants: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "the answer"}})
			s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}), poo.WithFeeds(tc.spec))
			runSession(t, s, "the prompt")
			offered := false
			for _, tool := range tr.Requests()[0].Tools {
				if tool.Name == poo.SubscribeFeedToolName {
					offered = true
				}
			}
			if offered != tc.wants {
				t.Errorf("the tool is offered %v, want %v", offered, tc.wants)
			}
		})
	}
}

// TestSubscribeFeedStartsTheFeedWithItsScope covers the call: the session
// starts the instance, the scope reaches the runner, and the input the
// instance emits says which instance it came from.
func TestSubscribeFeedStartsTheFeedWithItsScope(t *testing.T) {
	const url = "https://example.test/repo"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": url})}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	// The emit waits for the test, so the input lands while the session is
	// idle and wakes a run of its own: the run's record names the instance
	// the input came from.
	emitNow := make(chan struct{})
	spec := theOptionalFeedSpec(func(ctx context.Context, io poo.FeedIO) error {
		select {
		case <-emitNow:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := io.Emit("", "watching "+io.Scope["url"], nil, nil); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}, poo.OnIdleWake)
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(spec))
	runSession(t, s, "subscribe to the feed")
	waitRecords(t, sink, "feed.started", 1)
	close(emitNow)

	// The woken run reads the input, so the emit happened and reached the
	// model.
	waitRecords(t, sink, "input.delivered", 1)
	if _, _, ok := deliveredMessage(sink); !ok {
		t.Fatal("the feed's input did not reach the model")
	}
	starts := feedStarts(sink)
	if len(starts) != 1 {
		t.Fatalf("the session started %d feeds, want the one instance", len(starts))
	}
	if starts[0].Feed != "git-feedback" || starts[0].Attempt != 1 {
		t.Errorf("the attempt is %q #%d, want git-feedback #1", starts[0].Feed, starts[0].Attempt)
	}
	// The instance's origin names the feed and is not the unscoped one, so
	// a reader can tell two subscriptions of one feed apart.
	origin := starts[0].Meta().Origin
	if !poo.IsFeedOrigin(origin) {
		t.Fatalf("the attempt's origin is %q, want a feed's", origin)
	}
	if name, ok := poo.FeedName(origin); !ok || name != "git-feedback" {
		t.Errorf("the origin %q names the feed %q, %v; want git-feedback", origin, name, ok)
	}
	if origin == poo.FeedOrigin("git-feedback") {
		t.Errorf("a scoped instance's origin is the unscoped one: %q", origin)
	}
	// The input the instance emitted names the instance, and the scope
	// reached the runner's own environment seam.
	text, delivered, ok := deliveredMessage(sink)
	if !ok {
		t.Fatal("the feed's input did not reach the model")
	}
	if delivered != origin {
		t.Errorf("the input's origin is %q, want the instance's %q", delivered, origin)
	}
	label, _ := poo.FeedInstance(origin)
	if !strings.Contains(text, `name="`+label+`"`) {
		t.Errorf("the input %q does not name the instance %q", text, label)
	}
	if !strings.Contains(text, "watching "+url) {
		t.Errorf("the input %q does not carry the value the subscription named", text)
	}
	// The run the input woke names the instance, so a reader of the run's
	// record sees which instance started it.
	var woke *poo.RunStartedRecord
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.RunStartedRecord); ok && r.Trigger == poo.TriggerFeed {
			woke = r
		}
	}
	if woke == nil {
		t.Fatal("the log holds no run the feed's input woke")
	}
	if len(woke.Feeds) != 1 || woke.Feeds[0] != label {
		t.Errorf("the woken run names the feeds %v, want [%s]", woke.Feeds, label)
	}
	// The call reports the instance it started.
	results := toolResults(sink, poo.SubscribeFeedToolName)
	if len(results) != 1 || results[0].IsError {
		t.Fatalf("the call's results are %+v, want one that succeeded", results)
	}
	if !strings.Contains(results[0].Text, "is running") {
		t.Errorf("the call's result is %q, want it to report the instance", results[0].Text)
	}
}

// TestSubscribingTheSameScopeReachesOneInstance covers idempotence: a
// second call for the same feed and scope re-arms the instance that is
// already there, so one feed runs once however often the model asks.
func TestSubscribingTheSameScopeReachesOneInstance(t *testing.T) {
	scope := map[string]string{"url": "https://example.test/repo"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", scope)}},
		{calls: []poo.ToolCall{subscribeCall("c2", "git-feedback", scope)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")))
	runSession(t, s, "start it twice")

	waitRecords(t, sink, "feed.started", 1)
	if starts := feedStarts(sink); len(starts) != 1 {
		t.Fatalf("the session started %d attempts, want the one instance", len(starts))
	}
	results := toolResults(sink, poo.SubscribeFeedToolName)
	if results[0].IsError || results[1].IsError {
		t.Fatalf("a repeated subscription failed: %+v", results)
	}
	if !strings.Contains(results[1].Text, "already running") {
		t.Errorf("the repeated call says %q, want it to report the running instance", results[1].Text)
	}
	// Both calls name one instance, so the model can stop it under the id
	// the second call was given.
	first, second := instanceOf(t, results[0].Text), instanceOf(t, results[1].Text)
	if first != second || first == "" {
		t.Errorf("the calls name the instances %q and %q, want one", first, second)
	}
}

// instanceOf returns the instance a subscribe_feed result names.
func instanceOf(t *testing.T, text string) string {
	t.Helper()
	const open, shut = "The feed ", " is"
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("the result %q names no instance", text)
	}
	rest := text[start+len(open):]
	end := strings.Index(rest, shut)
	if end < 0 {
		t.Fatalf("the result %q names no instance", text)
	}
	return rest[:end]
}

// TestResubscribingAStoppedFeedStartsItAgain covers the re-arm: a
// subscription that stopped is started again by the next call, on the same
// instance and under the next attempt.
func TestResubscribingAStoppedFeedStartsItAgain(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
		{calls: []poo.ToolCall{subscribeCall("c2", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")))
	runSession(t, s, "start it")

	waitRecords(t, sink, "feed.started", 1)
	origin := feedStarts(sink)[0].Meta().Origin
	if err := s.CancelJob(string(origin)); err != nil {
		t.Fatalf("stopping the instance: %v", err)
	}
	waitRecords(t, sink, "feed.ended", 1)

	runSession(t, s, "start it again")
	waitRecords(t, sink, "feed.started", 2)
	starts := feedStarts(sink)
	if starts[1].Meta().Origin != origin {
		t.Errorf("the re-armed attempt's origin is %q, want the same instance %q", starts[1].Meta().Origin, origin)
	}
	if starts[1].Attempt != 2 {
		t.Errorf("the re-armed attempt is #%d, want #2", starts[1].Attempt)
	}
	results := toolResults(sink, poo.SubscribeFeedToolName)
	if len(results) != 2 || results[1].IsError {
		t.Fatalf("the second call's results are %+v, want a call that started it", results)
	}
	if strings.Contains(results[1].Text, "already running") {
		t.Errorf("the second call says %q, want it to report a start", results[1].Text)
	}
}

// TestSubscribingBeyondTheInstanceCapIsRefused covers the cap: a session runs
// at most MaxFeedInstances instances at once, and the call that would start
// one more is refused, naming the cap, so a loop that subscribes with a fresh
// scope value each time cannot grow the session's processes without bound.
func TestSubscribingBeyondTheInstanceCapIsRefused(t *testing.T) {
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, scripted(nil), &echoTool{}, sink), poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")))

	instances := make([]string, 0, poo.MaxFeedInstances)
	for i := range poo.MaxFeedInstances {
		scope := map[string]string{"url": fmt.Sprintf("https://example.test/%d", i)}
		instance, started, err := poo.SubscribeFeed(s, "git-feedback", scope)
		if err != nil || !started {
			t.Fatalf("starting instance %d: started=%v, err=%v", i, started, err)
		}
		instances = append(instances, instance)
	}
	waitRecords(t, sink, "feed.started", poo.MaxFeedInstances)

	// One more is past the cap, and the refusal says how many the session
	// runs and how to make room.
	refused, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/one-too-many"})
	if err == nil {
		t.Fatalf("the call past the cap reached the instance %q (started=%v), want a refusal", refused, started)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", poo.MaxFeedInstances)) || !strings.Contains(err.Error(), "cancel_job") {
		t.Errorf("the refusal is %q, want it to name the cap and how to make room", err)
	}
	if starts := feedStarts(sink); len(starts) != poo.MaxFeedInstances {
		t.Errorf("the session started %d attempts, want the %d instances the cap allows", len(starts), poo.MaxFeedInstances)
	}

	// The cap counts instances, not calls: a subscription that reaches one
	// that is already there is never refused.
	if instance, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/0"}); err != nil || started || instance != instances[0] {
		t.Fatalf("the repeated call reached %q (started=%v, err=%v), want the running instance %q", instance, started, err, instances[0])
	}

	// Stopping an instance makes room, and the freed slot is the one the
	// next call takes.
	if err := s.CancelJob(instances[0]); err != nil {
		t.Fatalf("stopping the instance: %v", err)
	}
	waitRecords(t, sink, "feed.ended", 1)
	instance, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/one-too-many"})
	if err != nil || !started {
		t.Fatalf("the call after a stop reached %q (started=%v, err=%v), want a new instance", instance, started, err)
	}
}

// TestSubscribingTwoScopesThatSpellEachOtherStartsTwoInstances covers
// the label's escaping: a value that spells another field's name= cannot
// collide with a scope that carries the same bytes in a different field,
// so two distinct scopes name two instances, not one.
func TestSubscribingTwoScopesThatSpellEachOtherStartsTwoInstances(t *testing.T) {
	spec := poo.FeedSpec{
		Name:     "two-field",
		Optional: true,
		Scope: []poo.FeedScopeField{
			{Name: "u1", Type: "string"},
			{Name: "u2", Type: "string"},
		},
		Run: waitingFeed,
	}
	// The first scope's u1 spells the second scope's u2, and the second
	// scope's u2 spells the first scope's u1: an escaping that does not
	// escape "," or "=" names the two scopes with one label.
	first := map[string]string{"u1": "a,u2=b", "u2": "c"}
	second := map[string]string{"u1": "a", "u2": "b,u2=c"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "two-field", first)}},
		{calls: []poo.ToolCall{subscribeCall("c2", "two-field", second)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(spec))
	runSession(t, s, "subscribe twice")

	waitRecords(t, sink, "feed.started", 2)
	starts := feedStarts(sink)
	if len(starts) != 2 {
		t.Fatalf("the session started %d instances, want two", len(starts))
	}
	if starts[0].Meta().Origin == starts[1].Meta().Origin {
		t.Fatalf("the two scopes name one origin: %q", starts[0].Meta().Origin)
	}
	results := toolResults(sink, poo.SubscribeFeedToolName)
	if len(results) != 2 || results[0].IsError || results[1].IsError {
		t.Fatalf("the calls' results are %+v, want two that started", results)
	}
	if strings.Contains(results[1].Text, "already running") {
		t.Errorf("the second call says %q, want it to report a start", results[1].Text)
	}
}

// TestAFeedInstanceOriginNamesItsFeedAndScope covers the shape an
// instance's origin takes: the feed's name stays readable, an unscoped feed
// keeps the origin it always had, and two scopes name two instances.
func TestAFeedInstanceOriginNamesItsFeedAndScope(t *testing.T) {
	if got := poo.FeedInstanceOrigin("git-feedback", ""); got != poo.FeedOrigin("git-feedback") {
		t.Errorf("an unscoped instance's origin is %q, want %q", got, poo.FeedOrigin("git-feedback"))
	}
	one := poo.FeedInstanceOrigin("git-feedback", "url=https%3A%2F%2Fone.test")
	two := poo.FeedInstanceOrigin("git-feedback", "url=https%3A%2F%2Ftwo.test")
	if one == two {
		t.Fatalf("two scopes name one origin: %q", one)
	}
	for _, origin := range []poo.Origin{one, two} {
		if !poo.IsFeedOrigin(origin) {
			t.Fatalf("the origin %q is not a feed's", origin)
		}
		if name, ok := poo.FeedName(origin); !ok || name != "git-feedback" {
			t.Errorf("the origin %q names the feed %q, %v; want git-feedback", origin, name, ok)
		}
	}
}

// TestSubscribeFeedRefusesWhatTheManifestCannotTake covers the refusals: a
// feed the session starts itself, a feed it does not load, a scope the
// feed does not declare, a field the call leaves out, a value the field's
// pattern refuses, and a scope passed to a feed that takes none.
func TestSubscribeFeedRefusesWhatTheManifestCannotTake(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  map[string]any
		wants string
	}{
		{
			name:  "a feed the session starts itself",
			args:  map[string]any{"feed": "inbox"},
			wants: "not one this session starts on demand",
		},
		{
			name:  "a feed the session does not load",
			args:  map[string]any{"feed": "nope"},
			wants: "not one this session starts on demand",
		},
		{
			name:  "a scope field the feed does not declare",
			args:  map[string]any{"feed": "git-feedback", "scope": map[string]string{"repo": "x"}},
			wants: `declares no scope field "repo"`,
		},
		{
			name:  "a field the call leaves out",
			args:  map[string]any{"feed": "git-feedback", "scope": map[string]string{}},
			wants: `needs a value for the scope field "url"`,
		},
		{
			name:  "a value the pattern refuses",
			args:  map[string]any{"feed": "git-feedback", "scope": map[string]string{"url": "http://insecure.test"}},
			wants: `must match "^https://.*$"`,
		},
		{
			name:  "a value longer than the cap",
			args:  map[string]any{"feed": "git-feedback", "scope": map[string]string{"url": strings.Repeat("a", 4097)}},
			wants: "at most 4096 bytes",
		},
		{
			name:  "a value no environment can carry",
			args:  map[string]any{"feed": "git-feedback", "scope": map[string]string{"url": "https://example.test/\u0000"}},
			wants: `carries a NUL byte`,
		},
		{
			name:  "a scope passed to a feed that takes none",
			args:  map[string]any{"feed": "audit", "scope": map[string]string{"url": "https://example.test"}},
			wants: `takes no scope`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatalf("encoding the call: %v", err)
			}
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{{CallID: "c1", Name: poo.SubscribeFeedToolName, Arguments: raw}}},
				{text: "the answer"},
			})
			sink := &recordingSink{}
			s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(
				theOptionalFeedSpec(waitingFeed, ""),
				// A feed with no scope, and one the session starts itself.
				poo.FeedSpec{Name: "audit", Optional: true, Run: waitingFeed},
				poo.FeedSpec{Name: "inbox", Run: waitingFeed},
			))
			runSession(t, s, "subscribe")

			waitRecords(t, sink, "tool.result", 1)
			results := toolResults(sink, poo.SubscribeFeedToolName)
			if len(results) == 0 {
				t.Fatal("the log holds no subscribe result")
			}
			result := results[0]
			if !result.IsError {
				t.Fatalf("the call succeeded: %q", result.Text)
			}
			if !strings.Contains(result.Text, tc.wants) {
				t.Errorf("the refusal is %q, want it to name %q", result.Text, tc.wants)
			}
			// Only the feed the session starts itself ran.
			for _, start := range feedStarts(sink) {
				if start.Feed != "inbox" {
					t.Errorf("the refused call started the feed %q", start.Feed)
				}
			}
		})
	}
}

// TestAnOptionalFeedWithoutAScopeStartsAsItself covers the feed that takes
// no values: a subscription with no scope starts it, under the origin it
// would have had if the session had started it.
func TestAnOptionalFeedWithoutAScopeStartsAsItself(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "audit", nil)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(
		poo.FeedSpec{Name: "audit", Optional: true, Run: waitingFeed},
	))
	runSession(t, s, "subscribe")

	waitRecords(t, sink, "feed.started", 1)
	if origin := feedStarts(sink)[0].Meta().Origin; origin != poo.FeedOrigin("audit") {
		t.Errorf("the instance's origin is %q, want %q", origin, poo.FeedOrigin("audit"))
	}
}

// TestAnOptionalFeedHoldsItsInputsUnlessItsManifestSaysWake covers the idle
// policy: a feed a subscription started holds its inputs unless the
// manifest asked for a wake, so an idle session is not woken by a feed the
// model chose to watch.
func TestAnOptionalFeedHoldsItsInputsUnlessItsManifestSaysWake(t *testing.T) {
	for _, tc := range []struct {
		name   string
		onIdle poo.OnIdle
		held   bool
	}{
		{name: "no policy holds", onIdle: "", held: true},
		{name: "the manifest's wake", onIdle: poo.OnIdleWake, held: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
				{text: "the answer"},
			})
			sink := &recordingSink{}
			// The emit waits for the test, so it lands while the session
			// is idle: the idle policy is what the assertion is about,
			// and an emit during a turn would be taken by that turn
			// whatever the policy said.
			emitNow := make(chan struct{})
			spec := theOptionalFeedSpec(func(ctx context.Context, io poo.FeedIO) error {
				select {
				case <-emitNow:
				case <-ctx.Done():
					return ctx.Err()
				}
				if err := io.Emit("", "watching", nil, nil); err != nil {
					return err
				}
				<-ctx.Done()
				return ctx.Err()
			}, tc.onIdle)
			s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(spec))
			runSession(t, s, "subscribe")
			close(emitNow)
			waitRecords(t, sink, "input.queued", 1)
			if tc.held {
				waitRecords(t, sink, "input.held", 1)
				if s.Running() {
					t.Error("the session woke a run for a held feed's input")
				}
				return
			}
			waitRecords(t, sink, "input.delivered", 1)
			if _, _, ok := deliveredMessage(sink); !ok {
				t.Fatal("the feed's input did not reach the model")
			}
		})
	}
}

// TestASubscriptionIsListedAndStoppedLikeAFeed covers the job side of a
// subscription: the instance a call started is a row in the jobs list, and
// stopping it takes the id that row names.
func TestASubscriptionIsListedAndStoppedLikeAFeed(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
		{calls: []poo.ToolCall{{CallID: "c2", Name: poo.JobsToolName}}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(theOptionalFeedSpec(waitingFeed, "")))

	// The first run subscribes, and the attempt is running before it is
	// stopped: a cancel that arrives before the first attempt is not an
	// attempt that ended.
	runSession(t, s, "subscribe")
	waitRecords(t, sink, "feed.started", 1)
	instance := string(feedStarts(sink)[0].Meta().Origin)

	runSession(t, s, "list it")
	results := toolResults(sink, poo.JobsToolName)
	if len(results) != 1 || results[0].IsError {
		t.Fatalf("the jobs results are %+v, want the one list", results)
	}
	if !strings.Contains(results[0].Text, poo.FeedToolName+" · "+poo.JobRunning) {
		t.Errorf("the jobs list is %q, want the running feed's row", results[0].Text)
	}
	if !strings.Contains(results[0].Text, instance) {
		t.Errorf("the jobs list is %q, want it to name %q", results[0].Text, instance)
	}

	// The id the list named stops the instance, whatever the scope's
	// values made of it.
	if err := s.CancelJob(instance); err != nil {
		t.Fatalf("stopping the instance: %v", err)
	}
	waitRecords(t, sink, "feed.ended", 1)
	if ends := feedEnds(sink); string(ends[0].Meta().Origin) != instance || ends[0].Outcome != poo.FeedCancelled {
		t.Errorf("the attempt ended as %q on %q, want cancelled on %q", ends[0].Outcome, ends[0].Meta().Origin, instance)
	}
}

// TestAnInstanceKeepsItsOwnBacklogCap covers the cap a subscription's
// instance runs with: the manifest's max_pending is the instance's own, so
// a scoped feed drops its oldest held input at the cap the manifest named.
func TestAnInstanceKeepsItsOwnBacklogCap(t *testing.T) {
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	emitNow := make(chan struct{})
	spec := theOptionalFeedSpec(func(ctx context.Context, io poo.FeedIO) error {
		select {
		case <-emitNow:
		case <-ctx.Done():
			return ctx.Err()
		}
		for _, text := range []string{"one", "two"} {
			if err := io.Emit("", text, nil, nil); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}, "")
	spec.MaxPending = 1
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink), poo.WithFeeds(spec))
	runSession(t, s, "subscribe")
	close(emitNow)

	// The drop's record is written before the acceptance (the fate is
	// recorded first, so a restore reads it), so the wait is on the
	// acceptance: at the cap of one, a second queued input means the
	// first one was dropped.
	waitRecords(t, sink, "input.queued", 2)
	if queued := sink.count("input.queued"); queued != 2 {
		t.Errorf("the instance accepted %d inputs, want 2", queued)
	}
	var dropped *poo.InputDroppedRecord
	for _, rec := range sink.allRecords() {
		if d, ok := rec.(*poo.InputDroppedRecord); ok {
			dropped = d
		}
	}
	if dropped == nil || dropped.Reason != poo.ReasonBacklog {
		t.Fatalf("the drop is %+v, want the backlog's reason", dropped)
	}
}
