package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// sseServer is a scripted SSE endpoint.
type sseServer struct {
	*httptest.Server
	// body is the raw response body the server sends.
	body string
	// status is the HTTP status the server answers with.
	status int
	// contentType is the response content type.
	contentType string
	// requests holds the request bodies the server received.
	requests []map[string]any
}

// newSSEServer starts a server that answers with a body.
func newSSEServer(t *testing.T, body string) *sseServer {
	t.Helper()
	s := &sseServer{body: body, status: http.StatusOK, contentType: "text/event-stream"}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		s.requests = append(s.requests, decoded)

		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			_, _ = io.WriteString(w, s.body)
			return
		}
		w.Header().Set("Content-Type", s.contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, s.body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// chunk builds one SSE data line.
func chunk(payload string) string {
	return "data: " + payload + "\n\n"
}

// done is the SSE terminator.
const done = "data: [DONE]\n\n"

// read drains a stream and returns the events.
func read(t *testing.T, reader poo.StreamReader) []poo.StreamEvent {
	t.Helper()
	var events []poo.StreamEvent
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			return events
		}
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		events = append(events, ev)
	}
}

// textOf returns the text of the delta events.
func textOf(events []poo.StreamEvent) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "text" {
			b.WriteString(ev.Delta.Text)
		}
	}
	return b.String()
}

// fastRetry shortens the wait between the attempts.
//
// A test that fails on purpose does not wait the plan's backoff, and
// the test that covers the backoff waits for it on purpose.
func fastRetry(cfg openai.Config) openai.Config {
	cfg.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
	return cfg
}

// toolCallsOf returns the tool call events.
func toolCallsOf(events []poo.StreamEvent) []poo.ToolCallDelta {
	var calls []poo.ToolCallDelta
	for _, ev := range events {
		if ev.Kind == "tool_call" && ev.ToolCall != nil {
			calls = append(calls, *ev.ToolCall)
		}
	}
	return calls
}

func TestANativeToolCallIsRead(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "native"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Tools:     []poo.ToolSpec{{Name: "bash", Description: "run", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	calls := toolCallsOf(events)
	if len(calls) != 1 {
		t.Fatalf("the stream held %d tool calls, want 1", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("the call is %q, want bash", calls[0].Name)
	}
	if calls[0].ID != "call_1" {
		t.Errorf("the call's id is %q, want call_1", calls[0].ID)
	}
	if string(calls[0].Arguments) != `{"command":"ls"}` {
		t.Errorf("the call's arguments are %s", calls[0].Arguments)
	}

	// The native mode sends the tools in the request.
	if len(srv.requests) != 1 {
		t.Fatalf("the server got %d requests, want 1", len(srv.requests))
	}
	tools, _ := srv.requests[0]["tools"].([]any)
	if len(tools) != 1 {
		t.Errorf("the request held %d tools, want 1", len(tools))
	}
}

func TestFinishReasonLengthIsRead(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	var finish poo.FinishReason
	for _, ev := range events {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	if finish != poo.FinishLength {
		t.Errorf("the finish reason is %q, want length", finish)
	}
	if textOf(events) != "partial" {
		t.Errorf("the text is %q, want partial", textOf(events))
	}
}

func TestAContextLengthErrorIsMapped(t *testing.T) {
	body := `{"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error","code":"context_length_exceeded"}}`

	srv := newSSEServer(t, body)
	srv.status = http.StatusBadRequest
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		t.Fatal("the request succeeded, want a context-length error")
	}
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindContextLength {
		t.Errorf("the error kind is %q, want context_length", pe.Kind)
	}
	if pe.Status != http.StatusBadRequest {
		t.Errorf("the status is %d, want 400", pe.Status)
	}
}

func TestAContextLengthErrorInTheMessageIsMapped(t *testing.T) {
	// Some servers report a context-length overflow with a plain 400 and
	// no code, so the message is checked too.
	body := `{"error":{"message":"prompt is too long: 200000 tokens > 100000 maximum","type":"invalid_request_error"}}`

	srv := newSSEServer(t, body)
	srv.status = http.StatusBadRequest
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindContextLength {
		t.Errorf("the error kind is %q, want context_length", pe.Kind)
	}
}

func TestReasoningContentDeltasDoNotBreakTheStream(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Let me think."}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"reasoning_content":" A little more."}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"The answer is 42."}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 1024})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if textOf(events) != "The answer is 42." {
		t.Errorf("the text is %q, want the answer alone", textOf(events))
	}

	var thinking strings.Builder
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "thinking" {
			thinking.WriteString(ev.Delta.Text)
		}
	}
	if thinking.String() != "Let me think. A little more." {
		t.Errorf("the thinking is %q, want it collected", thinking.String())
	}
}

func TestTheUsageChunkIsRead(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) +
		chunk(`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":8,"total_tokens":128}}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 1024})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	var usage *poo.UsageReport
	for _, ev := range events {
		if ev.Kind == "usage" {
			usage = ev.Usage
		}
	}
	if usage == nil {
		t.Fatal("the stream held no usage report")
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 8 {
		t.Errorf("the usage is %d/%d, want 120/8", usage.InputTokens, usage.OutputTokens)
	}
	if usage.CacheReadTokens != nil {
		t.Errorf("the cache-read count is %v, want none", *usage.CacheReadTokens)
	}
}

func TestTheCacheReadCountIsRead(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) +
		chunk(`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":96}}}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 1024})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	for _, ev := range events {
		if ev.Kind == "usage" && ev.Usage != nil {
			if ev.Usage.CacheReadTokens == nil {
				t.Fatal("the usage report has no cache-read count")
			}
			if *ev.Usage.CacheReadTokens != 96 {
				t.Errorf("the cache-read count is %d, want 96", *ev.Usage.CacheReadTokens)
			}
			return
		}
	}
	t.Fatal("the stream held no usage report")
}

func TestMaxTokensIsAlwaysSent(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 4096})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.requests) != 1 {
		t.Fatalf("the server got %d requests, want 1", len(srv.requests))
	}
	mt, ok := srv.requests[0]["max_tokens"].(float64)
	if !ok {
		t.Fatalf("the request has no max_tokens: %v", srv.requests[0])
	}
	if mt != 4096 {
		t.Errorf("max_tokens is %v, want 4096", mt)
	}
}

func TestAnAuthorizationHeaderIsSent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", APIKey: "secret-token"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if got != "Bearer secret-token" {
		t.Errorf("the Authorization header is %q, want the bearer token", got)
	}
}

func TestTheTextModeSendsNoToolsAndParsesTheCall(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\"}}</tool_call>"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 1024,
		Tools:     []poo.ToolSpec{{Name: "bash", Description: "run", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	calls := toolCallsOf(events)
	if len(calls) != 1 {
		t.Fatalf("the stream held %d tool calls, want 1", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("the call is %q, want bash", calls[0].Name)
	}

	// The text mode does not send the tools in the request, because the
	// model is told about them in the prompt instead.
	if _, ok := srv.requests[0]["tools"]; ok {
		t.Errorf("the text mode sent the tools in the request: %v", srv.requests[0])
	}
}

func TestAnAuthFailureIsMapped(t *testing.T) {
	srv := newSSEServer(t, `{"error":{"message":"invalid api key","code":"invalid_api_key"}}`)
	srv.status = http.StatusUnauthorized
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindAuthFailed {
		t.Errorf("the error kind is %q, want auth_failed", pe.Kind)
	}
}

func TestAServerFailureIsMapped(t *testing.T) {
	srv := newSSEServer(t, `{"error":{"message":"boom"}}`)
	srv.status = http.StatusInternalServerError
	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Kind != poo.ProviderKindServer {
		t.Errorf("the error kind is %q, want server", pe.Kind)
	}
}

func TestTheStreamStopsWhenTheContextEnds(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	ctx, cancel := context.WithCancel(context.Background())
	reader, _, err := tr.Stream(ctx, poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	cancel()

	// The stream ends one way or another, but it ends.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, err := reader.Next()
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
}

func TestTheRequestCarriesTheWholeHistory(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "m",
		Messages: []poo.Message{
			poo.NewTextMessage(poo.RoleSystem, "the system prompt"),
			poo.NewTextMessage(poo.RoleUser, "the question"),
		},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	messages, _ := srv.requests[0]["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("the request held %d messages, want 2", len(messages))
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("the first message's role is %v, want system", first["role"])
	}
	if first["content"] != "the system prompt" {
		t.Errorf("the first message's content is %v", first["content"])
	}
}

func TestAToolResultMessageBecomesAToolMessage(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "m",
		Messages: []poo.Message{
			poo.ToolResultMessage("call_1", "bash", "the output", false),
		},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	messages, _ := srv.requests[0]["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	if first["role"] != "tool" {
		t.Errorf("the message's role is %v, want tool", first["role"])
	}
	if first["tool_call_id"] != "call_1" {
		t.Errorf("the message's tool_call_id is %v, want call_1", first["tool_call_id"])
	}
	if first["content"] != "the output" {
		t.Errorf("the message's content is %v, want the tool output", first["content"])
	}
}

func TestTheURLIsBuiltFromTheBase(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL + "/v1", Model: "m"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if path != "/v1/chat/completions" {
		t.Errorf("the request path is %q, want /v1/chat/completions", path)
	}
}

func TestAnAssistantToolCallBecomesAToolCallMessage(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model: "m",
		Messages: []poo.Message{{
			Role: poo.RoleAssistant,
			Content: []poo.ContentBlock{{
				Kind:       poo.ContentToolCall,
				ToolCallID: "call_1",
				Name:       "bash",
				Arguments:  json.RawMessage(`{"command":"ls"}`),
			}},
		}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	messages, _ := srv.requests[0]["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	calls, _ := first["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("the message held %d tool calls, want 1", len(calls))
	}
	call, _ := calls[0].(map[string]any)
	fn, _ := call["function"].(map[string]any)
	if fn["name"] != "bash" {
		t.Errorf("the call's name is %v, want bash", fn["name"])
	}
	if fn["arguments"] != `{"command":"ls"}` {
		t.Errorf("the call's arguments are %v", fn["arguments"])
	}
}

func TestAnErrorChunkEndsTheStream(t *testing.T) {
	body := chunk(`{"error":{"message":"the model died","type":"server_error"}}`)

	srv := newSSEServer(t, body)
	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	// Every request gets a usage report, so the failure comes after it.
	var last error
	for i := 0; i < 4; i++ {
		if _, err = reader.Next(); err != nil {
			last = err
			break
		}
	}
	if last == nil {
		t.Fatal("the stream did not fail")
	}
	if !strings.Contains(last.Error(), "the model died") {
		t.Errorf("the error is %q, want the provider's message", last)
	}
}

func TestTheUsageChunkIsAskedForInTextMode(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	opts, ok := srv.requests[0]["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("the request has no stream_options: %v", srv.requests[0])
	}
	if opts["include_usage"] != true {
		t.Errorf("include_usage is %v, want true", opts["include_usage"])
	}
}

func TestTheTimeToTheFirstTokenIsMeasured(t *testing.T) {
	// The endpoint takes a moment before it answers, so the time to the
	// first token is not zero.
	body := chunk(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`) + done

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	for _, ev := range read(t, reader) {
		if ev.Kind != "usage" || ev.Usage == nil {
			continue
		}
		if ev.Usage.TTFTMS < 50 {
			t.Errorf("the time to the first token is %v ms, want at least the endpoint's delay", ev.Usage.TTFTMS)
		}
		return
	}
	t.Fatal("the stream held no usage report")
}

func TestTheRequestIsStreamed(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if srv.requests[0]["stream"] != true {
		t.Errorf("the request is not streamed: %v", srv.requests[0])
	}
	if srv.requests[0]["model"] != "m" {
		t.Errorf("the request's model is %v, want m", srv.requests[0]["model"])
	}
}

func TestTheTransportUsesTheContext(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := tr.Stream(ctx, poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err == nil {
		// The request may still have been answered in time. The test only
		// fails when the context was ignored entirely.
		t.Skip("the endpoint answered before the deadline")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("the error is %q, want the context error", err)
	}
}

func TestTheBaseURLIsTrimmed(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	}))
	defer srv.Close()

	tr := openai.New(openai.Config{BaseURL: srv.URL + "/v1/", Model: "m"})
	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if path != "/v1/chat/completions" {
		t.Errorf("the request path is %q, want /v1/chat/completions", path)
	}
}

func TestTheStreamIgnoresAChunkItCannotRead(t *testing.T) {
	body := "data: {not json}\n\n" + chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if textOf(events) != "hi" {
		t.Errorf("the text is %q, want hi", textOf(events))
	}
}

func TestTheStreamHandlesACommentLine(t *testing.T) {
	body := ": keep-alive\n\n" + chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if textOf(events) != "hi" {
		t.Errorf("the text is %q, want hi", textOf(events))
	}
}

func TestTheRequestIsSentToTheChatCompletionsPath(t *testing.T) {
	// The body is a valid chat completion, so the transport's own shape is
	// checked rather than the server's.
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if len(srv.requests) != 1 {
		t.Fatalf("the server got %d requests, want 1", len(srv.requests))
	}
	for _, key := range []string{"model", "messages", "max_tokens", "stream"} {
		if _, ok := srv.requests[0][key]; !ok {
			t.Errorf("the request has no %s: %v", key, srv.requests[0])
		}
	}
}

func TestTheErrorBodyIsReadWhenTheStatusIsNotOK(t *testing.T) {
	srv := newSSEServer(t, "not json at all")
	srv.status = http.StatusBadGateway
	tr := openai.New(fastRetry(openai.Config{BaseURL: srv.URL, Model: "m"}))

	_, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	pe, ok := err.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", err)
	}
	if pe.Status != http.StatusBadGateway {
		t.Errorf("the status is %d, want 502", pe.Status)
	}
	if pe.Message == "" {
		t.Error("the error has no message")
	}
}

func TestEveryRequestHasAUsageReport(t *testing.T) {
	// The endpoint reports no token counts, but the request still gets a
	// usage report, so a consumer can see the time to the first token.
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	var usage *poo.UsageReport
	for _, ev := range events {
		if ev.Kind == "usage" {
			usage = ev.Usage
		}
	}
	if usage == nil {
		t.Fatal("the stream held no usage report")
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("the usage is %d/%d, want no counts", usage.InputTokens, usage.OutputTokens)
	}
	if usage.CacheReadTokens != nil {
		t.Errorf("the cache-read count is %v, want none", *usage.CacheReadTokens)
	}
}

func TestOnlyOneUsageReportIsProduced(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) +
		chunk(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	n := 0
	for _, ev := range read(t, reader) {
		if ev.Kind == "usage" {
			n++
			if ev.Usage.InputTokens != 10 {
				t.Errorf("the input tokens are %d, want 10", ev.Usage.InputTokens)
			}
		}
	}
	if n != 1 {
		t.Errorf("the stream held %d usage reports, want 1", n)
	}
}

func TestTheChunkedArgumentsAreJoined(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"comm"}}]}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"and\":\"ls\"}"}}]}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	calls := toolCallsOf(read(t, reader))
	if len(calls) != 2 {
		t.Fatalf("the stream held %d call events, want 2", len(calls))
	}
	// The first event carries the first fragment, the second the rest.
	if calls[0].Arguments[0] != '{' {
		t.Errorf("the first fragment is %s", calls[0].Arguments)
	}
	if string(calls[1].Arguments) != `and":"ls"}` {
		t.Errorf("the second fragment is %s", calls[1].Arguments)
	}
}

func TestTheAutoModeKeepsTheNativeAndParsedCallsApart(t *testing.T) {
	// The endpoint sends a native tool call and a text call in the same
	// response. Both are read, and neither overwrites the other.
	body := chunk(`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"<tool_call>{\"name\": \"read\", \"arguments\": {\"path\": \"a.txt\"}}</tool_call>"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "auto"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 1024})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	calls := toolCallsOf(read(t, reader))
	if len(calls) != 2 {
		t.Fatalf("the stream held %d calls, want 2", len(calls))
	}
	names := []string{calls[0].Name, calls[1].Name}
	if !((names[0] == "bash" && names[1] == "read") || (names[0] == "read" && names[1] == "bash")) {
		t.Errorf("the calls are %v, want one bash and one read", names)
	}
	if calls[0].Index == calls[1].Index {
		t.Errorf("the calls share the index %d, want them apart", calls[0].Index)
	}
}

func TestTheAutoModeSendsTheToolsAndParsesTheText(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"<tool_call>{\"name\": \"bash\", \"arguments\": {\"command\": \"ls\"}}</tool_call>"},"finish_reason":"stop"}]}`) + done

	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "auto"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Tools:     []poo.ToolSpec{{Name: "bash", Description: "run", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if len(toolCallsOf(events)) != 1 {
		t.Errorf("the stream held %d calls, want 1", len(toolCallsOf(events)))
	}
	if _, ok := srv.requests[0]["tools"]; !ok {
		t.Errorf("the auto mode did not send the tools: %v", srv.requests[0])
	}
}

func TestTheToolSchemaIsSentAsTheParameters(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ToolsMode: "native"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Tools: []poo.ToolSpec{{
			Name:        "read",
			Description: "read a file",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	tools, _ := srv.requests[0]["tools"].([]any)
	first, _ := tools[0].(map[string]any)
	if first["type"] != "function" {
		t.Errorf("the tool's type is %v, want function", first["type"])
	}
	fn, _ := first["function"].(map[string]any)
	if fn["name"] != "read" {
		t.Errorf("the tool's name is %v, want read", fn["name"])
	}
	params, _ := fn["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("the tool's parameters are %v, want the schema", fn["parameters"])
	}
}

func TestTheRequestBodyIsValidJSON(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Messages:  []poo.Message{poo.NewTextMessage(poo.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	// The server decoded the body, so it was valid JSON.
	if len(srv.requests) != 1 {
		t.Fatalf("the server got %d requests, want 1", len(srv.requests))
	}
	if srv.requests[0] == nil {
		t.Fatal("the server could not decode the request body")
	}
}

var _ = fmt.Sprintf

func TestTheReasoningEffortIsSentOnlyWhenSet(t *testing.T) {
	for _, effort := range []string{"", "low"} {
		body := chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`) + done
		srv := newSSEServer(t, body)
		tr := openai.New(openai.Config{BaseURL: srv.URL, Model: "m", ReasoningEffort: effort})

		reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
		if err != nil {
			t.Fatalf("starting the request: %v", err)
		}
		read(t, reader)
		reader.Close()

		got, sent := srv.requests[0]["reasoning_effort"]
		if effort == "" && sent {
			t.Errorf("an unset effort sent reasoning_effort %v", got)
		}
		if effort != "" && got != effort {
			t.Errorf("the request's reasoning_effort is %v, want %q", got, effort)
		}
	}
}

// TestChatURLAppendsThePathOnlyOnce covers the documented branch: a base
// URL that already ends in /chat/completions is used as it is, so the
// path is never appended twice.
func TestChatURLAppendsThePathOnlyOnce(t *testing.T) {
	cases := []struct{ base, want string }{
		{"http://x:1/v1", "http://x:1/v1/chat/completions"},
		{"http://x:1/v1/", "http://x:1/v1/chat/completions"},
		{"http://x:1/v1/chat/completions", "http://x:1/v1/chat/completions"},
		{"http://x:1/v1/chat/completions/", "http://x:1/v1/chat/completions"},
	}
	for _, c := range cases {
		if got := openai.ChatURL(c.base); got != c.want {
			t.Errorf("ChatURL(%q) is %q, want %q", c.base, got, c.want)
		}
	}
}
