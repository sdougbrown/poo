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
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, error) {
	body, err := t.buildBody(req)
	if err != nil {
		return nil, err
	}

	// The timeout bounds the whole request, streaming included, so the
	// context is created before the request goes out.
	var cancel context.CancelFunc
	if t.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, t.cfg.Timeout)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.chatURL(), bytes.NewReader(body))
	if err != nil {
		if cancel != nil {
			cancel()
		}
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
		if cancel != nil {
			cancel()
		}
		return nil, classifyTransportError(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		if cancel != nil {
			cancel()
		}
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
		if cancel != nil {
			cancel()
		}
		return nil, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	if first == '{' {
		defer resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		body, _ := io.ReadAll(br)
		if kind, msg, code, ok := errorInBody(body); ok {
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
		scanner: bufio.NewScanner(br),
		parser:  newParser(t.cfg.ToolsMode),
		ctx:     ctx,
		cancel:  cancel,
		started: started,
	}, nil
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
	return classifyError(code, message), message, code, true
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
// too long.
func classifyError(code, message string) poo.ProviderKind {
	lower := strings.ToLower(code + " " + message)
	switch {
	case isContextLength(code, message):
		return poo.ProviderKindContextLength
	case strings.Contains(lower, "rate_limit"):
		return poo.ProviderKindRateLimited
	case strings.Contains(lower, "quota"):
		return poo.ProviderKindUsageLimit
	case strings.Contains(lower, "authentication"):
		return poo.ProviderKindAuthFailed
	default:
		return poo.ProviderKindServer
	}
}

// chatURL returns the endpoint's chat completion URL.
func (t *Transport) chatURL() string {
	base := strings.TrimRight(t.cfg.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

// buildBody turns a core request into the wire payload.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	out := chatRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
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
	for _, c := range m.Content {
		switch c.Kind {
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
		if strings.Contains(strings.ToLower(code+" "+message), "quota") ||
			strings.Contains(strings.ToLower(code+" "+message), "usage_limit") {
			kind = poo.ProviderKindUsageLimit
		}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		kind = poo.ProviderKindInvalidRequest
	}
	// A context-length failure is a context-length failure whatever
	// status it arrived with.
	if isContextLength(code, message) {
		kind = poo.ProviderKindContextLength
	}

	return &poo.ProviderError{
		Kind:    kind,
		Message: message,
		Status:  resp.StatusCode,
		Code:    code,
	}
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
