package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/anthropic"
)

// frame builds one SSE data line.
func frame(payload string) string {
	return "data: " + payload + "\n\n"
}

// sseServer is a scripted Messages endpoint.
type sseServer struct {
	*httptest.Server
	// body is the raw response body the server sends.
	body string
	// requests holds the request bodies the server received.
	requests []map[string]any
	// raw holds the request bodies as the bytes they arrived as.
	raw [][]byte
	// headers holds the headers the server received.
	headers []http.Header
}

// newSSEServer starts a server that answers with a body.
func newSSEServer(t *testing.T, body string) *sseServer {
	t.Helper()
	s := &sseServer{body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(raw, &decoded)
		s.requests = append(s.requests, decoded)
		s.raw = append(s.raw, raw)
		s.headers = append(s.headers, r.Header.Clone())

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, s.body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// read drains a stream and returns the events.
func read(t *testing.T, reader poo.StreamReader) []poo.StreamItem {
	t.Helper()
	var events []poo.StreamItem
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			return events
		}
		if err != nil {
			events = append(events, poo.StreamItem{})
			return events
		}
		events = append(events, ev)
	}
}

// thinkingModel is the model info a Messages model carries: an effort level
// rides as the output config's effort, and a temperature is never sent.
func thinkingModel() poo.ModelInfo {
	return poo.ModelInfo{
		ModelID: "claude-haiku-5-5",
		Tools:   poo.ToolCalling{Mode: poo.ToolsNative},
		Reasoning: poo.ReasoningControl{
			Mode:  poo.ReasoningEffort,
			Param: "output_config.effort",
		},
		ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"},
		Temperature:     poo.TempFixed,
	}
}

func request(session string, msgs ...poo.Message) poo.ChatRequest {
	if msgs == nil {
		msgs = []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")}
	}
	return poo.ChatRequest{Model: "claude-haiku-5-5", Messages: msgs, MaxTokens: 32, SessionID: session}
}

func TestTheMessagesRequestCarriesItsOwnShape(t *testing.T) {
	body := frame(`{"type":"message_start","message":{"usage":{"input_tokens":23,"cache_read_input_tokens":4}}}`) +
		frame(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`) +
		frame(`{"type":"content_block_stop","index":0}`) +
		frame(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`) +
		frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)

	msgs := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "be brief"),
		poo.NewTextMessage(poo.RoleUser, "hi"),
	}
	tr := anthropic.New(anthropic.Options{
		Endpoint:      srv.URL,
		Model:         "claude-haiku-5-5",
		ModelInfo:     thinkingModel(),
		APIKey:        "oc_sk_test",
		SessionHeader: "x-opencode-session",
	})
	reader, _, err := tr.Stream(context.Background(), request("ses_one", msgs...))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events := read(t, reader)
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	// The system prompt travels in its own field, and the output limit is
	// always sent.
	sent := srv.requests[0]
	if sent["system"] != "be brief" {
		t.Errorf("the system prompt is %v, want the system message's text", sent["system"])
	}
	if sent["max_tokens"] != float64(32) {
		t.Errorf("the output limit is %v, want the request's", sent["max_tokens"])
	}
	if sent["stream"] != true {
		t.Errorf("the request does not stream: %v", sent)
	}
	headers := srv.headers[0]
	if got := headers.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("the anthropic-version header is %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer oc_sk_test" {
		t.Errorf("the Authorization header is %q, want the bearer token", got)
	}
	if got := headers.Get("x-opencode-session"); got != "ses_one" {
		t.Errorf("the session header is %q, want the session's id", got)
	}

	var text string
	var finish poo.FinishReason
	for _, ev := range events {
		switch ev.Kind {
		case "delta":
			if ev.Delta.Kind == "text" {
				text += ev.Delta.Text
			}
		case "finish":
			finish = ev.FinishReason
		case "usage":
			// The input the API reports leaves the cached tokens out,
			// and the run charges the whole prompt.
			if ev.Usage.InputTokens != 27 || ev.Usage.OutputTokens != 9 {
				t.Errorf("the usage is %+v, want the whole prompt and the answer's tokens", ev.Usage)
			}
			if ev.Usage.CacheReadTokens == nil || *ev.Usage.CacheReadTokens != 4 {
				t.Errorf("the cache read is %v, want the reported count", ev.Usage.CacheReadTokens)
			}
		}
	}
	if text != "hi" {
		t.Errorf("the text is %q, want the streamed answer", text)
	}
	if finish != poo.FinishStop {
		t.Errorf("the finish reason is %q, want stop", finish)
	}
}

func TestAnEffortLevelRidesTheOutputConfig(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	req := request("")
	req.Reasoning = "high"
	reader, _, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	// The effort level rides as the output config's effort, and adaptive
	// thinking is what turns it on.
	sent := srv.requests[0]
	config, _ := sent["output_config"].(map[string]any)
	if config["effort"] != "high" {
		t.Errorf("the output config is %v, want effort high", sent["output_config"])
	}
	thinking, _ := sent["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" {
		t.Errorf("the thinking field is %v, want adaptive", sent["thinking"])
	}
}

func TestAToolUseStreamAccumulatesItsArguments(t *testing.T) {
	body := frame(`{"type":"message_start","message":{"usage":{"input_tokens":15}}}`) +
		frame(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\""}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":": \"Paris\"}"}}`) +
		frame(`{"type":"content_block_stop","index":0}`) +
		frame(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`) +
		frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)

	tools := []poo.ToolSpec{{
		Name:        "get_weather",
		Description: "Get the weather in a city",
		Schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}}
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	req := request("")
	req.Tools = tools
	reader, _, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events := read(t, reader)
	_ = reader.Close()

	// The request carries the tool in the Messages API's own shape.
	sent := srv.requests[0]
	toolsSent, _ := sent["tools"].([]any)
	if len(toolsSent) != 1 {
		t.Fatalf("the request carries %d tools, want one", len(toolsSent))
	}
	tool, _ := toolsSent[0].(map[string]any)
	if tool["name"] != "get_weather" {
		t.Errorf("the tool is %v", tool)
	}
	if _, ok := tool["input_schema"]; !ok {
		t.Errorf("the tool carries no input schema: %v", tool)
	}

	// The reader emits the fragments it is handed; the core assembles a
	// call by appending them. Each event names the block it belongs to,
	// so two open blocks keep their own arguments.
	var id, name string
	var arguments []byte
	var complete bool
	var finish poo.FinishReason
	for _, ev := range events {
		switch ev.Kind {
		case "tool_call":
			if ev.ToolCall.ID != "" && ev.ToolCall.Arguments == nil {
				id, name = ev.ToolCall.ID, ev.ToolCall.Name
			}
			arguments = append(arguments, ev.ToolCall.Arguments...)
			complete = ev.ToolCall.Complete
		case "finish":
			finish = ev.FinishReason
		}
	}
	if id != "toolu_1" || name != "get_weather" {
		t.Errorf("the call is %q/%q", id, name)
	}
	if string(arguments) != `{"city": "Paris"}` {
		t.Errorf("the assembled arguments are %q", arguments)
	}
	if !complete {
		t.Error("the call is not complete, want the block's stop to finish it")
	}
	if finish != poo.FinishToolCalls {
		t.Errorf("the finish reason is %q, want tool calls", finish)
	}
}

func TestAThinkingStreamReportsItsSignature(t *testing.T) {
	body := frame(`{"type":"message_start","message":{"usage":{"input_tokens":15}}}`) +
		frame(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me consider"}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_abc"}}`) +
		frame(`{"type":"content_block_stop","index":0}`) +
		frame(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`) +
		frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events := read(t, reader)
	_ = reader.Close()

	var thinking string
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta.Kind == "thinking" {
			thinking += ev.Delta.Text
		}
	}
	if thinking != "Let me consider" {
		t.Errorf("the thinking is %q, want the streamed thinking", thinking)
	}
	reporter, ok := reader.(poo.ProviderBlockReporter)
	if !ok {
		t.Fatal("the reader reports no provider blocks")
	}
	blocks := reporter.ReportProviderBlocks()
	if len(blocks) != 1 {
		t.Fatalf("the reader reported %d blocks, want the signature", len(blocks))
	}
	if blocks[0].API != poo.APIAnthropicMessages || blocks[0].Type != "thinking_signature" {
		t.Errorf("the block is %+v", blocks[0])
	}
	if string(blocks[0].Data) != `"sig_abc"` {
		t.Errorf("the signature is %s, want the signed string", blocks[0].Data)
	}
	if !blocks[0].Required {
		t.Error("the signature is not required, want a request that cannot leave it out")
	}
}

func TestTheThinkingBlockReturnsWithItsSignature(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	// The history holds the model's own thinking, with the signature the
	// reader reported riding beside it.
	signature, err := json.Marshal("sig_abc")
	if err != nil {
		t.Fatalf("encoding the signature: %v", err)
	}
	thinking := poo.Message{Role: poo.RoleAssistant, Content: []poo.ContentBlock{{
		Kind: poo.ContentThinking,
		Text: "Let me consider",
		Provider: &poo.ProviderBlock{
			API:      poo.APIAnthropicMessages,
			Type:     "thinking_signature",
			Data:     signature,
			Required: true,
		},
	}}}
	msgs := []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi"), thinking}

	reader, _, err := tr.Stream(context.Background(), request("", msgs...))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	assistant, _ := srv.requests[0]["messages"].([]any)
	last, _ := assistant[len(assistant)-1].(map[string]any)
	blocks, _ := last["content"].([]any)
	var found bool
	for _, raw := range blocks {
		block, _ := raw.((map[string]any))
		if block["type"] == "thinking" && block["thinking"] == "Let me consider" && block["signature"] == "sig_abc" {
			found = true
		}
	}
	if !found {
		t.Errorf("the thinking block did not return with its signature: %v", blocks)
	}
}

func TestAForkKeepsTheBodiesPrefixAndNamesTheSession(t *testing.T) {
	body := frame(`{"type":"message_start","message":{"usage":{"input_tokens":8}}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`) +
		frame(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`) +
		frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)
	tr := anthropic.New(anthropic.Options{
		Endpoint:      srv.URL,
		Model:         "claude-haiku-5-5",
		ModelInfo:     thinkingModel(),
		SessionHeader: "x-opencode-session",
	})

	reader, rendered, err := tr.Stream(context.Background(), request("ses_one"))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	reader, err = tr.Fork(context.Background(), rendered, []poo.Message{poo.NewTextMessage(poo.RoleUser, "more")}, 64)
	if err != nil {
		t.Fatalf("forking the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	sent := srv.requests[1]
	if got := sent["max_tokens"]; got != float64(64) {
		t.Errorf("the fork's output limit is %v, want the caller's", got)
	}
	messages, _ := sent["messages"].([]any)
	if len(messages) != 2 {
		t.Errorf("the fork carries %d messages, want the appended one", len(messages))
	}
	if got := srv.headers[1].Get("x-opencode-session"); got != "ses_one" {
		t.Errorf("the fork carried session %q, want the snapshot's", got)
	}
	// The fork's prefix is the one the endpoint already saw: the body it
	// was built from is the fork's own beginning.
	if !strings.HasPrefix(string(rendered.Body), `{"model":"claude-haiku-5-5"`) {
		t.Errorf("the snapshot begins %q", string(rendered.Body)[:40])
	}
}

func TestARefusalClassifiesItsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel(), MaxAttempts: 1})
	_, _, err := tr.Stream(context.Background(), request(""))
	if err == nil {
		t.Fatal("the request was not refused")
	}
	var perr *poo.ProviderError
	if !asProviderError(err, &perr) {
		t.Fatalf("the failure is %T, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindRateLimited {
		t.Errorf("the failure is %q, want rate limited", perr.Kind)
	}
}

func TestAMidStreamRefusalFailsTheStream(t *testing.T) {
	cases := []struct {
		event string
		kind  poo.ProviderKind
	}{
		{`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, poo.ProviderKindServer},
		{`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, poo.ProviderKindRateLimited},
	}
	for _, c := range cases {
		t.Run(c.event, func(t *testing.T) {
			srv := newSSEServer(t, frame(`{"type":"message_start","message":{"usage":{"input_tokens":8}}}`)+frame(c.event))
			tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})
			reader, _, err := tr.Stream(context.Background(), request(""))
			if err != nil {
				t.Fatalf("starting the request: %v", err)
			}
			var failure error
			for {
				_, err := reader.Next()
				if err != nil {
					failure = err
					break
				}
			}
			_ = reader.Close()
			if failure == io.EOF {
				t.Fatal("the stream ended cleanly, want the error event to fail it")
			}
			var perr *poo.ProviderError
			if !errors.As(failure, &perr) {
				t.Fatalf("the failure is %T, want a provider error", failure)
			}
			if perr.Kind != c.kind {
				t.Errorf("the failure is %q, want %q", perr.Kind, c.kind)
			}
		})
	}
}

func TestTheSamplingAModelTakesIsSent(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	info := thinkingModel()
	info.Temperature = poo.TempFree
	info.Sampling = poo.Sampling{Temperature: ptr(0.5), TopP: ptr(0.9), TopK: ptr(40)}
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: info})

	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	sent := srv.requests[0]
	if sent["temperature"] != 0.5 || sent["top_p"] != 0.9 || sent["top_k"] != float64(40) {
		t.Errorf("the sampling is %v/%v/%v, want the model's own", sent["temperature"], sent["top_p"], sent["top_k"])
	}

	for _, mode := range []poo.TempMode{poo.TempFixed, poo.TempUnsupported} {
		srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
		info := thinkingModel()
		info.Temperature = mode
		info.Sampling = poo.Sampling{Temperature: ptr(0.5)}
		tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: info})
		reader, _, err := tr.Stream(context.Background(), request(""))
		if err != nil {
			t.Fatalf("starting the request: %v", err)
		}
		read(t, reader)
		_ = reader.Close()
		if sent := srv.requests[0]; sent["temperature"] != nil {
			t.Errorf("a %q model was sent a temperature, want none", mode)
		}
	}
}

func TestTwoToolBlocksKeepTheirOwnArguments(t *testing.T) {
	body := frame(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"get_weather","input":{}}}`) +
		frame(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_b","name":"get_time","input":{}}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\""}}`) +
		frame(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"tz\""}}`) +
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":": \"Paris\"}"}}`) +
		frame(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":": \"CET\"}"}}`) +
		frame(`{"type":"content_block_stop","index":0}`) +
		frame(`{"type":"content_block_stop","index":1}`) +
		frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	assembled := map[int][]byte{}
	ids := map[int]string{}
	for _, ev := range read(t, reader) {
		if ev.Kind != "tool_call" {
			continue
		}
		if ev.ToolCall.ID != "" && ev.ToolCall.Arguments == nil {
			ids[ev.ToolCall.Index] = ev.ToolCall.ID
		}
		assembled[ev.ToolCall.Index] = append(assembled[ev.ToolCall.Index], ev.ToolCall.Arguments...)
	}
	_ = reader.Close()
	if ids[0] != "toolu_a" || string(assembled[0]) != `{"city": "Paris"}` {
		t.Errorf("the first call is %q with arguments %q", ids[0], assembled[0])
	}
	if ids[1] != "toolu_b" || string(assembled[1]) != `{"tz": "CET"}` {
		t.Errorf("the second call is %q with arguments %q", ids[1], assembled[1])
	}
}

func TestAForkResendsTheSnapshotAsItWas(t *testing.T) {
	body := frame(`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`) + frame(`{"type":"message_stop"}`)
	srv := newSSEServer(t, body)
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	reader, rendered, err := tr.Stream(context.Background(), request("ses_one"))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	first := srv.raw[0]

	// A fork that appends nothing and names no limit is the same bytes.
	reader, err = tr.Fork(context.Background(), rendered, nil, 0)
	if err != nil {
		t.Fatalf("forking the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	if string(srv.raw[1]) != string(first) {
		t.Errorf("a fork with nothing to add resent different bytes:\n%s\n%s", first, srv.raw[1])
	}
}

func TestAForkRefusesWhatIsNotItsOwn(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})

	if _, err := tr.Fork(context.Background(), poo.Rendered{API: poo.APIChatCompletions, Body: []byte(`{}`)}, nil, 0); err == nil {
		t.Error("a foreign snapshot was forked, want it refused")
	}
	if _, err := tr.Fork(context.Background(), poo.Rendered{API: poo.APIAnthropicMessages}, nil, 0); err == nil {
		t.Error("an empty snapshot was forked, want it refused")
	}
	if len(srv.requests) != 0 {
		t.Errorf("the refusals sent %d requests, want none to leave", len(srv.requests))
	}
}

func TestATruncatedAnswerReportsItsReason(t *testing.T) {
	srv := newSSEServer(t,
		frame(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)+
			frame(`{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`)+
			frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})
	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	var finish poo.FinishReason
	for _, ev := range read(t, reader) {
		if ev.Kind == "finish" {
			finish = ev.FinishReason
		}
	}
	_ = reader.Close()
	if finish != poo.FinishLength {
		t.Errorf("the finish reason is %q, want length", finish)
	}
}

func TestABudgetRidesTheThinkingObject(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	info := thinkingModel()
	info.Reasoning = poo.ReasoningControl{Mode: poo.ReasoningBudget, Param: poo.ReasoningPathBudget}
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: info})

	req := request("")
	req.Reasoning = "2048"
	reader, _, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	thinking, _ := srv.requests[0]["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Errorf("the thinking field is %v, want thinking enabled", srv.requests[0]["thinking"])
	}
	if thinking["budget_tokens"] != float64(2048) {
		t.Errorf("the thinking budget is %v, want 2048", thinking["budget_tokens"])
	}
}

func TestTheUserAgentNamesTheClient(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel(), UserAgent: "poo/test"})
	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	if got := srv.headers[0].Get("User-Agent"); got != "poo/test" {
		t.Errorf("the request named itself %q, want poo/test", got)
	}
}

func TestAKeylessEndpointSendsNoAuthorization(t *testing.T) {
	srv := newSSEServer(t, frame(`{"type":"message_stop"}`))
	tr := anthropic.New(anthropic.Options{Endpoint: srv.URL, Model: "claude-haiku-5-5", ModelInfo: thinkingModel()})
	reader, _, err := tr.Stream(context.Background(), request(""))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	if got := srv.headers[0].Get("Authorization"); got != "" {
		t.Errorf("the request carried %q, want no credential", got)
	}
}

// ptr returns a pointer to a float.
func ptr(v float64) *float64 { return &v }

// asProviderError reports whether an error, or any error it wraps, is a
// provider error.
func asProviderError(err error, target **poo.ProviderError) bool {
	perr, ok := err.(*poo.ProviderError)
	if ok {
		*target = perr
	}
	return ok
}
