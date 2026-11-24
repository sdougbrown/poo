package codex_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// A user message can carry a provider block this API produced: the
// provider-native window the core installs after a compaction is exactly that
// shape. The block is opaque and required, so it rides as the top-level input
// item it came from, in the place the message holds it.

// compactionItem is a compaction item the endpoint produced.
const compactionItem = `{"type":"compaction","id":"cmp_1","encrypted_content":"gAAAA-encrypted-window","summary":[{"type":"summary_text","text":"earlier turns"}]}`

// compactionBlock is the required block that carries compactionItem.
func compactionBlock() poo.ProviderBlock {
	return poo.ProviderBlock{
		API:      poo.APIResponses,
		Type:     "compaction",
		Data:     json.RawMessage(compactionItem),
		Required: true,
	}
}

// windowMessage is the message the core installs for a provider window: a
// user message made only of required provider blocks.
func windowMessage(blocks ...poo.ProviderBlock) poo.Message {
	content := make([]poo.ContentBlock, 0, len(blocks))
	for i := range blocks {
		content = append(content, poo.ContentBlock{Kind: poo.ContentProvider, Provider: &blocks[i]})
	}
	return poo.Message{Role: poo.RoleUser, Origin: poo.OriginCompaction, Content: content}
}

// reasoningBlock is the required block for a reasoning item the endpoint
// returned.
func reasoningBlock() poo.ProviderBlock {
	return poo.ProviderBlock{
		API:      poo.APIResponses,
		Type:     "reasoning",
		Data:     json.RawMessage(reasoningItem),
		Required: true,
	}
}

// TestAProviderWindowRidesAsItsOwnItems covers the history a compaction
// leaves: the window's items are the model's own compacted state, and every
// one of them is required. Each rides as the top-level item it came from,
// with its encrypted payload untouched.
func TestAProviderWindowRidesAsItsOwnItems(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	history := []poo.Message{
		windowMessage(compactionBlock(), reasoningBlock()),
		poo.NewTextMessage(poo.RoleUser, poo.CompactionMarkerText),
		poo.NewTextMessage(poo.RoleUser, "go on"),
	}
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  history,
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the one that carried the window", n)
	}
	req := srv.request(t, 0)
	for _, payload := range []string{compactionItem, reasoningItem} {
		if !strings.Contains(string(req.body), payload) {
			t.Errorf("the body does not carry %s verbatim: %s", payload, req.body)
		}
	}

	input := field[[]map[string]any](t, req, "input")
	want := []string{
		compactionItem,
		reasoningItem,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"` + poo.CompactionMarkerText + `"}]}`,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}`,
	}
	if len(input) != len(want) {
		t.Fatalf("the request carries %d input items, want the window and the messages: %s", len(input), req.body)
	}
	for i, item := range input {
		expectJSON(t, item, want[i])
	}
}

// TestAUserMessageKeepsTheOrderOfItsOpaqueBlocks covers a message that mixes
// text, an attachment and an opaque block. The block splits the message: the
// endpoint reads the text before it, the block, and the text after it, in
// history order.
func TestAUserMessageKeepsTheOrderOfItsOpaqueBlocks(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	compaction := compactionBlock()
	message := poo.Message{Role: poo.RoleUser, Content: []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "before"},
		{Kind: poo.ContentImage, MediaType: "image/png", Data: []byte("PNG")},
		{Kind: poo.ContentProvider, Provider: &compaction},
		{Kind: poo.ContentText, Text: "after"},
	}}
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "gpt-6.1-sol",
		ModelInfo: poo.ModelInfo{
			ModelID: "gpt-6.1-sol",
			Inputs:  []poo.Modality{poo.ModalityText, poo.ModalityImage},
		},
		Messages:  []poo.Message{message},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	req := srv.request(t, 0)
	input := field[[]map[string]any](t, req, "input")
	want := []string{
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"before"},{"type":"input_image","detail":"auto","image_url":"data:image/png;base64,UE5H"}]}`,
		compactionItem,
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"after"}]}`,
	}
	if len(input) != len(want) {
		t.Fatalf("the request carries %d input items, want the text, the block and the text: %s", len(input), req.body)
	}
	for i, item := range input {
		expectJSON(t, item, want[i])
	}
}

// TestAForkCarriesAnAppendedOpaqueBlock covers a fork whose appended message
// carries a required block of the API the snapshot was shaped for. The block
// rides after the snapshot's items, as its own item.
func TestAForkCarriesAnAppendedOpaqueBlock(t *testing.T) {
	srv := newServer(t, completedStream, completedStream)
	tr := newTransport(srv.URL)

	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	reader.Close()

	forked, err := tr.Fork(context.Background(), rendered, []poo.Message{windowMessage(compactionBlock())}, 64, poo.ForkOptions{})
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer forked.Close()
	read(t, forked)

	req := srv.request(t, 1)
	if !strings.Contains(string(req.body), compactionItem) {
		t.Errorf("the forked body does not carry the block verbatim: %s", req.body)
	}
	input := field[[]map[string]any](t, req, "input")
	want := []string{
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`,
		compactionItem,
	}
	if len(input) != len(want) {
		t.Fatalf("the request carries %d input items, want the snapshot's message and the appended block: %s", len(input), req.body)
	}
	for i, item := range input {
		expectJSON(t, item, want[i])
	}
}
