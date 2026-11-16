// Package openai implements the OpenAI-compatible Chat Completions transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape every
// OpenAI-compatible endpoint (llama.cpp, vLLM, sglang, Ollama, LiteLLM)
// accepts. It has no request cache: a failed request is retried within
// the plan's budget, and a request that exhausts it ends the run.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// Attempt is one attempt of a request that failed.
//
// The transport reports every attempt, so a consumer can write the
// attempt to the log and tell how a request ended. It is the core's
// type: the core writes the attempts to the session's record.
type Attempt = poo.Attempt

// Options configures the transport.
type Options struct {
	// Endpoint is the endpoint's base, e.g. "http://localhost:4000/v1".
	Endpoint string
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
	// ModelInfo is the model's info. It names the reasoning control,
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
	policy support.Policy
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
	policy := support.DefaultPolicy().With(cfg.MaxAttempts, cfg.Backoff)
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Transport{cfg: cfg, http: client, policy: policy, logger: logger}
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
//
// Content is a string for a message whose content is text, and the parts
// array when the message carries an image: the Chat Completions API takes an
// image only in a user message's parts, so an image a tool returned is sent
// in the message that follows its result.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatContentPart is one part of a message's parts array.
type chatContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   *chatImageURL   `json:"image_url,omitempty"`
	InputAudio *chatInputAudio `json:"input_audio,omitempty"`
	AudioURL   *chatAudioURL   `json:"audio_url,omitempty"`
}

// chatInputAudio is the audio a part carries: base64 and the format the API
// names for it.
type chatInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// chatImageURL is the image a part carries, as a data URL.
type chatImageURL struct {
	URL string `json:"url"`
}

// chatAudioURL is the audio a part carries, as a URL.
//
// The spec calls the part `input_audio` and carries base64 with a format name;
// a server that mirrors `image_url` for sound names this instead. The quirk
// that emits it is what a model on such a server names.
type chatAudioURL struct {
	URL string `json:"url"`
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
		Quirks:   settings.info.Quirks,
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, settings)
	}
	report := support.Reporter(t.cfg.OnAttempt, req.OnAttempt)
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, report)
	if err != nil {
		return nil, rendered, err
	}
	return support.NewRetrying(ctx, t.policy, open, report, reader, attempt), rendered, nil
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
	// The snapshot carries the profiles the body was shaped with, so the
	// appended messages are shaped by those and not by the transport's own
	// model info: a request may name a profile the transport's info does not.
	settings := t.requestSettings(poo.ChatRequest{})
	body, err := forkBody(prev.Body, appended, maxTokens, quirksFor(prev.Quirks, t.logger))
	if err != nil {
		return nil, err
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, settings)
	}
	report := support.Reporter(t.cfg.OnAttempt, nil)
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, report)
	if err != nil {
		return nil, err
	}
	return support.NewRetrying(ctx, t.policy, open, report, reader, attempt), nil
}

// open sends a serialized body and returns the response stream.
//
// Stream and Fork both go through it, so the timeout, the headers, the
// error classification and the time to the first token mean the same
// thing for both.
func (t *Transport) open(ctx context.Context, body []byte, settings requestSettings) (*streamReader, error) {
	return support.OpenStream(ctx, support.OpenOptions[*streamReader]{
		Client:  t.http,
		Timeout: t.cfg.Timeout,
		Request: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatURL(t.cfg.Endpoint), bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			if t.cfg.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
			}
			return req, nil
		},
		Build: func(stream support.Stream) *streamReader {
			return &streamReader{
				closer:      support.Closer{Body: stream.Body, Cancel: stream.Cancel},
				frames:      support.NewFrames(support.SSEScanner(stream.Reader)),
				filters:     t.textFilters(settings),
				deferParsed: t.sendsTools(settings),
				ctx:         stream.Context,
				parent:      stream.Parent,
				ttft:        stream.TTFT,
			}
		},
	})
}

// forkBody appends messages to a serialized request body.
//
// The body is decoded, the messages are appended after the last one and
// the output limit is replaced, and the result is serialized again. The
// key order is the one the body already had, because the keys are
// written in the transport's own order and any key it does not know
// keeps its position.
func forkBody(body []byte, appended []poo.Message, maxTokens int, quirks []quirkProfile) ([]byte, error) {
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
		converted, err := convertMessages(appended)
		if err != nil {
			return nil, err
		}
		for _, cm := range shapeAppended(quirks, converted) {
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
				Message: "the output limit does not encode",
			}
		}
		payload["max_tokens"] = raw
	}

	// The keys are written in the order the body had them, so the fork's
	// prefix is byte-identical to the original. Every key the payload
	// holds came from the body, except the two the fork may add; those
	// go last, in the order they are named.
	order, err := support.JSONKeyOrder(body)
	if err != nil {
		return nil, err
	}
	order = support.WithKeys(order, payload, []string{"messages", "max_tokens"})
	return support.EncodeObject(payload, order), nil
}

// shapeAppended applies the request-shaping quirks to the messages a fork
// appends.
//
// The snapshot's own body was shaped when it was built, and the fork re-sends
// it as it is. An appended message is new, so the model's quirks shape it the
// way they shape any other message: a model whose server needs a part in
// another shape would otherwise get that shape in a prompt request and the
// spec's shape in a fork.
func shapeAppended(quirks []quirkProfile, messages []chatMessage) []chatMessage {
	if len(quirks) == 0 || len(messages) == 0 {
		return messages
	}
	shape := &requestShape{
		// The parameters belong to the snapshot, so the profiles may write
		// them and the fork drops what they write.
		request: &chatRequest{Messages: messages},
		params:  map[string]json.RawMessage{},
	}
	shapeThrough(quirks, shape)
	return shape.request.Messages
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
func convertMessage(m poo.Message) (chatMessage, []chatContentPart, error) {
	cm := chatMessage{Role: string(m.Role)}

	var text strings.Builder
	var calls []chatToolCall
	var attachments []poo.ContentBlock
	toolResult := false
	// The provider blocks are checked in a pass of their own, so a block
	// after a tool result is checked too.
	for _, c := range m.Content {
		if c.Provider != nil {
			if err := support.CarryError(poo.APIChatCompletions, false, c.Provider); err != nil {
				return chatMessage{}, nil, err
			}
		}
	}
	for _, c := range m.Content {
		switch c.Kind {
		case poo.ContentProvider:
			// The block's payload is opaque and has no wire field here.
			// CarryError has already refused a required block, so
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
		case poo.ContentImage, poo.ContentAudio:
			attachments = append(attachments, c)
		case poo.ContentToolResult:
			// The loop keeps going: an attachment the tool returned follows
			// its result block, in the same message.
			toolResult = true
			cm.Role = "tool"
			cm.ToolCallID = c.ToolCallID
			cm.Content = c.Text
		}
	}
	if toolResult {
		parts, err := attachmentParts(attachments)
		return cm, parts, err
	}

	if text.Len() > 0 {
		cm.Content = text.String()
	}
	cm.ToolCalls = calls
	parts, err := attachmentParts(attachments)
	return cm, parts, err
}

// convertMessages turns core messages into the wire shape, one or two at a
// time.
//
// A tool result that carries attachments becomes the tool message plus a user
// message carrying them: the Chat Completions API takes an image only in a
// user message's parts, unlike the Responses and Messages APIs, which take one
// inside the tool result itself.
func convertMessages(msgs []poo.Message) ([]chatMessage, error) {
	out := make([]chatMessage, 0, len(msgs))
	var pending []chatContentPart
	flush := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, chatMessage{Role: "user", Content: pending})
		pending = nil
	}
	for _, m := range msgs {
		cm, attachments, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		// A batch of tool results answers one assistant message, so the
		// pictures ride in one user message after the last of them: a
		// server that enforces alternation reads one turn, not one per
		// result.
		if cm.Role != "tool" {
			flush()
		}
		out = append(out, cm)
		pending = append(pending, attachments...)
	}
	flush()
	return out, nil
}

// attachmentParts renders the blocks a tool returned as the parts a user
// message carries, one part per modality.
//
// A block the transport cannot render fails the request: an attachment the
// core accepts and this transport leaves out is a picture or a recording the
// model never hears about, and a silent drop is worse than a loud one.
func attachmentParts(attachments []poo.ContentBlock) ([]chatContentPart, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	parts := make([]chatContentPart, 0, len(attachments))
	for _, attachment := range attachments {
		switch attachment.Kind {
		case poo.ContentImage:
			parts = append(parts, chatContentPart{
				Type:     "image_url",
				ImageURL: &chatImageURL{URL: support.AttachmentDataURL(attachment)},
			})
		case poo.ContentAudio:
			format := support.AudioFormat(attachment.MediaType)
			if format == "" {
				return nil, fmt.Errorf("openai: the transport has no format name for %q", attachment.MediaType)
			}
			parts = append(parts, chatContentPart{
				Type: "input_audio",
				InputAudio: &chatInputAudio{
					Data:   base64.StdEncoding.EncodeToString(attachment.Data),
					Format: format,
				},
			})
		}
	}
	return parts, nil
}
