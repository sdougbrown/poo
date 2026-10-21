package poo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// The tests drive a session through the public API: a session is built,
// a prompt starts a run, and the run's result says how it ended.

// fakeTransport answers requests from a script.
type fakeTransport struct {
	mu sync.Mutex
	// responses holds one response per request. The last one repeats.
	responses []fakeResponse
	// requests holds the requests the transport received.
	requests []poo.ChatRequest
	// index is the next response to serve.
	index int
	// before is called before each response is served, so a test can
	// act at a known point without a sleep.
	before func(req poo.ChatRequest)
}

// fakeResponse is one scripted response.
type fakeResponse struct {
	// text is the assistant's text.
	text string
	// calls are the tool calls the model asked for.
	calls []poo.ToolCall
	// finish is why the model stopped.
	finish poo.FinishReason
	// err is the failure the request returns, if any.
	err error
	// usage is the accounting the stream reports, if any.
	usage *poo.UsageReport
}

// Stream answers the next scripted response.
func (f *fakeTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	if f.before != nil {
		f.before(req)
	}
	rendered := poo.Rendered{
		API:      poo.APIChatCompletions,
		Messages: len(req.Messages),
	}
	i := f.index
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	} else {
		f.index++
	}
	f.mu.Unlock()
	if i < 0 {
		return nil, rendered, fmt.Errorf("fakeTransport: no scripted response")
	}
	resp := f.responses[i]
	if resp.err != nil {
		return nil, rendered, resp.err
	}
	return &fakeReader{events: resp.events()}, rendered, nil
}

// Fork is not implemented by the fake: the compaction seam's fork is
// exercised with the scripted transport.
func (f *fakeTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, fmt.Errorf("fakeTransport: no fork")
}

// requestCount returns how many requests the transport received.
func (f *fakeTransport) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// requestsOf returns the requests the transport received.
func (f *fakeTransport) requestsOf() []poo.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]poo.ChatRequest(nil), f.requests...)
}

// events turns a response into the events a stream yields.
func (r fakeResponse) events() []poo.StreamEvent {
	var events []poo.StreamEvent
	if r.text != "" {
		events = append(events, poo.StreamEvent{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "text", Text: r.text},
		})
	}
	for _, c := range r.calls {
		events = append(events, poo.StreamEvent{
			Kind: "tool_call",
			ToolCall: &poo.ToolCallDelta{
				Index:     0,
				ID:        c.ID,
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
	events = append(events, poo.StreamEvent{Kind: "finish", FinishReason: finish})
	if r.usage != nil {
		events = append(events, poo.StreamEvent{Kind: "usage", Usage: r.usage})
	}
	return events
}

// fakeReader yields a fixed set of events.
type fakeReader struct {
	events []poo.StreamEvent
	index  int
}

// Next returns the next event.
func (r *fakeReader) Next() (poo.StreamEvent, error) {
	if r.index >= len(r.events) {
		return poo.StreamEvent{}, io.EOF
	}
	ev := r.events[r.index]
	r.index++
	return ev, nil
}

// Close releases the reader.
func (r *fakeReader) Close() error { return nil }

// recordingSink collects the records a run writes.
type recordingSink struct {
	mu      sync.Mutex
	records []poo.Record
}

// Append collects one record.
func (s *recordingSink) Append(ctx context.Context, rec poo.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *recordingSink) Flush(ctx context.Context) error { return nil }

// names returns the record names, in order.
func (s *recordingSink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, rec.Meta().Record)
	}
	return out
}

// count returns how many records carry a name.
func (s *recordingSink) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// messages returns the message records, in order.
func (s *recordingSink) messages() []*poo.MessageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*poo.MessageRecord
	for _, rec := range s.records {
		if m, ok := rec.(*poo.MessageRecord); ok {
			out = append(out, m)
		}
	}
	return out
}

// settled returns the run's settle record, or nil.
func (s *recordingSink) settled() *poo.RunSettled {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if r, ok := rec.(*poo.RunSettled); ok {
			return r
		}
	}
	return nil
}

// allRecords returns every record the sink kept.
func (s *recordingSink) allRecords() []poo.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]poo.Record(nil), s.records...)
}

// echoTool is a tool that records what it was asked to do.
type echoTool struct {
	// calls holds the arguments the tool received.
	calls []string
	// result is the text the tool returns.
	result string
	// class is the tool's side-effect class.
	class poo.ToolClass
	// primaryArg names the tool's primary argument.
	primaryArg string
	// prepareErr is the failure Prepare returns, when it is set.
	prepareErr error
	// call is called with the call's context, so a test can act at a
	// known point without a sleep.
	call func(ctx context.Context) (poo.ToolResult, error)
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
	t.calls = append(t.calls, p.text)
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
		ModelID:      "test-model",
		ContextLimit: 32768,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
}

// newTestSession builds a session for a test.
//
// The session is closed when the test ends.
func newTestSession(t *testing.T, opts poo.Options) *poo.Session {
	t.Helper()
	s, err := poo.New(opts)
	if err != nil {
		t.Fatalf("building the session: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
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
func (r *usageReader) Next() (poo.StreamEvent, error) {
	if r.sent {
		return poo.StreamEvent{}, io.EOF
	}
	r.sent = true
	return poo.StreamEvent{}, &poo.ProviderError{
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
