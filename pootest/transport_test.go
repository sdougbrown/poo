package pootest_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/pootest"
)

// The scripted transport serves a script in order, hands out the snapshot
// of each request and says so when the script runs out.

// read drains a stream and returns the events.
func read(t *testing.T, reader poo.StreamReader) []poo.StreamEvent {
	t.Helper()
	var events []poo.StreamEvent
	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		events = append(events, ev)
	}
}

// TestTheScriptedTransportServesTheScript covers the transport: the
// responses come out in order, and the script running out is a typed
// error rather than a repeated response.
func TestTheScriptedTransportServesTheScript(t *testing.T) {
	tr := pootest.NewTransport(
		pootest.Turn{Text: "first"},
		pootest.Turn{Text: "second"},
	)

	for i, want := range []string{"first", "second"} {
		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
		if err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
		var text string
		for _, ev := range read(t, reader) {
			if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "text" {
				text += ev.Delta.Text
			}
		}
		if text != want {
			t.Errorf("stream %d carried %q, want %q", i, text, want)
		}
		reader.Close()
	}

	if _, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"}); !errors.Is(err, pootest.ErrScriptExhausted) {
		t.Errorf("the exhausted script returned %v, want ErrScriptExhausted", err)
	}
}

// TestTheScriptedTransportHandsOutToolCalls covers the tool calls: they
// come out in order, each with its own id.
func TestTheScriptedTransportHandsOutToolCalls(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{
		Calls: []pootest.Call{
			{Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
			{Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)},
		},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var names []string
	for _, ev := range read(t, reader) {
		if ev.Kind == "tool_call" && ev.ToolCall != nil {
			names = append(names, ev.ToolCall.Name)
			if ev.ToolCall.ID == "" {
				t.Error("a tool call has no id")
			}
		}
	}
	if len(names) != 2 || names[0] != "bash" || names[1] != "read" {
		t.Errorf("the calls are %v, want bash and read", names)
	}
}

// TestTheScriptedTransportRecordsTheRequests covers the recorded
// requests and the snapshots.
func TestTheScriptedTransportRecordsTheRequests(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "ok"})

	req := poo.ChatRequest{
		Model:     "m",
		MaxTokens: 512,
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleUser, "the prompt"),
		},
	}
	_, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	if rendered.API != poo.APIChatCompletions {
		t.Errorf("the snapshot's api is %q, want chat_completions", rendered.API)
	}
	if rendered.Messages != 1 {
		t.Errorf("the snapshot counts %d messages, want 1", rendered.Messages)
	}
	if len(rendered.Body) == 0 {
		t.Error("the snapshot holds no body")
	}

	requests := tr.Requests()
	if len(requests) != 1 {
		t.Fatalf("the transport served %d requests, want 1", len(requests))
	}
	if requests[0].Model != "m" {
		t.Errorf("the request's model is %q, want m", requests[0].Model)
	}
	if requests[0].MaxTokens != 512 {
		t.Errorf("the request's ceiling is %d, want 512", requests[0].MaxTokens)
	}
}

// TestTheScriptedTransportForks covers the fork: the transport records
// what the fork carried and serves the forked responses.
func TestTheScriptedTransportForks(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "main"})
	tr.SetForkTurns(pootest.Turn{Text: "forked"})

	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}

	appended := []poo.Message{poo.NewTextMessage(poo.RoleUser, "the appended message")}
	reader, err := tr.Fork(context.Background(), rendered, appended, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()

	var text string
	for _, ev := range read(t, reader) {
		if ev.Kind == "delta" && ev.Delta != nil {
			text += ev.Delta.Text
		}
	}
	if text != "forked" {
		t.Errorf("the fork carried %q, want forked", text)
	}

	forks := tr.Forks()
	if len(forks) != 1 {
		t.Fatalf("the transport served %d forks, want 1", len(forks))
	}
	if forks[0].MaxTokens != 2048 {
		t.Errorf("the fork's ceiling is %d, want 2048", forks[0].MaxTokens)
	}
	if len(forks[0].Appended) != 1 {
		t.Fatalf("the fork appended %d messages, want 1", len(forks[0].Appended))
	}
	if forks[0].Appended[0].Text() != "the appended message" {
		t.Errorf("the fork appended %q, want the appended message", forks[0].Appended[0].Text())
	}
}

// TestTheRecordedRequestsAreCopies covers the transport's copies: a test
// that changes what it got back does not change what the transport
// recorded.
func TestTheRecordedRequestsAreCopies(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "ok"})

	req := poo.ChatRequest{
		Model: "m",
		Messages: []poo.Message{{
			Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{
				{Kind: poo.ContentToolCall, ToolCallID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
				{
					Kind: poo.ContentThinking,
					Text: "the thinking",
					Provider: &poo.ProviderBlock{
						API:  poo.APIAnthropicMessages,
						Type: "thinking_signature",
						Data: json.RawMessage(`"sig"`),
					},
				},
			},
		}},
	}
	if _, _, err := tr.Stream(context.Background(), req); err != nil {
		t.Fatalf("starting the request: %v", err)
	}

	got := tr.Requests()
	got[0].Messages[0].Content[0].Arguments[2] = 'X'
	got[0].Messages[0].Content[1].Provider.Data[1] = 'X'

	again := tr.Requests()
	if string(again[0].Messages[0].Content[0].Arguments) != `{"command":"ls"}` {
		t.Errorf("the recorded arguments are %s, want the original", again[0].Messages[0].Content[0].Arguments)
	}
	if string(again[0].Messages[0].Content[1].Provider.Data) != `"sig"` {
		t.Errorf("the recorded provider payload is %s, want the original", again[0].Messages[0].Content[1].Provider.Data)
	}
}

// TestTheRecordedForksAreCopies covers the fork's copies: the snapshot's
// body is copied, so a test that changes it changes nothing.
func TestTheRecordedForksAreCopies(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "ok"})
	tr.SetForkTurns(pootest.Turn{Text: "forked"})

	_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	appended := []poo.Message{poo.NewTextMessage(poo.RoleUser, "the appended message")}
	reader, err := tr.Fork(context.Background(), rendered, appended, 2048)
	if err != nil {
		t.Fatalf("forking: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	forks := tr.Forks()
	if len(forks) != 1 {
		t.Fatalf("the transport served %d forks, want 1", len(forks))
	}
	if len(forks[0].Prev.Body) > 0 {
		forks[0].Prev.Body[0] = 'X'
	}
	again := tr.Forks()
	if string(again[0].Prev.Body) != string(rendered.Body) {
		t.Errorf("the recorded body is %s, want the original", again[0].Prev.Body)
	}
}

// TestTheScriptedTransportFinishesAToolCallTurn covers the finish
// reason: a turn that made tool calls finishes as a tool call, the way
// a real provider reports it, so a test can drive the core's tool-call
// branch.
func TestTheScriptedTransportFinishesAToolCallTurn(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{
		Calls: []pootest.Call{{Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var finish poo.FinishReason
	for _, ev := range read(t, reader) {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	if finish != poo.FinishToolCalls {
		t.Errorf("the turn finished %q, want tool_calls", finish)
	}
}

// TestTheScriptedTransportFinishesAPlainTurnAsAStop covers the other
// side: a turn with no tool calls finishes as a stop.
func TestTheScriptedTransportFinishesAPlainTurnAsAStop(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{Text: "the answer"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var finish poo.FinishReason
	for _, ev := range read(t, reader) {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	if finish != poo.FinishStop {
		t.Errorf("the turn finished %q, want stop", finish)
	}
}

// TestTheScriptedFinishReasonWins covers the script: a turn that names
// its own finish reason keeps it.
func TestTheScriptedFinishReasonWins(t *testing.T) {
	tr := pootest.NewTransport(pootest.Turn{
		Text:   "the answer",
		Finish: "length",
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	var finish poo.FinishReason
	for _, ev := range read(t, reader) {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	if finish != "length" {
		t.Errorf("the turn finished %q, want length", finish)
	}
}

// TestTheSnapshotDistinguishesTheRequestsItHolds covers the snapshot's
// body: two requests that differ differ in their bodies, so a test can
// tell a request with a tool call from one without, and one with
// thinking from one without.
func TestTheSnapshotDistinguishesTheRequestsItHolds(t *testing.T) {
	call := poo.Message{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{{
			Kind:       poo.ContentToolCall,
			ToolCallID: "call_1",
			Name:       "bash",
			Arguments:  json.RawMessage(`{"command":"ls"}`),
		}},
	}
	thinking := poo.Message{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "the thinking"},
			{Kind: poo.ContentText, Text: "the answer"},
		},
	}
	plain := poo.NewTextMessage(poo.RoleAssistant, "the answer")

	bodyOf := func(messages ...poo.Message) string {
		tr := pootest.NewTransport(pootest.Turn{Text: "ok"})
		_, rendered, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", Messages: messages})
		if err != nil {
			t.Fatalf("starting the request: %v", err)
		}
		return string(rendered.Body)
	}

	plainBody := bodyOf(plain)
	callBody := bodyOf(call)
	thinkingBody := bodyOf(thinking)

	if plainBody == callBody {
		t.Errorf("a request with a tool call has the same body as one without: %s", plainBody)
	}
	if plainBody == thinkingBody {
		t.Errorf("a request with thinking has the same body as one without: %s", plainBody)
	}
	if callBody == thinkingBody {
		t.Errorf("a request with a tool call has the same body as one with thinking: %s", callBody)
	}

	// A signature on the thinking and the error flag on a tool result
	// change what the provider receives, so they change the body.
	signed := poo.Message{
		Role: poo.RoleAssistant,
		Content: []poo.ContentBlock{
			{Kind: poo.ContentThinking, Text: "the thinking", Provider: &poo.ProviderBlock{API: "anthropic_messages", Type: "thinking_signature", Data: json.RawMessage(`"sig"`)}},
			{Kind: poo.ContentText, Text: "the answer"},
		},
	}
	if bodyOf(signed) == thinkingBody {
		t.Errorf("a request with signed thinking has the same body as one without the signature: %s", thinkingBody)
	}
	result := func(isError bool) poo.Message {
		return poo.Message{Role: poo.RoleTool, Content: []poo.ContentBlock{{Kind: poo.ContentToolResult, ToolCallID: "call_1", Text: "out", IsError: isError}}}
	}
	if bodyOf(result(true)) == bodyOf(result(false)) {
		t.Errorf("a failed tool result has the same body as a successful one: %s", bodyOf(result(false)))
	}
	// The tool call's name reaches the body, so a test can see what the
	// model was asked to do.
	if !strings.Contains(callBody, "bash") {
		t.Errorf("the body's call does not name the tool: %s", callBody)
	}
	// The thinking reaches the body too.
	if !strings.Contains(thinkingBody, "the thinking") {
		t.Errorf("the body does not carry the thinking: %s", thinkingBody)
	}
}

// TestATurnWithoutTokenCountsReportsNoUsage covers a replay: a turn that
// recorded no token counts must not write a zero-token usage record.
func TestATurnWithoutTokenCountsReportsNoUsage(t *testing.T) {
	counted := func(turn pootest.Turn) bool {
		tr := pootest.NewTransport(turn)
		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m"})
		if err != nil {
			t.Fatalf("streaming: %v", err)
		}
		for {
			ev, err := reader.Next()
			if err == io.EOF {
				return false
			}
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if ev.Kind == "usage" {
				return true
			}
		}
	}
	if counted(pootest.Turn{Text: "hi"}) {
		t.Error("a turn with no token counts reported usage")
	}
	if !counted(pootest.Turn{Text: "hi", OutputTokens: 3}) {
		t.Error("a turn with token counts reported no usage")
	}
}
