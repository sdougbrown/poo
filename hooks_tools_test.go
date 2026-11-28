package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/tool/grep"
	"github.com/sdougbrown/poo/tool/read"
)

// The read-only probe seam is the one way a hook reaches the tree. The
// hook sees the tool's output and nothing else changes: no call of the
// model's, no record, no permission ask, no doom-loop count.

// probePoint is what one hook point's probe saw.
type probePoint struct {
	// workingDir is the working directory the hook's context carried.
	workingDir string
	// observation is the last probe's answer.
	observation poo.HookToolObservation
	// err is the probe's failure, when it failed.
	err error
}

// probeHook probes the session's read-only tools from every hook point
// and records what each probe saw.
//
// The hook decides nothing: its only effect is the probe.
type probeHook struct {
	// mu guards points, counts and probes.
	mu sync.Mutex
	// points holds each hook point's probe, by point name.
	points map[string]probePoint
	// counts holds how often each hook point probed.
	counts map[string]int
	// probes is how many probes the hook made in all.
	probes int
	// repeat holds the repeat counts each tool call's hook read, in
	// order.
	repeat []poo.RepeatCounts
	// tool is the tool the hook probes.
	tool string
	// args are the arguments it probes with.
	args string
	// repeats is how many probes it makes at one point.
	repeats int
}

// newProbeHook returns a hook that probes one tool from every point.
func newProbeHook(tool, args string) *probeHook {
	return &probeHook{points: map[string]probePoint{}, counts: map[string]int{}, tool: tool, args: args}
}

// probe runs the probes for one point and records the last.
func (h *probeHook) probe(point string, ctx poo.HookContext) {
	repeats := h.repeats
	if repeats < 1 {
		repeats = 1
	}
	if ctx.ReadOnlyTools == nil {
		h.record(point, probePoint{workingDir: ctx.WorkingDir, err: errors.New("the context carries no read-only tool caller")})
		return
	}
	var observation poo.HookToolObservation
	var err error
	for i := 0; i < repeats; i++ {
		observation, err = ctx.ReadOnlyTools.CallReadOnly(ctx, h.tool, json.RawMessage(h.args))
	}
	h.record(point, probePoint{workingDir: ctx.WorkingDir, observation: observation, err: err})
}

// record stores one point's probe.
func (h *probeHook) record(point string, p probePoint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points[point] = p
	h.counts[point]++
	h.probes++
}

// Probes returns how many probes the hook made in all.
func (h *probeHook) Probes() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.probes
}

// Counts returns the repeat counts each tool call's hook read, in order.
func (h *probeHook) Counts() []poo.RepeatCounts {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]poo.RepeatCounts(nil), h.repeat...)
}

// Points returns each hook point's probe, by point name.
func (h *probeHook) Points() map[string]probePoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]probePoint, len(h.points))
	for point, p := range h.points {
		out[point] = p
	}
	return out
}

// points returns the points that probed.
func (h *probeHook) probedPoints() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.points))
	for point := range h.points {
		out = append(out, point)
	}
	return out
}

// SessionStart probes at session start.
func (h *probeHook) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	h.probe("session_start", ctx)
	return poo.StartDecision{}, nil
}

// BeforeRun probes before the run.
func (h *probeHook) BeforeRun(ctx poo.HookContext, _ poo.RunTrigger) (poo.StartDecision, error) {
	h.probe("before_run", ctx)
	return poo.StartDecision{}, nil
}

// BeforeTool probes before the model's call, and records the doom loop's
// counts as the hook sees them.
func (h *probeHook) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	h.probe("before_tool", ctx)
	h.mu.Lock()
	h.repeat = append(h.repeat, poo.RepeatCounts{Exact: info.RepeatCount, Near: info.NearRepeatCount})
	h.mu.Unlock()
	return poo.Allow(), nil
}

// AfterTool probes after the model's call.
func (h *probeHook) AfterTool(ctx poo.HookContext, _ poo.ToolCallInfo, _ poo.ToolResult) (poo.ResultDecision, error) {
	h.probe("after_tool", ctx)
	return poo.Keep(), nil
}

// AfterTurn probes at the turn end.
func (h *probeHook) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	h.probe("after_turn", ctx)
	return poo.TurnDecision{}, nil
}

// BeforeSettle probes before the settle.
func (h *probeHook) BeforeSettle(ctx poo.HookContext, _ poo.Reason) (poo.SettleDecision, error) {
	h.probe("before_settle", ctx)
	return poo.Accept(), nil
}

// policyCounter is a policy slot that counts the calls it was asked
// about.
type policyCounter struct {
	// count is how many calls the slot saw.
	count int
}

// BeforeTool counts the call and allows it.
func (p *policyCounter) BeforeTool(poo.HookContext, poo.ToolCallInfo) (poo.ToolDecision, error) {
	p.count++
	return poo.Allow(), nil
}

// probeOptions builds the options for a session whose tools include a
// probeable read tool and the model's echo tool.
func probeOptions(t *testing.T, tr poo.ChatTransport, sink poo.RecordSink, echo *echoTool, read *namedTool) poo.Options {
	t.Helper()
	opts := testOptions(t, tr, echo, sink)
	opts.Tools["read"] = read
	opts.ToolOrder = []string{"echo", "read"}
	return opts
}

// countEvents returns how many events carry a name.
func countEvents(s *poo.Session, name poo.EventName) int {
	n := 0
	for _, ev := range s.Events() {
		if ev.Name == name {
			n++
		}
	}
	return n
}

// TestHookContextOffersWorkingDirAndAReadOnlyProbe covers the seam:
// every hook point's context names the session's working directory and
// carries a caller that runs the read tool, and a probe leaves the run's
// own records, events, asks and counts untouched.
func TestHookContextOffersWorkingDirAndAReadOnlyProbe(t *testing.T) {
	read := &namedTool{name: "read", echoTool: echoTool{result: "the tree's files"}}
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	sink := &recordingSink{}
	hook := newProbeHook("read", `{"path":"go.mod"}`)
	policy := &policyCounter{}
	opts := probeOptions(t, tr, sink, &echoTool{result: "the effect"}, read)
	opts.WorkingDir = t.TempDir()
	opts.SessionStart = []poo.SessionStartHook{hook}
	opts.BeforeRun = []poo.BeforeRunHook{hook}
	opts.BeforeTool = []poo.BeforeToolHook{hook}
	opts.AfterTool = []poo.AfterToolHook{hook}
	opts.AfterTurn = []poo.AfterTurnHook{hook}
	opts.BeforeSettle = []poo.BeforeSettleHook{hook}
	opts.Policy = policy
	s := newTestSession(t, opts)

	runSession(t, s, "start")

	points := hook.Points()
	want := []string{"session_start", "before_run", "before_tool", "after_tool", "after_turn", "before_settle"}
	for _, point := range want {
		p, ok := points[point]
		if !ok {
			t.Fatalf("the %s hook probed nothing; the points that probed are %v", point, hook.probedPoints())
		}
		if p.err != nil {
			t.Errorf("the %s probe failed with %v, want the tool's output", point, p.err)
		}
		if got, wantText := p.observation.Text, "the tree's files"; got != wantText {
			t.Errorf("the %s probe saw %q, want %q", point, got, wantText)
		}
		if got, wantDir := p.workingDir, opts.WorkingDir; got != wantDir {
			t.Errorf("the %s hook's working directory is %q, want %q", point, got, wantDir)
		}
	}
	if got, wantProbes := hook.Probes(), 7; got != wantProbes {
		// after_turn fires at every turn's end: the model's call ends
		// one turn and its answer ends another.
		t.Errorf("the hook points probed %d time(s), want %d", got, wantProbes)
	}
	if got, wantProbes := len(read.Calls()), hook.Probes(); got != wantProbes {
		t.Errorf("the read tool ran %d time(s), want %d, one per probe", got, wantProbes)
	}

	// The model's own call is the only call the run recorded.
	if got := sink.count("tool.call"); got != 1 {
		t.Errorf("the session wrote %d tool.call record(s), want 1: a probe writes none", got)
	}
	if got := sink.count("tool.result"); got != 1 {
		t.Errorf("the session wrote %d tool.result record(s), want 1: a probe writes none", got)
	}
	if got := countEvents(s, poo.EventToolCall); got != 1 {
		t.Errorf("the session emitted %d tool.call event(s), want 1: a probe emits none", got)
	}
	if got := countEvents(s, poo.EventToolResult); got != 1 {
		t.Errorf("the session emitted %d tool.result event(s), want 1: a probe emits none", got)
	}
	if got := countEvents(s, poo.EventPermissionAsked); got != 0 {
		t.Errorf("the session emitted %d permission.asked event(s), want none: a probe raises no ask", got)
	}
	if got, wantCount := policy.count, 1; got != wantCount {
		t.Errorf("the policy slot judged %d call(s), want %d: a probe is not a call of the model's", got, wantCount)
	}

	// The probe's environment is the session's, with no nested seam and
	// the probe's own caps.
	envs := read.Envs()
	if len(envs) == 0 {
		t.Fatal("the read tool recorded no environment")
	}
	for i, env := range envs {
		if env.Nested != nil {
			t.Errorf("probe %d carries a nested caller, want none: a probe makes no calls of its own", i)
		}
		if env.Decider != nil {
			t.Errorf("probe %d carries a decider, want none", i)
		}
		if got := env.Settings.ObservationCapChars; got != poo.HookToolMaxOutputChars {
			t.Errorf("probe %d's observation cap is %d, want %d", i, got, poo.HookToolMaxOutputChars)
		}
		if got := env.Settings.ReadOutputCapChars; got != poo.HookToolMaxOutputChars {
			t.Errorf("probe %d's read cap is %d, want %d", i, got, poo.HookToolMaxOutputChars)
		}
	}
	if got := envs[len(envs)-1].RunID; got == "" {
		t.Error("the settle probe's environment names no run")
	}
}

// TestAProbeLeavesTheDoomLoopsCountersAlone covers the counters: a hook
// that probes the tool and the arguments the model is about to call
// leaves the model's streak where it was, so the model's second call
// counts its two calls and the probes it ran count nothing.
func TestAProbeLeavesTheDoomLoopsCountersAlone(t *testing.T) {
	read := &namedTool{name: "read", echoTool: echoTool{result: "the tree's files"}}
	again := []byte(`{"path":"go.mod"}`)
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "read", Arguments: again}}},
		{calls: []poo.ToolCall{{CallID: "c2", Name: "read", Arguments: again}}},
		{text: "done"},
	})
	hook := newProbeHook("read", string(again))
	// More probes than the doom loop's own thresholds, of the call the
	// model is about to make: a probe that counted would leave the second
	// call's counts far past them.
	hook.repeats = 6
	opts := probeOptions(t, tr, &recordingSink{}, &echoTool{result: "the effect"}, read)
	opts.BeforeTool = []poo.BeforeToolHook{hook}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")

	// Every call's probes, and the model's own calls, which ran as asked.
	if got, want := len(read.Calls()), 2*hook.repeats+2; got != want {
		t.Fatalf("the read tool ran %d time(s), want %d", got, want)
	}
	counts := hook.Counts()
	if len(counts) != 2 {
		t.Fatalf("the hook read %d call(s) of counts, want 2", len(counts))
	}
	if counts[1].Exact != 2 || counts[1].Near != 2 {
		t.Errorf("the model's second call counts %+v, want 2 and 2: probes are not counted", counts[1])
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %s, want %s", res.Reason, poo.ReasonEndTurn)
	}
}

// detachedProbeHook probes with a context of its own, the way a hook that
// ignores the context it was handed does.
type detachedProbeHook struct {
	// tool is the tool the hook probes.
	tool string
	// done carries what the probe reported.
	done chan error
}

// newDetachedProbeHook returns a hook that probes one tool with a context
// of its own.
func newDetachedProbeHook(tool string) *detachedProbeHook {
	return &detachedProbeHook{tool: tool, done: make(chan error, 1)}
}

// BeforeTool probes the tool with a context the hook made itself.
func (h *detachedProbeHook) BeforeTool(ctx poo.HookContext, _ poo.ToolCallInfo) (poo.ToolDecision, error) {
	_, err := ctx.ReadOnlyTools.CallReadOnly(context.Background(), h.tool, json.RawMessage(`{"path":"go.mod"}`))
	h.done <- err
	return poo.Allow(), nil
}

// Wait returns what the probe reported.
func (h *detachedProbeHook) Wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the probe never returned")
		return nil
	}
}

// callerCapture keeps the read-only caller a session-start hook was given.
type callerCapture struct {
	// mu guards caller.
	mu sync.Mutex
	// caller is the hook context's read-only tool caller.
	caller poo.ReadOnlyToolCaller
}

// SessionStart keeps the caller and decides nothing.
func (c *callerCapture) SessionStart(ctx poo.HookContext) (poo.StartDecision, error) {
	c.mu.Lock()
	c.caller = ctx.ReadOnlyTools
	c.mu.Unlock()
	return poo.StartDecision{}, nil
}

// Caller returns the caller the session's hook context carried.
func (c *callerCapture) Caller() poo.ReadOnlyToolCaller {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caller
}

// TestAProbeStopsWithItsHookAndItsSession covers the bound: a probe ends
// with the hook that asked for it, even when it was handed a context of
// its own, and it ends with the session that owns it.
func TestAProbeStopsWithItsHookAndItsSession(t *testing.T) {
	t.Run("the hook's deadline", func(t *testing.T) {
		ended := make(chan error, 1)
		read := &namedTool{name: "read", echoTool: echoTool{call: func(ctx context.Context) (poo.ToolResult, error) {
			<-ctx.Done()
			ended <- ctx.Err()
			return poo.ToolResult{}, ctx.Err()
		}}}
		hook := newDetachedProbeHook("read")
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "c1", Name: "read", Arguments: []byte(`{"path":"go.mod"}`)}}},
			{text: "done"},
		})
		opts := probeOptions(t, tr, &recordingSink{}, &echoTool{result: "the effect"}, read)
		opts.HookTimeout = 200 * time.Millisecond
		opts.BeforeTool = []poo.BeforeToolHook{hook}
		s := newTestSession(t, opts)

		res := runSession(t, s, "start")

		select {
		case err := <-ended:
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("the probe's tool ended with %v, want the hook's deadline", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the probe outlived the hook's deadline")
		}
		if hook.Wait(t) == nil {
			t.Error("the probe reported no failure after its hook's deadline")
		}
		if res.Reason != poo.ReasonHookError {
			t.Errorf("the run settled %s, want %s", res.Reason, poo.ReasonHookError)
		}
	})

	// A probe in a before-tool hook is bound to the hook's context, and
	// Close ends that context with the run it settles. The stop is the
	// session's, and it arrives through the hook.
	t.Run("the session's close", func(t *testing.T) {
		started := make(chan struct{})
		ended := make(chan error, 1)
		read := &namedTool{name: "read", echoTool: echoTool{call: func(ctx context.Context) (poo.ToolResult, error) {
			close(started)
			<-ctx.Done()
			ended <- ctx.Err()
			return poo.ToolResult{}, ctx.Err()
		}}}
		hook := newDetachedProbeHook("read")
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "c1", Name: "read", Arguments: []byte(`{"path":"go.mod"}`)}}},
			{text: "done"},
		})
		capture := &callerCapture{}
		opts := probeOptions(t, tr, &recordingSink{}, &echoTool{result: "the effect"}, read)
		opts.SessionStart = []poo.SessionStartHook{capture}
		opts.BeforeTool = []poo.BeforeToolHook{hook}
		s := newTestSession(t, opts)

		go func() { _, _ = s.Prompt(context.Background(), "start") }()
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("the probe never started")
		}
		if err := s.Close(); err != nil {
			t.Fatalf("closing the session: %v", err)
		}
		select {
		case err := <-ended:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the probe's tool ended with %v, want the session's close", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the probe outlived the session")
		}
		if hook.Wait(t) == nil {
			t.Error("the probe of a closing session returned no error")
		}
		if _, err := capture.Caller().CallReadOnly(context.Background(), "read", json.RawMessage(`{"path":"go.mod"}`)); !errors.Is(err, poo.ErrSessionClosed) {
			t.Errorf("a probe after the close failed with %v, want %v", err, poo.ErrSessionClosed)
		}
	})
}

// TestAProbeRefusesEveryToolThatCouldAct covers the eligibility rule: a
// probe reaches the read-only tools a hook may name and nothing else, so
// a tool that acts, the run's own tools and the consumer's own read-only
// tools are all refused before anything starts.
func TestAProbeRefusesEveryToolThatCouldAct(t *testing.T) {
	sideEffects := poo.ClassSideEffecting
	tools := map[string]*namedTool{
		"bash":   {name: "bash", echoTool: echoTool{result: "ran", class: sideEffects}},
		"fetch":  {name: "fetch", echoTool: echoTool{result: "fetched", class: sideEffects}},
		"finish": {name: "finish", echoTool: echoTool{result: "finished"}},
		"grep":   {name: "grep", echoTool: echoTool{result: "grepped", class: sideEffects}},
		"done":   {name: "done", echoTool: echoTool{result: "done"}},
	}
	sink := &recordingSink{}
	opts := probeOptions(t, scripted([]fakeResponse{{text: "done"}}), sink, &echoTool{result: "the effect"}, &namedTool{name: "read", echoTool: echoTool{result: "read"}})
	for name, tool := range tools {
		opts.Tools[name] = tool
		opts.ToolOrder = append(opts.ToolOrder, name)
	}
	hooks := map[string]*probeHook{}
	for _, name := range append([]string{"bash", "fetch", "finish", "grep", "done"}, "unknown") {
		hooks[name] = newProbeHook(name, `{}`)
		opts.SessionStart = append(opts.SessionStart, hooks[name])
	}
	newTestSession(t, opts)

	for name, hook := range hooks {
		p, ok := hook.Points()["session_start"]
		if !ok {
			t.Fatalf("the %s probe recorded nothing", name)
		}
		if p.err == nil {
			t.Errorf("the %s probe succeeded, want it refused", name)
		}
		if p.observation.Text != "" {
			t.Errorf("the %s probe saw %q, want nothing", name, p.observation.Text)
		}
	}
	for name, tool := range tools {
		if calls := tool.Calls(); len(calls) != 0 {
			t.Errorf("the %s tool ran %d time(s) for a probe, want none", name, len(calls))
		}
	}
}

// TestAProbeValidatesItsArgumentsBeforePreparing covers the order: the
// arguments go through the same schema validator a call the model makes
// does, so an invalid request never reaches the tool's Prepare.
func TestAProbeValidatesItsArgumentsBeforePreparing(t *testing.T) {
	prepareErr := errors.New("the tool prepared the call")
	read := &namedTool{name: "read", echoTool: echoTool{prepareErr: prepareErr}}
	// A background-capable tool makes the session compile its argument
	// schemas, which is the validator both a call and a probe answer to.
	opts := probeOptions(t, scripted([]fakeResponse{{text: "done"}}), &recordingSink{}, &echoTool{result: "the effect", background: poo.BackgroundAllowed}, read)
	invalid := newProbeHook("read", `{"text":`)
	valid := newProbeHook("read", `{"text":"one"}`)
	opts.SessionStart = []poo.SessionStartHook{invalid, valid}
	newTestSession(t, opts)

	invalidProbe := invalid.Points()["session_start"]
	if invalidProbe.err == nil {
		t.Fatal("the probe with invalid arguments succeeded, want the schema to refuse it")
	}
	if errors.Is(invalidProbe.err, prepareErr) {
		t.Errorf("the probe with invalid arguments failed with %v, want the schema's own refusal before Prepare", invalidProbe.err)
	}
	if !errors.Is(valid.Points()["session_start"].err, prepareErr) {
		t.Errorf("the probe with valid arguments failed with %v, want it to reach Prepare", valid.Points()["session_start"].err)
	}
}

// TestAProbeTruncatesItsOutput covers the bound: a probe's text is held
// to HookToolMaxOutputChars and says so, so a hook that reasons over a
// whole tree holds a bounded observation.
func TestAProbeTruncatesItsOutput(t *testing.T) {
	long := strings.Repeat("é", poo.HookToolMaxOutputChars+10)
	tests := []struct {
		name      string
		result    string
		wantRunes int
		wantCut   bool
	}{
		{name: "within the cap", result: "a short observation", wantRunes: len("a short observation")},
		{name: "over the cap", result: long, wantRunes: poo.HookToolMaxOutputChars, wantCut: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := &namedTool{name: "read", echoTool: echoTool{result: tt.result}}
			opts := probeOptions(t, scripted([]fakeResponse{{text: "done"}}), &recordingSink{}, &echoTool{result: "the effect"}, read)
			hook := newProbeHook("read", `{"path":"go.mod"}`)
			opts.SessionStart = []poo.SessionStartHook{hook}
			newTestSession(t, opts)

			p := hook.Points()["session_start"]
			if p.err != nil {
				t.Fatalf("the probe failed with %v", p.err)
			}
			if got := utf8.RuneCountInString(p.observation.Text); got != tt.wantRunes {
				t.Errorf("the probe's text holds %d character(s), want %d", got, tt.wantRunes)
			}
			if p.observation.Truncated != tt.wantCut {
				t.Errorf("the probe reports Truncated %t, want %t", p.observation.Truncated, tt.wantCut)
			}
			if !strings.HasPrefix(tt.result, p.observation.Text) {
				t.Error("the probe's text is not the head of the tool's output")
			}
		})
	}
}

// TestAProbeSurvivesAToolThatPanics covers the crash: a probed tool runs
// on the hook's own goroutine, where nothing else recovers it, so a tool
// that panics comes back to the hook as an error and the run finishes.
func TestAProbeSurvivesAToolThatPanics(t *testing.T) {
	read := &namedTool{name: "read", echoTool: echoTool{call: func(context.Context) (poo.ToolResult, error) {
		panic("the tool came apart")
	}}}
	hook := newProbeHook("read", `{"path":"go.mod"}`)
	opts := probeOptions(t, scripted([]fakeResponse{{text: "done"}}), &recordingSink{}, &echoTool{result: "the effect"}, read)
	opts.SessionStart = []poo.SessionStartHook{hook}
	s := newTestSession(t, opts)

	res := runSession(t, s, "start")

	p := hook.Points()["session_start"]
	if p.err == nil {
		t.Fatalf("the probe of a panicking tool returned %q with no error, want the panic reported", p.observation.Text)
	}
	if got := p.err.Error(); !strings.Contains(got, "the tool came apart") {
		t.Errorf("the probe failed with %q, want the panic's own text", got)
	}
	if p.observation.Text != "" {
		t.Errorf("the probe saw %q, want nothing: the tool never returned", p.observation.Text)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %s, want %s: a panic in a probe is the probe's", res.Reason, poo.ReasonEndTurn)
	}
}

// TestAProbeObeysTheSessionsReadRestrictions covers the environment: a
// probe runs the real read and grep tools with the session's own
// restrictions, so a protected path and a path outside the working
// directory are refused before anything is read, and the search skips
// the protected file.
func TestAProbeObeysTheSessionsReadRestrictions(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("the guarded line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(notes, []byte("the public line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("the outside line\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Each hook probes one path, and names what it expects to see.
	tests := []struct {
		name     string
		tool     string
		args     string
		wantErr  bool
		wantText string
	}{
		{name: "a protected path", tool: "read", args: `{"path":"secret.txt"}`, wantErr: true},
		{name: "a path outside the working directory", tool: "read", args: fmt.Sprintf(`{"path":%q}`, outsideFile), wantErr: true},
		{name: "a search of the working directory", tool: "grep", args: `{"pattern":"the"}`, wantText: "the public line"},
		{name: "a search outside the working directory", tool: "grep", args: fmt.Sprintf(`{"path":%q,"pattern":"the"}`, outside), wantErr: true},
	}
	opts := testOptions(t, scripted([]fakeResponse{{text: "done"}}), &echoTool{result: "the effect"}, &recordingSink{})
	opts.WorkingDir = dir
	opts.ProtectedPaths = []string{secret}
	opts.RestrictRead = true
	opts.RestrictGrep = true
	opts.Tools["read"] = read.New()
	opts.Tools["grep"] = grep.New()
	opts.ToolOrder = []string{"echo", "read", "grep"}
	hooks := map[string]*probeHook{}
	for _, tt := range tests {
		hook := newProbeHook(tt.tool, tt.args)
		hooks[tt.name] = hook
		opts.SessionStart = append(opts.SessionStart, hook)
	}
	newTestSession(t, opts)

	for _, tt := range tests {
		p, ok := hooks[tt.name].Points()["session_start"]
		if !ok {
			t.Fatalf("the probe of %s recorded nothing", tt.name)
		}
		if tt.wantErr {
			if p.err == nil {
				t.Errorf("the probe of %s returned %q, want it refused", tt.name, p.observation.Text)
			}
			if p.observation.Text != "" {
				t.Errorf("the probe of %s saw %q, want nothing", tt.name, p.observation.Text)
			}
			continue
		}
		if p.err != nil {
			t.Fatalf("the probe of %s failed with %v, want the allowed content", tt.name, p.err)
		}
		if !strings.Contains(p.observation.Text, tt.wantText) {
			t.Errorf("the probe of %s saw %q, want it to hold %q", tt.name, p.observation.Text, tt.wantText)
		}
		if strings.Contains(p.observation.Text, "the guarded line") {
			t.Errorf("the probe of %s saw the protected file's content: %q", tt.name, p.observation.Text)
		}
	}
}

// TestAHookProbeNeverNamesABackgroundOnlyTool covers the eligibility the
// build check and the call share: a tool that only runs as a background
// job cannot answer a probe, so the build refuses it rather than every
// firing failing open.
func TestAHookProbeNeverNamesABackgroundOnlyTool(t *testing.T) {
	spec := poo.ToolSpec{Name: "read", Class: poo.ClassReadOnly, Background: poo.BackgroundAlways}
	if poo.HookReadOnlyToolAllowed("read", spec) {
		t.Error("a probe may name a tool that only runs in the background")
	}
	spec.Background = poo.BackgroundAllowed
	if !poo.HookReadOnlyToolAllowed("read", spec) {
		t.Error("a probe may not name a read-only tool the model may also background")
	}
}
