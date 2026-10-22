package poo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// blockingTool waits for its context to end before it returns.
type blockingTool struct {
	echoTool
	// started is closed once the call is running.
	started chan struct{}
}

// Call blocks until the run is cancelled.
func (t *blockingTool) Call(ctx context.Context, in poo.Prepared, env poo.ToolEnv) (poo.ToolResult, error) {
	close(t.started)
	<-ctx.Done()
	return poo.ToolResult{Text: "interrupted", IsError: true}, nil
}

// TestACancelDuringAToolStillSettlesIntoACancellingSink covers a run that
// is cancelled while a tool is running: the tool's result and the settle
// record are both written, because records are written with a context of
// their own.
func TestACancelDuringAToolStillSettlesIntoACancellingSink(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		calls: []poo.ToolCall{{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{}`)}},
	}}}
	sink := &cancellingSink{}
	tool := &blockingTool{started: make(chan struct{})}
	opts := testOptions(t, tr, &tool.echoTool, sink)
	opts.Tools["echo"] = tool

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-tool.started
		cancel()
	}()

	res, err := poo.NewRun(opts).Execute(ctx, "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonCancelled {
		t.Errorf("the run settled %q, want cancelled", res.Reason)
	}
	if sink.count("tool.result") != 1 {
		t.Errorf("the log holds %d tool.result records, want 1", sink.count("tool.result"))
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1; the sink refused %d writes",
			sink.count("run.settled"), sink.refused)
	}
}

// TestARefusedUsageWriteOnASuccessfulRequestDoesNotStopTheSettle covers a
// sink that refuses the usage record of a request that streamed to the
// end: the run still settles.
func TestARefusedUsageWriteOnASuccessfulRequestDoesNotStopTheSettle(t *testing.T) {
	tr := &fakeTransport{responses: []fakeResponse{{
		text:  "all done",
		usage: &poo.UsageReport{InputTokens: 10, OutputTokens: 2},
	}}}
	sink := &failingUsageSink{}
	tool := &echoTool{result: "echoed"}

	res, err := poo.NewRun(testOptions(t, tr, tool, sink)).Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if sink.count("run.settled") != 1 {
		t.Errorf("the log holds %d run.settled records, want 1", sink.count("run.settled"))
	}
	var seqs []int64
	for _, rec := range sink.records {
		seqs = append(seqs, rec.Meta().Seq)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Errorf("the record seqs are %v, want no gap where the refused usage was", seqs)
			break
		}
	}
}

// scriptedTransport answers every request with the same events.
type scriptedTransport struct {
	// turns holds the events for each request in order. The last repeats.
	turns [][]poo.StreamEvent
	// n is the next turn to serve.
	n int
}

// Stream serves the next turn's events.
func (s *scriptedTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	i := s.n
	if i >= len(s.turns) {
		i = len(s.turns) - 1
	} else {
		s.n++
	}
	return &fakeReader{events: s.turns[i]}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake: the compaction seam arrives with
// the fork.
func (s *scriptedTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, fmt.Errorf("scriptedTransport: no fork")
}

// TestTwoCallsAtOneIndexAreTwoCalls covers a provider that sends every
// parallel tool call at index 0, each with its own id: each id is its
// own call, rather than one call holding both names and both arguments.
func TestTwoCallsAtOneIndexAreTwoCalls(t *testing.T) {
	call := func(id, text string) poo.StreamEvent {
		return poo.StreamEvent{Kind: "tool_call", ToolCall: &poo.ToolCallDelta{
			Index: 0, ID: id, Name: "echo",
			Arguments: json.RawMessage(`{"text":"` + text + `"}`), Complete: true,
		}}
	}
	tr := &scriptedTransport{turns: [][]poo.StreamEvent{
		{call("call_a", "one"), call("call_b", "two"), {Kind: "finish", FinishReason: poo.FinishToolCalls}},
		{{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "done"}}, {Kind: "finish", FinishReason: poo.FinishStop}},
	}}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	res, err := poo.NewRun(testOptions(t, tr, tool, sink)).Execute(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if res.Reason != poo.ReasonEndTurn {
		t.Errorf("the run settled %q, want end_turn", res.Reason)
	}
	if len(tool.calls) != 2 {
		t.Fatalf("the tool ran %d times with %q, want 2 calls", len(tool.calls), tool.calls)
	}
	if !strings.Contains(tool.calls[0], "one") || !strings.Contains(tool.calls[1], "two") {
		t.Errorf("the calls are %q, want one then two", tool.calls)
	}
}

// runToolCalls runs one turn that streams the fragments and returns the
// arguments the tool ran with.
func runToolCalls(t *testing.T, fragments ...poo.ToolCallDelta) []string {
	t.Helper()
	var events []poo.StreamEvent
	for i := range fragments {
		events = append(events, poo.StreamEvent{Kind: "tool_call", ToolCall: &fragments[i]})
	}
	events = append(events, poo.StreamEvent{Kind: "finish", FinishReason: poo.FinishToolCalls})
	tr := &scriptedTransport{turns: [][]poo.StreamEvent{
		events,
		{{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "done"}}, {Kind: "finish", FinishReason: poo.FinishStop}},
	}}
	tool := &echoTool{result: "echoed"}
	if _, err := poo.NewRun(testOptions(t, tr, tool, &recordingSink{})).Execute(context.Background(), "go"); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	return tool.calls
}

func fragment(index int, id, name, args string) poo.ToolCallDelta {
	return poo.ToolCallDelta{Index: index, ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func TestInterleavedFragmentsAtOneIndexRejoinTheirCalls(t *testing.T) {
	got := runToolCalls(t,
		fragment(0, "a", "echo", `{"text":"on`),
		fragment(0, "b", "echo", `{"text":"tw`),
		fragment(0, "a", "", `e"}`),
		fragment(0, "b", "", `o"}`),
	)
	want := []string{`{"text":"one"}`, `{"text":"two"}`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the tool ran with %q, want %q", got, want)
	}
}

func TestACallAtAHighProviderIndexIsNotOverwrittenBySplitCalls(t *testing.T) {
	got := runToolCalls(t,
		fragment(500, "x", "echo", `{"text":"x"}`),
		fragment(0, "a", "echo", `{"text":"a"}`),
		fragment(0, "b", "echo", `{"text":"b"}`),
	)
	if len(got) != 3 {
		t.Fatalf("the tool ran with %q, want three distinct calls", got)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c] = true
	}
	if len(seen) != 3 {
		t.Errorf("the tool ran with %q, want x, a and b once each", got)
	}
}

func TestOnlyTheFirstFragmentCarriesTheID(t *testing.T) {
	got := runToolCalls(t,
		fragment(0, "a", "echo", `{"text":"on`),
		fragment(0, "", "", `e"}`),
		fragment(0, "b", "echo", `{"text":"tw`),
		fragment(0, "", "", `o"}`),
	)
	want := []string{`{"text":"one"}`, `{"text":"two"}`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the tool ran with %q, want %q", got, want)
	}
}
