package poo_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
)

// The fault: a feed's filter fails closed, and the session suspends the
// feed and wakes itself with the report.

// theFeedFault is the fault a test's runner fails its feed with.
func theFeedFault() *poo.FeedFault {
	return &poo.FeedFault{
		Code:    poo.FeedFaultExit,
		Message: "the filter exited with status 4",
		Stderr:  []byte("the filter could not read the record\n"),
	}
}

// faultingFeed returns a runner that fails with the fault the first time
// it is asked to fire, and waits to be stopped otherwise.
//
// The fault is the runner's own error, so the test drives the session's
// suspension without an executor: the config layer is where a real
// filter's record is turned into one of these.
func faultingFeed(fire <-chan struct{}) poo.FeedRunner {
	return func(ctx context.Context, io poo.FeedIO) error {
		select {
		case <-fire:
			return theFeedFault()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// faultedFeed returns a runner that fails with the fault as soon as the
// attempt starts.
func faultedFeed(ctx context.Context, io poo.FeedIO) error { return theFeedFault() }

// TestAFeedFaultSuspendsTheFeedAndWakesUnderHold covers the fault's two
// halves: the feed does not restart, whatever the restart policy says, and
// the report reaches the session even though the feed holds its inputs.
func TestAFeedFaultSuspendsTheFeedAndWakesUnderHold(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}})
	sink := &recordingSink{}
	dir := t.TempDir()
	fire := make(chan struct{})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionDir = dir
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name: "mail",
		// The restart policy does not apply to a suspension, and the wait
		// is zero so a restart the session did schedule would be an
		// attempt at once.
		Restart: true,
		OnIdle:  poo.OnIdleHold,
		Run:     faultingFeed(fire),
	}))
	restore := poo.SetFeedRestartDelay(s, func(ran, prev time.Duration) time.Duration { return 0 })
	defer restore()

	waitFor(t, "the feed's attempt to start", func() bool { return len(feedStarts(sink)) == 1 })
	close(fire)

	// The report wakes the session although the feed holds its inputs, and
	// the model reads it as the feed's own message.
	waitFor(t, "the fault report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, origin, _ := deliveredMessage(sink)
	if name, ok := poo.FeedName(origin); !ok || name != "mail" {
		t.Errorf("the report's origin is %q, want the feed's", origin)
	}
	if !strings.Contains(text, `fault="`+poo.FeedFaultExit+`"`) {
		t.Errorf("the report %q does not name the fault's code", text)
	}
	// The report names the file the filter's diagnostics went to, and the
	// file holds them: they never ride the input.
	path := faultLogPath(t, text)
	if !strings.HasPrefix(path, dir) {
		t.Errorf("the fault log is %q, want it beside the session's files in %q", path, dir)
	}
	stderr, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fault log: %v", err)
	}
	if string(stderr) != "the filter could not read the record\n" {
		t.Errorf("the fault log holds %q, want the filter's stderr", stderr)
	}
	if strings.Contains(text, "the filter could not read the record") {
		t.Errorf("the report %q carries the filter's stderr", text)
	}
	// One failure, one attempt: the suspension is what the log says.
	waitFor(t, "the attempt's end to be recorded", func() bool { return len(feedEnds(sink)) == 1 })
	ends := feedEnds(sink)
	if ends[0].Outcome != poo.FeedSuspended {
		t.Errorf("the attempt ended %q, want %q", ends[0].Outcome, poo.FeedSuspended)
	}
	if ends[0].RestartInMS != 0 {
		t.Errorf("the attempt scheduled a restart in %dms, want none", ends[0].RestartInMS)
	}
	if n := len(feedStarts(sink)); n != 1 {
		t.Errorf("the feed started %d attempts, want 1", n)
	}
	// The listing says the feed is suspended, so the model can see it is
	// down without asking again.
	suspended := false
	for _, job := range s.Jobs() {
		if job.Tool == poo.FeedToolName && job.Status == poo.JobSuspended {
			suspended = true
		}
	}
	if !suspended {
		t.Errorf("the jobs listing does not show the feed suspended: %+v", s.Jobs())
	}
}

// faultLogPath returns the path the report's log attribute names.
func faultLogPath(t *testing.T, text string) string {
	t.Helper()
	const open = ` log="`
	start := strings.Index(text, open)
	if start < 0 {
		t.Fatalf("the report %q names no log file", text)
	}
	rest := text[start+len(open):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("the report %q has an unterminated log attribute", text)
	}
	return rest[:end]
}

// TestResubscribingASuspendedFeedStartsItAgain covers the re-arm: the
// report names the call that starts the feed again, and that call does.
func TestResubscribingASuspendedFeedStartsItAgain(t *testing.T) {
	const url = "https://example.test/repo"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": url})}},
		{text: "the answer"},
		{calls: []poo.ToolCall{subscribeCall("c2", "git-feedback", map[string]string{"url": url})}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	var attempts atomic.Int32
	spec := theOptionalFeedSpec(func(ctx context.Context, io poo.FeedIO) error {
		if attempts.Add(1) == 1 {
			return theFeedFault()
		}
		<-ctx.Done()
		return ctx.Err()
	}, poo.OnIdleHold)
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionDir = t.TempDir()
	s := newTestSession(t, opts, poo.WithFeeds(spec))

	runSession(t, s, "start the feed")
	// The first attempt failed and the session delivered the report, which
	// names the call that starts the feed again with the scope it runs
	// with.
	waitFor(t, "the fault report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, _, _ := deliveredMessage(sink)
	if !strings.Contains(text, poo.SubscribeFeedToolName) {
		t.Errorf("the report %q does not name %s", text, poo.SubscribeFeedToolName)
	}
	if !strings.Contains(text, `url="`+url+`"`) {
		t.Errorf("the report %q does not name the scope the instance runs with", text)
	}
	// A woken run may still be going, and it holds the session until it
	// settles: the report landed in whichever run was open when it was
	// queued.
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run the report landed in: %v", err)
	}
	// The second call starts the suspended instance again rather than
	// leaving it down.
	runSession(t, s, "start it again")
	waitFor(t, "the instance's second attempt", func() bool { return len(feedStarts(sink)) == 2 })
	result := toolResults(sink, poo.SubscribeFeedToolName)
	if len(result) != 2 || result[1].IsError {
		t.Fatalf("the calls' results are %+v, want two that succeeded", result)
	}
	if !strings.Contains(result[1].Text, "is running") || strings.Contains(result[1].Text, "already running") {
		t.Errorf("the second call says %q, want it to report the instance running again", result[1].Text)
	}
}

// TestCancelStopsASuspendedFeed covers cancel_job on a feed its filter
// suspended: it stops like any other job, and a subscription starts it
// again.
func TestCancelStopsASuspendedFeed(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "the answer"}, {text: "the answer"}, {text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionDir = t.TempDir()
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(faultedFeed, poo.OnIdleHold)))
	scope := map[string]string{"url": "https://example.test/repo"}
	origin, started, err := poo.SubscribeFeed(s, "git-feedback", scope)
	if err != nil || !started {
		t.Fatalf("subscribing to the feed: started=%v err=%v", started, err)
	}
	waitFor(t, "the feed to be listed suspended", func() bool {
		return listedAs(s, origin) == poo.JobSuspended
	})
	if err := s.CancelJob(origin); err != nil {
		t.Fatalf("stopping the suspended feed: %v", err)
	}
	waitFor(t, "the feed to leave the listing", func() bool {
		return listedAs(s, origin) == ""
	})
	// A stopped instance is started by the next subscription, under the
	// next attempt.
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", scope); err != nil || !started {
		t.Fatalf("subscribing to the stopped feed: started=%v err=%v", started, err)
	}
	waitFor(t, "the instance's next attempt", func() bool { return len(feedStarts(sink)) == 2 })
}

// listedAs returns the status the jobs listing gives one job, or the empty
// string when it lists none.
func listedAs(s *poo.Session, id string) string {
	for _, job := range s.Jobs() {
		if job.JobID == id {
			return job.Status
		}
	}
	return ""
}

// TestOneSuspensionIsOneWakeAndCountsWhatJoinedIt covers the firebreak: a
// fault that arrives while the last report is still queued joins it, so the
// session wakes once and the report counts what joined.
func TestOneSuspensionIsOneWakeAndCountsWhatJoinedIt(t *testing.T) {
	gate := newGateTool()
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{gateCall("c1")}}, {text: "the answer"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionDir = t.TempDir()
	opts.Tools = map[string]poo.Tool{gate.Spec().Name: gate}
	opts.ToolOrder = []string{gate.Spec().Name}
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(faultedFeed, poo.OnIdleHold)))

	// The run is open in a tool, so the report a fault queues waits for
	// the next turn end.
	if _, err := s.Prompt(context.Background(), "hold the run open"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-gate.started
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/repo"}); err != nil || !started {
		t.Fatalf("subscribing to the feed: started=%v err=%v", started, err)
	}
	waitFor(t, "the first fault's end", func() bool { return len(feedEnds(sink)) == 1 })
	// The first report must be queued before the re-arm: a second arm that
	// fails before it is would queue its own report, and the fold the test
	// covers would miss.
	waitFor(t, "the first fault report to be queued", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.InputQueuedRecord); ok && strings.Contains(r.Text, ` fault="`) {
				return true
			}
		}
		return false
	})
	// A second arm fails again while the first report is still queued: it
	// joins that report rather than queueing one of its own.
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/repo"}); err != nil || !started {
		t.Fatalf("re-arming the feed: started=%v err=%v", started, err)
	}
	waitFor(t, "the second fault's end", func() bool { return len(feedEnds(sink)) == 2 })

	close(gate.release)
	waitFor(t, "the joined report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, _, _ := deliveredMessage(sink)
	if !strings.Contains(text, "1 more arrived while suspended") {
		t.Errorf("the report %q does not count the fault that joined it", text)
	}
	// One suspension, one report: the two faults queued one input and woke
	// nothing of their own.
	faults := 0
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.InputQueuedRecord); ok && strings.Contains(r.Text, ` fault="`) {
			faults++
		}
	}
	if faults != 1 {
		t.Errorf("the log holds %d fault reports, want one", faults)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
}

// gateCall is a call to the gate tool.
func gateCall(callID string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{})
	return poo.ToolCall{CallID: callID, Name: "gate", Arguments: raw}
}

// gateTool is a tool that keeps a run open until the test releases it.
type gateTool struct {
	// started closes when the tool is called.
	started chan struct{}
	// release lets the call return.
	release chan struct{}
	once    sync.Once
}

// newGateTool returns a gate that has not been called yet.
func newGateTool() *gateTool {
	return &gateTool{started: make(chan struct{}), release: make(chan struct{})}
}

// Spec describes the tool.
func (t *gateTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "gate",
		Description: "a tool a test holds open",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts the call.
func (t *gateTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return gatePrepared{}, nil
}

// Call waits for the test to release it.
func (t *gateTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.once.Do(func() { close(t.started) })
	select {
	case <-t.release:
		return poo.ToolResult{Text: "released"}, nil
	case <-ctx.Done():
		return poo.ToolResult{}, ctx.Err()
	}
}

// gatePrepared is a prepared gate call.
type gatePrepared struct{}

// Summary describes the call.
func (gatePrepared) Summary() string { return "gate" }
