package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
