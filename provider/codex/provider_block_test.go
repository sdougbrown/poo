package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
)

// The provider-block side: the history may carry opaque data another
// wire API produced. This transport can only give a block back to the
// API that produced it. An optional block is therefore left out of the
// request, and a required one makes the request impossible.

// foreignBlock is a block another wire API produced.
func foreignBlock(required bool) poo.ProviderBlock {
	return poo.ProviderBlock{
		API:      poo.APIAnthropicMessages,
		Type:     "thinking_signature",
		Data:     json.RawMessage(`"foreign-payload"`),
		Required: required,
	}
}

// providerMessage is an assistant message with one visible block and one
// provider block.
func providerMessage(block poo.ProviderBlock) poo.Message {
	return poo.Message{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "visible"},
			{Kind: poo.ContentProvider, Provider: &block},
		},
	}
}

// providerRequest is a history that holds the block.
func providerRequest(block poo.ProviderBlock) poo.ChatRequest {
	return poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi"), providerMessage(block)},
	}
}

// TestAnOptionalForeignBlockIsOmitted covers a block the request does not
// need. The visible text still travels, and the payload the endpoint cannot
// read is left out rather than sent.
func TestAnOptionalForeignBlockIsOmitted(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), providerRequest(foreignBlock(false)))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the one that carried the history", n)
	}
	req := srv.request(t, 0)
	if strings.Contains(string(req.body), "foreign-payload") {
		t.Errorf("the body carries the foreign payload: %s", req.body)
	}
	input := field[[]map[string]any](t, req, "input")
	if len(input) != 2 {
		t.Fatalf("the request carries %d input items, want the user message and the visible text: %s", len(input), req.body)
	}
	expectJSON(t, input[1],
		`{"type":"message","role":"assistant","id":"msg_poo_0","status":"completed","content":[{"type":"output_text","text":"visible","annotations":[]}]}`)
}

// toolResultMessage is a tool message whose result carries the block
// beside it.
func toolResultMessage(block poo.ProviderBlock) poo.Message {
	return poo.Message{Role: poo.RoleTool, Content: []poo.ContentBlock{
		{Kind: poo.ContentToolResult, ToolCallID: "call_1", Text: "a.txt"},
		{Kind: poo.ContentProvider, Provider: &block},
	}}
}

// userMessage is a user message whose text carries the block beside it.
func userMessage(block poo.ProviderBlock) poo.Message {
	return poo.Message{Role: poo.RoleUser, Content: []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "hi"},
		{Kind: poo.ContentProvider, Provider: &block},
	}}
}

// systemMessage is a system message whose text carries the block beside
// it.
func systemMessage(block poo.ProviderBlock) poo.Message {
	return poo.Message{Role: poo.RoleSystem, Content: []poo.ContentBlock{
		{Kind: poo.ContentText, Text: "you are terse"},
		{Kind: poo.ContentProvider, Provider: &block},
	}}
}

// TestARequiredForeignBlockFailsTheRequest covers a block the request
// cannot do without: the block is never sent, so the request is refused
// before it goes out.
func TestARequiredForeignBlockFailsTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	_, _, err := tr.Stream(context.Background(), providerRequest(foreignBlock(true)))
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none: the history holds a block the request cannot carry", n)
	}

	// A fork's appended messages follow the same rule.
	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	reader.Close()
	_, err = tr.Fork(context.Background(), rendered, []poo.Message{providerMessage(foreignBlock(true))}, 64, poo.ForkOptions{})
	assertSessionIncompatible(t, err)
}

// TestARequiredForeignCompanionFailsTheRequest covers a block that rides
// beside a thinking block: the companion is checked the same way.
func TestARequiredForeignCompanionFailsTheRequest(t *testing.T) {
	block := foreignBlock(true)
	tr := newTransport("http://127.0.0.1:1")
	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages: []poo.Message{{
			Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentThinking, Text: "hmm", Provider: &block},
			},
		}},
	})
	assertSessionIncompatible(t, err)
}

// TestARequiredBlockAfterAToolResultFailsTheRequest covers a block that
// follows a tool result in the same message. The block is refused rather than
// dropped. The request cannot carry it, and the history says it cannot do
// without it.
func TestARequiredBlockAfterAToolResultFailsTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)
	message := toolResultMessage(foreignBlock(true))

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{message},
	})
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none: the tool result carries a block the request cannot drop", n)
	}

	// A fork's appended messages follow the same rule.
	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	reader.Close()
	_, err = tr.Fork(context.Background(), rendered, []poo.Message{message}, 64, poo.ForkOptions{})
	assertSessionIncompatible(t, err)
}

// TestAnOptionalBlockAfterAToolResultIsOmitted covers a block the request
// does not need: the tool result travels and the payload is left out.
func TestAnOptionalBlockAfterAToolResultIsOmitted(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{toolResultMessage(foreignBlock(false))},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the one that carried the tool result", n)
	}
	req := srv.request(t, 0)
	if strings.Contains(string(req.body), "foreign-payload") {
		t.Errorf("the body carries the foreign payload: %s", req.body)
	}
	input := field[[]map[string]any](t, req, "input")
	if len(input) != 1 {
		t.Fatalf("the request carries %d input items, want the tool result alone: %s", len(input), req.body)
	}
	expectJSON(t, input[0], `{"type":"function_call_output","call_id":"call_1","output":"a.txt"}`)
}

// TestARequiredForeignBlockInAUserMessageFailsTheRequest covers a block the
// caller put in a user message. The block is refused rather than left out. The
// request cannot carry it, and the history says it cannot do without it.
func TestARequiredForeignBlockInAUserMessageFailsTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)
	message := userMessage(foreignBlock(true))

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{message},
	})
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none: the user message carries a block the request cannot drop", n)
	}

	// A fork's appended messages follow the same rule.
	reader, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "go on")},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	reader.Close()
	_, err = tr.Fork(context.Background(), rendered, []poo.Message{message}, 64, poo.ForkOptions{})
	assertSessionIncompatible(t, err)
}

// TestAnOptionalForeignBlockInAUserMessageIsOmitted covers a block the
// request does not need: the text travels and the payload is left out.
func TestAnOptionalForeignBlockInAUserMessageIsOmitted(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{userMessage(foreignBlock(false))},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the one that carried the text", n)
	}
	req := srv.request(t, 0)
	if strings.Contains(string(req.body), "foreign-payload") {
		t.Errorf("the body carries the foreign payload: %s", req.body)
	}
	input := field[[]map[string]any](t, req, "input")
	if len(input) != 1 {
		t.Fatalf("the request carries %d input items, want the user message alone: %s", len(input), req.body)
	}
	expectJSON(t, input[0], `{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`)
}

// TestARequiredForeignBlockInASystemMessageFailsTheRequest covers a block
// in the message the request turns into its instructions: the block is
// refused rather than dropped.
func TestARequiredForeignBlockInASystemMessageFailsTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{systemMessage(foreignBlock(true))},
	})
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none: the instructions carry a block the request cannot drop", n)
	}
}

// emptyBlock is a block this API produced that carries no payload.
func emptyBlock(required bool) poo.ProviderBlock {
	return poo.ProviderBlock{
		API:      poo.APIResponses,
		Type:     "compaction",
		Required: required,
	}
}

// TestARequiredEmptyBlockFailsTheRequest covers a required block of this API
// with no payload to give back. The request is refused, in a stream and in a
// fork's appended messages, rather than shaped as if the block were absent.
func TestARequiredEmptyBlockFailsTheRequest(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{userMessage(emptyBlock(true))},
	})
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 0 {
		t.Errorf("the endpoint received %d requests, want none: the history holds required data the request cannot drop", n)
	}

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

	_, err = tr.Fork(context.Background(), rendered, []poo.Message{userMessage(emptyBlock(true))}, 64, poo.ForkOptions{})
	assertSessionIncompatible(t, err)
	if n := srv.count(); n != 1 {
		t.Errorf("the endpoint received %d requests, want the one the fork was refused for", n)
	}
}

// TestAnOptionalEmptyBlockIsOmitted covers an optional block with no payload:
// the text travels and the empty block is left out.
func TestAnOptionalEmptyBlockIsOmitted(t *testing.T) {
	srv := newServer(t, completedStream)
	tr := newTransport(srv.URL)

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "gpt-6.1-sol",
		MaxTokens: 64,
		Messages:  []poo.Message{userMessage(emptyBlock(false))},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if n := srv.count(); n != 1 {
		t.Fatalf("the endpoint received %d requests, want the one that carried the text", n)
	}
	req := srv.request(t, 0)
	input := field[[]map[string]any](t, req, "input")
	if len(input) != 1 {
		t.Fatalf("the request carries %d input items, want the user message alone: %s", len(input), req.body)
	}
	expectJSON(t, input[0], `{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}`)
}

// assertSessionIncompatible reports that the failure is the one a history
// this API cannot carry settles with.
func assertSessionIncompatible(t *testing.T, err error) {
	t.Helper()
	var perr *poo.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("the error is %v, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindSessionIncompatible {
		t.Errorf("the kind is %q, want session_incompatible", perr.Kind)
	}
	if perr.Retryable() {
		t.Error("a session incompatibility is retryable, want terminal")
	}
}
