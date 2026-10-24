package openai_test

import (
	"context"
	"os"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestTheChatTransportTalksToALiveEndpoint covers the live chat
// endpoint.
//
// The test is skipped unless the environment names one: the fixture
// tests cover the shapes, and this one covers a real server.
func TestTheChatTransportTalksToALiveEndpoint(t *testing.T) {
	url := os.Getenv("POO_CHAT_URL")
	model := os.Getenv("POO_CHAT_MODEL")
	if url == "" || model == "" {
		t.Skip("POO_CHAT_URL and POO_CHAT_MODEL are not set")
	}
	tr := openai.New(openai.Options{
		BaseURL: url,
		Model:   model,
		APIKey:  os.Getenv("POO_CHAT_API_KEY"),
	})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     model,
		MaxTokens: 16,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "Say hello in one word.")},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	var text string
	for {
		ev, err := reader.Next()
		if err != nil {
			break
		}
		if ev.Kind == "delta" && ev.Delta != nil {
			text += ev.Delta.Text
		}
	}
	if text == "" {
		t.Error("the live endpoint answered with no text")
	}
}
