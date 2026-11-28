package poo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The write-ahead records: an effect whose record did not durably land
// does not start, so a durable session can trust the log as the account
// of what may have happened.

// refusingRecordSink refuses every append of one record name and keeps
// the rest, so a test can wait for the log's other records.
type refusingRecordSink struct {
	recordingSink
	// record is the name the sink refuses.
	record string
}

// Append refuses the named record.
func (s *refusingRecordSink) Append(ctx context.Context, rec poo.Record) error {
	if rec.Meta().Record == s.record {
		return errors.New("the store refused the record")
	}
	return s.recordingSink.Append(ctx, rec)
}

// TestASideEffectingToolWhoseStartRecordIsRefusedDoesNotRun covers the
// sync path: a tool.started record the store refuses leaves the call
// unstarted, and the store refusal settles the run broken.
func TestASideEffectingToolWhoseStartRecordIsRefusedDoesNotRun(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	// The tool blocks on a channel the test never signals, so a tool that
	// wrongly starts is caught by the entry signal and does not hang the
	// run.
	t.Cleanup(func() { close(release) })
	tool := &echoTool{
		class: poo.ClassSideEffecting,
		call: func(ctx context.Context) (poo.ToolResult, error) {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return poo.ToolResult{Text: "the effect"}, nil
		},
	}
	sink := &refusingRecordSink{record: "tool.started"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	s := newTestSession(t, testOptions(t, tr, tool, sink))

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	select {
	case <-entered:
		t.Fatal("the tool ran although its tool.started record was refused")
	default:
	}
}

// TestAJobWhoseStartRecordIsRefusedDoesNotRun covers the job path: a
// job whose tool.started record the store refuses never runs its tool,
// and completes with an error result.
func TestAJobWhoseStartRecordIsRefusedDoesNotRun(t *testing.T) {
	// The release is open, so a tool that wrongly starts finishes and the
	// assertion below reports it instead of the run hanging on it.
	tool := pootest.NewJobTool("job", poo.BackgroundAllowed)
	tool.ReleaseAll()
	sink := &refusingRecordSink{record: "tool.started"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{jobCall("one", true)}},
		{text: "done"},
	})
	s := newTestSession(t, jobOptions(t, tr, tool, sink))

	runSession(t, s, "start")
	if got := tool.Calls(); got != 0 {
		t.Fatalf("the job's tool ran %d time(s) after its tool.started record was refused, want 0", got)
	}
	if !s.Broken() {
		t.Error("the session is not broken after the store refused the job's start record")
	}
}

// TestAHookWhoseStartRecordIsRefusedDoesNotRun covers the turn hook: a
// turn.hook record the store refuses leaves the hook unrun.
func TestAHookWhoseStartRecordIsRefusedDoesNotRun(t *testing.T) {
	hook := &recordingTurnHook{}
	sink := &refusingRecordSink{record: "turn.hook"}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if got := hook.Calls(); got != 0 {
		t.Fatalf("the hook ran %d time(s) after its turn.hook record was refused, want 0", got)
	}
}

// TestABeforeSettleHookWhoseStartRecordIsRefusedDoesNotRun covers the
// settle hook: a turn.hook record the store refuses leaves the hook
// unrun on the before_settle path.
func TestABeforeSettleHookWhoseStartRecordIsRefusedDoesNotRun(t *testing.T) {
	hook := &recordingBeforeSettle{}
	sink := &refusingRecordSink{record: "turn.hook"}
	tr := scripted([]fakeResponse{{text: "the answer"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	hook.mu.Lock()
	calls := hook.calls
	hook.mu.Unlock()
	if calls != 0 {
		t.Fatalf("the before_settle hook ran %d time(s) after its turn.hook record was refused, want 0", calls)
	}
}

// TestARefusedSubscriptionLeavesNoActiveInstance covers the feed: an
// instance whose feed.subscribed record the store refuses is rolled back,
// so the session's own state does not call it active and no supervisor
// runs it.
func TestARefusedSubscriptionLeavesNoActiveInstance(t *testing.T) {
	ran := make(chan struct{}, 1)
	runner := func(ctx context.Context, io poo.FeedIO) error {
		ran <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	sink := &refusingRecordSink{record: "feed.subscribed"}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
	})
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, sink),
		poo.WithFeeds(theOptionalFeedSpec(runner, "")))

	res := runSession(t, s, "subscribe")
	if res.Reason != poo.ReasonStoreError {
		t.Errorf("the run settled %q, want store_error", res.Reason)
	}
	if jobs := s.Jobs(); len(jobs) != 0 {
		t.Errorf("the session lists %d job(s) after the refused subscription, want none: %+v", len(jobs), jobs)
	}
	select {
	case <-ran:
		t.Fatal("the feed's supervisor ran although its feed.subscribed record was refused")
	default:
	}
}
