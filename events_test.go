package poo_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
)

// deltaTransport answers with a fixed set of stream events.
type deltaTransport struct {
	events []poo.StreamEvent
}

// Stream hands out the fixed events.
func (t *deltaTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	return &fakeReader{events: t.events}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake.
func (t *deltaTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// TestMessageDeltaCarriesItsFragment covers the streaming event: every
// delta carries the fragment's text, so a client can display it and
// reconstruct the message across several multi-byte fragments.
func TestMessageDeltaCarriesItsFragment(t *testing.T) {
	tr := &deltaTransport{events: []poo.StreamEvent{
		{Kind: "delta", Delta: &poo.Delta{Kind: "thinking", Text: "rea"}},
		{Kind: "delta", Delta: &poo.Delta{Kind: "thinking", Text: "soning 🐈"}},
		{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "hel"}},
		{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "lo "}},
		{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "🐈!"}},
		{Kind: "finish", FinishReason: poo.FinishStop},
	}}
	s := newTestSession(t, testOptions(t, tr, &echoTool{}, &recordingSink{}))
	runSession(t, s, "start")

	text := map[string]string{}
	for _, ev := range s.Events() {
		if ev.Name != poo.EventMessageDelta {
			continue
		}
		fragment, ok := ev.Payload["text"].(string)
		if !ok {
			t.Fatalf("message.delta has no text: %v", ev.Payload)
		}
		kind, _ := ev.Payload["kind"].(string)
		text[kind] += fragment
	}
	if got, want := text["text"], "hello 🐈!"; got != want {
		t.Errorf("the text deltas reconstruct %q, want %q", got, want)
	}
	if got, want := text["thinking"], "reasoning 🐈"; got != want {
		t.Errorf("the thinking deltas reconstruct %q, want %q", got, want)
	}
}
