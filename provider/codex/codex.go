// Package codex implements the ChatGPT Codex Responses transport.
//
// The Codex endpoint speaks the OpenAI Responses API with a base URL, an
// authentication scheme and a handful of body fields of its own. Requests go
// to <endpoint>/codex/responses, and the bearer token is the ChatGPT
// credential. The account rides beside the token in a header. The body asks
// for the reasoning summaries the endpoint returns.
//
// The transport is stateless like every other. The request carries the whole
// history, `store` is always false, and the reasoning the endpoint returned
// travels back as an opaque provider block.
//
// The reader a request returns implements two reporting seams besides the
// stream itself. ReportUsage reports the endpoint's own counts.
// ReportProviderBlocks holds the reasoning items the endpoint returned, with
// the payload that lets it take them back. The core records those with the
// response's message, and the next request gives them back. That is what
// carries the reasoning across turns of a storeless endpoint. An item the
// endpoint cut short before it carried that payload is not a block. Its
// summary already travelled as the thinking text, and replaying it would name
// an item the endpoint cannot match.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sdougbrown/poo"
	"github.com/sdougbrown/poo/provider/internal/support"
)

// DefaultEndpoint is the endpoint the Codex requests go to when the
// configuration names none.
const DefaultEndpoint = "https://chatgpt.com/backend-api"

// DefaultOriginator is the originator header the Codex endpoint expects
// when the configuration names none.
const DefaultOriginator = "poo"

// Credential is what a Codex request authenticates with.
type Credential struct {
	// Token is the bearer token.
	Token string
	// AccountID is the ChatGPT account the token belongs to. It is the
	// chatgpt-account-id header, and a credential without one fails the
	// request before it goes out.
	//
	// The resolver names it, because the resolver is what read the token:
	// a credential store that keeps the account beside the token has
	// nothing to re-derive here.
	AccountID string
}

// Options configures the transport.
type Options struct {
	// Endpoint is the endpoint's base, e.g.
	// "https://chatgpt.com/backend-api". Empty means DefaultEndpoint.
	Endpoint string
	// Model is the model identifier. A request that names its own wins.
	Model string
	// Credentials resolves the credential a request goes out with.
	//
	// It is called once per attempt, so an expired token is replaced
	// between attempts rather than failing the run. The transport never
	// reads a credential file or an environment variable itself. The
	// consumer that links this package resolves the token and the account,
	// and a test resolves a fixed one.
	//
	// A failure the resolver reports is classified by what it is. A
	// *poo.ProviderError keeps the kind it names, and a retryable kind is
	// retried: the resolver is asked again for the next attempt. That is
	// how a consumer says its token store failed transiently. A resolution
	// the run's own context ended is the run's failure. The transport's
	// Timeout can end the resolution while the run is alive. That
	// resolution did not finish in time, so it is transient too. Any other
	// failure is an authentication failure: the request could not name its
	// account.
	//
	// A nil resolver fails every request with a configuration error.
	Credentials func(ctx context.Context) (Credential, error)
	// ModelInfo is the model's info. It names the reasoning control, the
	// tools mode and the attachments the model reads.
	//
	// Its sampling defaults are not sent: the endpoint refuses the
	// temperature and the top-p a Responses request may carry, so the
	// body carries neither.
	ModelInfo poo.ModelInfo
	// ToolsMode is how the run asks for tool calls: "native", "auto" or
	// "none". A model info that names a mode wins over this one.
	//
	// The endpoint makes native calls only, so "text" is a
	// configuration error.
	ToolsMode string
	// Reasoning is the reasoning setting: "off", a named level or a
	// number of thinking tokens. A request that carries its own wins.
	Reasoning string
	// ReasoningSummary is the summary the endpoint returns with the
	// reasoning: "auto", "concise" or "detailed". Empty means "auto"
	// when the request reasons, and nothing when it does not.
	ReasoningSummary string
	// TextVerbosity is the answer's verbosity: "low", "medium" or
	// "high". Empty means "low".
	TextVerbosity string
	// SessionID is the session a request that names none belongs to.
	//
	// A request's own SessionID wins, because one transport may serve
	// several sessions and each keys the endpoint's prompt cache by its
	// own id. It rides in the session and client-request headers and in
	// the body's prompt-cache key. Empty sends none of them.
	SessionID string
	// Originator is the originator header. Empty means
	// DefaultOriginator.
	Originator string
	// Headers are extra headers, applied last, so a caller can override
	// one the transport set.
	Headers map[string]string
	// HTTPClient is the client to use. Nil means a default client.
	//
	// The transport uses a copy of it, and refuses redirects on that
	// copy whatever the client's own policy. The request carries the
	// conversation and the account, so it goes only to the endpoint the
	// transport was configured with. A change the caller makes to the
	// client after New does not reach the requests.
	HTTPClient *http.Client
	// Timeout bounds a whole request, streaming included. Zero means no
	// extra bound beyond the run's context.
	Timeout time.Duration
	// MaxAttempts is the number of attempts one request gets. Zero
	// means the plan's three.
	MaxAttempts int
	// Backoff is the wait before the second and the third attempt.
	// Empty means the plan's one second and then two.
	Backoff []time.Duration
}

// Transport is the ChatGPT Codex Responses transport.
type Transport struct {
	cfg    Options
	http   *http.Client
	policy support.Policy
}

// New returns a transport.
func New(cfg Options) *Transport {
	// The client is the consumer's, so it is copied rather than changed
	// under the consumer's feet. A redirect is refused for every request
	// this transport makes.
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}}
	} else {
		copied := *client
		client = &copied
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return &redirectError{Location: req.URL.String()}
	}
	policy := support.DefaultPolicy().With(cfg.MaxAttempts, cfg.Backoff)
	return &Transport{cfg: cfg, http: client, policy: policy}
}

// Stream starts a request and returns the response stream along with the
// snapshot of the request that was sent.
//
// The snapshot carries the attachments the body was shaped for and the
// session it was sent in. The model info that shaped the request is not part
// of the body, so the snapshot is what says how a fork shapes what it
// appends and which cache key it names.
func (t *Transport) Stream(ctx context.Context, req poo.ChatRequest) (poo.StreamReader, poo.Rendered, error) {
	body, err := t.buildBody(req)
	if err != nil {
		return nil, poo.Rendered{}, err
	}
	session := t.sessionID(req.SessionID)
	rendered := poo.Rendered{
		API:       poo.APIResponses,
		Body:      body,
		Messages:  len(req.Messages),
		Inputs:    modalitiesOf(t.modelInfo(req)).inputs(),
		SessionID: session,
	}

	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, session)
	}
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, req.OnAttempt)
	if err != nil {
		return nil, rendered, err
	}
	return support.NewRetrying(ctx, t.policy, open, req.OnAttempt, reader, attempt), rendered, nil
}

// Fork sends a body that is byte-identical to the snapshot, apart from
// the input items appended after the last one.
//
// The appended messages are shaped by the attachments the snapshot carries.
// The model info a request was shaped for is not part of the body. The
// snapshot's inputs are therefore what say which attachments the model
// reads, and its session is the cache key the fork names. The output limit
// is the caller's. The endpoint takes none, so the fork does not rewrite
// one.
func (t *Transport) Fork(ctx context.Context, prev poo.Rendered, appended []poo.Message, maxTokens int) (poo.StreamReader, error) {
	if prev.API != poo.APIResponses {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: fmt.Sprintf("the request snapshot belongs to the %q API, not the Responses API", prev.API),
		}
	}
	if len(prev.Body) == 0 {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot holds no body to fork",
		}
	}
	body, err := forkBody(prev.Body, appended, modalitiesOfInputs(prev.Inputs))
	if err != nil {
		return nil, err
	}
	open := func(ctx context.Context) (support.Reader, error) {
		return t.open(ctx, body, prev.SessionID)
	}
	reader, attempt, err := support.OpenWithRetry(ctx, t.policy, open, nil)
	if err != nil {
		return nil, err
	}
	return support.NewRetrying(ctx, t.policy, open, nil, reader, attempt), nil
}

// redirectError is the refusal a redirect turns into.
//
// The request carries the conversation and the account, so it goes only
// to the endpoint the transport was configured with. A client's own
// redirect policy does not change that: the transport refuses every
// redirect on the client it uses.
type redirectError struct {
	// Location is where the endpoint pointed the request.
	Location string
}

func (e *redirectError) Error() string {
	return fmt.Sprintf("the endpoint redirected the request to %s; a request carries the conversation and the account, "+
		"so it goes only to the configured endpoint", e.Location)
}

// open resolves the credential, sends a serialized body and returns the
// response stream.
//
// Stream and Fork both go through it, so the credential, the timeout,
// the headers and the error classification mean the same thing for both.
//
// The credential is resolved under the request's own bound, so a token
// store that never answers cannot hold an attempt. The bound ending while
// the run is alive is a resolution that did not finish in time, not the
// run stopping. That is why the parent is kept apart from it.
func (t *Transport) open(ctx context.Context, body []byte, session string) (*streamReader, error) {
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

	credential, err := t.credential(parent, ctx)
	if err != nil {
		release()
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ResponsesURL(t.cfg.Endpoint), bytes.NewReader(body))
	if err != nil {
		release()
		return nil, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	t.setHeaders(httpReq, credential, session)

	// The request is timed from the moment it goes out, so the time to
	// the first token includes the endpoint's own latency.
	started := time.Now()

	resp, err := t.http.Do(httpReq)
	if err != nil {
		// The failure is classified before the request's context is
		// released, because releasing it would replace the failure with
		// the context's own error.
		var redirect *redirectError
		if errors.As(err, &redirect) {
			// The client hands back the response it refused to follow,
			// and its body is the transport's to close.
			failure := &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: redirect.Error(),
			}
			if resp != nil {
				failure.Status = resp.StatusCode
				_ = resp.Body.Close()
			}
			release()
			return nil, failure
		}
		perr := support.ClassifyTransportError(parent, ctx, err)
		release()
		return nil, perr
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		// The body is read under the request's timeout, so the context is
		// released after it.
		defer release()
		return nil, support.ClassifyStatus(resp)
	}

	// A failed request answered with 200 and a JSON body is not a stream.
	// The first byte tells the two apart. The stream is therefore not
	// buffered, and the time to the first token measures the endpoint's
	// latency rather than the reader's.
	br := bufio.NewReader(resp.Body)
	first, err := support.PeekFirstByte(br)
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
		if perr, ok := support.ErrorInBody(errBody, resp.StatusCode, resp.Header); ok {
			return nil, perr
		}
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint returned a body that is not a stream",
			Status:  resp.StatusCode,
		}
	}

	return &streamReader{
		body:    resp.Body,
		scanner: support.SSEScanner(br),
		ctx:     ctx,
		parent:  parent,
		cancel:  cancel,
		ttft:    support.NewTTFT(started),
	}, nil
}

// sessionID returns the session a request names, clamped to the length the
// endpoint takes.
//
// The request's own session wins: the transport may serve several sessions,
// and each of them keys the endpoint's prompt cache by its own id. A request
// that names none falls back to the transport's configuration.
func (t *Transport) sessionID(request string) string {
	if request != "" {
		return clampSessionID(request)
	}
	return clampSessionID(t.cfg.SessionID)
}

// setHeaders sets the headers a Codex request carries.
//
// The transport's own headers are set first and the caller's last, so a
// caller can override one. The credential headers are the transport's, and
// are set after the caller's.
func (t *Transport) setHeaders(req *http.Request, credential Credential, session string) {
	originator := t.cfg.Originator
	if originator == "" {
		originator = DefaultOriginator
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", originator)
	if session != "" {
		req.Header.Set("session-id", session)
		req.Header.Set("x-client-request-id", session)
	}
	for name, value := range t.cfg.Headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	req.Header.Set("chatgpt-account-id", credential.AccountID)
}

// credential resolves the credential a request goes out with.
//
// A resolver that returns no token names no account, so the request never
// goes out: the credential is what says which account it spends. The two
// contexts are the request's parent and the request's own bound, which a
// failure the resolver reports is read against.
func (t *Transport) credential(parent, ctx context.Context) (Credential, error) {
	if t.cfg.Credentials == nil {
		return Credential{}, support.ConfigErrorf("the Codex transport has no credential resolver")
	}
	credential, err := t.cfg.Credentials(ctx)
	if err != nil {
		return Credential{}, credentialError(parent, ctx, err)
	}
	if credential.Token == "" {
		return Credential{}, &poo.ProviderError{
			Kind:    poo.ProviderKindAuthFailed,
			Message: "the credential resolver returned no token",
		}
	}
	if credential.AccountID == "" {
		return Credential{}, &poo.ProviderError{
			Kind:    poo.ProviderKindAuthFailed,
			Message: "the credential resolver named no account, and a Codex request spends the one its token belongs to",
		}
	}
	return credential, nil
}

// credentialError classifies the failure a resolver reported.
//
// The resolver is the one that knows what its failure means, so a
// *poo.ProviderError it returns keeps its kind. The request is retried when
// that kind is retryable, whatever ended at the same time. A resolver that
// reports a terminal failure has traded what cannot be traded twice. What is
// left is read by what it is. A run that is stopping reports its own
// cancellation. The transport's own bound ending while the run is alive is a
// resolution that did not finish in time, which another attempt resolves. A
// resolver whose own wait ended failed transiently. Any other failure means
// the request could not name its account, which is an authentication
// failure.
func credentialError(parent, request context.Context, err error) error {
	var perr *poo.ProviderError
	if errors.As(err, &perr) {
		return perr
	}
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if request != nil && errors.Is(request.Err(), context.DeadlineExceeded) {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: request.Err().Error()}
	}
	if request != nil && request.Err() != nil {
		return request.Err()
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	return &poo.ProviderError{Kind: poo.ProviderKindAuthFailed, Message: err.Error()}
}

// ResponsesURL returns the Codex responses URL of an endpoint.
//
// A base URL that already ends in /codex/responses is used as it is, and
// one that ends in /codex gets /responses appended.
func ResponsesURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultEndpoint
	}
	switch {
	case strings.HasSuffix(base, "/codex/responses"):
		return base
	case strings.HasSuffix(base, "/codex"):
		return base + "/responses"
	default:
		return base + "/codex/responses"
	}
}

// forkBody appends messages to a serialized request body.
//
// The body is decoded, and the converted messages are appended after the
// last input item. The result is serialized again in the order the body
// already had its keys. Every other byte is the body's own, so the fork's
// prefix is the one the endpoint has already cached.
func forkBody(body []byte, appended []poo.Message, reads modalities) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &poo.ProviderError{
			Kind:    poo.ProviderKindInvalidRequest,
			Message: "the request snapshot does not hold a JSON body",
		}
	}

	if len(appended) > 0 {
		var items []json.RawMessage
		if raw, ok := payload["input"]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the request snapshot's input does not decode",
				}
			}
		}
		converted, instructions, err := inputItems(appended, reads, len(items))
		if err != nil {
			return nil, err
		}
		// A system message the fork appends cannot become the request's
		// instructions: those are part of the snapshot's prefix. It rides
		// as the instruction item a later system message rides as.
		if instructions != "" {
			raw, err := json.Marshal(inputMessage{
				Role:    "developer",
				Content: []inputPart{{Type: "input_text", Text: instructions}},
			})
			if err != nil {
				return nil, &poo.ProviderError{
					Kind:    poo.ProviderKindInvalidRequest,
					Message: "the appended instruction does not encode",
				}
			}
			items = append(items, raw)
		}
		items = append(items, converted...)
		raw, err := json.Marshal(items)
		if err != nil {
			return nil, &poo.ProviderError{
				Kind:    poo.ProviderKindInvalidRequest,
				Message: "the input items do not encode",
			}
		}
		payload["input"] = raw
	}

	order, err := jsonKeyOrder(body)
	if err != nil {
		return nil, err
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
// The order matters for a fork. The forked body has to be byte-identical to
// the body it came from, apart from the fields that changed. A JSON object
// decoded into a map loses its order.
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
				Message: "the request snapshot's keys are not strings",
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

// maxSessionIDLength is the longest session identifier the endpoint takes.
const maxSessionIDLength = 64

// clampSessionID cuts a session identifier to the length the endpoint
// takes, counted in characters rather than bytes.
//
// The identifier names the endpoint's prompt cache, so a request that
// carries one longer than the endpoint accepts is refused.
func clampSessionID(id string) string {
	chars := []rune(id)
	if len(chars) <= maxSessionIDLength {
		return id
	}
	return string(chars[:maxSessionIDLength])
}
