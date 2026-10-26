package openai_test

import (
	"context"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// TestTextWithAToolCallsFinishIsNotAFailure covers the plan's
// guard on the empty-tool_calls failure: a response that carries text
// and ends with finish_reason "tool_calls" is an answer, not a server
// failure.
//
// No shipped test covers this: the text fixtures end with "stop".
func TestTextWithAToolCallsFinishIsNotAFailure(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"I will run the tests."},"finish_reason":null}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) +
		done
	srv := newSSEServer(t, body)
	tr := openai.New(fastRetry(openai.Options{Endpoint: srv.URL, Model: "m"}))

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var last error
	for {
		_, err := reader.Next()
		if err != nil {
			last = err
			break
		}
	}
	if last != nil && last.Error() != "EOF" {
		t.Fatalf("the stream failed: %v", last)
	}
	if len(srv.requests) != 1 {
		t.Errorf("the endpoint got %d requests, want 1", len(srv.requests))
	}
}
