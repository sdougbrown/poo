// Package anthropic implements the Anthropic Messages API transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape the Messages
// API accepts. It has no request cache: a failed request is retried within
// the plan's budget, and a request that exhausts it ends the run.
//
// What the Messages API says is not here: the request's shape, the messages,
// the answer's events and the signatures of the thinking blocks are the
// provider/internal/anthropic package's. What is here is what this transport
// needs: the endpoint, the credential, the headers, the retry policy, and
// the request policy the body follows.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/anthropic"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// APIVersion is the Messages API version the transport speaks.
const APIVersion = "2023-06-01"

// Attempt is one attempt of a request that failed.
//
// It is the openai transport's own type, aliased here so a consumer reads
// one type for the same thing.
type Attempt = poo.Attempt

// Options configures the transport.
type Options struct {
	// Endpoint is the endpoint's base, e.g.
	// "https://opencode.ai/inference/go/anthropic/v1".
	Endpoint string
	// Model is the model identifier.
	Model string
	// APIKey is the bearer token, empty when the endpoint needs none.
	APIKey string
	// ModelInfo is the model's info. It names the reasoning control, the
	// temperature control and the modalities.
	ModelInfo poo.ModelInfo
	// Reasoning is the reasoning setting: a named level, or a number of
	// thinking tokens. A model info that names a reasoning control wins
	// over this one.
	Reasoning string
	// Sampling holds the sampling overrides. They win over the model
	// info's own defaults, field by field.
	Sampling poo.Sampling
	// Logger receives the transport's notes. A nil logger drops them.
	Logger *slog.Logger
	// OnAttempt is called for every attempt that failed, so a consumer
	// can write the attempt to its log.
	OnAttempt func(Attempt)
	// MaxAttempts is the number of attempts one request gets. Zero means
	// the plan's three.
	MaxAttempts int
	// Backoff is the wait before the second and the third attempt. Empty
	// means the plan's one second and then two.
	Backoff []time.Duration
	// HTTPClient is the client to use. Nil means a default client.
	HTTPClient *http.Client
	// Timeout bounds a whole request, streaming included. Zero means no
	// extra bound beyond the run's context.
	Timeout time.Duration
	// SessionHeader is the name of a header that carries the request's
	// session id, for an endpoint whose gateway routes one conversation's
	// requests together. Empty sends none.
	SessionHeader string
}

// Transport is the Anthropic Messages chat transport.
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

// Stream starts a Messages request and returns the response stream.
//
// The snapshot is the serialized request body, exactly as it went out. A
// fork is built from it, so the fork sends the same prefix.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	body, err := t.buildBody(req)
	if err != nil {
		return nil, poo.Rendered{}, err
	}
	rendered := poo.Rendered{
		API:       poo.APIAnthropicMessages,
		Body:      body,
		Messages:  len(req.Messages),
		SessionID: req.SessionID,
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, req.SessionID)
	}
	report := support.Reporter(t.cfg.OnAttempt, req.OnAttempt)
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, report)
	if err != nil {
		return nil, rendered, err
	}
	return support.NewRetrying(ctx, t.policy, open, report, reader, attempt), rendered, nil
}

// Fork sends a body that is byte-identical to the snapshot, apart from the
// appended messages and the output limit.
//
// The snapshot holds the serialized body, so the fork re-sends it with the
// appended messages added after the last one. The session is the snapshot's,
// because the fork belongs to the request it forks: a session header is what
// tells the gateway the two are one conversation.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	if prev.API != poo.APIAnthropicMessages {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: fmt.Sprintf("the request snapshot belongs to the %q API, not the Messages API", prev.API),
		}
	}
	if len(prev.Body) == 0 {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot holds no body to fork",
		}
	}
	messages, _, err := anthropic.Messages(appended)
	if err != nil {
		return nil, err
	}
	body, err := anthropic.AppendMessages(prev.Body, messages, maxTokens)
	if err != nil {
		return nil, err
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, prev.SessionID)
	}
	report := support.Reporter(t.cfg.OnAttempt, nil)
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, report)
	if err != nil {
		return nil, err
	}
	return support.NewRetrying(ctx, t.policy, open, report, reader, attempt), nil
}

// buildBody turns a request into the wire body it is sent as.
func (t *Transport) buildBody(req poo.ChatRequest) ([]byte, error) {
	info := req.ModelInfo
	if support.ZeroModelInfo(info) {
		info = t.cfg.ModelInfo
	}
	messages, system, err := anthropic.Messages(req.Messages)
	if err != nil {
		return nil, err
	}
	request := anthropic.Request{
		Model:     model(req, t.cfg.Model),
		Messages:  messages,
		System:    system,
		MaxTokens: maxTokens(req, info),
		Stream:    true,
	}
	// A model that takes its calls as text has them described in its
	// prompt by the core; the wire carries the calls of a model that takes
	// them natively.
	if len(req.Tools) > 0 && takesNativeTools(info) {
		request.Tools = anthropic.Tools(support.SortedTools(req.Tools))
	}
	params, err := t.params(req, info)
	if err != nil {
		return nil, err
	}
	request.Params = params
	return anthropic.Encode(request)
}

// takesNativeTools reports whether the model's own tools mode carries calls
// on the wire. A model that names no mode takes them natively, which is what
// the Messages API serves.
func takesNativeTools(info poo.ModelInfo) bool {
	switch info.Tools.Mode {
	case poo.ToolsNative, poo.ToolsAuto, "":
		return true
	}
	return false
}

// maxTokens returns the output limit a request is sent with.
//
// The API takes no request without one. A caller that computed none gets the
// core's own rule — the model's limit or the default, clamped to the window
// its context leaves — floored at one token, the way the Chat Completions
// transport answers the same question.
func maxTokens(req poo.ChatRequest, info poo.ModelInfo) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	limit := info.MaxTokens(0)
	if limit < 1 {
		limit = 1
	}
	return limit
}

// params returns the request's named parameters: the reasoning the request
// asks the model to run at, and the sampling the model takes.
func (t *Transport) params(req poo.ChatRequest, info poo.ModelInfo) (map[string]json.RawMessage, error) {
	params := map[string]json.RawMessage{}
	fields, err := info.ReasoningFields(t.reasoningSetting(req))
	if err != nil {
		return nil, support.ConfigErrorf("%s", support.ReasonError(err))
	}
	for _, field := range fields {
		support.SetNested(params, field.Path, field.Value)
	}
	// The thinking field names the kind of thinking a request asks for,
	// and the API takes no value without it. The companion is decided from
	// the paths the reasoning fields named — the paths the core owns and a
	// model may override — so a budget in the thinking object comes with
	// thinking enabled, and an effort in the output config comes with
	// adaptive thinking.
	adaptive, enabled := false, false
	for _, field := range fields {
		switch {
		case strings.HasPrefix(field.Path, "output_config."):
			adaptive = true
		case strings.HasPrefix(field.Path, "thinking."):
			enabled = true
		}
	}
	if adaptive {
		support.SetNested(params, "thinking.type", "adaptive")
	} else if enabled {
		support.SetNested(params, "thinking.type", "enabled")
	}
	// The sampling the model takes is the core's own rule: the effective
	// sampling clears the temperature for a model that takes none, and
	// leaves the rest of the fields alone.
	sampling := info.EffectiveSampling(t.cfg.Sampling)
	if v := sampling.Temperature; v != nil {
		support.SetNested(params, "temperature", *v)
	}
	if v := sampling.TopP; v != nil {
		support.SetNested(params, "top_p", *v)
	}
	if v := sampling.TopK; v != nil {
		support.SetNested(params, "top_k", *v)
	}
	if len(params) == 0 {
		return nil, nil
	}
	return params, nil
}

// reasoningSetting returns the reasoning setting a request uses.
//
// The request's own level wins: it is the session's, and a session that
// changed it between the runs means it. A request that carries none falls
// back to the transport's configuration.
func (t *Transport) reasoningSetting(req poo.ChatRequest) string {
	if req.Reasoning != "" {
		return req.Reasoning
	}
	return t.cfg.Reasoning
}

// model returns the model identifier a request names.
func model(req poo.ChatRequest, fallback string) string {
	if req.Model != "" {
		return req.Model
	}
	return fallback
}

// open sends a serialized body and returns the response stream.
//
// Stream and Fork both go through it, so the timeout, the headers, the
// error classification and the time to the first token mean the same thing
// for both.
func (t *Transport) open(ctx context.Context, body []byte, session string) (*anthropic.Reader, error) {
	return support.OpenStream(ctx, support.OpenOptions[*anthropic.Reader]{
		Client:  t.http,
		Timeout: t.cfg.Timeout,
		Request: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, MessagesURL(t.cfg.Endpoint), bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set("anthropic-version", APIVersion)
			if t.cfg.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+t.cfg.APIKey)
			}
			if t.cfg.SessionHeader != "" && session != "" {
				req.Header.Set(t.cfg.SessionHeader, session)
			}
			return req, nil
		},
		Build: func(stream support.Stream) *anthropic.Reader {
			return anthropic.NewReader(stream)
		},
	})
}

// MessagesURL returns the messages URL of an endpoint.
//
// A base URL that already ends in /messages is used as it is.
func MessagesURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/messages") {
		return base
	}
	return base + "/messages"
}
