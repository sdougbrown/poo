package openai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/openai"
)

// The quirk profiles.
//
// The plan: the descriptor names the profiles a model needs, and the
// transport applies them in that order. A profile shapes the request, or
// it repairs the response, or both.

// TestTheThinkTagsAreStrippedIntoThinking covers the response repair: the
// thinking a model writes in its own tags becomes a thinking block, and
// the answer is what is left.
func TestTheThinkTagsAreStrippedIntoThinking(t *testing.T) {
	srv := newSSEServer(t, fixture(t, "vllm-think-tags.sse"))
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{Name: "gemma4", Quirks: []string{openai.QuirkThinkTags}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if got := textOf(events); got != "The answer is 42." {
		t.Errorf("the text is %q, want the answer alone", got)
	}
	var thinking strings.Builder
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta != nil && ev.Delta.Kind == "thinking" {
			thinking.WriteString(ev.Delta.Text)
		}
	}
	if thinking.String() != "Let me think about it." {
		t.Errorf("the thinking is %q, want it in a thinking block", thinking.String())
	}
}

// TestTheChatTemplateThinkingIsAskedFor covers the request shaping: the
// model is asked to think through its chat template.
func TestTheChatTemplateThinkingIsAskedFor(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		ModelInfo: poo.ModelInfo{Name: "gemma4", Quirks: []string{openai.QuirkChatTemplateThinking}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	kwargs, ok := srv.requests[0]["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("the request has no chat_template_kwargs: %v", srv.requests[0])
	}
	if kwargs["enable_thinking"] != true {
		t.Errorf("enable_thinking is %v, want true", kwargs["enable_thinking"])
	}
}

// TestTheToolSchemasAreSimplified covers the request shaping: a weak
// schema parser reads the simplified schema.
func TestTheToolSchemasAreSimplified(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		ToolsMode: "native",
		ModelInfo: poo.ModelInfo{Name: "weak", Quirks: []string{openai.QuirkSimplifyToolSchemas}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Tools: []poo.ToolSpec{{
			Name:        "read",
			Description: "read a file",
			Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"the path"}},` +
				`"required":["path"],"additionalProperties":false,"$schema":"https://json-schema.org/draft/2020-12/schema"}`),
		}},
	})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	tools, _ := srv.requests[0]["tools"].([]any)
	first, _ := tools[0].(map[string]any)
	fn, _ := first["function"].(map[string]any)
	params, _ := fn["parameters"].(map[string]any)
	if _, present := params["additionalProperties"]; present {
		t.Errorf("the schema kept the keywords a weak parser does not know: %v", params)
	}
	if _, present := params["$schema"]; present {
		t.Errorf("the schema kept the $schema keyword: %v", params)
	}
	if params["type"] != "object" {
		t.Errorf("the schema's type is %v, want object", params["type"])
	}
	if _, present := params["properties"]; !present {
		t.Errorf("the schema lost its properties: %v", params)
	}
}

// TestTheDegenerateStreamIsOnlyLogged covers the plan's rule: a stream
// that repeats itself is noted, and the stream runs on.
func TestTheDegenerateStreamIsOnlyLogged(t *testing.T) {
	body := chunk(`{"choices":[{"index":0,"delta":{"content":"again"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"again"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"again"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{"content":"again"}}]}`) +
		chunk(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + done

	var log bytes.Buffer
	srv := newSSEServer(t, body)
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		Logger:    slog.New(slog.NewTextHandler(&log, nil)),
		ModelInfo: poo.ModelInfo{Name: "gemma4", Quirks: []string{openai.QuirkDegenerateStream}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()

	events := read(t, reader)
	if got := textOf(events); got != "againagainagainagain" {
		t.Errorf("the text is %q, want the stream to run on", got)
	}
	if !strings.Contains(log.String(), "repeated") {
		t.Errorf("the log holds %q, want a note about the repetition", log.String())
	}
}

// TestTheTextToolCallsAreReadInNativeMode covers the quirk: a model
// whose mode says native but which writes its calls as text has its
// calls read.
func TestTheTextToolCallsAreReadInNativeMode(t *testing.T) {
	srv := newSSEServer(t, fixture(t, "vllm-text-tool-call.sse"))
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		ToolsMode: "native",
		ModelInfo: poo.ModelInfo{Name: "gemma4", Quirks: []string{openai.QuirkTextToolCalls}},
	})

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
}

// TestTheQuirksAreAppliedInTheDescriptorsOrder covers the rule: the
// profiles are applied in the order the descriptor names them.
func TestTheQuirksAreAppliedInTheDescriptorsOrder(t *testing.T) {
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		BaseURL: srv.URL,
		Model:   "m",
		ModelInfo: poo.ModelInfo{
			Name:   "gemma4",
			Quirks: []string{openai.QuirkChatTemplateThinking, openai.QuirkSimplifyToolSchemas},
		},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if _, ok := srv.requests[0]["chat_template_kwargs"]; !ok {
		t.Errorf("the request has no chat_template_kwargs: %v", srv.requests[0])
	}
}

// TestAnUnknownQuirkIsNoted covers the rule: a descriptor may name a
// quirk the transport does not know, and the transport says so and goes
// on.
func TestAnUnknownQuirkIsNoted(t *testing.T) {
	var log bytes.Buffer
	srv := newSSEServer(t, chunk(`{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)+done)
	tr := openai.New(openai.Options{
		BaseURL:   srv.URL,
		Model:     "m",
		Logger:    slog.New(slog.NewTextHandler(&log, nil)),
		ModelInfo: poo.ModelInfo{Name: "gemma4", Quirks: []string{"not_a_quirk"}},
	})

	reader, _, err := tr.Stream(context.Background(), poo.ChatRequest{Model: "m", MaxTokens: 16})
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	defer reader.Close()
	read(t, reader)

	if !strings.Contains(log.String(), "not_a_quirk") {
		t.Errorf("the log holds %q, want a note about the unknown quirk", log.String())
	}
}
