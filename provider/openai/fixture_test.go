package openai_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The recorded streams.
//
// The plan: the transport reads the shapes the endpoints send, in the
// llama.cpp shape and in the vLLM shape, with and without a usage
// chunk.

// fixture reads a recorded stream.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return string(raw)
}

// TestTheRecordedStreamsAreRead covers the shapes: a llama.cpp stream
// and a vLLM stream, with and without a usage chunk.
func TestTheRecordedStreamsAreRead(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		wantText   string
		wantInput  int
		wantOutput int
		wantCached bool
	}{
		{
			name:       "a llama.cpp stream with a usage chunk",
			fixture:    "llamacpp-answer.sse",
			wantText:   "Hello there",
			wantInput:  12,
			wantOutput: 3,
		},
		{
			name:     "a llama.cpp stream without a usage chunk",
			fixture:  "llamacpp-answer-no-usage.sse",
			wantText: "Hello",
		},
		{
			name:       "a vLLM stream with a usage chunk",
			fixture:    "vllm-answer.sse",
			wantText:   "The answer is 42.",
			wantInput:  120,
			wantOutput: 8,
			wantCached: true,
		},
		{
			name:     "a vLLM stream without a usage chunk",
			fixture:  "vllm-answer-no-usage.sse",
			wantText: "The answer is 42.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newSSEServer(t, fixture(t, c.fixture))
			tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m"})

			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()

			events := read(t, reader)
			if got := textOf(events); got != c.wantText {
				t.Errorf("the text is %q, want %q", got, c.wantText)
			}
			var usage *poo.UsageReport
			for _, ev := range events {
				if ev.Kind == "usage" {
					usage = ev.Usage
				}
			}
			if usage == nil {
				t.Fatal("the stream held no usage report")
			}
			if usage.InputTokens != c.wantInput || usage.OutputTokens != c.wantOutput {
				t.Errorf("the usage is %d/%d, want %d/%d",
					usage.InputTokens, usage.OutputTokens, c.wantInput, c.wantOutput)
			}
			if c.wantCached {
				if usage.CacheReadTokens == nil {
					t.Error("the usage report has no cache-read count")
				} else if *usage.CacheReadTokens != 96 {
					t.Errorf("the cache-read count is %d, want 96", *usage.CacheReadTokens)
				}
			}
		})
	}
}

// TestTheTextToolCallFormatsAreParsed covers the plan's rule: every
// format a model writes its calls in parses into the same call, and the
// call is flagged as one the model wrote as text.
//
// Every format has a fixture: the stream an endpoint sent, in the shape
// the model writes its calls in.
func TestTheTextToolCallFormatsAreParsed(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		wantArgs string
	}{
		{name: "hermes", fixture: "vllm-text-tool-call.sse", wantArgs: `{"command": "ls -la"}`},
		{name: "qwen xml", fixture: "vllm-qwen-xml-tool-call.sse", wantArgs: `{"command":"ls -la"}`},
		{name: "mistral", fixture: "vllm-mistral-tool-calls.sse", wantArgs: `{"command": "ls -la"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newSSEServer(t, fixture(t, c.fixture))
			tr := openai.New(openai.Options{BaseURL: srv.URL, Model: "m", ToolsMode: "text"})

			reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			defer reader.Close()

			calls := toolCallsOf(read(t, reader))
			if len(calls) != 1 {
				t.Fatalf("the stream held %d calls, want 1", len(calls))
			}
			if calls[0].Name != "bash" {
				t.Errorf("the call is %q, want bash", calls[0].Name)
			}
			if string(calls[0].Arguments) != c.wantArgs {
				t.Errorf("the call's arguments are %s, want %s", calls[0].Arguments, c.wantArgs)
			}
			if calls[0].Origin != poo.CallOriginParsedText {
				t.Errorf("the call's origin is %q, want parsed_text", calls[0].Origin)
			}
		})
	}
}

// TestTheEmptyToolCallsAreAFailure covers the plan's rule: a response
// whose finish reason is tool_calls but which carries no tool call and
// no text is the server's own failure, and it is retried like one.
//
// The fixture is the recorded one: the homelab vLLM's gemma4 parser
// answers a tool call with no tool call.
func TestTheEmptyToolCallsAreAFailure(t *testing.T) {
	srv := newSSEServer(t, fixture(t, "vllm-empty-tool-calls.sse"))
	tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))

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
	if last == nil {
		t.Fatal("the stream did not fail")
	}
	pe, ok := last.(*poo.ProviderError)
	if !ok {
		t.Fatalf("the error is %T, want a provider error", last)
	}
	if pe.Kind != poo.ProviderKindServer {
		t.Errorf("the failure's kind is %q, want server", pe.Kind)
	}
	if len(srv.requests) != 3 {
		t.Errorf("the endpoint got %d requests, want 3: the failure is retried", len(srv.requests))
	}
	// Every attempt cost tokens, and the report says so.
	reporter, ok := reader.(poo.UsageReporter)
	if !ok {
		t.Fatal("the reader reports no usage")
	}
	usage := reporter.ReportUsage()
	if usage == nil {
		t.Fatal("the failed request reported no usage")
	}
	if usage.OutputTokens != 54 {
		t.Errorf("the request's output tokens are %d, want the three attempts' 54", usage.OutputTokens)
	}
}

// TestTheEmptyToolCallsSettleTheRunAsAProviderError covers the same
// failure from the run's side: the run settles as a provider error
// when the budget runs out.
func TestTheEmptyToolCallsSettleTheRunAsAProviderError(t *testing.T) {
	srv := newSSEServer(t, fixture(t, "vllm-empty-tool-calls.sse"))
	tr := openai.New(fastRetry(openai.Options{BaseURL: srv.URL, Model: "m"}))

	res := settleOf(t, tr)
	if res.Reason != poo.ReasonProviderError {
		t.Errorf("the run settled %q, want provider_error", res.Reason)
	}
	if res.Detail.Provider == nil {
		t.Fatalf("the settle record has no provider detail: %+v", res.Detail)
	}
	if res.Detail.Provider.Kind != "server" {
		t.Errorf("the failure's kind is %q, want server", res.Detail.Provider.Kind)
	}
}
