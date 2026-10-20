package poo_test

import (
	"context"
	"encoding/json"
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
