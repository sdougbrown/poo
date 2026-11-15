package poo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The core records the opaque blocks a provider reports with the response
// that produced them, and gives them back on the next request. The tests
// drive that seam through a session: the readers are root test fakes with
// a block reporter, and the record and the next request are what they
// measure.

// reasoningBlock returns one opaque provider block, shaped like the
// encrypted reasoning a storeless Responses endpoint sends.
func reasoningBlock(encrypted string) poo.ProviderBlock {
	return poo.ProviderBlock{
		API:  poo.APIResponses,
		Type: "reasoning",
		Data: json.RawMessage(`{"type":"reasoning","encrypted_content":"` + encrypted + `"}`),
	}
}

// blockEvents returns the events of a completed response.
func blockEvents(thinking, text string) []poo.StreamItem {
	var events []poo.StreamItem
	if thinking != "" {
		events = append(events, poo.StreamItem{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "thinking", Text: thinking},
		})
	}
	if text != "" {
		events = append(events, poo.StreamItem{
			Kind:  "delta",
			Delta: &poo.Delta{Kind: "text", Text: text},
		})
	}
	return append(events, poo.StreamItem{Kind: "finish", FinishReason: poo.FinishStop})
}

// providerBlockReader is a root fakeReader with the block reporter seam a
// storeless provider implements. When err is set, the stream reports it
// after its events, so a test can drive the partial path.
type providerBlockReader struct {
	fakeReader
	blocks []poo.ProviderBlock
	err    error
	// failed reports whether the reader already gave its failure out.
	failed bool
}

// ReportProviderBlocks returns the reported blocks.
func (r *providerBlockReader) ReportProviderBlocks() []poo.ProviderBlock { return r.blocks }

// Next hands out the events and then the failure.
func (r *providerBlockReader) Next() (poo.StreamItem, error) {
	ev, err := r.fakeReader.Next()
	if r.err != nil && errors.Is(err, io.EOF) && !r.failed {
		r.failed = true
		return poo.StreamItem{}, r.err
	}
	return ev, err
}

// providerBlockTransport serves its readers in order and keeps the
// requests, so a test can read what a later request carried.
type providerBlockTransport struct {
	mu       sync.Mutex
	readers  []poo.StreamReader
	served   int
	requests []poo.ChatRequest
}

// Stream hands out the next reader.
func (t *providerBlockTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, req)
	if t.served >= len(t.readers) {
		return nil, poo.Rendered{}, pootest.ErrScriptExhausted
	}
	reader := t.readers[t.served]
	t.served++
	return reader, poo.Rendered{API: poo.APIResponses}, nil
}

// Fork is not implemented by the fake.
func (t *providerBlockTransport) Fork(context.Context, poo.Rendered, []poo.Message, int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// Requests returns the requests the transport received, in order.
func (t *providerBlockTransport) Requests() []poo.ChatRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]poo.ChatRequest(nil), t.requests...)
}

// recordedAssistant returns the assistant message record the sink kept
// so far.
func recordedAssistant(t *testing.T, sink *recordingSink) *poo.MessageRecord {
	t.Helper()
	var assistant *poo.MessageRecord
	for _, rec := range sink.messages() {
		if rec.Role == poo.RoleAssistant {
			assistant = rec
		}
	}
	if assistant == nil {
		t.Fatalf("the record holds no assistant message; the records are %v", sink.Names())
	}
	return assistant
}

// assistantIn returns the assistant message a request carried.
func assistantIn(t *testing.T, messages []poo.Message) poo.Message {
	t.Helper()
	for _, m := range messages {
		if m.Role == poo.RoleAssistant {
			return m
		}
	}
	t.Fatal("the request carried no assistant message")
	return poo.Message{}
}

// wantBlock fails unless the block holds want's payload.
func wantBlock(t *testing.T, block *poo.ProviderBlock, want poo.ProviderBlock) {
	t.Helper()
	if block == nil {
		t.Fatalf("the block is missing, want the payload %s", want.Data)
	}
	if block.API != want.API || block.Type != want.Type || string(block.Data) != string(want.Data) {
		t.Errorf("the block is %s %s %s, want %s %s %s",
			block.API, block.Type, block.Data, want.API, want.Type, want.Data)
	}
}

// blockKinds names the kinds of a message's blocks, for a failure message.
func blockKinds(blocks []poo.ContentBlock) []poo.ContentKind {
	kinds := make([]poo.ContentKind, len(blocks))
	for i, block := range blocks {
		kinds[i] = block.Kind
	}
	return kinds
}

// TestTheReportedReasoningRidesWithTheThinkingBlock covers the companion:
// the first reported block rides beside the thinking text it belongs to,
// the rest follow it as blocks of their own ahead of the text, and the
// next request gives them back in the order the response produced them.
func TestTheReportedReasoningRidesWithTheThinkingBlock(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	tr := &providerBlockTransport{readers: []poo.StreamReader{
		&providerBlockReader{
			fakeReader: fakeReader{events: blockEvents("weighing", "the answer")},
			blocks:     []poo.ProviderBlock{first, second},
		},
		&providerBlockReader{fakeReader: fakeReader{events: blockEvents("", "the second answer")}},
	}}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.API = poo.APIResponses
	s := newTestSession(t, opts)

	runSession(t, s, "first")

	msg := recordedAssistant(t, sink)
	if len(msg.Content) != 3 {
		t.Fatalf("the recorded message holds %v, want thinking, one lone block and text", blockKinds(msg.Content))
	}
	if msg.Content[0].Kind != poo.ContentThinking || msg.Content[0].Text != "weighing" {
		t.Errorf("the first block is %q %q, want the thinking", msg.Content[0].Kind, msg.Content[0].Text)
	}
	wantBlock(t, msg.Content[0].Provider, first)
	if msg.Content[1].Kind != poo.ContentProvider {
		t.Errorf("the second block is %q, want a lone provider block", msg.Content[1].Kind)
	}
	wantBlock(t, msg.Content[1].Provider, second)
	if msg.Content[2].Kind != poo.ContentText || msg.Content[2].Text != "the answer" {
		t.Errorf("the last block is %q %q, want the answer's text", msg.Content[2].Kind, msg.Content[2].Text)
	}

	runSession(t, s, "second")

	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want two", len(requests))
	}
	carried := assistantIn(t, requests[1].Messages)
	if len(carried.Content) != 3 {
		t.Fatalf("the second request's assistant message holds %v, want the three blocks", blockKinds(carried.Content))
	}
	wantBlock(t, carried.Content[0].Provider, first)
	if carried.Content[1].Kind != poo.ContentProvider {
		t.Errorf("the second request's second block is %q, want the lone provider block", carried.Content[1].Kind)
	}
	wantBlock(t, carried.Content[1].Provider, second)
}

// TestTheReportedBlocksStandAloneWithoutThinking covers a response that
// reports blocks and no reasoning text: every block rides alone, ahead of
// the text, and the next request gives them back in order.
func TestTheReportedBlocksStandAloneWithoutThinking(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	tr := &providerBlockTransport{readers: []poo.StreamReader{
		&providerBlockReader{
			fakeReader: fakeReader{events: blockEvents("", "the answer")},
			blocks:     []poo.ProviderBlock{first, second},
		},
		&providerBlockReader{fakeReader: fakeReader{events: blockEvents("", "the second answer")}},
	}}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.API = poo.APIResponses
	s := newTestSession(t, opts)

	runSession(t, s, "first")

	msg := recordedAssistant(t, sink)
	if len(msg.Content) != 3 {
		t.Fatalf("the recorded message holds %v, want two lone blocks and text", blockKinds(msg.Content))
	}
	if msg.Content[0].Kind != poo.ContentProvider {
		t.Errorf("the first block is %q, want a lone provider block", msg.Content[0].Kind)
	}
	wantBlock(t, msg.Content[0].Provider, first)
	if msg.Content[1].Kind != poo.ContentProvider {
		t.Errorf("the second block is %q, want a lone provider block", msg.Content[1].Kind)
	}
	wantBlock(t, msg.Content[1].Provider, second)
	if msg.Content[2].Kind != poo.ContentText || msg.Content[2].Text != "the answer" {
		t.Errorf("the last block is %q %q, want the answer's text", msg.Content[2].Kind, msg.Content[2].Text)
	}

	runSession(t, s, "second")

	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want two", len(requests))
	}
	carried := assistantIn(t, requests[1].Messages)
	if len(carried.Content) != 3 {
		t.Fatalf("the second request's assistant message holds %v, want the three blocks", blockKinds(carried.Content))
	}
	wantBlock(t, carried.Content[0].Provider, first)
	wantBlock(t, carried.Content[1].Provider, second)
}

// TestTheOneReportedBlockRidesOnlyOnTheThinking covers a response that
// reports a single block beside its reasoning: the block is the thinking
// block's companion, so the recorded message and the next request hold
// the thinking and the answer, with no lone block.
func TestTheOneReportedBlockRidesOnlyOnTheThinking(t *testing.T) {
	only := reasoningBlock("enc_one")
	tr := &providerBlockTransport{readers: []poo.StreamReader{
		&providerBlockReader{
			fakeReader: fakeReader{events: blockEvents("weighing", "the answer")},
			blocks:     []poo.ProviderBlock{only},
		},
		&providerBlockReader{fakeReader: fakeReader{events: blockEvents("", "the second answer")}},
	}}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.API = poo.APIResponses
	s := newTestSession(t, opts)

	runSession(t, s, "first")

	msg := recordedAssistant(t, sink)
	if len(msg.Content) != 2 {
		t.Fatalf("the recorded message holds %v, want the thinking and the text", blockKinds(msg.Content))
	}
	if msg.Content[0].Kind != poo.ContentThinking || msg.Content[0].Text != "weighing" {
		t.Errorf("the first block is %q %q, want the thinking", msg.Content[0].Kind, msg.Content[0].Text)
	}
	wantBlock(t, msg.Content[0].Provider, only)
	if msg.Content[1].Kind != poo.ContentText || msg.Content[1].Text != "the answer" {
		t.Errorf("the second block is %q %q, want the answer's text", msg.Content[1].Kind, msg.Content[1].Text)
	}

	runSession(t, s, "second")

	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want two", len(requests))
	}
	carried := assistantIn(t, requests[1].Messages)
	if len(carried.Content) != 2 {
		t.Fatalf("the second request's assistant message holds %v, want the thinking and the text", blockKinds(carried.Content))
	}
	wantBlock(t, carried.Content[0].Provider, only)
	if carried.Content[1].Kind != poo.ContentText || carried.Content[1].Text != "the answer" {
		t.Errorf("the second request's second block is %q %q, want the answer's text", carried.Content[1].Kind, carried.Content[1].Text)
	}
}

// TestTheReportedBlocksSurviveABrokenStream covers the partial path: a
// stream that breaks after its reasoning still costs the endpoint its
// reasoning, so the blocks are recorded with the partial message, the
// first beside the thinking and the rest on their own, and the next
// request carries them back.
func TestTheReportedBlocksSurviveABrokenStream(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	tr := &providerBlockTransport{readers: []poo.StreamReader{
		&providerBlockReader{
			fakeReader: fakeReader{events: []poo.StreamItem{
				{Kind: "delta", Delta: &poo.Delta{Kind: "thinking", Text: "weighing"}},
			}},
			blocks: []poo.ProviderBlock{first, second},
			err:    &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: "the stream broke"},
		},
		&providerBlockReader{fakeReader: fakeReader{events: blockEvents("", "the second answer")}},
	}}
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	opts.Model.API = poo.APIResponses
	s := newTestSession(t, opts)

	res := runSession(t, s, "first")
	if res.Reason != poo.ReasonProviderError {
		t.Fatalf("the run settled %q, want provider_error", res.Reason)
	}

	msg := recordedAssistant(t, sink)
	if !msg.Partial {
		t.Error("the recorded message is not partial, want the broken stream's part")
	}
	if msg.Cause != poo.CauseProviderError {
		t.Errorf("the partial message's cause is %q, want %q", msg.Cause, poo.CauseProviderError)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("the partial message holds %v, want the thinking and one lone block", blockKinds(msg.Content))
	}
	if msg.Content[0].Kind != poo.ContentThinking || msg.Content[0].Text != "weighing" {
		t.Errorf("the first block is %q %q, want the thinking", msg.Content[0].Kind, msg.Content[0].Text)
	}
	wantBlock(t, msg.Content[0].Provider, first)
	if msg.Content[1].Kind != poo.ContentProvider {
		t.Errorf("the second block is %q, want a lone provider block", msg.Content[1].Kind)
	}
	wantBlock(t, msg.Content[1].Provider, second)

	runSession(t, s, "second")

	requests := tr.Requests()
	if len(requests) != 2 {
		t.Fatalf("the transport got %d requests, want two", len(requests))
	}
	carried := assistantIn(t, requests[1].Messages)
	if len(carried.Content) != 2 {
		t.Fatalf("the second request's assistant message holds %v, want the thinking and one lone block", blockKinds(carried.Content))
	}
	if carried.Content[0].Kind != poo.ContentThinking || carried.Content[0].Text != "weighing" {
		t.Errorf("the second request's first block is %q %q, want the thinking", carried.Content[0].Kind, carried.Content[0].Text)
	}
	wantBlock(t, carried.Content[0].Provider, first)
	if carried.Content[1].Kind != poo.ContentProvider {
		t.Errorf("the second request's second block is %q, want the lone provider block", carried.Content[1].Kind)
	}
	wantBlock(t, carried.Content[1].Provider, second)
}
