package perm

import (
	"context"
	"github.com/sdougbrown/poo"
	"runtime"
	"testing"
	"time"
)

type sol2ShortAsker struct {
	clock *fakeClock
	fire  bool
}

func (*sol2ShortAsker) Available() bool       { return true }
func (*sol2ShortAsker) Budget() time.Duration { return time.Minute }
func (a *sol2ShortAsker) Ask(context.Context, Ask) (Decision, error) {
	if a.fire {
		a.clock.Fire(time.Minute)
	} else {
		a.clock.mu.Lock()
		a.clock.now = a.clock.now.Add(2 * time.Minute)
		a.clock.mu.Unlock()
	}
	return DecisionAllow, nil
}
func TestExpiredPerAskerApprovalDefers(t *testing.T) {
	clock := newFakeClock()
	c := &chain{askers: []Asker{&sol2ShortAsker{clock: clock}}, timeout: time.Hour, fallback: ActionDeny, clock: clock}
	got := c.ask(context.Background(), Ask{})
	if got.decision != DecisionDeny {
		t.Fatalf("answer after 1m Asker budget (2m elapsed, 1h chain): %+v; want fallback deny", got)
	}
}
func TestReadyPerAskerAnswerCannotBeatExpiredBudget(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)
	allowed := 0
	for i := 0; i < 100; i++ {
		clock := newFakeClock()
		c := &chain{askers: []Asker{&sol2ShortAsker{clock: clock, fire: true}}, timeout: time.Hour, fallback: ActionDeny, clock: clock}
		got := c.ask(reviewYieldContext{context.Background()}, Ask{})
		if got.decision == DecisionAllow {
			allowed++
		}
	}
	if allowed != 0 {
		t.Fatalf("answer and per-Asker timer both ready: allowed %d/100 expired answers", allowed)
	}
}
func TestAskerNamePanicDefers(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AskerName panic escaped permission engine: %v", r)
		}
	}()
	a := &reviewFuncAsker{fn: func(context.Context) (Decision, error) { return DecisionAllow, nil }, name: func() string { panic("name failure") }}
	c := &chain{askers: []Asker{a}, timeout: time.Hour, fallback: ActionDeny, clock: systemClock{}}
	got := c.ask(context.Background(), Ask{})
	if got.decision != DecisionDeny {
		t.Fatalf("panicking name: %+v; want fallback deny", got)
	}
}

type sol2IDReporter struct{ id string }

func (r *sol2IDReporter) Asked(a poo.PermissionAsk)   { r.id = a.AskID }
func (*sol2IDReporter) Answered(poo.PermissionAnswer) {}
func (*sol2IDReporter) ResetRepeats(string)           {}
func sol2HookContext(session string, r poo.PermissionReporter) poo.HookContext {
	return poo.HookContext{Context: context.Background(), SessionID: session, Permissions: r}
}
func sol2CallInfo() poo.ToolCallInfo {
	return poo.ToolCallInfo{Spec: poo.ToolSpec{Name: "bash", Class: poo.ClassSideEffecting}}
}

// One shared Policy counter is monotone across sessions, not reset per session.
func TestSharedPolicyAskIDsDoNotCollide(t *testing.T) {
	p, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, session := range []string{"s1", "s2", "s1", "s2"} {
		reporter := &sol2IDReporter{}
		_, err := p.BeforeTool(sol2HookContext(session, reporter), sol2CallInfo())
		if err != nil {
			t.Fatal(err)
		}
		if seen[reporter.id] {
			t.Fatalf("duplicate %q", reporter.id)
		}
		seen[reporter.id] = true
		t.Logf("session=%s ask_id=%s", session, reporter.id)
	}
}
