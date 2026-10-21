package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// providerMessage is an assistant message with one visible block and one
// provider block of its own.
func providerMessage(block poo.ProviderBlock) poo.Message {
	return poo.Message{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentText, Text: "visible"},
			{Kind: poo.ContentProvider, Provider: &block},
		},
	}
}

func providerRequest(block poo.ProviderBlock) poo.ChatRequest {
	return poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi"), providerMessage(block)},
	}
}

func rawData(s string) json.RawMessage { return json.RawMessage(s) }

// TestAnOptionalForeignBlockIsOmitted covers a provider block for another
// API that the request does not need.
func TestAnOptionalForeignBlockIsOmitted(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	block := poo.ProviderBlock{API: poo.APIResponses, Type: "reasoning", Data: rawData(`"secret-payload"`)}
	reader, _, err := tr.Stream(context.Background(), providerRequest(block))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.bodies) != 1 {
		t.Fatalf("the server received %d bodies, want 1", len(srv.bodies))
	}
	var sent struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(srv.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Messages) != 2 || sent.Messages[1].Content != "visible" {
		t.Errorf("the sent messages are %+v, want the visible text alone", sent.Messages)
	}
	if contains(srv.bodies[0], "secret-payload") {
		t.Errorf("the body carries the foreign payload: %s", srv.bodies[0])
	}
}

// TestARequiredForeignBlockFailsTheRequest covers a provider block for
// another API that the request cannot do without.
func TestARequiredForeignBlockFailsTheRequest(t *testing.T) {
	srv := newForkServer(t)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	block := poo.ProviderBlock{API: poo.APIResponses, Type: "compaction", Data: rawData(`{}`), Required: true}

	_, _, err := tr.Stream(context.Background(), providerRequest(block))
	assertSessionIncompatible(t, err)
	if len(srv.bodies) != 0 {
		t.Errorf("the server received %d bodies, want none", len(srv.bodies))
	}

	// A fork's appended messages follow the same rule.
	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	_, err = tr.Fork(context.Background(), rendered, []poo.Message{providerMessage(block)}, 16)
	assertSessionIncompatible(t, err)
}

// A required block on a thinking block's companion is checked as well.
func TestARequiredForeignCompanionFailsTheRequest(t *testing.T) {
	tr := openai.New(openai.Config{BaseURL: "http://127.0.0.1:1", Model: "m"})
	block := poo.ProviderBlock{API: poo.APIAnthropicMessages, Type: "thinking_signature", Data: rawData(`"sig"`), Required: true}
	req := poo.ChatRequest{Model: "m", MaxTokens: 16, Messages: []poo.Message{{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "hmm", Provider: &block},
		},
	}}}
	_, _, err := tr.Stream(context.Background(), req)
	assertSessionIncompatible(t, err)
}

// TestAChatCompletionsBlockIsNotSentAndDoesNotFail covers the transport's
// own API: the wire has no field for opaque data, so the block is left
// out, required or not.
func TestAChatCompletionsBlockIsNotSentAndDoesNotFail(t *testing.T) {
	for _, required := range []bool{false, true} {
		srv := newForkServer(t)
		tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
		block := poo.ProviderBlock{API: poo.APIChatCompletions, Type: "x", Data: rawData(`"own-payload"`), Required: required}
		reader, _, err := tr.Stream(context.Background(), providerRequest(block))
		if err != nil {
			t.Fatalf("required=%v: starting the request: %v", required, err)
		}
		read(t, reader)
		reader.Close()
		if contains(srv.bodies[0], "own-payload") {
			t.Errorf("required=%v: the body carries the payload: %s", required, srv.bodies[0])
		}
	}
}

func assertSessionIncompatible(t *testing.T, err error) {
	t.Helper()
	var pe *poo.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("the error is %v, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindSessionIncompatible {
		t.Errorf("the kind is %q, want session_incompatible", pe.Kind)
	}
	if pe.Retryable() {
		t.Error("a session incompatibility is retryable, want terminal")
	}
}
