package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/exec"
	"github.com/sdougbrown/poo/mcp/manifest"
	"github.com/sdougbrown/poo/pootest"
)

// relaunchLaunch is what one relaunch test launches with: the backoff
// it observes, the stability window it shortens, and the
// startup-failure counter it wires.
type relaunchLaunch struct {
	// backoff is the wait before a relaunch attempt, so a test can
	// observe the wait and keep a window open long enough to read.
	backoff time.Duration
	// stability is how long a generation must run before its exit
	// stops counting against the budget. Zero keeps the client's
	// default, which no test's generation outlives: every kill there
	// spends the budget.
	stability time.Duration
	// failStarts wires the startup-failure counter the test seeds.
	failStarts bool
	// dropSubscribeAfter is the first generation that advertises no
	// resource subscriptions: zero keeps every generation granting,
	// and a relaunch past it runs a server the feed cannot subscribe.
	dropSubscribeAfter int
}

// fixtureStampTool is the fixture's generation-carrying tool.
const fixtureStampTool = "stamp"

// relaunchFixture is one fixture server launched for a relaunch test:
// the group that owns it, the pid file its processes write, the file
// they count their starts in, and the startup-failure counter the test
// may seed.
type relaunchFixture struct {
	group       *Group
	state       *serverState
	pidfile     string
	generations string
	counts      string
}

// launchRelaunch starts the fixture server the relaunch tests kill and
// bring back, launched with what the test wants to observe.
func launchRelaunch(t *testing.T, launch relaunchLaunch) *relaunchFixture {
	t.Helper()
	binary := buildFixture(t, "server")
	dir := t.TempDir()
	f := &relaunchFixture{
		pidfile:     filepath.Join(dir, "fixture.pid"),
		generations: filepath.Join(dir, "fixture.generations"),
	}
	env := map[string]manifest.Template{
		"POO_FIXTURE_PIDFILE":    template(t, f.pidfile),
		"POO_FIXTURE_GENERATION": template(t, f.generations),
	}
	if launch.failStarts {
		f.counts = filepath.Join(dir, "fail-starts")
		env["POO_FIXTURE_FAIL_STARTS"] = template(t, f.counts)
	}
	if launch.dropSubscribeAfter > 0 {
		env["POO_FIXTURE_DROP_SUBSCRIBE"] = template(t, strconv.Itoa(launch.dropSubscribeAfter))
	}
	g, err := Launch(context.Background(), LaunchOptions{
		SessionID: "s-42",
		Dir:       dir,
		HostEnv: func(name string) (string, bool) {
			if name == "FIXTURE" {
				return binary, true
			}
			return exec.Lookup(os.Environ())(name)
		},
		RelaunchBackoff:   launch.backoff,
		RelaunchStability: launch.stability,
		Servers: []manifest.Server{{
			Name:    "fixture",
			Command: template(t, "${FIXTURE}"),
			Env:     env,
		}},
	})
	if err != nil {
		t.Fatalf("launching the fixture: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	f.group = g
	f.state = g.state("fixture")
	return f
}

// pid returns the pid the running process wrote, once it wrote one.
func (f *relaunchFixture) pid(t *testing.T) int {
	t.Helper()
	return pootest.FixturePid(t, f.pidfile)
}

// seedStarts stores how many further launches of this server fail
// before they speak.
func (f *relaunchFixture) seedStarts(t *testing.T, left int) {
	t.Helper()
	if err := os.WriteFile(f.counts, []byte(fmt.Sprintf("%d", left)), 0o600); err != nil {
		t.Fatalf("seeding the startup failures: %v", err)
	}
}

// kill ends the running process without answering the call, so the
// watcher is the one that sees the exit.
func (f *relaunchFixture) kill(t *testing.T) {
	t.Helper()
	if _, err := f.group.CallTool(context.Background(), "fixture/exit", json.RawMessage(`{}`)); err == nil {
		t.Fatal("the call to the exiting tool was answered")
	}
}

// tools returns what the server offers the search now, and fails when
// the group no longer holds it at all.
func (f *relaunchFixture) tools(t *testing.T) []Tool {
	t.Helper()
	tools, ok := f.group.Tools("fixture")
	if !ok {
		t.Fatal("the group holds no server named fixture")
	}
	return tools
}

// stamp returns the description of the fixture's generation-carrying
// tool: the value a listing — and so a search — reads from the server
// the group runs now.
func (f *relaunchFixture) stamp(t *testing.T) string {
	t.Helper()
	for _, tool := range f.tools(t) {
		if tool.Name == fixtureStampTool {
			return tool.Description
		}
	}
	t.Fatalf("the fixture listed no %s tool", fixtureStampTool)
	return ""
}

// closeGroup closes the group from its own goroutine and fails the
// test when the close does not return: the production close waits for
// every watcher, so a test that waits on it without a bound hangs
// instead of failing.
func closeGroup(t *testing.T, g *Group) error {
	t.Helper()
	closed := make(chan error, 1)
	go func() { closed <- g.Close() }()
	select {
	case err := <-closed:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the close never returned")
		return nil
	}
}

// waitFile waits until the path exists.
func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitNewPid waits until the pid file holds a process's pid other than
// the one it held, and returns the new one.
func (f *relaunchFixture) waitNewPid(t *testing.T, old int) int {
	t.Helper()
	return pootest.WaitNewPid(t, f.pidfile, old)
}

// waitPhase waits until the server's phase is the one named and
// returns the snapshot that carries it.
func (f *relaunchFixture) waitPhase(t *testing.T, want serverPhase) serverSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		snap := f.state.snapshot()
		if snap.phase == want {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture never read %s; it reads %s", want, snap.phase)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitGeneration waits until the server runs the generation the test
// named: a relaunch installs it and moves the server to ready.
func (f *relaunchFixture) waitGeneration(t *testing.T, want int) serverSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		snap := f.state.snapshot()
		if snap.phase == phaseReady && snap.generation == want {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fixture never ran generation %d; it runs generation %d, %s", want, snap.generation, snap.phase)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// pidStays reports whether the pid file still holds the pid it held,
// watched for the window a relaunch would have written inside.
func (f *relaunchFixture) pidStays(t *testing.T, old int) bool {
	t.Helper()
	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		if pid, ok := pootest.ReadPid(f.pidfile); !ok || pid != old {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRelaunchBringsTheServerBackOnANewGeneration covers the cycle a
// session survives: the process ends mid-session, its generation is
// retired — the process reaped, so no zombie is left — a relaunch
// follows the backoff, and the call and the tool listing read the new
// generation.
func TestRelaunchBringsTheServerBackOnANewGeneration(t *testing.T) {
	const backoff = 200 * time.Millisecond
	f := launchRelaunch(t, relaunchLaunch{backoff: backoff})
	old := f.pid(t)

	// The listing is a generation's own: the fixture's stamp carries
	// the count of its starts, so the offering names the process that
	// listed it.
	if got := f.stamp(t); !strings.Contains(got, "generation 1") {
		t.Fatalf("the first server's stamp reads %q, want its first generation", got)
	}

	// The killed call is not the subject; the exit is. The relaunch
	// waits its backoff first, so a new process cannot appear before
	// the wait has passed.
	started := time.Now()
	f.kill(t)
	fresh := f.waitNewPid(t, old)
	if elapsed := time.Since(started); elapsed < backoff {
		t.Errorf("the relaunch came after %v, before its %v backoff", elapsed, backoff)
	}
	// The old process is gone and reaped: a zombie is a process the
	// group still owns. The new one is a process of its own.
	if fresh == old {
		t.Error("the relaunch kept the killed process's pid")
	}
	pootest.WaitGone(t, "killed fixture", old)

	// The server is ready on its second generation, and a call
	// succeeds on the one it runs now.
	snap := f.waitPhase(t, phaseReady)
	if snap.generation != 2 {
		t.Errorf("the server installed %d generations, want 2", snap.generation)
	}
	res, err := f.group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("calling the relaunched server: %v", err)
	}
	var answer struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &answer); err != nil {
		t.Fatalf("the relaunched echo's answer is not a report: %v\n%s", err, res.Text[0])
	}
	if answer.Text != "hi" {
		t.Errorf("the relaunched echo answered %q, want hi", answer.Text)
	}
	// The listing the search reads is the new generation's own: the
	// tools are the ones this process listed, and the stamp says which
	// process that is.
	tools := f.tools(t)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"echo", "push", "exit", "stamp"} {
		if !contains(names, want) {
			t.Errorf("the relaunched server listed no %q tool: %q", want, names)
		}
	}
	if got := f.stamp(t); !strings.Contains(got, "generation 2") {
		t.Errorf("the relaunched server's stamp reads %q, want its second generation", got)
	}
}

// TestRelaunchCallsWhileTheServerIsDownReportIt covers the down
// window: a call that arrives between the process's end and its
// relaunch reports ErrServerDown, so it never leaves the session, and
// it is never retried.
func TestRelaunchCallsWhileTheServerIsDownReportIt(t *testing.T) {
	// The backoff is short enough that a close which failed to wake the
	// watcher would start its relaunch inside the window the close
	// asserts through, and the close itself cancels the relaunch the
	// test never waits for.
	f := launchRelaunch(t, relaunchLaunch{backoff: 100 * time.Millisecond})
	old := f.pid(t)
	f.kill(t)

	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := f.group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{}`))
		if errors.Is(err, ErrServerDown) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the call during the down window returned %v, want ErrServerDown", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The call reports the server down before the watcher retires the
	// generation, and the close reads the phase: it waits for the
	// retirement here, so the pending relaunch it wins over is the one
	// the test means.
	f.waitPhase(t, phaseDown)
	if err := closeGroup(t, f.group); err != nil {
		t.Fatalf("closing the group: %v", err)
	}
	pootest.WaitGone(t, "killed fixture", old)
	if !f.pidStays(t, old) {
		t.Error("a relaunch followed the close")
	}
}

// TestTheLateWaitOfARetiredGenerationIsIgnored covers the return the
// close makes: a close ends the running generation — the watcher's
// wait still hangs on it — and the wait's return arrives after the
// generation was retired. The watcher ignores it: no relaunch starts
// for a session that is ending, and the close returns before any
// could.
func TestTheLateWaitOfARetiredGenerationIsIgnored(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 100 * time.Millisecond})
	old := f.pid(t)

	closed := make(chan error, 1)
	go func() { closed <- f.group.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("closing the group: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the close waited on a relaunch a retired generation asked for")
	}
	pootest.WaitGone(t, "closed fixture", old)
	if !f.pidStays(t, old) {
		t.Error("the retired generation's return started a relaunch")
	}
}

// TestRelaunchStopsAfterItsBudget covers the bounded failure: a server
// whose launches keep failing is failed for good once the budget is
// spent. Its calls report the server down, it offers no tool a search
// could name, and a resource feed's next attempt returns the fault that
// suspends the feed, so the feed stops instead of restarting against a
// server that will not come back.
func TestRelaunchStopsAfterItsBudget(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 10 * time.Millisecond, failStarts: true})
	old := f.pid(t)
	// The counter is seeded after the first launch, so the session was
	// built on a server that started, and it holds more failures than
	// the budget does: the kill and the launches that follow are
	// certain to spend it whatever else counts.
	f.seedStarts(t, relaunchBudget)
	f.kill(t)
	f.waitPhase(t, phaseFailed)
	pootest.WaitGone(t, "killed fixture", old)

	if _, err := f.group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{}`)); !errors.Is(err, ErrServerDown) {
		t.Errorf("the call on a failed server returned %v, want ErrServerDown", err)
	}
	// A failed server reports down whatever tool the call names: its
	// dead generation is still the one installed, so a tool that
	// generation never held is the server's own down state, not an
	// unknown tool for the model to go searching for.
	if _, err := f.group.CallTool(context.Background(), "fixture/nope", json.RawMessage(`{}`)); !errors.Is(err, ErrServerDown) {
		t.Errorf("the call for a tool the dead generation never held returned %v, want ErrServerDown", err)
	}
	// The group still answers for the server, and a failed one offers
	// nothing: the search stops naming tools a call would refuse.
	if tools, ok := f.group.Tools("fixture"); !ok {
		t.Error("the failed server left the group")
	} else if len(tools) != 0 {
		t.Errorf("the failed server still offers %d tools, want none", len(tools))
	}

	run := ResourceFeed(f.group, "fixture", inboxURI)
	_, io, _ := newFeedIO()
	err := run(context.Background(), io)
	var fault *poo.FeedFault
	if !errors.As(err, &fault) {
		t.Fatalf("the feed's attempt on a failed server returned %v, want a feed fault", err)
	}
	if fault.Code != poo.FeedFaultServer {
		t.Errorf("the feed fault's code is %q, want %q", fault.Code, poo.FeedFaultServer)
	}
	want := fmt.Sprintf("server fixture stopped after %d relaunches", relaunchBudget)
	if fault.Message != want {
		t.Errorf("the feed fault's message is %q, want %q", fault.Message, want)
	}
}

// TestAResourceFeedAttemptOnADownServerNamesTheDown covers the down
// branch: an attempt that starts while the server is down — the
// process has ended and the relaunch has not brought it back — ends
// naming the server down, so the feed's restart policy tries again
// instead of suspending the feed the way a fault would.
func TestAResourceFeedAttemptOnADownServerNamesTheDown(t *testing.T) {
	// The backoff outlives the attempt: the server is still down when
	// the attempt runs, as it is in production, where the relaunch
	// backoffs outlive the feed's retry interval.
	f := launchRelaunch(t, relaunchLaunch{backoff: 2 * time.Second})
	f.kill(t)
	f.waitPhase(t, phaseDown)

	run := ResourceFeed(f.group, "fixture", inboxURI)
	_, io, _ := newFeedIO()
	err := run(context.Background(), io)
	var fault *poo.FeedFault
	if errors.As(err, &fault) {
		t.Fatalf("the feed's attempt on a down server returned the fault %v: it would suspend the feed", err)
	}
	if !errors.Is(err, ErrServerDown) {
		t.Fatalf("the feed's attempt on a down server returned %v, want ErrServerDown", err)
	}
}

// TestRelaunchRecoversAfterFailedStarts covers the other side of the
// budget: a server whose launches fail twice — the count is spent
// without reaching it — comes back on its third, and the session's
// calls read the generation that made it.
func TestRelaunchRecoversAfterFailedStarts(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 10 * time.Millisecond, failStarts: true})
	old := f.pid(t)
	f.seedStarts(t, 2)
	f.kill(t)

	fresh := f.waitNewPid(t, old)
	snap := f.waitPhase(t, phaseReady)
	if snap.generation != 2 {
		t.Errorf("the server installed %d generations, want 2", snap.generation)
	}
	res, err := f.group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{"text":"back"}`))
	if err != nil {
		t.Fatalf("calling the recovered server: %v", err)
	}
	var answer struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.Text[0]), &answer); err != nil {
		t.Fatalf("the recovered echo's answer is not a report: %v\n%s", err, res.Text[0])
	}
	if answer.Text != "back" {
		t.Errorf("the recovered echo answered %q, want back", answer.Text)
	}
	pootest.WaitGone(t, "killed fixture", old)
	if fresh == old {
		t.Error("the relaunch kept the killed process's pid")
	}
}

// TestAResourceFeedSubscribesAgainOnTheNewGeneration covers the feed
// across a relaunch: the attempt the server's exit ended does not
// carry into the new generation — its registrations and its
// subscription were the retired generation's — and the feed's next
// attempt, the one its restart policy would start, subscribes on the
// generation the group runs then and delivers what arrived after the
// relaunch.
func TestAResourceFeedSubscribesAgainOnTheNewGeneration(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 20 * time.Millisecond})
	run := ResourceFeed(f.group, "fixture", inboxURI)
	records, io, _ := newFeedIO()
	cancel, done := runFeed(t, run, io)
	defer cancel()

	if _, err := f.group.CallTool(context.Background(), "fixture/push", json.RawMessage(`{"text":"before the exit"}`)); err != nil {
		t.Fatalf("pushing before the exit: %v", err)
	}
	if got := records.waitEmit(t, "the message before the exit"); got != "before the exit" {
		t.Errorf("the first delivery is %q", got)
	}

	old := f.pid(t)
	f.kill(t)
	// The attempt ends with the generation it subscribed to, not with
	// a failed read of the process that is gone.
	if err := joinFeed(t, done); err == nil {
		t.Fatal("the attempt over the killed server succeeded")
	}
	f.waitNewPid(t, old)
	f.waitPhase(t, phaseReady)

	// The feed's next attempt registers on the new generation: what
	// the relaunched server queues arrives, and the first read is the
	// subscription's own again.
	cancel2, done2 := runFeed(t, run, io)
	defer cancel2()
	if _, err := f.group.CallTool(context.Background(), "fixture/push", json.RawMessage(`{"text":"after the relaunch"}`)); err != nil {
		t.Fatalf("pushing after the relaunch: %v", err)
	}
	if got := records.waitEmit(t, "the message after the relaunch"); got != "after the relaunch" {
		t.Errorf("the delivery after the relaunch is %q", got)
	}
	cancel2()
	if err := joinFeed(t, done2); !errors.Is(err, context.Canceled) {
		t.Errorf("the second attempt returned %v, want its context's error", err)
	}
	pootest.WaitGone(t, "killed fixture", old)
}

// TestRelaunchACrashLoopReachesFailed covers the budget a server spends
// by dying at once: a generation that ends inside the stability window
// counts the way a launch that failed does, so a server that starts and
// then exits reaches failed instead of relaunching for the session's
// whole life.
func TestRelaunchACrashLoopReachesFailed(t *testing.T) {
	// The stability window is the client's default, longer than any
	// generation here lives, so every crash spends the budget.
	f := launchRelaunch(t, relaunchLaunch{backoff: 10 * time.Millisecond})
	for crash := 1; crash <= relaunchBudget; crash++ {
		f.waitGeneration(t, crash)
		f.kill(t)
	}
	f.waitPhase(t, phaseFailed)
	if _, err := f.group.CallTool(context.Background(), "fixture/echo", json.RawMessage(`{}`)); !errors.Is(err, ErrServerDown) {
		t.Errorf("the call on the crash-looping server returned %v, want ErrServerDown", err)
	}
	if tools := f.tools(t); len(tools) != 0 {
		t.Errorf("the failed server still offers %d tools, want none", len(tools))
	}
}

// TestRelaunchClearsTheBudgetAfterAStableGeneration covers the count's
// other end: a generation that runs past the stability window clears
// the failures before it, so a server that worked and then crashed is
// given the whole budget again rather than failing on the next
// stumble.
func TestRelaunchClearsTheBudgetAfterAStableGeneration(t *testing.T) {
	const stability = 100 * time.Millisecond
	f := launchRelaunch(t, relaunchLaunch{backoff: 10 * time.Millisecond, stability: stability, failStarts: true})
	// Two launches fail before the third brings the server back, so the
	// recovery leaves three failures behind it.
	f.seedStarts(t, 2)
	f.kill(t)
	f.waitGeneration(t, 2)

	// The recovered generation lives past the window: its crash is the
	// crash of a server that works, and it clears the count. The wait
	// is the test's own subject — nothing observable says a process has
	// lived long enough — so it is the one clock here.
	time.Sleep(3 * stability)
	killed := f.pid(t)

	// One short of the budget: a count that was cleared comes back on
	// the fifth attempt, and one carried over from the recovery would
	// fail the server after two.
	f.seedStarts(t, relaunchBudget-1)
	f.kill(t)
	f.waitGeneration(t, 3)
	pootest.WaitGone(t, "killed fixture", killed)
}

// TestRelaunchTheBackoffDoublesThroughTheFailedPhase pins the doubling:
// each failed launch waits twice the last, so a server whose launches
// keep failing reaches the failed phase no sooner than the waits sum.
func TestRelaunchTheBackoffDoublesThroughTheFailedPhase(t *testing.T) {
	const (
		backoff   = 200 * time.Millisecond
		stability = 100 * time.Millisecond
	)
	f := launchRelaunch(t, relaunchLaunch{backoff: backoff, stability: stability, failStarts: true})
	f.pid(t)
	// The generation lives past the stability window, so the kill is a
	// stumble that clears the count: every wait that follows belongs
	// to one failed launch, starting at the seed's own backoff.
	time.Sleep(3 * stability)
	f.seedStarts(t, relaunchBudget)

	started := time.Now()
	f.kill(t)
	f.waitPhase(t, phaseFailed)
	// 200+400+800+1600+3200: one wait per failed launch, each twice
	// the last. The bound is one-directional, so a loaded machine can
	// only make it longer, never flake.
	if elapsed := time.Since(started); elapsed < 6200*time.Millisecond {
		t.Errorf("reaching the failed phase took %v, want at least the doubling waits' 6.2s", elapsed)
	}
}

// TestAResourceFeedOnARelaunchedGenerationWithoutSubscriptionsFaults
// covers the capability a relaunch can take away: a generation whose
// initialize result advertises no resource subscriptions cannot grant
// the subscription, so the attempt ends with the fault that suspends
// the feed instead of the plain refusal the restart policy would
// restart forever.
func TestAResourceFeedOnARelaunchedGenerationWithoutSubscriptionsFaults(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 20 * time.Millisecond, dropSubscribeAfter: 2})
	run := ResourceFeed(f.group, "fixture", inboxURI)
	records, io, _ := newFeedIO()
	cancel, done := runFeed(t, run, io)
	defer cancel()
	if _, err := f.group.CallTool(context.Background(), "fixture/push", json.RawMessage(`{"text":"before the exit"}`)); err != nil {
		t.Fatalf("pushing before the exit: %v", err)
	}
	if got := records.waitEmit(t, "the message before the exit"); got != "before the exit" {
		t.Errorf("the first delivery is %q", got)
	}

	old := f.pid(t)
	f.kill(t)
	if err := joinFeed(t, done); err == nil {
		t.Fatal("the attempt over the killed server succeeded")
	}
	f.waitNewPid(t, old)
	f.waitGeneration(t, 2)

	// The relaunched generation advertises no resource subscriptions:
	// the next attempt ends with the fault, and the feed suspends
	// rather than restart against a server that cannot grant it.
	err := run(context.Background(), io)
	var fault *poo.FeedFault
	if !errors.As(err, &fault) {
		t.Fatalf("the feed's attempt on the relaunched generation returned %v, want a feed fault", err)
	}
	if fault.Code != poo.FeedFaultServer {
		t.Errorf("the feed fault's code is %q, want %q", fault.Code, poo.FeedFaultServer)
	}
	want := `server "fixture" advertises no resource subscriptions`
	if fault.Message != want {
		t.Errorf("the feed fault's message is %q, want %q", fault.Message, want)
	}
}

// TestRelaunchACallInFlightWhenTheServerEndsReportsIt covers the call
// the generation's end catches: a call the server holds when its
// process ends reports ErrServerDown — the server it reached is gone —
// and not the transport error the connection's end leaves behind.
func TestRelaunchACallInFlightWhenTheServerEndsReportsIt(t *testing.T) {
	f := launchRelaunch(t, relaunchLaunch{backoff: 10 * time.Millisecond})
	dir := filepath.Dir(f.pidfile)
	entered := filepath.Join(dir, "hold-entered")
	release := filepath.Join(dir, "hold-release")

	answered := make(chan error, 1)
	go func() {
		_, err := f.group.CallTool(context.Background(), "fixture/hold",
			json.RawMessage(fmt.Sprintf(`{"entered":%q,"release":%q}`, entered, release)))
		answered <- err
	}()
	// The call is inside the server once the tool wrote its file. The
	// exit that follows cuts it off, and the release it waits for never
	// comes.
	waitFile(t, entered)
	f.kill(t)
	select {
	case err := <-answered:
		if !errors.Is(err, ErrServerDown) {
			t.Errorf("the call the server's end caught returned %v, want ErrServerDown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the call in flight never returned after its server ended")
	}
}

// TestRelaunchTheOfferingHoldsOnlyAReadyServer covers what a search is
// offered: a server that is down offers nothing, so no search names a
// tool a call would refuse, and the tools come back with the server
// that lists them.
func TestRelaunchTheOfferingHoldsOnlyAReadyServer(t *testing.T) {
	// Two launches fail before the third brings the server back: the
	// down window the read happens in lasts several backoffs, and the
	// recovery still fits inside the budget.
	f := launchRelaunch(t, relaunchLaunch{backoff: 100 * time.Millisecond, failStarts: true})
	f.seedStarts(t, 2)
	old := f.pid(t)
	if tools := f.tools(t); len(tools) == 0 {
		t.Fatal("the ready server offered no tools")
	}

	f.kill(t)
	f.waitPhase(t, phaseDown)
	if tools := f.tools(t); len(tools) != 0 {
		t.Errorf("the down server offered %d tools, want none", len(tools))
	}

	f.waitGeneration(t, 2)
	if tools := f.tools(t); len(tools) == 0 {
		t.Error("the relaunched server offered no tools")
	}
	if got := f.stamp(t); !strings.Contains(got, "generation 2") {
		t.Errorf("the offered stamp reads %q, want the second generation", got)
	}
	pootest.WaitGone(t, "killed fixture", old)
}
