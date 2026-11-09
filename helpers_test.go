package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The tests drive a session through the public API: a session is built,
// a prompt starts a run, and the run's result says how it ended.

// fakeResponse is one scripted response.
type fakeResponse struct {
	// text is the assistant's text.
	text string
	// calls are the tool calls the model asked for.
	calls []poo.ToolCall
	// thinking is the assistant's reasoning, if any.
	thinking string
	// finish is why the model stopped.
	finish poo.FinishReason
	// err is the failure the request returns, if any.
	err error
	// usage is the accounting the stream reports, if any.
	usage *poo.UsageReport
}

// scriptedLen is how many turns a scripted transport holds.
//
// The last response repeats up to it, so a test that scripts one answer
// for a run of many turns does not have to write the answer out each time.
const scriptedLen = 256

// scripted returns a pootest transport that serves the responses in order
// and then repeats the last one.
func scripted(responses []fakeResponse) *pootest.Transport {
	turns := make([]pootest.Turn, 0, scriptedLen)
	for i := 0; i < scriptedLen && len(responses) > 0; i++ {
		r := responses[len(responses)-1]
		if i < len(responses) {
			r = responses[i]
		}
		turns = append(turns, r.turn())
	}
	return pootest.NewTransport(turns...)
}

// turn converts a response into the scripted transport's turn.
func (r fakeResponse) turn() pootest.Turn {
	t := pootest.Turn{
		Text:     r.text,
		Thinking: r.thinking,
		Finish:   string(r.finish),
		Err:      r.err,
	}
	for _, c := range r.calls {
		t.Calls = append(t.Calls, pootest.Call{Name: c.Name, Arguments: c.Arguments, ID: c.CallID})
	}
	if r.usage != nil {
		t.InputTokens = r.usage.InputTokens
		t.OutputTokens = r.usage.OutputTokens
		t.CacheReadTokens = r.usage.CacheReadTokens
	}
	return t
}

// events turns a response into the events a stream yields.
func (r fakeResponse) events() []poo.StreamItem {
	var events []poo.StreamItem
	if r.thinking != "" {
		events = append(events, poo.StreamItem{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "thinking", Text: r.thinking},
		})
	}
	if r.text != "" {
		events = append(events, poo.StreamItem{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "text", Text: r.text},
		})
	}
	for _, c := range r.calls {
		events = append(events, poo.StreamItem{
			Kind: "tool_call",
			ToolCall: &poo.ToolCallDelta{
				Index:     0,
				ID:        c.CallID,
				Name:      c.Name,
				Arguments: c.Arguments,
				Complete:  true,
			},
		})
	}
	finish := r.finish
	if finish == "" {
		finish = poo.FinishStop
	}
	events = append(events, poo.StreamItem{Kind: "finish", FinishReason: finish})
	if r.usage != nil {
		events = append(events, poo.StreamItem{Kind: "usage", Usage: r.usage})
	}
	return events
}

// fakeReader yields a fixed set of events.
type fakeReader struct {
	events []poo.StreamItem
	index  int
}

// Next returns the next event.
func (r *fakeReader) Next() (poo.StreamItem, error) {
	if r.index >= len(r.events) {
		return poo.StreamItem{}, io.EOF
	}
	ev := r.events[r.index]
	r.index++
	return ev, nil
}

// Close releases the reader.
func (r *fakeReader) Close() error { return nil }

// recordingSink collects the records a run writes.
// recordingSink is the root tests' sink. The stamping, the write
// serialization and the record accessors are pootest.Sink's; the type
// stays for the field literals and adds the name counter.
type recordingSink struct {
	pootest.Sink
}

// count returns how many records carry a name.
func (s *recordingSink) count(name string) int {
	n := 0
	for _, rec := range s.Records() {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// messages returns the message records, in order.
func (s *recordingSink) messages() []*poo.MessageRecord { return s.Messages() }

// allRecords returns every record the sink kept.
func (s *recordingSink) allRecords() []poo.Record { return s.Records() }

// namedTool is an echo tool under a different name.
//
// The name is what the model calls it by, and the before-tool hooks
// see it.
type namedTool struct {
	echoTool
	// name is the tool's name.
	name string
}

// Spec describes the tool under its own name.
func (t *namedTool) Spec() poo.ToolSpec {
	spec := t.echoTool.Spec()
	spec.Name = t.name
	return spec
}

// echoTool is a tool that records what it was asked to do.
//
// The recording is guarded, because a read-only batch runs its calls at
// the same time.
type echoTool struct {
	// mu guards calls.
	mu sync.Mutex
	// calls holds the arguments the tool received.
	calls []string
	// result is the text the tool returns.
	result string
	// class is the tool's side-effect class.
	class poo.ToolKind
	// background is the tool's background mode.
	background poo.Background
	// primaryArg names the tool's primary argument.
	primaryArg string
	// prepareErr is the failure Prepare returns, when it is set.
	prepareErr error
	// call is called with the call's context, so a test can act at a
	// known point without a sleep.
	call func(ctx context.Context) (poo.ToolResult, error)
}

// Calls returns the arguments the tool received, in order.
func (t *echoTool) Calls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	class := t.class
	if class == "" {
		class = poo.ClassReadOnly
	}
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       class,
		Background:  t.background,
		PrimaryArg:  t.primaryArg,
	}
}

// Prepare accepts anything.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	if t.prepareErr != nil {
		return nil, t.prepareErr
	}
	return echoPrepared{text: string(raw)}, nil
}

// Call records the call and returns the result.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p := in.(echoPrepared)
	t.mu.Lock()
	t.calls = append(t.calls, p.text)
	t.mu.Unlock()
	if t.call != nil {
		return t.call(ctx)
	}
	return poo.ToolResult{Text: t.result}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct {
	text string
}

// Summary describes the call.
func (p echoPrepared) Summary() string { return p.text }

// testOptions builds the options for a test session.
func testOptions(t *testing.T, tr poo.ChatTransport, tool *echoTool, sink poo.RecordSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"echo": tool,
		},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		Model:        poo.ModelInfo{ModelID: "test-model", ContextLimit: 32768},
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
}

// newTestSession builds a session for a test.
//
// The session is closed when the test ends.
func newTestSession(t *testing.T, opts poo.Options, fns ...poo.Option) *poo.Session {
	t.Helper()
	s, err := poo.New(opts, fns...)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { checkTransitions(t, s) })
	t.Cleanup(func() { s.Close() })
	return s
}

// checkTransitions fails the test for every state change the session made
// that the transition table does not list.
func checkTransitions(t *testing.T, s *poo.Session) {
	t.Helper()
	from := poo.StateIdle
	for _, to := range s.Transitions() {
		if !poo.IsLegal(from, to) {
			t.Errorf("the session moved %s -> %s, which the transition table does not list", from, to)
		}
		from = to
	}
}

// runSession prompts a session and waits for the run to settle.
func runSession(t *testing.T, s *poo.Session, prompt string) poo.Result {
	t.Helper()
	if _, err := s.Prompt(context.Background(), prompt); err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	res, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("waiting for the run: %v", err)
	}
	return res
}

// usageReader is a stream reader that fails and reports the usage of
// the request it read.
//
// It is the shape a transport has when its stream breaks before the
// endpoint's usage chunk arrives.
type usageReader struct {
	// usage is the report the reader holds.
	usage *poo.UsageReport
	// sent reports whether the reader already gave its usage out.
	sent bool
}

// Next fails the stream without yielding an event.
func (r *usageReader) Next() (poo.StreamItem, error) {
	if r.sent {
		return poo.StreamItem{}, io.EOF
	}
	r.sent = true
	return poo.StreamItem{}, &poo.ProviderError{
		Kind:    poo.ProviderKindNetwork,
		Message: "the stream broke",
	}
}

// Close releases the reader.
func (r *usageReader) Close() error { return nil }

// ReportUsage returns the usage the request produced.
func (r *usageReader) ReportUsage() *poo.UsageReport { return r.usage }

// usageTransport answers with a reader that reports its usage.
type usageTransport struct {
	// reader is the reader the transport hands out.
	reader *usageReader
}

// Stream hands out the reader.
func (t *usageTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return t.reader, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake: the compaction seam's fork is
// exercised with the scripted transport.
func (t *usageTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, fmt.Errorf("usageTransport: no fork")
}

// errNoFork is the error the fake transports return for a fork.
var errNoFork = errors.New("the fake transport has no fork")

// toolCallTurn is a response whose turn asks for one echo call.
func toolCallTurn() fakeResponse {
	return fakeResponse{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}}
}

// summaryCompaction is a compaction that replaces the first turn with a
// summary message.
func summaryCompaction() poo.Compaction {
	return poo.Compaction{Strategy: "summary", Replace: &poo.RangeReplacement{
		Cut:      poo.TurnRange{First: 1, Last: 1},
		Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "summary")},
	}}
}

// windowCompaction is a compaction that hands back a provider window.
func windowCompaction() poo.Compaction {
	return poo.Compaction{Strategy: "native", Window: &poo.ProviderWindow{Items: []poo.ContentBlock{{
		Kind: poo.ContentProvider,
		Provider: &poo.ProviderBlock{
			API:      poo.APIChatCompletions,
			Type:     "compaction",
			Required: true,
			Data:     []byte(`{"fake":true}`),
		},
	}}}}
}

// scriptedCompactor is a compactor whose two decisions the test scripts.
type scriptedCompactor struct {
	should  func(poo.View, poo.ContextBudget) bool
	compact func(context.Context, poo.View, poo.ForkFunc) (poo.Compaction, error)
}

func (c *scriptedCompactor) ShouldCompact(v poo.View, b poo.ContextBudget) bool {
	return c.should(v, b)
}

func (c *scriptedCompactor) Compact(ctx context.Context, v poo.View, f poo.ForkFunc) (poo.Compaction, error) {
	return c.compact(ctx, v, f)
}

// heldToolCallReader yields one complete tool call and then waits for the
// context to end, so a test can hold the turn open.
type heldToolCallReader struct {
	ctx     context.Context
	started chan struct{}
	sent    bool
}

func (r *heldToolCallReader) Next() (poo.StreamItem, error) {
	if !r.sent {
		r.sent = true
		return poo.StreamItem{Kind: "tool_call", ToolCall: &poo.ToolCallDelta{Index: 0, ID: "c2", Name: "echo", Arguments: []byte(`{}`), Complete: true}}, nil
	}
	close(r.started)
	<-r.ctx.Done()
	return poo.StreamItem{}, r.ctx.Err()
}

func (*heldToolCallReader) Close() error { return nil }

// holdPrepared is a prepared call for holdTool.
type holdPrepared struct{ text string }

// Summary describes the call.
func (p holdPrepared) Summary() string { return p.text }

// holdTool is a tool a test can hold open and stop.
type holdTool struct {
	name       string
	partial    string
	block      bool
	background poo.Background

	mu      sync.Mutex
	calls   int
	started chan struct{}
}

// newHoldTool returns a tool that returns its partial output.
func newHoldTool(name, partial string) *holdTool {
	return &holdTool{name: name, partial: partial, started: make(chan struct{}, 16)}
}

// Spec describes the tool.
func (t *holdTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        t.name,
		Description: "a tool the test can hold open",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassSideEffecting,
		Background:  t.background,
	}
}

// Prepare accepts anything.
func (t *holdTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &a)
	return holdPrepared{text: a.Text}, nil
}

// Call records the call and waits when the tool is told to block.
func (t *holdTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
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
func (t *holdTool) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// oneToolOptions builds the options for a one-tool session.
func oneToolOptions(t *testing.T, tr poo.ChatTransport, tool poo.Tool, sink poo.RecordSink) poo.Options {
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

// backgroundCall is a call of a tool the test can hold open.
func backgroundCall(name, text string) poo.ToolCall {
	raw, _ := json.Marshal(map[string]any{"text": text, "background": true})
	return poo.ToolCall{CallID: "call_" + text, Name: name, Arguments: raw}
}

// jobEndedRecord returns the job.ended record the sink holds.
func jobEndedRecord(t *testing.T, sink *recordingSink) *poo.JobEndedRecord {
	t.Helper()
	for _, rec := range sink.allRecords() {
		if ended, ok := rec.(*poo.JobEndedRecord); ok {
			return ended
		}
	}
	t.Fatal("the store holds no job.ended record")
	return nil
}

// sinkJSON renders every record as JSON, so a test can look for a
// secret anywhere in the store.
func sinkJSON(sink *recordingSink, needle string) string {
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

// afterToolFunc adapts a function to an after-tool hook.
type afterToolFunc func(poo.HookContext, poo.ToolCallInfo, poo.ToolResult) (poo.ResultDecision, error)

// AfterTool calls the function.
func (f afterToolFunc) AfterTool(ctx poo.HookContext, info poo.ToolCallInfo, result poo.ToolResult) (poo.ResultDecision, error) {
	return f(ctx, info, result)
}

// beforeRunFunc adapts a function to a before-run hook.
type beforeRunFunc func(poo.HookContext, poo.RunTrigger) (poo.StartDecision, error)

// BeforeRun calls the function.
func (f beforeRunFunc) BeforeRun(ctx poo.HookContext, trigger poo.RunTrigger) (poo.StartDecision, error) {
	return f(ctx, trigger)
}

// afterTurnFunc adapts a function to an after-turn hook.
type afterTurnFunc func(poo.HookContext) (poo.TurnDecision, error)

// AfterTurn calls the function.
func (f afterTurnFunc) AfterTurn(ctx poo.HookContext) (poo.TurnDecision, error) {
	return f(ctx)
}

// beforeSettleFunc adapts a function to a before-settle hook.
type beforeSettleFunc func(poo.HookContext, poo.Reason) (poo.SettleDecision, error)

// BeforeSettle calls the function.
func (f beforeSettleFunc) BeforeSettle(ctx poo.HookContext, candidate poo.Reason) (poo.SettleDecision, error) {
	return f(ctx, candidate)
}
