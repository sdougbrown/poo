// Package openai implements the OpenAI-compatible Chat Completions transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape every
// OpenAI-compatible endpoint (llama.cpp, vLLM, sglang, Ollama, LiteLLM)
// accepts. It has no retries and no request cache: a failed request ends
// the run.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// Config configures the transport.
type Config struct {
	// BaseURL is the endpoint's base, e.g. "http://localhost:4000/v1".
	BaseURL string
	// Model is the model identifier.
	Model string
	// APIKey is the bearer token, empty when the endpoint needs none.
	APIKey string
	// ToolsMode is how the run asks for tool calls: "native", "text" or
	// "auto".
	ToolsMode string
	// ReasoningEffort is sent as the request's reasoning_effort when it
	// is set, for endpoints whose models think only when asked to.
	ReasoningEffort string
	// HTTPClient is the client to use. Nil means a default client.
	HTTPClient *http.Client
	// Timeout bounds a whole request, streaming included. Zero means no
	// extra bound beyond the run's context.
	Timeout time.Duration
}

// Transport is the OpenAI-compatible chat transport.
type Transport struct {
	cfg  Config
	http *http.Client
}

// New returns a transport.
func New(cfg Config) *Transport {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}}
	}
	return &Transport{cfg: cfg, http: client}
}

// chatRequest is the wire shape of a chat completion request.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
	// MaxTokens is always sent, because some local servers default to 512.
	MaxTokens int  `json:"max_tokens"`
	Stream    bool `json:"stream"`
	// ReasoningEffort asks a thinking model to think. It is omitted when
	// unset, so an endpoint that does not know the field never sees it.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// StreamOptions asks for the usage chunk, which some servers honour
	// and others ignore.
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

// streamOptions asks for a final usage chunk.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage is the wire shape of one message.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatToolCall is the wire shape of a tool call.
type chatToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// chatTool is the wire shape of a tool.
type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// errorResponse is the wire shape of an OpenAI-style error.
type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// errorBody is the wire shape of an error payload, without the envelope.
// A stream chunk carries the error directly rather than in the envelope.
type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// Stream starts a chat completion request and returns the response stream.
//
// The snapshot is the serialized request body, exactly as it went out. A
// fork is built from it, so the fork sends the same prefix.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	body, err := t.buildBody(req)
	if err != nil {
		return nil, poo.Rendered{}, err
	}
	rendered := poo.Rendered{
		API:      poo.APIChatCompletions,
		Body:     body,
		Messages: len(req.Messages),
	}

	reader, err := t.open(ctx, body)
	if err != nil {
		return nil, rendered, err
	}
	return reader, rendered, nil
}

// Fork sends a body that is byte-identical to the snapshot, apart from
// the appended messages and the output ceiling.
//
// The snapshot holds the serialized body, so the fork re-sends it with
// the appended messages added after the last one. The key order and
// every other field are unchanged, because the body was serialized once
// and only the two changed fields are rewritten.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	if prev.API != poo.APIChatCompletions {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: fmt.Sprintf("the request snapshot belongs to the %q API, not Chat Completions", prev.API),
		}
	}
	if len(prev.Body) == 0 {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot holds no body to fork",
		}
	}
	body, err := forkBody(prev.Body, appended, maxTokens)
	if err != nil {
		return nil, err
	}

	reader, err := t.open(ctx, body)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// open sends a serialized body and returns the response stream.
//
// Stream and Fork both go through it, so the timeout, the headers, the
// error classification and the time to the first token mean the same
// thing for both.
func (t *Transport) open(ctx context.Context, body []byte) (*streamReader, error) {
	// The timeout bounds the whole request, streaming included, so the
	// context is created before the request goes out.
	var cancel context.CancelFunc
	if t.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, t.cfg.Timeout)
	}
	release := func() {
		if cancel != nil {
			cancel()
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatURL(t.cfg.BaseURL), bytes.NewReader(body))
	if err != nil {
		release()
		return nil, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if t.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
	}

	// The request is timed from the moment it goes out, so the time to
	// the first token includes the endpoint's own latency.
	started := time.Now()

	resp, err := t.http.Do(httpReq)
	if err != nil {
		// The failure is classified before the request's context is
		// released, because releasing it would replace the failure with
		// the context's own error.
		perr := classifyTransportError(ctx, err)
		release()
		return nil, perr
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		// The body is read under the request's timeout, so the context is
		// released after it.
		defer release()
		return nil, classifyStatus(resp)
	}

	// Some servers answer a failed request with a 200 and a JSON error
	// body instead of the SSE stream. The first byte tells the two apart,
	// so the stream is not buffered and the time to the first token
	// measures the endpoint's latency rather than the reader's.
	br := bufio.NewReader(resp.Body)
	first, err := peekFirstByte(br)
	if err != nil {
		resp.Body.Close()
		release()
		return nil, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	if first == '{' {
		defer resp.Body.Close()
		defer release()
		errBody, err := io.ReadAll(io.LimitReader(br, 1<<20))
		if err != nil {
			return nil, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
		}
		if kind, msg, code, ok := errorInBody(errBody); ok {
			return nil, &poo.ProviderError{
				Kind:    kind,
				Message: msg,
				Code:    code,
				Status:  resp.StatusCode,
			}
		}
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint returned a body that is not a stream",
			Status:  resp.StatusCode,
		}
	}

	return &streamReader{
		body:    resp.Body,
		scanner: newSSEScanner(br),
		parser:  newParser(t.cfg.ToolsMode),
		ctx:     ctx,
		cancel:  cancel,
		started: started,
	}, nil
}

// forkBody appends messages to a serialized request body.
//
// The body is decoded, the messages are appended after the last one and
// the output ceiling is replaced, and the result is serialized again. The
// key order is the one the body already had, because the keys are
// written in the transport's own order and any key it does not know
// keeps its position.
func forkBody(body []byte, appended []poo.Message, maxTokens int) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}

	if len(appended) > 0 {
		var messages []json.RawMessage
		if raw, ok := payload["messages"]; ok {
			if err := json.Unmarshal(raw, &messages); err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the request snapshot's messages do not decode",
				}
			}
		}
		for _, m := range appended {
			cm, err := convertMessage(m)
			if err != nil {
				return nil, err
			}
			raw, err := json.Marshal(cm)
			if err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the appended message does not encode",
				}
			}
			messages = append(messages, raw)
		}
		raw, err := json.Marshal(messages)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the messages do not encode",
			}
		}
		payload["messages"] = raw
	}

	if maxTokens > 0 {
		raw, err := json.Marshal(maxTokens)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the output ceiling does not encode",
			}
		}
		payload["max_tokens"] = raw
	}

	// The keys are written in the order the body had them, so the fork's
	// prefix is byte-identical to the original. Every key the payload
	// holds came from the body, except the two the fork may add; those
	// go last, in the order they are named.
	order, err := jsonKeyOrder(body)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, k := range order {
		seen[k] = true
	}
	for _, k := range []string{"messages", "max_tokens"} {
		if !seen[k] {
			if _, ok := payload[k]; ok {
				order = append(order, k)
				seen[k] = true
			}
		}
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range order {
		if i > 0 {
			b.WriteByte(',')
		}
		// A string always encodes.
		key, _ := json.Marshal(k)
		b.Write(key)
		b.WriteByte(':')
		b.Write(payload[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// jsonKeyOrder returns the top-level keys of a JSON object, in the order
// the object holds them.
//
// The order matters for a fork: the forked body has to be byte-identical
// to the body it came from, apart from the fields that changed, and a
// JSON object decoded into a map loses its order.
func jsonKeyOrder(body []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON object",
		}
	}
	var order []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's keys do not decode",
			}
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's keys do not decode",
			}
		}
		order = append(order, key)
		// The value is skipped: the payload map already holds it.
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the request snapshot's values do not decode",
			}
		}
	}
	return order, nil
}

// peekFirstByte returns the first non-space byte of the body, without
// consuming it.
func peekFirstByte(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			if _, err := br.ReadByte(); err != nil {
				return 0, err
			}
		default:
			return b[0], nil
		}
	}
}

// errorInBody reports whether a response body is a JSON error rather than
// an SSE stream.
func errorInBody(body []byte) (poo.ProviderKind, string, string, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", "", "", false
	}
	var payload struct {
		Error *errorBody `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload.Error == nil {
		return "", "", "", false
	}
	code := codeString(payload.Error.Code)
	message := payload.Error.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return classifyError(code, message, poo.ProviderKindInvalidRequest), message, code, true
}

// codeString renders an error code, which a server may send as a string
// or as a number.
func codeString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case float64:
		return fmt.Sprintf("%v", c)
	default:
		return ""
	}
}

// classifyError names the failure class an error code and message imply.
//
// A context-length failure is recognised whatever status it arrived with,
// because a server that reports one with a 500 still means the request was
// too long. A failure the body does not describe falls to the default the
// caller names: a body that answers a request with a refusal is a
// rejected request, and an error chunk in a stream is the server's own
// failure.
func classifyError(code, message string, fallback poo.ProviderKind) poo.ProviderKind {
	lower := strings.ToLower(code + " " + message)
	switch {
	case isContextLength(code, message):
		return poo.ProviderKindContextLength
	case strings.Contains(lower, "rate_limit"):
		return poo.ProviderKindRateLimited
	case strings.Contains(lower, "quota"), strings.Contains(lower, "usage_limit"):
		return poo.ProviderKindUsageLimit
	case strings.Contains(lower, "authentication"):
		return poo.ProviderKindAuthFailed
	default:
		return fallback
	}
}

// ChatURL returns the chat completion URL of an endpoint.
//
// A base URL that already ends in /chat/completions is used as it is.
func ChatURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

// buildBody turns a core request into the wire payload.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	out := chatRequest{
		Model:           req.Model,
		MaxTokens:       req.MaxTokens,
		Stream:          true,
		ReasoningEffort: t.cfg.ReasoningEffort,
		// The usage chunk is asked for in every mode, because the log's
		// usage record needs the token accounting and a server that
		// reports it on its own still ignores the field.
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	for _, m := range req.Messages {
		cm, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, cm)
	}

	// In native mode the tools go in the request, and the model answers
	// with tool_calls. In text mode the tools are described in the
	// prompt instead, and the model answers with text that the parser
	// reads. In auto mode both happen.
	if t.cfg.ToolsMode == "native" || t.cfg.ToolsMode == "auto" {
		for _, spec := range req.Tools {
			var ct chatTool
			ct.Type = "function"
			ct.Function.Name = spec.Name
			ct.Function.Description = spec.Description
			ct.Function.Parameters = spec.Schema
			if len(ct.Function.Parameters) == 0 {
				ct.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			out.Tools = append(out.Tools, ct)
		}
	}

	return json.Marshal(out)
}

// convertMessage turns a core message into the wire shape.
func convertMessage(m poo.Message) (chatMessage, error) {
	cm := chatMessage{Role: string(m.Role)}

	var text strings.Builder
	var calls []chatToolCall
	// The provider blocks are checked in a pass of their own, so a block
	// after a tool result is checked too.
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := checkProviderBlock(c.Provider); err != nil {
				return chatMessage{}, err
			}
		}
	}
	for _, c := range m.Content {
		switch c.Kind {
		case poo.ContentProvider:
			// The block's payload is opaque and has no wire field here.
			// checkProviderBlock has already refused a required block, so
			// what is left is optional and omitted. The plan's "the
			// omission is recorded" is the loop's job (Stage 3/6): the
			// transport cannot write records.
		case poo.ContentText:
			text.WriteString(c.Text)
		case poo.ContentThinking:
			// The wire shape has no separate thinking field, so thinking is
			// dropped rather than merged into the answer.
		case poo.ContentToolCall:
			var ct chatToolCall
			ct.Index = len(calls)
			ct.ID = c.ToolCallID
			ct.Type = "function"
			ct.Function.Name = c.Name
			ct.Function.Arguments = string(c.Arguments)
			calls = append(calls, ct)
		case poo.ContentToolResult:
			cm.Role = "tool"
			cm.ToolCallID = c.ToolCallID
			cm.Content = c.Text
			return cm, nil
		}
	}

	cm.Content = text.String()
	cm.ToolCalls = calls
	return cm, nil
}

// checkProviderBlock applies the opaque-block rule to one block.
//
// Chat Completions has no field that carries opaque data, so no block can
// be sent. An optional block is omitted, and a required one fails the
// request, whichever API produced it.
func checkProviderBlock(p *poo.ProviderBlock) error {
	if p.Required {
		return &poo.ProviderError{
			Kind: poo.ProviderKindSessionIncompatible,
			Message: fmt.Sprintf("the history holds a required %q block (%s) that Chat Completions cannot carry",
				p.Type, p.API),
		}
	}
	return nil
}

// classifyStatus turns a non-200 response into a provider error.
func classifyStatus(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	er := errorResponse{}
	_ = json.Unmarshal(body, &er)

	code := codeString(er.Error.Code)

	message := er.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = resp.Status
	}

	kind := poo.ProviderKindServer
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		kind = poo.ProviderKindAuthFailed
	case resp.StatusCode == http.StatusPaymentRequired:
		kind = poo.ProviderKindPlanInactive
	case resp.StatusCode == http.StatusTooManyRequests:
		kind = poo.ProviderKindRateLimited
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		kind = poo.ProviderKindInvalidRequest
	}
	// The payload's own words refine the status's guess, so the same
	// error classifies the same way whichever door it came through.
	kind = classifyError(code, message, kind)

	return &poo.ProviderError{
		Kind:       kind,
		Message:    message,
		Status:     resp.StatusCode,
		Code:       code,
		RetryAfter: retryAfter(resp),
	}
}

// retryAfter returns how long the endpoint asked the client to wait.
//
// The header is either a number of seconds or an HTTP date, and the plan
// reads a Retry-After over thirty seconds as a provider limit rather than
// a failure to retry.
func retryAfter(resp *http.Response) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	d := time.Until(when)
	if d <= 0 {
		return 0
	}
	return d
}

// isContextLength reports whether an error means the request was too long.
func isContextLength(code, message string) bool {
	text := strings.ToLower(code + " " + message)
	for _, needle := range []string{
		"context_length",
		"context length",
		"maximum context",
		"max context",
		"too many tokens",
		"reduce the length",
		"prompt is too long",
		"input is too long",
		"exceeds the context",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// classifyTransportError turns a request failure into a provider error.
func classifyTransportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
}
