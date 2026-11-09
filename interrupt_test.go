package poo_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo"
)

// acrossRunsTransport hands the third request a stream that carries a
// tool call and then breaks, so the interrupt lands on the session's
// second run.
type acrossRunsTransport struct {
	poo.ChatTransport
	n       atomic.Int32
	started chan struct{}
}

func (tr *acrossRunsTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	if tr.n.Add(1) == 3 {
		return &heldToolCallReader{ctx: ctx, started: tr.started}, poo.Rendered{API: poo.APIChatCompletions}, nil
	}
	return tr.ChatTransport.Stream(ctx, req)
}

// TestSecondRunInterruptDoesNotReuseOldHandle covers the skip lookup's
// turn identity: a call the interrupt skips on the session's second run
// keeps that run's handle, not one from an earlier run.
func TestSecondRunInterruptDoesNotReuseOldHandle(t *testing.T) {
	tr := &acrossRunsTransport{
		ChatTransport: scripted([]fakeResponse{toolCallTurn(), {text: "first run done"}, {text: "second run done"}}),
		started:       make(chan struct{}),
	}
	sink := &recordingSink{}
	s := newTestSession(t, testOptions(t, tr, &echoTool{result: "output"}, sink))
	runSession(t, s, "first run")
	if _, err := s.Prompt(context.Background(), "second run"); err != nil {
		t.Fatal(err)
	}
	<-tr.started
	if _, err := s.Send("interrupt", poo.Interrupt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.count("tool.result") != 2 {
		t.Fatalf("second run's interrupted c2 has no result: tool.result=%d (want 2), tool.call=%d", sink.count("tool.result"), sink.count("tool.call"))
	}
}
