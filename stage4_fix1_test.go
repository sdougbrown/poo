package poo_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
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
		Transport:    tr,
		Tools:        map[string]poo.Tool{tool.Spec().Name: tool},
		ToolOrder:    []string{tool.Spec().Name},
		Sink:         sink,
		SessionID:    "s1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		WorkingDir:   t.TempDir(),
	}
}

// fixCall is a call of a fix-pass tool.
func fixCall(name, text string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{"text": text, "background": true})
	return poo.ToolCall{ID: "call_" + text, Name: name, Arguments: raw}
}

// fixJobEnded returns the job.ended record the sink holds.
func fixJobEnded(t *testing.T, sink *recordingSink) *poo.JobEnded {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if ended, ok := rec.(*poo.JobEnded); ok {
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
	waitFor(t, "the run to settle", func() bool { return sink.settled() != nil })

	if got := tool.Calls(); got != 0 {
		t.Errorf("the tool was called %d time(s) after an interrupt, want 0", got)
	}
	rec := fixResultRecord(t, sink)
	if rec.Text != synthetic {
		t.Errorf("the call's result is %q, want the first hook's %q", rec.Text, synthetic)
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
		{calls: []poo.ToolCall{{ID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}},
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
