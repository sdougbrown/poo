package interactions_test

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
	"github.com/sdougbrown/poo/provider/interactions"
)

// frame builds one SSE frame in the shape Google sends: the event's name on its
// own line, and the payload, which repeats it, on the next.
func frame(eventType, payload string) string {
	return "event: " + eventType + "\ndata: " + payload + "\n\n"
}

// step frames one step event.
func step(eventType string, payload string) string {
	return frame(eventType, `{"event_type":"`+eventType+`",`+payload+`}`)
}

// sseServer is a scripted Interactions endpoint.
type sseServer struct {
	*httptest.Server
	// body is the raw response body the server sends.
	body string
	// requests holds the request bodies the server received.
	requests []map[string]any
	// raw holds the request bodies as the bytes they arrived as.
	raw [][]byte
	// paths holds the paths the requests reached.
	paths []string
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
		s.paths = append(s.paths, r.URL.Path)
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

// read drains a stream and returns the events, and the failure that ended it.
func read(t *testing.T, reader poo.StreamReader) ([]poo.StreamItem, error) {
	t.Helper()
	var events []poo.StreamItem
	for {
		ev, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			return events, err
		}
		events = append(events, ev)
	}
}

// complete closes a stream with the event that says how it ended.
func complete(status string) string {
	return frame("interaction.completed", `{"event_type":"interaction.completed","interaction":{"id":"v1_abc","status":"`+status+`"}}`)
}

// geminiModel is the model info the built-in record gives a Gemini model: an
// effort level rides the generation config's thinking level, the levels are the
// four the Gemini 4 family takes, and no sampling field is ever sent.
func geminiModel() poo.ModelInfo {
	return poo.ModelInfo{
		ModelID: "gemini-4-hydrogen",
		Tools:   poo.ToolCalling{Mode: poo.ToolsNative},
		Reasoning: poo.ReasoningControl{
			Mode:  poo.ReasoningEffort,
			Param: "generation_config.thinking_level",
		},
		ReasoningLevels: []string{"low", "medium", "high", "xhigh"},
		Temperature:     poo.TempFixed,
	}
}

// request builds a chat request for the fake endpoint.
func request(msgs ...poo.Message) poo.ChatRequest {
	if msgs == nil {
		msgs = []poo.Message{poo.NewTextMessage(poo.RoleUser, "hi")}
	}
	return poo.ChatRequest{Model: "gemini-4-hydrogen", Messages: msgs, MaxTokens: 32}
}

// asProviderError reads a failure as a provider error.
func asProviderError(err error) (*poo.ProviderError, bool) {
	var perr *poo.ProviderError
	if errors.As(err, &perr) {
		return perr, true
	}
	return nil, false
}

// TestTheInteractionRequestCarriesItsOwnShape covers one whole turn: the URL
// the request reaches, the header the key travels in, the body's stateless
// shape, and the answer's text.
func TestTheInteractionRequestCarriesItsOwnShape(t *testing.T) {
	body := frame("interaction.created", `{"event_type":"interaction.created","interaction":{"id":"v1_abc","status":"in_progress"}}`) +
		step("step.start", `"index":0,"step":{"type":"model_output"}`) +
		step("step.delta", `"index":0,"delta":{"type":"text","text":"hi"}`) +
		step("step.stop", `"index":0`) +
		frame("interaction.completed", `{"event_type":"interaction.completed","interaction":{"id":"v1_abc","status":"completed","usage":{"total_input_tokens":11,"total_output_tokens":9,"total_cached_tokens":4,"total_thought_tokens":5}}}`) +
		frame("done", "[DONE]")
	srv := newSSEServer(t, body)

	msgs := []poo.Message{
		poo.NewTextMessage(poo.RoleSystem, "be brief"),
		poo.NewTextMessage(poo.RoleUser, "hi"),
	}
	tr := interactions.New(interactions.Options{
		Endpoint:  srv.URL + "/v1beta",
		Model:     "gemini-4-hydrogen",
		ModelInfo: geminiModel(),
		APIKey:    "gm_test",
		UserAgent: "poo/1.0",
	})
	reader, rendered, err := tr.Stream(context.Background(), request(msgs...))
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events, err := read(t, reader)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}

	if srv.paths[0] != "/v1beta/interactions" {
		t.Errorf("the request reached %q, want the interaction path", srv.paths[0])
	}
	headers := srv.headers[0]
	if got := headers.Get("x-goog-api-key"); got != "gm_test" {
		t.Errorf("the key header is %q, want the key", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Errorf("the request carried an Authorization header %q, want none", got)
	}
	if got := headers.Get("User-Agent"); got != "poo/1.0" {
		t.Errorf("the User-Agent is %q", got)
	}

	sent := srv.requests[0]
	if sent["model"] != "gemini-4-hydrogen" {
		t.Errorf("the model is %v", sent["model"])
	}
	if sent["stream"] != true {
		t.Errorf("the request does not stream: %v", sent)
	}
	// The request is stateless: the endpoint keeps nothing of the
	// conversation, and the whole history travels as steps.
	if sent["store"] != false {
		t.Errorf("the request stores the interaction: %v", sent["store"])
	}
	if sent["system_instruction"] != "be brief" {
		t.Errorf("the system instruction is %v, want the system message's text", sent["system_instruction"])
	}
	config, _ := sent["generation_config"].(map[string]any)
	if config["max_output_tokens"] != float64(32) {
		t.Errorf("the generation config is %v, want the request's output limit", sent["generation_config"])
	}
	if float64(rendered.MaxTokens) != config["max_output_tokens"] {
		t.Errorf("the snapshot's output limit is %d, want the body's %v", rendered.MaxTokens, config["max_output_tokens"])
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
			// The input count is the whole prompt, the cached tokens
			// included, and the output count takes the thought tokens the
			// API reports beside it.
			if ev.Usage.InputTokens != 11 || ev.Usage.OutputTokens != 14 {
				t.Errorf("the usage is %+v, want the whole prompt and the answer's own cost", ev.Usage)
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

// TestAThinkingStreamCarriesItsSignature covers the reasoning half: the
// summaries stream as thinking, and the signature the thought carries is the
// provider block the next request returns.
func TestAThinkingStreamCarriesItsSignature(t *testing.T) {
	body := step("step.start", `"index":0,"step":{"type":"thought"}`) +
		step("step.delta", `"index":0,"delta":{"type":"thought_summary","content":{"type":"text","text":"Need the file."}}`) +
		step("step.delta", `"index":0,"delta":{"type":"thought_signature","signature":"c2ln"}`) +
		step("step.stop", `"index":0`) +
		complete("completed") +
		frame("done", "[DONE]")
	srv := newSSEServer(t, body)
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel()})

	reader, _, err := tr.Stream(context.Background(), request())
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events, err := read(t, reader)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	_ = reader.Close()

	var thinking string
	for _, ev := range events {
		if ev.Kind == "delta" && ev.Delta.Kind == "thinking" {
			thinking += ev.Delta.Text
		}
	}
	if thinking != "Need the file." {
		t.Errorf("the thinking is %q, want the streamed summary", thinking)
	}
	reporter, ok := reader.(poo.ProviderBlockReporter)
	if !ok {
		t.Fatal("the reader reports no provider blocks")
	}
	blocks := reporter.ReportProviderBlocks()
	if len(blocks) != 1 {
		t.Fatalf("the reader reported %d blocks, want the signature", len(blocks))
	}
	if blocks[0].API != poo.APIGoogleInteractions || blocks[0].Type != "thought_signature" {
		t.Errorf("the block is %+v", blocks[0])
	}
	if string(blocks[0].Data) != `"c2ln"` {
		t.Errorf("the signature is %s, want the signed string", blocks[0].Data)
	}
	if !blocks[0].Required {
		t.Error("the signature is not required, want a request that cannot leave it out")
	}
}

// TestAToolCallStreamAccumulatesItsArguments covers the call half: the step
// opens the call, the fragments are the pieces of its arguments, and the step's
// stop says they are whole.
func TestAToolCallStreamAccumulatesItsArguments(t *testing.T) {
	body := step("step.start", `"index":0,"step":{"type":"function_call","id":"call_1","name":"read_file","arguments":{}}`) +
		step("step.delta", `"index":0,"delta":{"type":"arguments_delta","arguments":"{\"path\":"}`) +
		step("step.delta", `"index":0,"delta":{"type":"arguments_delta","arguments":"\"a.py\"}"}`) +
		step("step.stop", `"index":0`) +
		complete("requires_action") +
		frame("done", "[DONE]")
	srv := newSSEServer(t, body)

	tools := []poo.ToolSpec{{
		Name:        "read_file",
		Description: "Read a file",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}}
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel()})

	req := request()
	req.Tools = tools
	reader, _, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	events, err := read(t, reader)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	_ = reader.Close()

	// The request carries the tool in the API's own shape: a function, not
	// a tool with a nested function object.
	sent := srv.requests[0]
	sentTools, _ := sent["tools"].([]any)
	if len(sentTools) != 1 {
		t.Fatalf("the request carries %d tools, want one", len(sentTools))
	}
	tool, _ := sentTools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "read_file" {
		t.Errorf("the tool is %v, want a function named read_file", tool)
	}
	if _, ok := tool["parameters"]; !ok {
		t.Errorf("the tool carries no parameters: %v", tool)
	}

	var id, name string
	var arguments []byte
	var completeCall bool
	var finish poo.FinishReason
	for _, ev := range events {
		switch ev.Kind {
		case "tool_call":
			if ev.ToolCall.ID != "" && ev.ToolCall.Arguments == nil {
				id, name = ev.ToolCall.ID, ev.ToolCall.Name
			}
			arguments = append(arguments, ev.ToolCall.Arguments...)
			completeCall = ev.ToolCall.Complete
		case "finish":
			finish = ev.FinishReason
		}
	}
	if id != "call_1" || name != "read_file" {
		t.Errorf("the call is %q/%q", id, name)
	}
	if string(arguments) != `{"path":"a.py"}` {
		t.Errorf("the assembled arguments are %q", arguments)
	}
	if !completeCall {
		t.Error("the call is not complete, want the step's stop to finish it")
	}
	if finish != poo.FinishToolCalls {
		t.Errorf("the finish reason is %q, want tool calls", finish)
	}
}

// TestAThinkingLevelRidesTheGenerationConfig covers the reasoning control: the
// level is the config's own field, the summaries ride beside it so the thinking
// comes back as text, and a model asked to be quiet is sent no level at all.
func TestAThinkingLevelRidesTheGenerationConfig(t *testing.T) {
	srv := newSSEServer(t, complete("completed")+frame("done", "[DONE]"))
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel()})

	req := request()
	req.Reasoning = "high"
	reader, _, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	config, _ := srv.requests[0]["generation_config"].(map[string]any)
	if config["thinking_level"] != "high" {
		t.Errorf("the generation config is %v, want thinking level high", srv.requests[0]["generation_config"])
	}
	if config["thinking_summaries"] != "auto" {
		t.Errorf("the summaries are %v, want auto", config["thinking_summaries"])
	}

	// No model in this family can be told not to think, so quiet is a request
	// with no level at all.
	req = request()
	req.Reasoning = "off"
	reader, _, err = tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the quiet request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()
	quiet, _ := srv.requests[1]["generation_config"].(map[string]any)
	if _, ok := quiet["thinking_level"]; ok {
		t.Errorf("the quiet request carries %v, want no thinking level", quiet)
	}
	if _, ok := quiet["thinking_summaries"]; ok {
		t.Errorf("the quiet request carries %v, want no summaries", quiet)
	}
}

// TestASamplingParameterIsNeverSent covers the wire's own shape: the generation
// config has no sampling field, so a model whose settings declare one is sent
// none rather than a field the endpoint would refuse.
func TestASamplingParameterIsNeverSent(t *testing.T) {
	srv := newSSEServer(t, complete("completed")+frame("done", "[DONE]"))
	info := geminiModel()
	info.Temperature = poo.TempFree
	info.Sampling = poo.Sampling{Temperature: ptr(0.5), TopP: ptr(0.9)}
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: info})

	reader, _, err := tr.Stream(context.Background(), request())
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	sent := srv.requests[0]
	if _, ok := sent["temperature"]; ok {
		t.Errorf("the body carries a temperature: %v", sent)
	}
	config, _ := sent["generation_config"].(map[string]any)
	for _, name := range []string{"temperature", "top_p", "top_k"} {
		if _, ok := config[name]; ok {
			t.Errorf("the generation config carries %s: %v", name, config)
		}
	}
}

// TestAStreamThatEndsBeforeTheInteractionIsAFailure covers the interrupted
// turn: a stream that stops talking before it says how the interaction ended is
// not a short answer.
func TestAStreamThatEndsBeforeTheInteractionIsAFailure(t *testing.T) {
	body := step("step.start", `"index":0,"step":{"type":"model_output"}`) +
		step("step.delta", `"index":0,"delta":{"type":"text","text":"Partial"}`) +
		step("step.stop", `"index":0`) +
		frame("done", "[DONE]")
	srv := newSSEServer(t, body)
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel(), MaxAttempts: 1})

	reader, _, err := tr.Stream(context.Background(), request())
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	_, err = read(t, reader)
	_ = reader.Close()
	if err == nil {
		t.Fatal("the stream ended cleanly, want the missing completion to fail it")
	}
	if !strings.Contains(err.Error(), "before the interaction completed") {
		t.Errorf("the failure is %v, want the missing completion named", err)
	}
}

// TestAMidStreamRefusalFailsTheStream covers the error event: the stream says
// what happened, and the class comes from the code the event carries.
func TestAMidStreamRefusalFailsTheStream(t *testing.T) {
	srv := newSSEServer(t, step("step.start", `"index":0,"step":{"type":"model_output"}`)+
		frame("error", `{"event_type":"error","error":{"message":"slow down","code":"resource_exhausted"}}`))
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel(), MaxAttempts: 1})

	reader, _, err := tr.Stream(context.Background(), request())
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	_, err = read(t, reader)
	_ = reader.Close()
	perr, ok := asProviderError(err)
	if !ok {
		t.Fatalf("the failure is %v, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindRateLimited {
		t.Errorf("the failure is %q, want rate limited", perr.Kind)
	}
}

// TestARefusalClassifiesItsStatus covers the HTTP answer: the API's error body
// is the envelope every transport reads, and a rate limit is worth another
// attempt.
func TestARefusalClassifiesItsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Resource has been exhausted.","status":"RESOURCE_EXHAUSTED"}}`)
	}))
	defer srv.Close()

	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel(), MaxAttempts: 1})
	_, _, err := tr.Stream(context.Background(), request())
	if err == nil {
		t.Fatal("the request was not refused")
	}
	perr, ok := asProviderError(err)
	if !ok {
		t.Fatalf("the failure is %v, want a provider error", err)
	}
	if perr.Kind != poo.ProviderKindRateLimited {
		t.Errorf("the failure is %q, want rate limited", perr.Kind)
	}
}

// TestAForkKeepsThePrefixAndForbidsWork covers the compaction seam: the fork
// re-sends the snapshot's steps, appends the new one, replaces the output limit
// and sets the tool choice to none, and every other byte is the snapshot's own.
func TestAForkKeepsThePrefixAndForbidsWork(t *testing.T) {
	body := step("step.start", `"index":0,"step":{"type":"model_output"}`) +
		step("step.delta", `"index":0,"delta":{"type":"text","text":"hi"}`) +
		step("step.stop", `"index":0`) +
		complete("completed") +
		frame("done", "[DONE]")
	srv := newSSEServer(t, body)
	tr := interactions.New(interactions.Options{Endpoint: srv.URL, Model: "gemini-4-hydrogen", ModelInfo: geminiModel()})

	req := request()
	req.Tools = []poo.ToolSpec{{Name: "read_file", Description: "Read a file"}}
	reader, rendered, err := tr.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("starting the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	reader, err = tr.Fork(context.Background(), rendered, []poo.Message{poo.NewTextMessage(poo.RoleUser, "more")}, 64, poo.ForkOptions{})
	if err != nil {
		t.Fatalf("forking the request: %v", err)
	}
	read(t, reader)
	_ = reader.Close()

	sent := srv.requests[1]
	config, _ := sent["generation_config"].(map[string]any)
	if config["max_output_tokens"] != float64(64) {
		t.Errorf("the fork's output limit is %v, want the caller's", config["max_output_tokens"])
	}
	if config["tool_choice"] != "none" {
		t.Errorf("the fork's tool choice is %v, want none", config["tool_choice"])
	}
	input, _ := sent["input"].([]any)
	if len(input) != 2 {
		t.Errorf("the fork carries %d steps, want the appended one", len(input))
	}
	// The fork's prefix is the one the endpoint already saw: the body it was
	// built from is the fork's own beginning, and the steps it carried are
	// the same bytes.
	if !strings.HasPrefix(string(srv.raw[1]), `{"model":"gemini-4-hydrogen"`) {
		t.Errorf("the fork begins %q", string(srv.raw[1])[:40])
	}
	var before, after struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(rendered.Body, &before); err != nil {
		t.Fatalf("decoding the snapshot: %v", err)
	}
	if err := json.Unmarshal(srv.raw[1], &after); err != nil {
		t.Fatalf("decoding the fork: %v", err)
	}
	if len(before.Input) == 0 || len(after.Input) == 0 {
		t.Fatalf("the fork carries %d steps, the snapshot %d", len(after.Input), len(before.Input))
	}
	if string(after.Input[0]) != string(before.Input[0]) {
		t.Errorf("the fork's first step is %s, want the snapshot's %s", after.Input[0], before.Input[0])
	}
}

// TestAnEndpointThatNamesTheInteractionPathIsUsedAsItIs covers the plan's
// endpoint: it answers on its own method path, so poo appends nothing to the
// base it was given.
func TestAnEndpointThatNamesTheInteractionPathIsUsedAsItIs(t *testing.T) {
	cases := map[string]string{
		"https://example.test/v1beta":                                 "https://example.test/v1beta/interactions",
		"https://example.test/v1beta/":                                "https://example.test/v1beta/interactions",
		"https://example.test/v1beta/interactions":                    "https://example.test/v1beta/interactions",
		"https://example.test/v1beta/interactions:createPerUserQuota": "https://example.test/v1beta/interactions:createPerUserQuota",
	}
	for base, want := range cases {
		if got := interactions.InteractionsURL(base); got != want {
			t.Errorf("the URL of %q is %q, want %q", base, got, want)
		}
	}
}

// ptr returns a pointer to a value, for the sampling a model may declare.
func ptr[T any](v T) *T {
	return &v
}
