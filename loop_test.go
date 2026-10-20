package poo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// fakeTransport answers requests from a script.
type fakeTransport struct {
	// responses holds one response per request. The last one repeats.
	responses []fakeResponse
	// requests holds the requests the transport received.
	requests []poo.ChatRequest
	// index is the next response to serve.
	index int
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
}

// Stream answers the next scripted response.
func (f *fakeTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, error) {
	f.requests = append(f.requests, req)
	i := f.index
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	} else {
		f.index++
	}
	if i < 0 {
		return nil, fmt.Errorf("fakeTransport: no scripted response")
	}
	resp := f.responses[i]
	if resp.err != nil {
		return nil, resp.err
	}
	return &fakeReader{events: resp.events()}, nil
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
	records []poo.Record
}

// Append collects one record.
func (s *recordingSink) Append(ctx context.Context, rec poo.Record) error {
	s.records = append(s.records, rec)
	return nil
}

// Flush does nothing.
func (s *recordingSink) Flush(ctx context.Context) error { return nil }

// names returns the record names, in order.
func (s *recordingSink) names() []string {
	out := make([]string, 0, len(s.records))
	for _, rec := range s.records {
		out = append(out, rec.Meta().Record)
	}
	return out
}

// count returns how many records carry a name.
func (s *recordingSink) count(name string) int {
	n := 0
	for _, rec := range s.records {
		if rec.Meta().Record == name {
			n++
		}
	}
	return n
}

// echoTool is a tool that records what it was asked to do.
type echoTool struct {
	// calls holds the arguments the tool received.
	calls []string
	// result is the text the tool returns.
	result string
}

// Spec describes the tool.
func (t *echoTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{
		Name:        "echo",
		Description: "echo the argument back",
		Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Class:       poo.ClassReadOnly,
	}
}

// Prepare accepts anything.
func (t *echoTool) Prepare(raw json.RawMessage, env poo.PrepareEnv) (poo.Prepared, error) {
	return echoPrepared{text: string(raw)}, nil
}

// Call records the call and returns the result.
func (t *echoTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	p := in.(echoPrepared)
	t.calls = append(t.calls, p.text)
	return poo.ToolResult{Text: t.result}, nil
}

// echoPrepared is a prepared echo call.
type echoPrepared struct {
	text string
}

// Summary describes the call.
func (p echoPrepared) Summary() string { return p.text }

// testOptions builds the options for a test run.
func testOptions(t *testing.T, tr poo.ChatTransport, tool *echoTool, sink *recordingSink) poo.Options {
	t.Helper()
	return poo.Options{
		Transport: tr,
		Tools: map[string]poo.Tool{
			"echo": tool,
		},
		ToolOrder:    []string{"echo"},
		Sink:         sink,
		SessionID:    "s1",
		RunID:        "r1",
		ModelID:      "test-model",
		ContextLimit: 32768,
		SystemPrompt: "the system prompt",
		WorkingDir:   t.TempDir(),
	}
}

func TestARunThatEndsOnItsOwnSettlesEndTurn(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "all done"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 1 {
		t.Errorf("the run made %d turns, want 1", res.Turns)
	}

	names := sink.names()
	// The system message, the user's prompt, the model's answer and the
	// settle record.
	want := []string{"run.started", "message", "message", "message", "run.settled"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the records are %v, want %v", names, want)
	}
}

func TestARunThatCallsAToolExecutesIt(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if res.Turns != 2 {
		t.Errorf("the run made %d turns, want 2", res.Turns)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("the tool ran %d times, want 1", len(tool.calls))
	}
	if !strings.Contains(tool.calls[0], "hello") {
		t.Errorf("the tool received %q, want the call's arguments", tool.calls[0])
	}

	// The tool call and its result are both recorded.
	if sink.count("tool.call") != 1 {
		t.Errorf("the log holds %d tool.call records, want 1", sink.count("tool.call"))
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheSecondRequestCarriesTheToolResult(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "the echoed text"}

	r := poo.NewRun(testOptions(t, tr, tool, &recordingSink{}))
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	if len(tr.requests) != 2 {
		t.Fatalf("the transport got %d requests, want 2", len(tr.requests))
	}
	second := tr.requests[1].Messages
	// system, user, assistant (with its tool call), tool result
	if len(second) != 4 {
		t.Fatalf("the second request carried %d messages, want 4", len(second))
	}
	last := second[3]
	if last.Role != poo.RoleTool {
		t.Errorf("the last message's role is %q, want tool", last.Role)
	}
	if last.Content[0].Text != "the echoed text" {
		t.Errorf("the last message holds %q, want the tool's output", last.Content[0].Text)
	}
	// The result carries the provider's call id, because that is the id
	// the provider matches it against.
	if last.Content[0].ToolCallID != "call_1" {
		t.Errorf("the result's tool call id is %q, want call_1", last.Content[0].ToolCallID)
	}
	// The assistant's tool call is in the history, so the provider can
	// match the result to it.
	assistant := second[2]
	if len(assistant.ToolCalls()) != 1 {
		t.Fatalf("the assistant message holds %d tool calls, want 1", len(assistant.ToolCalls()))
	}
	if assistant.ToolCalls()[0].ToolCallID != "call_1" {
		t.Errorf("the tool call's id is %q, want call_1", assistant.ToolCalls()[0].ToolCallID)
	}
}

func TestTheMaxTurnsCeilingSettlesMaxTurns(t *testing.T) {
	// Every response asks for another tool call, so the run only stops at
	// its ceiling.
	tr := &fakeTransport{responses: []fakeResponse{{
		calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"again"}`),
		}},
	}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	opts.MaxTurns = 3

	r := poo.NewRun(opts)
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonMaxTurns {
		t.Errorf("the run settled %q, want max_turns", res.Reason)
	}
	if res.Turns != 3 {
		t.Errorf("the run made %d turns, want 3", res.Turns)
	}
	if len(tool.calls) != 3 {
		t.Errorf("the tool ran %d times, want 3", len(tool.calls))
	}
}

func TestACancelledContextSettlesCancelled(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(ctx, "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
}

func TestAProviderErrorSettlesProviderError(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint is down",
			Status:  500,
		},
	}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatal("the settle detail has no provider information")
	}
	if res.Detail.Provider.Kind != string(poo.ProviderKindServer) {
		t.Errorf("the provider kind is %q, want server", res.Detail.Provider.Kind)
	}
}

func TestAContextLengthErrorSettlesContextExhausted(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		err: &poo.ProviderError{
			Kind:    poo.ProviderKindContextLength,
			Message: "the prompt is too long",
			Status:  400,
		},
	}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
}

func TestAFinishReasonOfLengthSettlesProviderError(t *testing.T) {
	// The skeleton's reason enum has no output_truncated, so a provider
	// that stopped on the output limit is a provider error.
	tr := &fakeTransport{responses: []fakeResponse{{
		text:   "a truncated ans",
		finish: poo.FinishLength,
	}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
}

func TestNoOutputRoomSettlesContextExhaustedWithoutSending(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, sink)
	// A context limit this small leaves less than 256 tokens of room once
	// the prompt is counted.
	opts.ContextLimit = 300

	r := poo.NewRun(opts)
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	if len(tr.requests) != 0 {
		t.Errorf("the transport got %d requests, want none", len(tr.requests))
	}
}

func TestTheMaxTokensIsComputedFromTheContext(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 4096
	opts.MaxOutput = 0

	r := poo.NewRun(opts)
	if _, err := r.Execute(context.Background(), "a short prompt"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(tr.requests) != 1 {
		t.Fatalf("the transport got %d requests, want 1", len(tr.requests))
	}
	mt := tr.requests[0].MaxTokens
	// The room is the limit minus the estimated input minus 256, and the
	// model's own ceiling is 8192 by default.
	if mt <= 0 || mt > 4096 {
		t.Errorf("max_tokens is %d, want it under the context limit", mt)
	}
	if mt >= 4096 {
		t.Errorf("max_tokens is %d, want the 256-token reserve taken off", mt)
	}
}

func TestTheMaxOutputCapsTheMaxTokens(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 262144
	opts.MaxOutput = 512

	r := poo.NewRun(opts)
	if _, err := r.Execute(context.Background(), "a short prompt"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if got := tr.requests[0].MaxTokens; got != 512 {
		t.Errorf("max_tokens is %d, want 512", got)
	}
}

func TestTheDefaultMaxTokensIsEightThousandAndNinetyTwo(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	tool := &echoTool{result: "echoed"}

	opts := testOptions(t, tr, tool, &recordingSink{})
	opts.ContextLimit = 262144
	opts.MaxOutput = 0

	r := poo.NewRun(opts)
	if _, err := r.Execute(context.Background(), "a short prompt"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if got := tr.requests[0].MaxTokens; got != 8192 {
		t.Errorf("max_tokens is %d, want 8192", got)
	}
}

func TestAnUnknownToolIsARecoverableError(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "nope",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{text: "finished"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
}

func TestTheRunMovesThroughTheLoopStates(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hello"}`),
			}},
		},
		{text: "finished"},
	}}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, &recordingSink{}))
	if r.State() != poo.StateRequesting {
		t.Errorf("a new run is in %q, want requesting", r.State())
	}
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if r.State() != poo.StateSettled {
		t.Errorf("a finished run is in %q, want settled", r.State())
	}
}

func TestTheContextRunningOutMidRunSettlesContextExhausted(t *testing.T) {
	// The model asks for a call whose result is large, so the next
	// request has no room left.
	tr := &fakeTransport{responses: []fakeResponse{{
		calls: []poo.ToolCall{{
			ID:        "call_1",
			Name:      "echo",
			Arguments: json.RawMessage(`{"text":"hello"}`),
		}},
	}}}
	tool := &echoTool{result: string(make([]byte, 20000))}
	sink := &recordingSink{}

	opts := testOptions(t, tr, tool, sink)
	opts.ContextLimit = 2000

	r := poo.NewRun(opts)
	res, err := r.Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonContextExhausted {
		t.Errorf("the run settled %q, want context_exhausted", res.Reason)
	}
	if res.Detail.Source != "no_output_room" {
		t.Errorf("the settle source is %q, want no_output_room", res.Detail.Source)
	}
	// The first request went out, and the second did not.
	if len(tr.requests) != 1 {
		t.Errorf("the transport got %d requests, want 1", len(tr.requests))
	}
}

func TestTheSettleRecordCarriesTheReason(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{text: "hello"}}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	last := sink.records[len(sink.records)-1]
	settled, ok := last.(*poo.RunSettled)
	if !ok {
		t.Fatalf("the last record is %T, want a run.settled", last)
	}
	if settled.Reason != poo.ReasonEndTurn {
		t.Errorf("the settle reason is %q, want end_turn", settled.Reason)
	}
	if settled.RunID != "r1" {
		t.Errorf("the settle record's run id is %q, want r1", settled.RunID)
	}
	if settled.Seq == 0 {
		t.Error("the settle record has no sequence number")
	}
	if settled.TS == "" {
		t.Error("the settle record has no timestamp")
	}
}

func TestMalformedArgumentsAreRecordedAsText(t *testing.T) {
	// The model sent arguments that are not valid JSON. The record still
	// holds valid JSON, so a consumer can decode it without a special
	// case, and the raw text is preserved.
	tr := &fakeTransport{responses: []fakeResponse{
		{
			calls: []poo.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text": broken`),
			}},
		},
		{text: "finished"},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	r := poo.NewRun(testOptions(t, tr, tool, sink))
	if _, err := r.Execute(context.Background(), "do the thing"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	var call *poo.ToolCallRecord
	for _, rec := range sink.records {
		if c, ok := rec.(*poo.ToolCallRecord); ok {
			call = c
		}
	}
	if call == nil {
		t.Fatal("the log holds no tool.call record")
	}
	if !json.Valid(call.Arguments) {
		t.Fatalf("the recorded arguments are not valid JSON: %s", call.Arguments)
	}
	var text string
	if err := json.Unmarshal(call.Arguments, &text); err != nil {
		t.Fatalf("the recorded arguments are not a JSON string: %s", call.Arguments)
	}
	if text != `{"text": broken` {
		t.Errorf("the recorded arguments are %q, want the raw text", text)
	}
}
