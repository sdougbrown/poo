package poo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

	waitForSink(t, sink, "the feed's attempt to start", func() bool { return len(feedStarts(sink)) >= 1 })
	close(fire)

	// The report wakes the session although the feed holds its inputs, and
	// the model reads it as the feed's own message.
	waitForSink(t, sink, "the fault report to reach the chat model", func() bool {
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
	waitForSink(t, sink, "the attempt's end to be recorded", func() bool { return len(feedEnds(sink)) >= 1 })
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

// TestALongScopeStillWritesTheFaultLog covers a fault on an instance whose
// label is far longer than a file name component may be: the log's name is
// bounded, and the report names a file that is there to read.
func TestALongScopeStillWritesTheFaultLog(t *testing.T) {
	scope := map[string]string{"url": "https://example.test/" + strings.Repeat("a", 2100)}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", scope)}},
		{text: "the answer"},
	})
	sink := &recordingSink{}
	dir := t.TempDir()
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SessionDir = dir
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(faultedFeed, poo.OnIdleHold)))
	debugTrack(s)

	runSession(t, s, "start the feed")
	waitForSink(t, sink, "the fault report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, _, _ := deliveredMessage(sink)
	path := faultLogPath(t, text)
	// The scope value is over two thousand bytes, and the label carries it
	// in full: a name taken from the label would not fit a component.
	if got := filepath.Base(path); len(got) >= 255 {
		t.Errorf("the fault log's name is %d bytes, want under the filesystem's 255-byte limit", len(got))
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("the fault log is %q, want it beside the session's files in %q", path, dir)
	}
	stderr, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fault log the report names: %v", err)
	}
	if string(stderr) != "the filter could not read the record\n" {
		t.Errorf("the fault log holds %q, want the filter's stderr", stderr)
	}
	// The report still names the whole label, so the remedy names the scope
	// the instance runs with rather than the bounded file name.
	if !strings.Contains(text, scope["url"]) {
		t.Errorf("the report does not name the scope the instance runs with")
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run the report landed in: %v", err)
	}
}

// TestAFaultReportWithoutItsLogSaysSo covers a fault whose diagnostics
// cannot be written: the report names no log file, because there is none to
// name, and says the diagnostics are not on disk.
func TestAFaultReportWithoutItsLogSaysSo(t *testing.T) {
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "the answer"}}), &echoTool{}, sink)
	// A file where the directory should be: every name under it fails to
	// open, whatever the name is.
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("writing the file that stands where the directory should be: %v", err)
	}
	opts.SessionDir = filepath.Join(file, "logs")
	s := newTestSession(t, opts, poo.WithFeeds(poo.FeedSpec{
		Name:   "mail",
		OnIdle: poo.OnIdleWake,
		Run:    faultedFeed,
	}))

	waitForSink(t, sink, "the fault report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, _, _ := deliveredMessage(sink)
	if strings.Contains(text, ` log="`) {
		t.Errorf("the report %q names a log file that was not written", text)
	}
	if !strings.Contains(text, "could not be written") {
		t.Errorf("the report %q does not say the diagnostics are missing", text)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run the report landed in: %v", err)
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
	debugTrack(s)

	runSession(t, s, "start the feed")
	// The first attempt failed and the session delivered the report, which
	// names the call that starts the feed again with the scope it runs
	// with.
	waitForSink(t, sink, "the fault report to reach the chat model", func() bool {
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
	waitForSink(t, sink, "the instance's second attempt", func() bool { return len(feedStarts(sink)) >= 2 })
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
	waitForSink(t, sink, "the instance's next attempt", func() bool { return len(feedStarts(sink)) >= 2 })
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
	// The firebreak's two seams choreograph the race the fold covers:
	// the first report's delivery waits for the test, the second fault
	// folds into the report in flight, and the delivery writes the
	// joined count.
	folded := make(chan struct{})
	deliveryHold := make(chan struct{})
	deliveryReleased := make(chan struct{})
	restore := s.SetJobRaceHooks(&poo.JobRaceHooks{
		AfterFaultFold:      func() { close(folded) },
		BeforeFaultDelivery: func() { close(deliveryHold); <-deliveryReleased },
	})
	defer restore()

	// The run is open in a tool, so the report a fault queues waits for
	// the next turn end.
	if _, err := s.Prompt(context.Background(), "hold the run open"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-gate.started
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/repo"}); err != nil || !started {
		t.Fatalf("subscribing to the feed: started=%v err=%v", started, err)
	}
	waitForSink(t, sink, "the first fault's end", func() bool { return len(feedEnds(sink)) >= 1 })
	// The first report must be queued before the re-arm: a second arm that
	// fails before it is would queue its own report, and the fold the test
	// covers would miss.
	waitForSink(t, sink, "the first fault report to be queued", func() bool {
		for _, rec := range sink.allRecords() {
			if r, ok := rec.(*poo.InputQueuedRecord); ok && strings.Contains(r.Text, ` fault="`) {
				return true
			}
		}
		return false
	})
	// The record is written before the report joins the job queue, and the
	// fold looks in the queue: wait for the queue so a fault that arrives
	// now folds in rather than queueing one of its own.
	waitFor(t, "the first report to join the job queue", func() bool {
		return s.QueuedJobResults() >= 1
	})
	// The run can be released now: the drain takes the queued report
	// for delivery and waits, so the second fault folds into the report
	// in flight rather than racing it.
	close(gate.release)
	<-deliveryHold
	// A second arm fails again while the first report is in flight: it
	// joins that report rather than queueing one of its own.
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", map[string]string{"url": "https://example.test/repo"}); err != nil || !started {
		t.Fatalf("re-arming the feed: started=%v err=%v", started, err)
	}
	waitForSink(t, sink, "the second fault's end", func() bool { return len(feedEnds(sink)) >= 2 })
	// The fold ran, so the in-flight report counts the second fault:
	// the delivery can write the joined report now.
	<-folded
	close(deliveryReleased)
	waitForSink(t, sink, "the joined report to reach the chat model", func() bool {
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

// faultReportTexts returns the texts of the fault reports the session
// queued, in order.
func faultReportTexts(sink *recordingSink) []string {
	var out []string
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.InputQueuedRecord); ok && r.Mode == poo.InputModeFeed && strings.Contains(r.Text, ` fault="`) {
			out = append(out, r.Text)
		}
	}
	return out
}

// waitForNoFaultReservation waits until the instance's fault report is queued
// and its reservation retired. The report path writes the record before it
// retires the reservation. A record in the sink therefore does not say the
// attempt's report is done. A second report for that attempt would fold into
// the one still reserved.
func waitForNoFaultReservation(t *testing.T, s *poo.Session, instance string) {
	t.Helper()
	waitFor(t, "the fault report's reservation to be retired", func() bool {
		_, ok := poo.PendingFaultReportAttempt(s, instance)
		return !ok
	})
}

// TestAStaleSuspensionReportIsDropped covers the race between a suspension
// and a re-subscribe: an instance that was started again is running, so the
// old supervisor's report is stale and is not queued. A fault on the running
// attempt still reports on its own.
func TestAStaleSuspensionReportIsDropped(t *testing.T) {
	fire := make(chan struct{})
	var attempts atomic.Int32
	run := func(ctx context.Context, io poo.FeedIO) error {
		if attempts.Add(1) == 1 {
			return theFeedFault()
		}
		select {
		case <-fire:
			return theFeedFault()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{{text: "the answer"}}), &echoTool{}, sink)
	opts.SessionDir = t.TempDir()
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(run, poo.OnIdleHold)))
	scope := map[string]string{"url": "https://example.test/repo"}

	origin, started, err := poo.SubscribeFeed(s, "git-feedback", scope)
	if err != nil || !started {
		t.Fatalf("subscribing to the feed: started=%v err=%v", started, err)
	}
	waitForSink(t, sink, "the first fault's report", func() bool { return len(faultReportTexts(sink)) >= 1 })
	// The assertion below reads the reservation, and the report path retires
	// it after the record lands. Wait for the retire: otherwise the first
	// attempt's leftover reads as one the stale report took.
	waitForNoFaultReservation(t, s, origin)

	// The instance is started again after its suspension, and the second
	// attempt runs.
	if _, started, err := poo.SubscribeFeed(s, "git-feedback", scope); err != nil || !started {
		t.Fatalf("re-arming the feed: started=%v err=%v", started, err)
	}
	waitForSink(t, sink, "the instance's second attempt", func() bool { return len(feedStarts(sink)) >= 2 })
	// The record is written before the instance's state moves to running,
	// and the stale check reads the state: wait for the listing so the
	// report for the first attempt is seen as stale.
	waitFor(t, "the re-armed feed to be listed running", func() bool {
		return listedAs(s, origin) == poo.JobRunning
	})

	// The first attempt's supervisor reports after the re-arm. The instance
	// is running, so there is nothing to remedy: the report is dropped.
	poo.ReportFeedFault(s, origin, 1, theFeedFault())
	if got := faultReportTexts(sink); len(got) != 1 {
		t.Fatalf("the stale report queued %d fault reports, want the first alone", len(got))
	}
	if _, ok := poo.PendingFaultReportAttempt(s, origin); ok {
		t.Error("the stale report took a reservation")
	}
	if got := listedAs(s, origin); got != poo.JobRunning {
		t.Errorf("the re-armed feed is listed %q, want %q", got, poo.JobRunning)
	}

	// The running attempt's own fault is reported: the stale path left no
	// reservation to fold into, and no stale report to answer.
	close(fire)
	waitForSink(t, sink, "the second fault's report", func() bool { return len(faultReportTexts(sink)) >= 2 })
	reports := faultReportTexts(sink)
	if strings.Contains(reports[1], "more arrived") {
		t.Errorf("the second report %q folded into the stale one", reports[1])
	}

	// A stopped instance has nothing to remedy either, even for the attempt
	// that suspended it.
	if err := s.CancelJob(origin); err != nil {
		t.Fatalf("stopping the suspended feed: %v", err)
	}
	poo.ReportFeedFault(s, origin, 2, theFeedFault())
	if got := faultReportTexts(sink); len(got) != 2 {
		t.Errorf("the report for a stopped instance queued %d reports, want 2", len(got))
	}
}

// faultReportGate holds the appends of a feed's fault reports, so a test can
// act while a report is accepted and not yet queued.
type faultReportGate struct {
	*recordingSink
	mu      sync.Mutex
	blocked int
	once    []sync.Once
	// entered receives the index of each append that is held.
	entered chan int
	gates   []chan struct{}
}

// newFaultReportGate returns a sink that holds the first n appends.
func newFaultReportGate(t *testing.T, n int) *faultReportGate {
	t.Helper()
	g := &faultReportGate{
		recordingSink: &recordingSink{},
		entered:       make(chan int, n),
		once:          make([]sync.Once, n),
		gates:         make([]chan struct{}, n),
	}
	for i := range g.gates {
		g.gates[i] = make(chan struct{})
	}
	t.Cleanup(func() {
		for i := range g.gates {
			g.release(i)
		}
	})
	return g
}

// release lets append i go.
func (g *faultReportGate) release(i int) {
	g.once[i].Do(func() { close(g.gates[i]) })
}

// Append holds the appends the gate was built for and keeps the rest.
func (g *faultReportGate) Append(ctx context.Context, rec poo.Record) error {
	if _, ok := rec.(*poo.InputQueuedRecord); ok {
		g.mu.Lock()
		n := g.blocked
		g.blocked++
		g.mu.Unlock()
		if n < len(g.gates) {
			g.entered <- n
			<-g.gates[n]
		}
	}
	return g.recordingSink.Append(ctx, rec)
}

// TestAFaultReportReservationBelongsToItsAttempt covers the reservation's
// owner: the cleanup that retires it names the attempt that took it, so an
// older attempt's cleanup cannot retire the reservation a newer attempt's
// fault took.
func TestAFaultReportReservationBelongsToItsAttempt(t *testing.T) {
	gate := newFaultReportGate(t, 1)
	opts := testOptions(t, scripted([]fakeResponse{{text: "the answer"}}), &echoTool{}, gate)
	opts.SessionDir = t.TempDir()
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(faultedFeed, poo.OnIdleHold)))
	scope := map[string]string{"url": "https://example.test/repo"}

	origin, started, err := poo.SubscribeFeed(s, "git-feedback", scope)
	if err != nil || !started {
		t.Fatalf("subscribing to the feed: started=%v err=%v", started, err)
	}
	<-gate.entered
	// The report is accepted and not yet queued: its reservation is attempt
	// 1's.
	if attempt, ok := poo.PendingFaultReportAttempt(s, origin); !ok || attempt != 1 {
		t.Fatalf("the reservation is attempt %d, %v; want attempt 1", attempt, ok)
	}
	// A newer attempt's cleanup arrives first, and leaves the reservation
	// alone.
	poo.RetireFaultReport(s, origin, 2)
	if attempt, ok := poo.PendingFaultReportAttempt(s, origin); !ok || attempt != 1 {
		t.Fatalf("another attempt's cleanup left the reservation at %d, %v; want attempt 1", attempt, ok)
	}
	// The attempt's own cleanup retires it.
	poo.RetireFaultReport(s, origin, 1)
	if _, ok := poo.PendingFaultReportAttempt(s, origin); ok {
		t.Error("the reservation outlived its attempt's cleanup")
	}
	gate.release(0)
	waitForSink(t, gate.recordingSink, "the report to be queued", func() bool {
		return len(faultReportTexts(gate.recordingSink)) >= 1
	})
}

// TestAServerFaultReportSpeaksForTheServer covers a feed fault whose
// code names the server: the fault carries no filter stderr, so the
// report is not rendered as a filter failure, names no diagnostics
// file, and the fault's own error opens with the server's sentence.
func TestAServerFaultReportSpeaksForTheServer(t *testing.T) {
	sink := &recordingSink{}
	opts := testOptions(t, scripted([]fakeResponse{
		{calls: []poo.ToolCall{subscribeCall("c1", "git-feedback", map[string]string{"url": "https://example.test/repo"})}},
		{text: "the answer"},
	}), &echoTool{}, sink)
	s := newTestSession(t, opts, poo.WithFeeds(theOptionalFeedSpec(
		func(ctx context.Context, io poo.FeedIO) error {
			return &poo.FeedFault{
				Code:    poo.FeedFaultServer,
				Message: `server "mail" is gone and cannot be relaunched`,
			}
		}, poo.OnIdleHold)))

	runSession(t, s, "start the feed")
	waitForSink(t, sink, "the fault report to reach the chat model", func() bool {
		_, _, ok := deliveredMessage(sink)
		return ok
	})
	text, _, _ := deliveredMessage(sink)
	if strings.Contains(text, "The feed's filter failed") {
		t.Errorf("the server fault's report %q renders as a filter failure", text)
	}
	if !strings.Contains(text, `server "mail" is gone and cannot be relaunched`) {
		t.Errorf("the report %q does not carry the server's diagnosis", text)
	}
	if strings.Contains(text, ` log="`) {
		t.Errorf("the report %q names a diagnostics file the server fault never wrote", text)
	}
	if strings.Contains(text, "could not be written") {
		t.Errorf("the report %q claims missing diagnostics where the server left none by design", text)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run the report landed in: %v", err)
	}

	err := (&poo.FeedFault{Code: poo.FeedFaultServer, Message: "gone"}).Error()
	if !strings.HasPrefix(err, "the feed's server failed (server_failed): ") {
		t.Errorf("the server fault's error %q does not open with the server's sentence", err)
	}
}
