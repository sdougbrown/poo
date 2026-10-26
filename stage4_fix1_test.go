package poo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/eval"
	"github.com/sdougbrown/poo/pootest"
	"github.com/sdougbrown/poo/prompt"
)

// The stage-4 fix pass's tool: it can block until its context ends and
// return the partial output it had.

// fixPrepared is a prepared call for fixTool.
type fixPrepared struct{ text string }

// Summary describes the call.
func (p fixPrepared) Summary() string { return p.text }

// fixTool is a tool a test can hold open and stop.
type fixTool struct {
	name       string
	partial    string
	block      bool
	background poo.Background

	mu      sync.Mutex
	calls   int
	started chan struct{}
}

// newFixTool returns a tool that returns its partial output.
func newFixTool(name, partial string) *fixTool {
	return &fixTool{name: name, partial: partial, started: make(chan struct{}, 16)}
}

// Spec describes the tool.
func (t *fixTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        t.name,
		Description: "a fix-pass tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassSideEffecting,
		Background:  t.background,
	}
}

// Prepare accepts anything.
func (t *fixTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &a)
	return fixPrepared{text: a.Text}, nil
}

// Call records the call and waits when the tool is told to block.
func (t *fixTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	select {
	case t.started <- struct{}{}:
	default:
	}
	if t.block {
		<-ctx.Done()
	}
	return poo.ToolResult{Text: t.partial}, nil
}

// Calls returns how many times the tool ran.
func (t *fixTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// fixOpts builds the options for a one-tool session.
func fixOpts(t *testing.T, tr poo.ChatTransport, tool poo.Tool, sink poo.RecordSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport:  tr,
		Tools:      map[string]poo.Tool{tool.Spec().Name: tool},
		ToolOrder:  []string{tool.Spec().Name},
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
	}
}

// fixCall is a call of a fix-pass tool.
func fixCall(name, text string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{"text": text, "background": true})
	return poo.ToolCall{CallID: "call_" + text, Name: name, Arguments: raw}
}

// fixJobEnded returns the job.ended record the sink holds.
func fixJobEnded(t *testing.T, sink *recordingSink) *poo.JobEndedRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if ended, ok := rec.(*poo.JobEndedRecord); ok {
			return ended
		}
	}
	t.Fatal("the store holds no job.ended record")
	return nil
}

// fixSinkJSON renders every record as JSON, so a test can look for a
// secret anywhere in the store.
func fixSinkJSON(sink *recordingSink, needle string) string {
	var b strings.Builder
	for _, rec := range sink.allRecords() {
		raw, err := json.Marshal(rec)
		if err != nil || !strings.Contains(string(raw), needle) {
			continue
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}

// fixResultRecord returns the first tool.result record the sink holds.
func fixResultRecord(t *testing.T, sink *recordingSink) *poo.ToolResultRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if r, ok := rec.(*poo.ToolResultRecord); ok {
			return r
		}
	}
	t.Fatal("the store holds no tool.result record")
	return nil
}

// fixBlockingBeforeTool blocks the chain until the test releases it.
type fixBlockingBeforeTool struct {
	entered chan struct{}
	release chan struct{}
}

// BeforeTool waits for the release and allows the call.
func (h *fixBlockingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	select {
	case <-h.entered:
	default:
		close(h.entered)
	}
	<-h.release
	return poo.Allow(), nil
}

// fixShortCircuit answers the call itself.
type fixShortCircuit struct{ text string }

// BeforeTool returns the synthetic result.
func (h *fixShortCircuit) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	return poo.ShortCircuit(poo.ToolResult{Text: h.text}), nil
}

// TestAnInterruptDuringTheBeforeToolChainKeepsTheFirstResult covers the
// plan's rule: the first result in registration order is the call's
// result, and an interrupt that arrives while the chain runs stops the
// call instead of starting it.
func TestAnInterruptDuringTheBeforeToolChainKeepsTheFirstResult(t *testing.T) {
	const synthetic = "the synthetic result"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{fixCall("quick", "one")}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := newFixTool("quick", "the tool ran")
	opts := fixOpts(t, tr, tool, sink)
	blocker := &fixBlockingBeforeTool{entered: make(chan struct{}), release: make(chan struct{})}
	opts.BeforeTool = []poo.BeforeToolHook{&fixShortCircuit{text: synthetic}, blocker}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	select {
	case <-blocker.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the before-tool chain never ran")
	}
	if _, err := s.Send("stop", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	close(blocker.release)
	waitFor(t, "the run to settle", func() bool { return sink.Settled() != nil })

	if got := tool.Calls(); got != 0 {
		t.Errorf("the tool was called %d time(s) after an interrupt, want 0", got)
	}
	rec := fixResultRecord(t, sink)
	if want := poo.HandleLine("r1", "quick") + "\n" + synthetic; rec.Text != want {
		t.Errorf("the call's result is %q, want the first hook's %q", rec.Text, want)
	}
}

// TestAnInterruptedBeforeToolStartsNoJob covers the plan's rule: a job
// is admitted only after an authorization that finished. An interrupt
// that arrives while the chain runs leaves the call unapproved.
func TestAnInterruptedBeforeToolStartsNoJob(t *testing.T) {
	entered := make(chan struct{})
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{jobCall("one", true)}}, {text: "done"}})
	sink := &recordingSink{}
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	opts := jobOptions(t, tr, tool, sink)
	opts.MaxTurns = 1
	opts.BeforeTool = []poo.BeforeToolHook{&fixInterruptingBeforeTool{entered: entered}}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-entered
	if _, err := s.Send("instead", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if n := sink.count("job.started"); n != 0 {
		t.Errorf("an interrupted, unapproved call started %d background job(s)", n)
	}
}

// fixInterruptingBeforeTool waits for its context to end.
type fixInterruptingBeforeTool struct{ entered chan struct{} }

// BeforeTool waits for the interrupt and allows the call.
func (h *fixInterruptingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	close(h.entered)
	<-ctx.Done()
	return poo.Allow(), nil
}

// fixPolicyOnInterrupt waits for the policy's context to end and then
// returns its cancellation as the policy's error.
type fixPolicyOnInterrupt struct{ entered chan struct{} }

// BeforeTool waits for the interrupt.
func (h *fixPolicyOnInterrupt) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	close(h.entered)
	<-ctx.Done()
	return poo.ToolDecision{}, ctx.Err()
}

// TestAPolicyThatFailsOnInterruptDoesNotSettleHookError covers the
// plan's rule: an interrupted policy decision is discarded and the call
// is refused. Its cancellation is not a hook failure.
func TestAPolicyThatFailsOnInterruptDoesNotSettleHookError(t *testing.T) {
	entered := make(chan struct{})
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
		{text: "done"},
	})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Policy = &fixPolicyOnInterrupt{entered: entered}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-entered
	if _, err := s.Send("instead", poo.Interrupt); err != nil {
		t.Fatalf("interrupting the run: %v", err)
	}
	got, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	if got.Reason != poo.ReasonEndTurn {
		t.Errorf("the interrupt settled %s (%s), want end_turn", got.Reason, got.Detail.Text)
	}
}

// TestAStoppedSyncCallStillRunsItsAfterToolChain covers the plan's rule:
// the after-tool chain runs for every call whose tool ran, on a context
// of its own, so an interrupt or a cancellation does not bypass a
// redaction.
func TestAStoppedSyncCallStillRunsItsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-call"
	for _, mode := range []string{"interrupt", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{fixCall("slow", "one")}},
				{text: "done"},
			})
			sink := &recordingSink{}
			tool := newFixTool("slow", "the output with "+secret)
			tool.block = true
			opts := fixOpts(t, tr, tool, sink)
			hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
			opts.AfterTool = []poo.AfterToolHook{hook}
			s := newTestSession(t, opts)

			if _, err := s.Prompt(context.Background(), "go"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			<-tool.started
			switch mode {
			case "interrupt":
				if _, err := s.Send("stop", poo.Interrupt); err != nil {
					t.Fatalf("interrupting the run: %v", err)
				}
			case "cancel":
				s.Cancel("cancel:test")
			}
			waitFor(t, "the run to settle", func() bool { return sink.Settled() != nil })

			if got := hook.Calls(); got != 1 {
				t.Errorf("the chain ran %d time(s), want 1 at the call's completion", got)
			}
			rec := fixResultRecord(t, sink)
			if strings.Contains(rec.Text, secret) {
				t.Errorf("the tool.result record holds the secret: %q", rec.Text)
			}
			for _, ev := range s.Events() {
				if ev.Name != poo.EventToolResult {
					continue
				}
				text, _ := ev.Payload["text"].(string)
				if strings.Contains(text, secret) {
					t.Errorf("the tool.result event holds the secret: %q", text)
				}
			}
			if store := fixSinkJSON(sink, secret); strings.Contains(store, secret) {
				t.Errorf("the secret reached the store:\n%s", store)
			}
		})
	}
}

// fixAfterFunc adapts a function to an after-tool hook.
type fixAfterFunc func(poo.HookContext, poo.ToolCallInfo, poo.ToolResult) (poo.ResultDecision, error)

// AfterTool calls the function.
func (f fixAfterFunc) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	return f(ctx, info, result)
}

// TestARedactionSurvivesALaterHookFailure covers the plan's rule: any
// Redact makes the whole chain redacted, so a hook that fails afterwards
// must not expose text an earlier decision carried.
func TestARedactionSurvivesALaterHookFailure(t *testing.T) {
	const secret = "REVIEW_SECRET_4AB"
	for _, mode := range []string{"error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}},
				{text: "done"},
			})
			sink := &recordingSink{}
			opts := testOptions(t, tr, &echoTool{result: secret}, sink)
			opts.HookTimeout = 10 * time.Millisecond
			opts.AfterTool = []poo.AfterToolHook{
				fixAfterFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					return poo.Replace(result), nil
				}),
				fixAfterFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					return poo.Redact(poo.ToolResult{Text: "safe"}), nil
				}),
				fixAfterFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
					if mode == "timeout" {
						<-ctx.Done()
						return poo.Keep(), ctx.Err()
					}
					return poo.Keep(), context.Canceled
				}),
			}
			s := newTestSession(t, opts)
			runSession(t, s, "start")
			if store := fixSinkJSON(sink, secret); strings.Contains(store, secret) {
				t.Errorf("the redacted secret persisted after %s: %s", mode, store)
			}
		})
	}
}

// TestAFatalErrorsTextIsTheHookProcessedResult covers the plan's rule:
// only the hook-processed result is recorded or emitted. A fatal tool
// error keeps its tool_fatal classification, but its published text is
// the result the chain returned.
func TestAFatalErrorsTextIsTheHookProcessedResult(t *testing.T) {
	const secret = "REVIEW_FATAL_SECRET"
	tool := &echoTool{call: func(context.Context) (poo.ToolResult, error) {
		return poo.ToolResult{}, poo.Fatalf("failed with token %s", secret)
	}}
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}}})
	opts := testOptions(t, tr, tool, sink)
	opts.AfterTool = []poo.AfterToolHook{fixAfterFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
		return poo.Redact(poo.ToolResult{Text: "safe failure", IsError: true}), nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "start")

	if store := fixSinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the fatal error's secret reached the store: %s", store)
	}
	events, _ := json.Marshal(s.Events())
	if strings.Contains(string(events), secret) {
		t.Error("the fatal error's secret reached the events")
	}
	if got := sink.Settled(); got == nil || got.Reason != poo.ReasonToolFatal {
		t.Fatalf("the run settled %v, want tool_fatal", got)
	}
	if got := sink.Settled().Detail.Text; strings.Contains(got, secret) {
		t.Errorf("the settle detail holds the secret: %q", got)
	}
}

// TestAJobTimeoutKeepsItsPartialOutputAndItsFlag covers the plan's rule:
// timeout is an error with timed_out true and the partial output as its
// result. The chain runs at completion on a context of its own, so a
// hook cannot turn the timeout into a missed hook deadline.
func TestAJobTimeoutKeepsItsPartialOutputAndItsFlag(t *testing.T) {
	const partial = "the job's partial output"
	for _, withHook := range []bool{false, true} {
		name := "no hook"
		if withHook {
			name = "with an after-tool hook"
		}
		t.Run(name, func(t *testing.T) {
			tr := scripted([]fakeResponse{
				{calls: []poo.ToolCall{fixCall("slow", "one")}},
				{text: "done"},
			})
			sink := &recordingSink{}
			tool := newFixTool("slow", partial)
			tool.block = true
			tool.background = poo.BackgroundAlways
			opts := fixOpts(t, tr, tool, sink)
			opts.Jobs = poo.JobOptions{Timeout: 40 * time.Millisecond}
			hook := &recordingAfterTool{decision: poo.Keep()}
			if withHook {
				opts.AfterTool = []poo.AfterToolHook{hook}
			}
			s := newTestSession(t, opts)

			if _, err := s.Prompt(context.Background(), "go"); err != nil {
				t.Fatalf("starting the run: %v", err)
			}
			waitFor(t, "the job to end", func() bool { return sink.count("job.ended") == 1 })
			ended := fixJobEnded(t, sink)
			if !ended.TimedOut {
				t.Errorf("job.ended.TimedOut is false, want true (outcome %q, text %q)", ended.Outcome, ended.Text)
			}
			if !strings.Contains(ended.Text, partial) {
				t.Errorf("job.ended.Text is %q, want the partial output %q", ended.Text, partial)
			}
			if withHook && hook.Calls() != 1 {
				t.Errorf("the chain ran %d time(s), want 1", hook.Calls())
			}
		})
	}
}

// TestACancelledJobRunsItsAfterToolChain covers the plan's rule: a
// cancelled job's partial output is delivered as its result, and the
// chain's redaction is what gets recorded.
func TestACancelledJobRunsItsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-job"
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{fixCall("slow", "one")}}})
	sink := &recordingSink{}
	tool := newFixTool("slow", "the output with "+secret)
	tool.block = true
	tool.background = poo.BackgroundAlways
	opts := fixOpts(t, tr, tool, sink)
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	opts.AfterTool = []poo.AfterToolHook{hook}
	s := newTestSession(t, opts)

	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.started
	if err := s.CancelJob("j1"); err != nil {
		t.Fatalf("cancelling the job: %v", err)
	}
	waitFor(t, "the job to end", func() bool { return sink.count("job.ended") == 1 })

	ended := fixJobEnded(t, sink)
	if strings.Contains(ended.Text, secret) {
		t.Errorf("job.ended.Text holds the secret: %q", ended.Text)
	}
	if got := hook.Calls(); got != 1 {
		t.Errorf("the job's chain ran %d time(s), want 1 at its completion", got)
	}
	if store := fixSinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the secret reached the store:\n%s", store)
	}
}

// TestASessionCloseRunsTheJobsAfterToolChain covers the plan's rule: a
// close is a job completion. The chain runs once, and the held result
// never leaks what a hook would redact.
func TestASessionCloseRunsTheJobsAfterToolChain(t *testing.T) {
	const secret = "s3cr3t-in-a-job"
	tr := scripted([]fakeResponse{
		{calls: []poo.ToolCall{fixCall("slow", "one")}},
		{text: "done"},
	})
	sink := &recordingSink{}
	tool := newFixTool("slow", "the output with "+secret)
	tool.block = true
	tool.background = poo.BackgroundAlways
	opts := fixOpts(t, tr, tool, sink)
	hook := &recordingAfterTool{decision: poo.Redact(poo.ToolResult{Text: "[the output was redacted]"})}
	opts.AfterTool = []poo.AfterToolHook{hook}
	s, err := poo.New(opts)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	if _, err := s.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.started
	if err := s.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	ended := fixJobEnded(t, sink)
	if strings.Contains(ended.Text, secret) {
		t.Errorf("job.ended.Text holds the secret: %q", ended.Text)
	}
	if got := hook.Calls(); got != 1 {
		t.Errorf("the job's chain ran %d time(s), want 1: the close is the job's completion", got)
	}
	if store := fixSinkJSON(sink, secret); strings.Contains(store, secret) {
		t.Errorf("the secret reached the store:\n%s", store)
	}
	if ended.Cause != poo.CauseSessionClosed {
		t.Errorf("the job's cause is %q, want session_closed", ended.Cause)
	}
}

// TestAReplacedJobKeepsTheOriginalInItsRecord covers the plan's rule for
// a job completion: the same provenance a synchronous tool.result keeps
// is recorded on job.ended, including the original a hook replaced.
func TestAReplacedJobKeepsTheOriginalInItsRecord(t *testing.T) {
	tool := pootest.NewJobTool("job", poo.BackgroundAlways)
	tool.SetResult("original job result")
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{jobCall("one", true)}}, {text: "done"}})
	opts := jobOptions(t, tr, tool, sink)
	opts.Jobs.OnIdle = poo.OnIdleHold
	opts.AfterTool = []poo.AfterToolHook{fixAfterFunc(func(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
		return poo.Replace(poo.ToolResult{Text: "replacement"}), nil
	})}
	s := newTestSession(t, opts)
	if _, err := s.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	<-tool.Started()
	s.Cancel("cancel:review")
	tool.ReleaseAll()
	waitFor(t, "job.ended", func() bool { return sink.count("job.ended") == 1 })

	ended := fixJobEnded(t, sink)
	if ended.Text != "replacement" {
		t.Errorf("job.ended.Text is %q, want the replacement", ended.Text)
	}
	if ended.Original == nil || ended.Original.Text != "original job result" {
		t.Errorf("job.ended.Original is %+v, want the original job result", ended.Original)
	}
	if len(ended.Hooks) != 1 || ended.Hooks[0].Action != poo.ResultActionReplace {
		t.Errorf("job.ended.Hooks is %+v, want the replacement decision", ended.Hooks)
	}
}

// fixSealSink holds a hook's first message write and lets the second
// one race the run's seal.
type fixSealSink struct {
	recordingSink
	first        chan struct{}
	releaseFirst chan struct{}
	settled      chan struct{}
	once         sync.Once
}

// Append holds the first hook message until the test releases it, and
// the second one until the run settles or its write is ended.
func (s *fixSealSink) Append(ctx context.Context, rec poo.Record) error {
	if m, ok := rec.(*poo.MessageRecord); ok && len(m.Content) > 0 {
		switch m.Content[0].Text {
		case "first hook message":
			close(s.first)
			select {
			case <-s.releaseFirst:
			case <-ctx.Done():
				return ctx.Err()
			}
		case "second hook message":
			select {
			case <-s.settled:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	err := s.recordingSink.Append(ctx, rec)
	if rec.Meta().Record == "run.settled" {
		s.once.Do(func() { close(s.settled) })
	}
	return err
}

// TestAClaimedHookWriteIsEndedAtTheSeal covers the plan's seal: a hook
// write claimed before finalization is waited for, and the seal ends it,
// so it never lands after run.settled. The history mutation follows only
// a successful record write.
func TestAClaimedHookWriteIsEndedAtTheSeal(t *testing.T) {
	sink := &fixSealSink{first: make(chan struct{}), releaseFirst: make(chan struct{}), settled: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
		return poo.StartDecision{Append: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "first hook message"),
			poo.NewTextMessage(poo.RoleUser, "second hook message"),
		}}, nil
	})}
	s := newTestSession(t, opts)
	poo.SetRecordWriteBudget(s, 80*time.Millisecond)
	promptDone := make(chan struct{})
	cancelDone := make(chan struct{})
	go func() { defer close(promptDone); _, _ = s.Prompt(context.Background(), "start") }()
	<-sink.first
	go func() { defer close(cancelDone); s.Cancel("cancel:review") }()
	waitFor(t, "settling", func() bool { return s.State() == poo.StateSettling })
	close(sink.releaseFirst)
	<-promptDone
	<-cancelDone

	sealed := false
	for _, rec := range sink.allRecords() {
		if sealed {
			if m, ok := rec.(*poo.MessageRecord); ok && len(m.Content) > 0 && m.Content[0].Text == "second hook message" {
				t.Error("a claimed hook record was appended after run.settled")
			}
		}
		if rec.Meta().Record == "run.settled" {
			sealed = true
		}
	}
}

// fixStartFunc adapts a function to a before-run hook.
type fixStartFunc func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error)

// BeforeRun calls the function.
func (f fixStartFunc) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	return f(ctx, trigger)
}

// TestStartupWritesNothingAfterTheSeal covers the plan's seal: a
// cancellation during the prompt render settles the run, and the
// startup writes nothing else for it.
//
// The session renders its prompt when it is built, so the run's render is
// the one of the prompt a hook edited: the build's render is the first
// call, and the run's is the second.
func TestStartupWritesNothingAfterTheSeal(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	sink := &recordingSink{}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, sink)
	renders := 0
	opts.Prompt = []func(*prompt.Builder){func(b *prompt.Builder) {
		b.SetRender("base", func(prompt.Env) string {
			renders++
			if renders < 2 {
				return ""
			}
			close(entered)
			<-release
			return ""
		})
	}}
	opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
		return poo.StartDecision{SystemPrompt: func(s prompt.Sections) {
			s.Append("base", "the hook's edit")
		}}, nil
	})}
	s := newTestSession(t, opts)
	go func() {
		defer close(done)
		_, _ = s.Prompt(context.Background(), "late user prompt")
	}()
	<-entered
	s.Cancel("cancel:review")
	close(release)
	<-done

	settled := false
	for _, name := range sink.Names() {
		if settled {
			t.Errorf("record %s appended after run.settled; order=%v", name, sink.Names())
		}
		if name == "run.settled" {
			settled = true
		}
	}
	for _, m := range sink.messages() {
		if m.Role == poo.RoleUser && len(m.Content) > 0 && m.Content[0].Text == "late user prompt" {
			t.Error("the late user prompt was recorded after the seal")
		}
	}
}

// TestCloseIsBoundedWithTwoStalledObservers covers the plan's shutdown
// bound: the 2s deadline is one deadline for the whole wait, so a second
// stalled observer does not wait for another timeout.
func TestCloseIsBoundedWithTwoStalledObservers(t *testing.T) {
	first := &stallingObserver{name: "first", entered: make(chan struct{}), release: make(chan struct{})}
	second := &stallingObserver{name: "second", entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Observers = []poo.Observer{first, second}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	<-first.entered
	<-second.entered
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Close() }()
	select {
	case <-done:
	case <-time.After(2300 * time.Millisecond):
		t.Error("Close still blocked after 2.3s with two stalled observers")
	}
	close(first.release)
	close(second.release)
	<-done
}

// TestTheObserverDropDiagnosticIsRecordOnly covers the plan's rule: the
// coalesced overflow diagnostic is written to the record only. It is not
// an event, so a client never receives it.
func TestTheObserverDropDiagnosticIsRecordOnly(t *testing.T) {
	restoreBuffer := poo.SetObserverBuffer(1)
	defer restoreBuffer()
	restoreInterval := poo.SetObserverDropInterval(20 * time.Millisecond)
	defer restoreInterval()
	observer := &stallingObserver{name: "stalled", entered: make(chan struct{}), release: make(chan struct{})}
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Observers = []poo.Observer{observer}
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	<-observer.entered
	waitFor(t, "the overflow record", func() bool { return sink.count("observer.dropped") > 0 })
	for _, ev := range s.Events() {
		if ev.Name == poo.EventObserverDropped {
			t.Errorf("the overflow diagnostic was emitted as an event: %+v", ev)
		}
	}
	close(observer.release)
}

// fixBadBackgroundTool defines the reserved background argument, so its
// registration fails.
type fixBadBackgroundTool struct{ echoTool }

// Spec describes the bad tool.
func (t *fixBadBackgroundTool) Spec() poo.ToolSpec {
	spec := t.echoTool.Spec()
	spec.Background = poo.BackgroundAllowed
	spec.Schema = json.RawMessage(`{"properties":{"background":{"type":"boolean"}}}`)
	return spec
}

// fixReporterGoroutines counts the event bus's reporter goroutines.
func fixReporterGoroutines() int {
	b := make([]byte, 1<<20)
	n := runtime.Stack(b, true)
	return strings.Count(string(b[:n]), "(*eventBus).reportDrops(")
}

// TestAFailedNewLeavesNoReporterGoroutine covers the plan's construction
// rollback: a New that fails stops everything it started, including the
// event bus's reporter.
func TestAFailedNewLeavesNoReporterGoroutine(t *testing.T) {
	before := fixReporterGoroutines()
	tr := scripted([]fakeResponse{{text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	opts.Tools["echo"] = &fixBadBackgroundTool{}
	for i := 0; i < 5; i++ {
		if _, err := poo.New(opts); err == nil {
			t.Fatal("the bad tool was accepted")
		}
	}
	runtime.Gosched()
	if after := fixReporterGoroutines(); after != before {
		t.Errorf("a failed New leaked %d reporter goroutine(s)", after-before)
	}
}

// TestMaxDurationCoversTheBeforeRunHook covers the plan's run budget: the
// wall-clock timer starts when the run is admitted, so a blocked
// BeforeRun cannot outlive MaxDuration.
func TestMaxDurationCoversTheBeforeRunHook(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tr := scripted([]fakeResponse{{text: "done"}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.MaxDuration = 10 * time.Millisecond
	opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
		close(entered)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return poo.StartDecision{}, nil
	})}
	s := newTestSession(t, opts)
	go func() {
		defer close(done)
		_, _ = s.Prompt(context.Background(), "start")
	}()
	<-entered
	time.Sleep(60 * time.Millisecond)
	settled := sink.Settled() != nil
	close(release)
	<-done
	_, _ = s.Wait(context.Background())
	if !settled {
		t.Error("MaxDuration=10ms did not settle a blocked BeforeRun within 60ms")
	}
}

// fixTurnFunc adapts a function to an after-turn hook.
type fixTurnFunc func(poo.HookContext) (poo.TurnDecision, error)

// AfterTurn calls the function.
func (f fixTurnFunc) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	return f(ctx)
}

// fixSecondRunPrompt rewrites the prompt of the session's second run.
type fixSecondRunPrompt struct {
	mu   sync.Mutex
	runs int
	text string
}

// BeforeRun changes the second run's prompt.
func (h *fixSecondRunPrompt) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	h.mu.Lock()
	h.runs++
	second := h.runs == 2
	h.mu.Unlock()
	if !second {
		return poo.StartDecision{}, nil
	}
	return poo.StartDecision{SystemPrompt: func(s prompt.Sections) { s.Set("base", h.text) }}, nil
}

// TestTheSecondRunsRequestUsesOnlyItsOwnPrompt covers the plan's rule:
// the prompt is rendered once per run and frozen for that run. An earlier
// run's system message stays in the record but is not sent again.
func TestTheSecondRunsRequestUsesOnlyItsOwnPrompt(t *testing.T) {
	const second = "THE SECOND RUN'S PROMPT"
	tr := scripted([]fakeResponse{{text: "one"}, {text: "two"}})
	sink := &recordingSink{}
	opts := poo.Options{
		Transport:  tr,
		Sink:       sink,
		SessionID:  "s1",
		Model:      poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		WorkingDir: t.TempDir(),
		BeforeRun:  []poo.BeforeRunHook{&fixSecondRunPrompt{text: second}},
	}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	requests := tr.Requests()
	if len(requests) < 2 {
		t.Fatalf("the transport saw %d request(s), want at least 2", len(requests))
	}
	systems := 0
	for _, m := range requests[1].Messages {
		if m.Role == poo.RoleSystem {
			systems++
		}
	}
	if systems != 1 {
		t.Errorf("the second run's request carries %d system message(s), want 1", systems)
	}
	if got := requests[1].Messages[0]; got.Role != poo.RoleSystem {
		t.Errorf("the second run's first message is %q, want a system message", got.Role)
	} else if !strings.Contains(got.Text(), second) {
		t.Errorf("the second run's first message is %q, want the run's own prompt %q", got.Text(), second)
	}
}

// TestTheViewOrderMatchesTheRequestOrder covers the plan's rule: the view
// a hook reads is the model's history in the order the request sends it.
func TestTheViewOrderMatchesTheRequestOrder(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}, {text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	var mu sync.Mutex
	var histories [][]poo.Message
	opts.AfterTurn = []poo.AfterTurnHook{fixTurnFunc(func(ctx poo.HookContext) (poo.TurnDecision, error) {
		mu.Lock()
		histories = append(histories, ctx.HistoryView.Messages())
		mu.Unlock()
		return poo.TurnDecision{}, nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	mu.Lock()
	view := histories[1]
	mu.Unlock()
	req := tr.Requests()[1].Messages
	if len(view) < len(req) {
		t.Fatalf("the view holds %d messages, the request %d", len(view), len(req))
	}
	view = view[:len(req)]
	for i := range req {
		if req[i].Role != view[i].Role || req[i].Text() != view[i].Text() {
			t.Fatalf("the view and request differ at %d: view=%s:%q request=%s:%q", i, view[i].Role, view[i].Text(), req[i].Role, req[i].Text())
		}
	}
}

// TestAPromptReplacementDoesNotReachTheNextRequest covers the plan's rule
// for a run-local prompt: a replacement stays in its own run's request,
// while the earlier run's system message stays in the record.
func TestAPromptReplacementDoesNotReachTheNextRequest(t *testing.T) {
	tr := scripted([]fakeResponse{{text: "done"}, {text: "done"}})
	opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
	var n int
	opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
		n++
		text := "only instructions for run 1"
		if n == 2 {
			text = "only instructions for run 2"
		}
		return poo.StartDecision{SystemPrompt: func(s prompt.Sections) { s.Replace(text) }}, nil
	})}
	s := newTestSession(t, opts)
	runSession(t, s, "one")
	runSession(t, s, "two")

	var systems []string
	for _, m := range tr.Requests()[1].Messages {
		if m.Role == poo.RoleSystem {
			systems = append(systems, m.Text())
		}
	}
	if len(systems) != 1 || systems[0] != "only instructions for run 2" {
		t.Errorf("the second request's system instructions are %q, want only run 2", systems)
	}
}

// TestReplayUsesTheRecordedPrompt covers the plan's rule: replay uses the
// recorded system prompt and never re-renders it, so a prompt change
// between the recording and the replay does not change the replay.
func TestReplayUsesTheRecordedPrompt(t *testing.T) {
	tr := scripted([]fakeResponse{{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{}`)}}}})
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.SystemPrompt = "x"
	opts.Model.ContextLimit = 530
	opts.MaxTurns = 1
	s := newTestSession(t, opts)
	runSession(t, s, "start")
	if len(tr.Requests()) != 1 {
		t.Fatal("the original run did not make its request")
	}
	var data []byte
	for _, rec := range sink.allRecords() {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	path := filepath.Join(t.TempDir(), "recording.jsonl")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := eval.Replay(path, eval.ReplayOptions{Context: context.Background(), ContextLimit: 530})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Match {
		t.Errorf("the replay of a one-request recorded prompt diverged: %v", result.Differences)
	}
}

// TestARawSystemPromptIsSentVerbatim covers the field's contract: a raw
// Options.SystemPrompt is the whole prompt, byte for byte. The builder's
// trailing newline belongs to a rendered prompt, not to a raw one.
func TestARawSystemPromptIsSentVerbatim(t *testing.T) {
	for _, raw := range []string{"the raw prompt", "the raw prompt\n"} {
		t.Run("raw", func(t *testing.T) {
			tr := scripted([]fakeResponse{{text: "done"}})
			opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
			opts.SystemPrompt = raw
			s := newTestSession(t, opts)
			runSession(t, s, "start")
			reqs := tr.Requests()
			if len(reqs) != 1 {
				t.Fatalf("the transport got %d request(s), want 1", len(reqs))
			}
			if got := systemText(reqs[0].Messages); got != raw {
				t.Errorf("the system prompt is %q, want %q", got, raw)
			}
		})
	}
}

// fixSettleFunc adapts a function to a before-settle hook.
type fixSettleFunc func(poo.HookContext, poo.Reason) (poo.SettleDecision, error)

// BeforeSettle calls the function.
func (f fixSettleFunc) BeforeSettle(ctx poo.HookContext, candidate poo.Reason) (poo.SettleDecision, error) {
	return f(ctx, candidate)
}

// fixStallingBeforeTool never answers, so its deadline names it.
type fixStallingBeforeTool struct{}

// BeforeTool waits for its deadline.
func (h *fixStallingBeforeTool) BeforeTool(ctx poo.HookContext, info poo.ToolCallInfo) (poo.ToolDecision, error) {
	<-ctx.Done()
	return poo.Allow(), nil
}

// TestAHookErrorsSourceNamesTheHook covers the plan's rule: an executable
// hook's error or deadline settles hook_error and detail.source names the
// hook.
func TestAHookErrorsSourceNamesTheHook(t *testing.T) {
	t.Run("before_tool", func(t *testing.T) {
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
			{text: "done"},
		})
		opts := testOptions(t, tr, &echoTool{result: "the effect"}, &recordingSink{})
		opts.HookTimeout = 20 * time.Millisecond
		opts.BeforeTool = []poo.BeforeToolHook{&fixStallingBeforeTool{}}
		s := newTestSession(t, opts)
		res := runSession(t, s, "start")
		if res.Reason != poo.ReasonHookError {
			t.Fatalf("the run settled %q, want hook_error", res.Reason)
		}
		if res.Detail.Source != "before_tool:fixStallingBeforeTool" {
			t.Errorf("the settle source is %q, want the hook's name", res.Detail.Source)
		}
	})
	t.Run("after_tool", func(t *testing.T) {
		tr := scripted([]fakeResponse{
			{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
			{text: "done"},
		})
		opts := testOptions(t, tr, &echoTool{result: "the effect"}, &recordingSink{})
		opts.AfterTool = []poo.AfterToolHook{fixAfterFunc(func(poo.HookContext, poo.ToolCallInfo, poo.ToolResult) (poo.ResultDecision, error) {
			return poo.Keep(), context.Canceled
		})}
		s := newTestSession(t, opts)
		res := runSession(t, s, "start")
		if res.Reason != poo.ReasonHookError {
			t.Fatalf("the run settled %q, want hook_error", res.Reason)
		}
		if res.Detail.Source != "after_tool:fixAfterFunc" {
			t.Errorf("the settle source is %q, want the hook's name", res.Detail.Source)
		}
	})
	t.Run("before_settle", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
		opts.BeforeSettle = []poo.BeforeSettleHook{fixSettleFunc(func(poo.HookContext, poo.Reason) (poo.SettleDecision, error) {
			return poo.SettleDecision{}, context.Canceled
		})}
		s := newTestSession(t, opts)
		res := runSession(t, s, "start")
		if res.Reason != poo.ReasonHookError {
			t.Fatalf("the run settled %q, want hook_error", res.Reason)
		}
		if res.Detail.Source != "before_settle:fixSettleFunc" {
			t.Errorf("the settle source is %q, want the hook's name", res.Detail.Source)
		}
	})
	t.Run("before_run", func(t *testing.T) {
		tr := scripted([]fakeResponse{{text: "done"}})
		opts := testOptions(t, tr, &echoTool{}, &recordingSink{})
		opts.BeforeRun = []poo.BeforeRunHook{fixStartFunc(func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error) {
			return poo.StartDecision{}, context.Canceled
		})}
		s := newTestSession(t, opts)
		res := runSession(t, s, "start")
		if res.Reason != poo.ReasonHookError {
			t.Fatalf("the run settled %q, want hook_error", res.Reason)
		}
		if res.Detail.Source != "before_run:fixStartFunc" {
			t.Errorf("the settle source is %q, want the hook's name", res.Detail.Source)
		}
	})
}
