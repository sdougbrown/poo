package poo_test

import (
	"encoding/json"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// These tests drive the provider-block seam through a session. The core
// records the blocks a provider reports with the response that produced
// them, and gives them back on the next request. The scripted transport
// reports the blocks a turn names, so a test measures the recorded message
// and the next request.

// reasoningBlock returns one opaque provider block, shaped like the
// encrypted reasoning a storeless Responses endpoint sends.
func reasoningBlock(encrypted string) poo.ProviderBlock {
	return poo.ProviderBlock{
		API:  poo.APIResponses,
		Type: "reasoning",
		Data: json.RawMessage(`{"type":"reasoning","encrypted_content":"` + encrypted + `"}`),
	}
}

// blocksSession builds a session whose script is the turns, and returns it
// with the transport and the sink the record lands in.
func blocksSession(t *testing.T, turns ...pootest.Turn) (*poo.Session, *pootest.Transport, *recordingSink) {
	t.Helper()
	tr := pootest.NewTransport(turns...)
	sink := &recordingSink{}
	opts := testOptions(t, tr, &echoTool{}, sink)
	// The model speaks the Responses API, so the core keeps the blocks
	// that API produced instead of reading them as another provider's.
	opts.Model.API = poo.APIResponses
	return newTestSession(t, opts), tr, sink
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

// blockKinds names a message's block kinds for a failure message.
func blockKinds(blocks []poo.ContentBlock) []poo.ContentKind {
	kinds := make([]poo.ContentKind, len(blocks))
	for i, block := range blocks {
		kinds[i] = block.Kind
	}
	return kinds
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

// TestTheReportedReasoningRidesWithTheThinkingBlock covers the companion:
// the first reported block rides beside the thinking text it belongs to.
// The rest follow as blocks of their own ahead of the text. The next
// request gives them back in the order the response produced them.
func TestTheReportedReasoningRidesWithTheThinkingBlock(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	s, tr, sink := blocksSession(t,
		pootest.Turn{Thinking: "weighing", Text: "the answer", Blocks: []poo.ProviderBlock{first, second}},
		pootest.Turn{Text: "the second answer"},
	)

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
// reports blocks and no reasoning text. Every block rides alone ahead of
// the text, and the next request gives them back in order.
func TestTheReportedBlocksStandAloneWithoutThinking(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	s, tr, sink := blocksSession(t,
		pootest.Turn{Text: "the answer", Blocks: []poo.ProviderBlock{first, second}},
		pootest.Turn{Text: "the second answer"},
	)

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
// reports one block beside its reasoning. That block is the thinking
// block's companion, so the recorded message and the next request hold
// the thinking and the answer, with no lone block.
func TestTheOneReportedBlockRidesOnlyOnTheThinking(t *testing.T) {
	only := reasoningBlock("enc_one")
	s, tr, sink := blocksSession(t,
		pootest.Turn{Thinking: "weighing", Text: "the answer", Blocks: []poo.ProviderBlock{only}},
		pootest.Turn{Text: "the second answer"},
	)

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
		t.Errorf("the second request's second block is %q, want the answer's text", carried.Content[1].Kind)
	}
}

// TestTheReportedBlocksSurviveABrokenStream covers the partial path. A
// stream that breaks after its reasoning must not cost the endpoint that
// reasoning. The blocks are recorded with the partial message: the first
// beside the thinking, the rest on their own. The next request carries
// them back.
func TestTheReportedBlocksSurviveABrokenStream(t *testing.T) {
	first := reasoningBlock("enc_one")
	second := reasoningBlock("enc_two")
	s, tr, sink := blocksSession(t,
		pootest.Turn{
			Thinking: "weighing",
			Blocks:   []poo.ProviderBlock{first, second},
			Fail:     &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: "the stream broke"},
		},
		pootest.Turn{Text: "the second answer"},
	)

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
