// Package openai implements the OpenAI-compatible Chat Completions transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape every
// OpenAI-compatible endpoint (llama.cpp, vLLM, sglang, Ollama, LiteLLM)
// accepts. It has no request cache: a failed request is retried within
// the plan's budget, and a request that exhausts it ends the run.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
)

// Options configures the transport.
type Options struct {
	// BaseURL is the endpoint's base, e.g. "http://localhost:4000/v1".
	BaseURL string
	// Model is the model identifier.
	Model string
	// APIKey is the bearer token, empty when the endpoint needs none.
	APIKey string
	// ToolsMode is how the run asks for tool calls: "native", "text" or
	// "auto". A model info that names a mode wins over this one.
	ToolsMode string
	// ReasoningEffort is sent as the request's reasoning_effort when it
	// is set, for endpoints whose models think only when asked to. A
	// model info that names a reasoning control wins over this one.
	ReasoningEffort string
	// ModelInfo is the model's model info. It names the reasoning control,
	// the temperature control, the tools mode and the quirks.
	ModelInfo poo.ModelInfo
	// Reasoning is the reasoning setting: "off", "low", "medium",
	// "high", or a number of thinking tokens.
	Reasoning string
	// Sampling holds the sampling overrides. They win over the
	// model info's own defaults, field by field.
	Sampling poo.Sampling
	// Logger receives the transport's notes, such as a degenerate
	// stream. A nil logger drops them.
	Logger *slog.Logger
	// OnAttempt is called for every attempt that failed, so a consumer
	// can write the attempt to the log.
	//
	// The request's own reporter is the core's channel: it hears the
	// attempts of one request, and the core writes them to the
	// session's record.
	OnAttempt func(Attempt)
	// MaxAttempts is the number of attempts one request gets. Zero
	// means the plan's three.
	MaxAttempts int
	// Backoff is the wait before the second and the third attempt.
	// Empty means the plan's one second and then two.
	Backoff []time.Duration
	// HTTPClient is the client to use. Nil means a default client.
	HTTPClient *http.Client
	// Timeout bounds a whole request, streaming included. Zero means no
	// extra bound beyond the run's context.
	Timeout time.Duration
}

// Transport is the OpenAI-compatible chat transport.
type Transport struct {
	cfg  Options
	http *http.Client
	// policy is the retry policy.
	policy retryPolicy
	// logger receives the transport's notes.
	logger *slog.Logger
}

// New returns a transport.
func New(cfg Options) *Transport {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}}
	}
	policy := defaultRetryPolicy()
	if cfg.MaxAttempts > 0 {
		policy.attempts = cfg.MaxAttempts
	}
	if len(cfg.Backoff) > 0 {
		policy.backoff = cfg.Backoff
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Transport{cfg: cfg, http: client, policy: policy, logger: logger}
}

// toolsMode returns how the run asks for tool calls.
//
// A model info that names a mode wins over the configured one.
func (t *Transport) toolsMode(info poo.ModelInfo) string {
	if mode := info.Tools.Mode; mode != "" {
		return string(mode)
	}
	return t.cfg.ToolsMode
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
	// Params holds the parameters whose names the endpoint chooses: the
	// reasoning parameter, the sampling parameters and whatever a
	// quirk adds. They are written after the fields above, in name
	// order, so the same request always has the same body.
	Params map[string]json.RawMessage `json:"-"`
}

// MarshalJSON writes the request, with the named parameters merged into
// the top level.
//
// The parameters are written last and in name order, so the body of two
// requests that ask for the same thing is the same bytes.
func (r chatRequest) MarshalJSON() ([]byte, error) {
	type plain chatRequest
	body, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	if len(r.Params) == 0 {
		return body, nil
	}
	names := make([]string, 0, len(r.Params))
	for name := range r.Params {
		names = append(names, name)
	}
	sort.Strings(names)

	var b bytes.Buffer
	b.Write(body[:len(body)-1])
	for _, name := range names {
		b.WriteByte(',')
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(r.Params[name])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
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
	// ResetsAt is when the endpoint says the limit resets. Some
	// endpoints put it beside the error rather than in it.
	ResetsAt any `json:"resets_at"`
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
//
// A request that fails before the endpoint answered is retried within
// the plan's budget, and a stream that breaks before it carried a delta
// is retried too.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	settings := t.requestSettings(req)
	body, err := t.buildBodyWith(req, settings)
	if err != nil {
		return nil, poo.Rendered{}, err
	}
	rendered := poo.Rendered{
		API:      poo.APIChatCompletions,
		Body:     body,
		Messages: len(req.Messages),
	}

	reader, attempt, err := t.openWithRetry(ctx, body, req.OnAttempt, settings)
	if err != nil {
		return nil, rendered, err
	}
	return &streamWithRetry{
		transport: t,
		ctx:       ctx,
		body:      body,
		attempt:   attempt,
		current:   reader,
		report:    req.OnAttempt,
		settings:  settings,
	}, rendered, nil
}

// Fork sends a body that is byte-identical to the snapshot, apart from
// the appended messages and the output limit.
//
// The snapshot holds the serialized body, so the fork re-sends it with
// the appended messages added after the last one. The key order and
// every other field are unchanged, because the body was serialized once
// and only the two changed fields are rewritten.
//
// A fork is retried within the plan's budget, like any other request.
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

	settings := t.requestSettings(poo.ChatRequest{})
	reader, attempt, err := t.openWithRetry(ctx, body, nil, settings)
	if err != nil {
		return nil, err
	}
	return &streamWithRetry{
		transport: t,
		ctx:       ctx,
		body:      body,
		attempt:   attempt,
		current:   reader,
		settings:  settings,
	}, nil
}

// open sends a serialized body and returns the response stream.
//
// Stream and Fork both go through it, so the timeout, the headers, the
// error classification and the time to the first token mean the same
// thing for both.
func (t *Transport) open(ctx context.Context, body []byte, settings requestSettings) (*streamReader, error) {
	// The timeout bounds the whole request, streaming included, so the
	// context is created before the request goes out. The parent stays
	// apart, so a timeout the transport imposed is told from the run's
	// own cancellation.
	parent := ctx
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
		perr := classifyTransportError(parent, ctx, err)
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
		if perr, ok := errorInBody(errBody, resp.StatusCode, resp.Header); ok {
			return nil, perr
		}
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint returned a body that is not a stream",
			Status:  resp.StatusCode,
		}
	}

	return &streamReader{
		body:        resp.Body,
		scanner:     newSSEScanner(br),
		filters:     t.textFilters(settings),
		deferParsed: t.sendsTools(settings),
		ctx:         ctx,
		parent:      parent,
		cancel:      cancel,
		started:     started,
	}, nil
}

// forkBody appends messages to a serialized request body.
//
// The body is decoded, the messages are appended after the last one and
// the output limit is replaced, and the result is serialized again. The
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
//
// A body that names no failure is not one: the endpoint answered with
// something the transport cannot read, and the caller says so.
func errorInBody(body []byte, status int, header http.Header) (*poo.ProviderError, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var payload struct {
		Error *errorBody `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload.Error == nil {
		return nil, false
	}
	code := codeString(payload.Error.Code)
	message := payload.Error.Message
	if message == "" {
		message = "the endpoint returned an error"
	}
	return &poo.ProviderError{
		Kind:     failureKind(status, code, payload.Error.Type, message, poo.ProviderKindInvalidRequest),
		Message:  message,
		Code:     code,
		Status:   status,
		ResetsAt: resetAt(header, body),
	}, true
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
			// omission is recorded" is the loop's responsibility: the
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

// classifyTransportError turns a request failure into a provider error.
func classifyTransportError(parent, request context.Context, err error) error {
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if request != nil && request.Err() != nil {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: request.Err().Error()}
	}
	return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
}
