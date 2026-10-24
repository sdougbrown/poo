package poo_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sdougbrown/poo"
)

// The origin of a tool call.
//
// The plan: a call the model wrote as text and a transport read out of
// it is flagged "parsed_text", and the flag reaches the record.

// originTransport asks for a tool once and then answers.
//
// The call carries an origin, so a test can see whether the origin
// reaches the record.
type originTransport struct {
	mu sync.Mutex
	// requests counts the requests the transport answered.
	requests int
	// origin is the origin the call carries.
	origin poo.CallOrigin
}

// Stream answers with the call, and with an answer after it.
func (t *originTransport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	t.mu.Lock()
	t.requests++
	first := t.requests == 1
	t.mu.Unlock()

	var events []poo.StreamItem
	if first {
		events = append(events, poo.StreamItem{
			Kind: "tool_call",
			ToolCall: &poo.ToolCallDelta{
				Index:     0,
				ID:        "call_1",
				Name:      "echo",
				Arguments: json.RawMessage(`{"text":"hi"}`),
				Complete:  true,
				Origin:    t.origin,
			},
		})
		events = append(events, poo.StreamItem{Kind: "finish", FinishReason: poo.FinishToolCalls})
	} else {
		events = append(events, poo.StreamItem{Kind: "delta", Delta: &poo.Delta{Kind: "text", Text: "the answer"}})
		events = append(events, poo.StreamItem{Kind: "finish", FinishReason: poo.FinishStop})
	}
	return &fakeReader{events: events}, poo.Rendered{API: poo.APIChatCompletions}, nil
}

// Fork is not implemented by the fake: the compaction seam's fork is
// exercised with the scripted transport.
func (t *originTransport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	return nil, errNoFork
}

// TestTheCallOriginReachesTheRecord covers the seam: the origin a
// transport gave a call is the origin the record carries.
func TestTheCallOriginReachesTheRecord(t *testing.T) {
	tr := &originTransport{origin: poo.CallOriginParsedText}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	var found bool
	for _, msg := range sink.messages() {
		if msg.Role != poo.RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolCall {
				continue
			}
			found = true
			if block.Origin != poo.CallOriginParsedText {
				t.Errorf("the call's origin is %q, want parsed_text", block.Origin)
			}
		}
	}
	if !found {
		t.Fatal("the log holds no tool call block")
	}
}

// TestTheCallOriginIsEmptyWhenTheTransportSaysNothing covers the other
// side: a transport that names no origin leaves the field empty, and the
// record says so.
func TestTheCallOriginIsEmptyWhenTheTransportSaysNothing(t *testing.T) {
	tr := &originTransport{}
	sink := &recordingSink{}
	tool := &echoTool{result: "echoed"}

	s := newTestSession(t, testOptions(t, tr, tool, sink))
	runSession(t, s, "do the thing")

	var found bool
	for _, msg := range sink.messages() {
		if msg.Role != poo.RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind != poo.ContentToolCall {
				continue
			}
			found = true
			if block.Origin != "" {
				t.Errorf("the call's origin is %q, want none", block.Origin)
			}
		}
	}
	if !found {
		t.Fatal("the log holds no tool call block")
	}
}
