package poo_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sdougbrown/poo"
)

// The fix-pass-2 reproducers: the defects the second review found in the
// compaction seam, the prompt variants, the Logger, the run seal and the
// job completion chain.

// fix2Compactor is a compactor whose two decisions the test scripts.
type fix2Compactor struct {
	should  func(poo.View, poo.ContextBudget) bool
	compact func(context.Context, poo.View, poo.ForkFunc) (poo.Compaction, error)
}

func (c *fix2Compactor) ShouldCompact(v poo.View, b poo.ContextBudget) bool {
	return c.should(v, b)
}

func (c *fix2Compactor) Compact(ctx context.Context, v poo.View, f poo.ForkFunc) (poo.Compaction, error) {
	return c.compact(ctx, v, f)
}

func fix2Turn() fakeResponse {
	return fakeResponse{calls: []poo.ToolCall{{CallID: "c1", Name: "echo", Arguments: []byte(`{"text":"one"}`)}}}
}

func fix2Summary() poo.Compaction {
	return poo.Compaction{Strategy: "summary", Replace: &poo.RangeReplacement{
		Cut:      poo.TurnRange{First: 1, Last: 1},
		Messages: []poo.Message{poo.NewTextMessage(poo.RoleUser, "summary")},
	}}
}

func fix2Window() poo.Compaction {
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

// TestSecondRunInterruptDoesNotReuseOldHandle covers the rule that a
// skip lookup uses the session's turn identity.
type fix2AcrossRunsTransport struct {
	poo.ChatTransport
	n       atomic.Int32
	started chan struct{}
}

func (tr *fix2AcrossRunsTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	if tr.n.Add(1) == 3 {
		return &fix2PartialReader{ctx: ctx, started: tr.started}, poo.Rendered{API: poo.APIChatCompletions}, nil
	}
	return tr.ChatTransport.Stream(ctx, req)
}

type fix2PartialReader struct {
	ctx     context.Context
	started chan struct{}
	sent    bool
}

func (r *fix2PartialReader) Next() (poo.StreamItem, error) {
	if !r.sent {
		r.sent = true
		return poo.StreamItem{Kind: "tool_call", ToolCall: &poo.ToolCallDelta{Index: 0, ID: "c2", Name: "echo", Arguments: []byte(`{}`), Complete: true}}, nil
	}
	close(r.started)
	<-r.ctx.Done()
	return poo.StreamItem{}, r.ctx.Err()
}

func (*fix2PartialReader) Close() error { return nil }

func TestSecondRunInterruptDoesNotReuseOldHandle(t *testing.T) {
	tr := &fix2AcrossRunsTransport{
		ChatTransport: scripted([]fakeResponse{fix2Turn(), {text: "first run done"}, {text: "second run done"}}),
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
