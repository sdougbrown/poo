// Package openai implements the OpenAI-compatible Chat Completions transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape every
// OpenAI-compatible endpoint (llama.cpp, vLLM, sglang, Ollama, LiteLLM)
// accepts. It has no request cache: a failed request is retried within
// the plan's budget, and a request that exhausts it ends the run.
//
// What the Chat Completions API says is not here: the request's shape, the
// messages, the answer's events and the repairs a model's text may need are
// the provider/internal/chat package's. What is here is what this transport
// needs: the endpoint, the credential, the headers, the retry policy, and the
// request policy the body follows.
package openai

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
	"github.com/sdougbrown/poo/provider/internal/chat"
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
	// SessionHeader is the name of a header that carries the request's
	// session id, for an endpoint whose gateway routes one conversation's
	// requests together. Empty sends none.
	SessionHeader string
	// UserAgent is what the client calls itself on the request. A gateway
	// that watches its traffic asks each client to name itself, and a
	// client that says nothing is named by the HTTP library alone. Empty
	// sends none.
	UserAgent string
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
		API:       poo.APIChatCompletions,
		Body:      body,
		Messages:  len(req.Messages),
		Quirks:    settings.info.Quirks,
		SessionID: settings.sessionID,
		MaxTokens: t.maxTokens(req, settings.info),
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
// the appended messages, the output limit and the tool choice, which
// the fork sets to none.
//
// The snapshot holds the serialized body, so the fork re-sends it with
// the appended messages added after the last one. The key order and
// every other field are unchanged, because the body was serialized once
// and only the two changed fields are rewritten.
//
// A fork is retried within the plan's budget, like any other request.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int, opts poo.ForkOptions) (poo.StreamReader, error) {
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
	// The session is the snapshot's, because the fork belongs to the request
	// it forks: a session header is what tells the gateway the two are one
	// conversation.
	settings := t.requestSettings(poo.ChatRequest{SessionID: prev.SessionID})
	converted, err := chat.Messages(appended)
	if err != nil {
		return nil, err
	}
	body, err := chat.AppendMessages(prev.Body, shapeAppended(quirksFor(prev.Quirks, t.logger), converted), maxTokens)
	if err != nil {
		return nil, err
	}
	body, err = chat.WithoutTools(body)
	if err != nil {
		return nil, err
	}
	if opts.JSON {
		body, err = chat.JSONResponse(body)
		if err != nil {
			return nil, err
		}
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
func (t *Transport) open(ctx context.Context, body []byte, settings requestSettings) (*chat.Reader, error) {
	return support.OpenStream(ctx, support.OpenOptions[*chat.Reader]{
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
			if t.cfg.SessionHeader != "" && settings.sessionID != "" {
				req.Header.Set(t.cfg.SessionHeader, settings.sessionID)
			}
			if t.cfg.UserAgent != "" {
				req.Header.Set("User-Agent", t.cfg.UserAgent)
			}
			return req, nil
		},
		Build: func(stream support.Stream) *chat.Reader {
			return chat.NewReader(stream, chat.ReaderOptions{
				Filters:     t.textFilters(settings),
				DeferParsed: t.sendsTools(settings),
			})
		},
	})
}

// shapeAppended applies the request-shaping quirks to the messages a fork
// appends.
//
// The snapshot's own body was shaped when it was built, and the fork re-sends
// it as it is. An appended message is new, so the model's quirks shape it the
// way they shape any other message: a model whose server needs a part in
// another shape would otherwise get that shape in a prompt request and the
// spec's shape in a fork.
func shapeAppended(quirks []quirkProfile, messages []chat.Message) []chat.Message {
	if len(quirks) == 0 || len(messages) == 0 {
		return messages
	}
	shape := &requestShape{
		// The parameters belong to the snapshot, so the profiles may write
		// them and the fork drops what they write.
		request: &chat.Request{Messages: messages},
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
