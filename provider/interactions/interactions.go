// Package interactions implements the Google Interactions API transport.
//
// The transport speaks SSE over a plain HTTP POST, in the shape the
// Interactions API accepts: a stateless request that carries the whole history
// as steps, and an answer made of step events. It has no request cache: a
// failed request is retried within the plan's budget, and a request that
// exhausts it ends the run.
//
// What the Interactions API says is not here: the request's shape, the steps,
// the answer's events and the signature a thought carries are the
// provider/internal/interactions package's. What is here is what this transport
// needs: the endpoint, the credential, the headers, the retry policy, and the
// request policy the body follows.
package interactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/interactions"
	"github.com/sdougbrown/poo/provider/internal/support"
	"github.com/sdougbrown/poo/provider/internal/transport"
)

// Attempt is one attempt of a request that failed.
//
// It is the openai transport's own type, aliased here so a consumer reads one
// type for the same thing.
type Attempt = poo.Attempt

// Options configures the transport.
type Options struct {
	// Endpoint is the endpoint's base, e.g.
	// "https://generativelanguage.googleapis.com/v1beta".
	Endpoint string
	// APIKey is the key the endpoint reads, empty when it needs none.
	APIKey string
	// Model is the model identifier.
	Model string
	// ModelInfo is the model's info. It names the reasoning control, the
	// temperature control and the quirks.
	ModelInfo poo.ModelInfo
	// Reasoning is the reasoning setting: a named level. A model info that
	// names a reasoning control wins over this one.
	Reasoning string
	// OnAttempt is called for every attempt that failed, so a consumer can
	// write the attempt to its log.
	OnAttempt func(Attempt)
	// MaxAttempts is the number of attempts one request gets. Zero means the
	// plan's three.
	MaxAttempts int
	// Backoff is the wait before the second and the third attempt. Empty
	// means the plan's one second and then two.
	Backoff []time.Duration
	// HTTPClient is the client to use. Nil means a default client.
	HTTPClient *http.Client
	// Timeout bounds a whole request, streaming included. Zero means no extra
	// bound beyond the run's context.
	Timeout time.Duration
	// UserAgent is what the client calls itself on the request. A gateway that
	// watches its traffic asks each client to name itself, and a client that
	// says nothing is named by the HTTP library alone. Empty sends none.
	UserAgent string
}

// Transport is the Interactions chat transport.
type Transport struct {
	cfg  Options
	http *http.Client
	// policy is the retry policy.
	policy support.Policy
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
	return &Transport{cfg: cfg, http: client, policy: policy}
}

// Stream starts an interaction and returns the response stream.
//
// The snapshot is the serialized request body, exactly as it went out. A fork
// is built from it, so the fork sends the same prefix.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	body, err := t.buildBody(req)
	if err != nil {
		return nil, poo.Rendered{}, err
	}
	rendered := poo.Rendered{
		API:       poo.APIGoogleInteractions,
		Body:      body,
		Messages:  len(req.Messages),
		MaxTokens: maxTokens(req, t.modelInfo(req)),
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body)
	}
	report := support.Reporter(t.cfg.OnAttempt, req.OnAttempt)
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, report)
	if err != nil {
		return nil, rendered, err
	}
	return support.NewRetrying(ctx, t.policy, open, report, reader, attempt), rendered, nil
}

// Fork sends a body that is byte-identical to the snapshot, apart from the
// appended steps, the output limit and the tool choice, which the fork sets to
// none.
//
// The snapshot holds the serialized body, so the fork re-sends it with the
// converted messages added after the last step. The key order and every other
// field are unchanged, because the body was serialized once and only the
// changed fields are rewritten.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int, opts poo.ForkOptions) (poo.StreamReader, error) {
	if prev.API != poo.APIGoogleInteractions {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: fmt.Sprintf("the request snapshot belongs to the %q API, not the Interactions API", prev.API),
		}
	}
	if len(prev.Body) == 0 {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot holds no body to fork",
		}
	}
	body, err := interactions.AppendSteps(prev.Body, appended, maxTokens)
	if err != nil {
		return nil, err
	}
	body, err = interactions.WithoutTools(body)
	if err != nil {
		return nil, err
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body)
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
	info := t.modelInfo(req)
	steps, instruction, err := interactions.Messages(req.Messages)
	if err != nil {
		return nil, err
	}
	request := interactions.Request{
		Model:             model(req, t.cfg.Model),
		Input:             steps,
		SystemInstruction: instruction,
		// The request is stateless: the endpoint keeps nothing of it, and the
		// whole history travels as steps.
		Store:  false,
		Stream: true,
	}
	// A model that takes its calls as text has them described in its prompt by
	// the core; the wire carries the calls of a model that takes them natively.
	if len(req.Tools) > 0 && takesNativeTools(info) {
		request.Tools = interactions.Tools(transport.SortedTools(req.Tools))
	}
	params, err := t.params(req, info)
	if err != nil {
		return nil, err
	}
	request.Params = params
	return interactions.Encode(request)
}

// modelInfo returns the model info a request is shaped by: the request's own,
// or the transport's when the request names none.
func (t *Transport) modelInfo(req poo.ChatRequest) poo.ModelInfo {
	if transport.ZeroModelInfo(req.ModelInfo) {
		return t.cfg.ModelInfo
	}
	return req.ModelInfo
}

// takesNativeTools reports whether the model's own tools mode carries calls on
// the wire. A model that names no mode takes them natively, which is what the
// Interactions API serves.
func takesNativeTools(info poo.ModelInfo) bool {
	switch info.Tools.Mode {
	case poo.ToolsNative, poo.ToolsAuto, "":
		return true
	}
	return false
}

// maxTokens returns the output limit a request is sent with.
//
// The session computes one for every request, so the limit is always carried
// and never left to the endpoint's default. A caller that computed none gets
// the core's own rule, floored at one token, the way the other transports
// answer the same question.
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

// params returns the request's named parameters: the generation config the
// request asks for.
//
// The API takes the output limit and the reasoning control in its generation
// config and nothing else of the request's: its config has no sampling field,
// so a temperature, a top_p or a top_k a model declares is not sent, and a
// request that stops on the output limit is the only thing truncation can mean.
func (t *Transport) params(req poo.ChatRequest, info poo.ModelInfo) (map[string]json.RawMessage, error) {
	params := map[string]json.RawMessage{}
	transport.SetNested(params, "generation_config.max_output_tokens", maxTokens(req, info))
	fields, err := info.ReasoningFields(t.reasoningSetting(req))
	if err != nil {
		return nil, support.ConfigErrorf("%s", support.ReasonError(err))
	}
	for _, field := range fields {
		transport.SetNested(params, field.Path, field.Value)
		// The summaries are what brings the model's thinking back as text: a
		// request that asks for a thinking level and no summaries is answered
		// with thought steps whose summary is empty.
		if field.Path == ThinkingLevelParam {
			transport.SetNested(params, "generation_config.thinking_summaries", "auto")
		}
	}
	if len(params) == 0 {
		return nil, nil
	}
	return params, nil
}

// reasoningSetting returns the reasoning setting a request uses.
//
// The request's own level wins: it is the session's, and a session that changed
// it between the runs means it. A request that carries none falls back to the
// transport's configuration.
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
// Stream and Fork both go through it, so the timeout, the headers, the error
// classification and the time to the first token mean the same thing for both.
func (t *Transport) open(ctx context.Context, body []byte) (*interactions.Reader, error) {
	return support.OpenStream(ctx, support.OpenOptions[*interactions.Reader]{
		Client:  t.http,
		Timeout: t.cfg.Timeout,
		Request: func(ctx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, InteractionsURL(t.cfg.Endpoint), bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			if t.cfg.APIKey != "" {
				req.Header.Set(APIKeyHeader, t.cfg.APIKey)
			}
			if t.cfg.UserAgent != "" {
				req.Header.Set("User-Agent", t.cfg.UserAgent)
			}
			return req, nil
		},
		Build: func(stream support.Stream) *interactions.Reader {
			return interactions.NewReader(stream)
		},
	})
}

// ThinkingLevelParam is the generation config field a model's thinking level
// rides in, which is the param a Gemini model declares for its reasoning
// control.
const ThinkingLevelParam = "generation_config.thinking_level"

// APIKeyHeader is the header the endpoint reads a Gemini API key from.
//
// The key travels in the API's own header rather than as a bearer token: the
// endpoint takes a key and an OAuth access token in two different places, and
// the key's header is the one that says which is which.
const APIKeyHeader = "x-goog-api-key"

// InteractionsURL returns the interaction URL of an endpoint.
//
// A base URL that already names an interaction path is used as it is: the
// sign-in's endpoint is the one the plan's quota method answers at, and it is
// spelled as the whole path rather than as a base poo appends to.
func InteractionsURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.Contains(base, "/interactions") {
		return base
	}
	return base + "/interactions"
}
