package poo_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

type sol2OrderPrepared string

func (p sol2OrderPrepared) Summary() string { return string(p) }

type sol2OrderTool struct{ slowStarted, fastFinished, release chan struct{} }

func (*sol2OrderTool) Spec() poo.ToolSpec {
	return poo.ToolSpec{Name: "orderread", Class: poo.ClassReadOnly, Schema: json.RawMessage(`{"type":"object"}`)}
}
func (*sol2OrderTool) Prepare(raw json.RawMessage, _ poo.PrepareEnv) (poo.Prepared, error) {
	var a struct{ Path string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return sol2OrderPrepared(a.Path), nil
}
func (t *sol2OrderTool) Call(_ context.Context, p poo.Prepared, _ poo.ToolEnv) (poo.ToolResult, error) {
	if p.(sol2OrderPrepared) == "slow" {
		close(t.slowStarted)
		<-t.release
		return poo.ToolResult{Text: "slow real output"}, nil
	}
	close(t.fastFinished)
	return poo.ToolResult{Text: "fast real output"}, nil
}
func TestCancellationPreservesCompletedParallelSibling(t *testing.T) {
	tool := &sol2OrderTool{slowStarted: make(chan struct{}), fastFinished: make(chan struct{}), release: make(chan struct{})}
	sink := pootest.NewSink()
	tr := pootest.NewTransport(pootest.Turn{Calls: []pootest.Call{{ID: "slow", Name: "orderread", Arguments: []byte(`{"path":"slow"}`)}, {ID: "fast", Name: "orderread", Arguments: []byte(`{"path":"fast"}`)}}}, pootest.Turn{Text: "done"})
	s, err := poo.New(poo.Options{Transport: tr, Tools: map[string]poo.Tool{"orderread": tool}, ToolOrder: []string{"orderread"}, Sink: sink, Model: poo.ModelInfo{ModelID: "test", ContextLimit: 32768}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer close(tool.release)
	poo.SetRecordWriteBudget(s, 20*time.Millisecond)
	if _, err = s.Prompt(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	<-tool.slowStarted
	<-tool.fastFinished
	// Give the already-completed fast call time to reach the record-order gate.
	time.Sleep(10 * time.Millisecond)
	s.Cancel("sol2")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range sink.Records() {
		if result, ok := r.(*poo.ToolResultRecord); ok && result.Handle == "r2" {
			if result.Outcome != poo.OutcomeOK {
				t.Fatalf("second call completed before cancel, but record=%s %q; want ok/fast real output", result.Outcome, result.Text)
			}
			return
		}
	}
	t.Fatal("no result for completed second call")
}
