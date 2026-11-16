package support

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/sdougbrown/poo"
)

// The response opening.
//
// Every SSE transport does the same sequence around the primitives: bound the
// request with the transport's timeout and keep the caller's context apart from
// it, send the request, classify a failed send before the bound is released,
// read a 200 that answered with a JSON error instead of a stream, tell the
// stream from the error by its first byte, and hand the answer to the
// transport's own reader. The sequence is here, so a failure rule fixed once is
// fixed for every transport that speaks this way.

// defaultErrorBodyLimit bounds the JSON error body read from a 200 answer.
const defaultErrorBodyLimit = 1 << 20

// Stream is the answer a request opened.
type Stream struct {
	// Body is the answer's body. The reader closes it.
	Body io.ReadCloser
	// Reader is the body, buffered and positioned at its first payload.
	Reader *bufio.Reader
	// Context is the request's context, with the transport's timeout applied.
	Context context.Context
	// Parent is the caller's context, before that timeout was applied.
	Parent context.Context
	// Cancel releases the request's context. It is nil when the transport
	// set no timeout, and a reader that closes the body releases it too.
	Cancel context.CancelFunc
	// TTFT is the clock, started when the request went out.
	TTFT TTFT
}

// OpenOptions configures one request's opening.
type OpenOptions[T Reader] struct {
	// Client sends the request.
	Client *http.Client
	// Request builds the request under the context OpenStream bounds. A
	// transport sets its own URL, headers and body here.
	Request func(ctx context.Context) (*http.Request, error)
	// Timeout bounds the whole request, streaming included. Zero applies no
	// bound beyond the caller's context.
	Timeout time.Duration
	// Classify reads a failure the send reported. A nil Classify is
	// ClassifyTransportError with the caller's context and the request's.
	//
	// The response is the one the client handed back beside the failure,
	// which a client that refused to follow a redirect returns with the
	// refusal. It is closed after Classify returns.
	Classify func(parent, request context.Context, resp *http.Response, err error) error
	// Limit bounds the JSON error body a 200 answer is read as. Zero means
	// one mebibyte.
	Limit int64
	// Build makes the transport's reader from the answer.
	Build func(Stream) T
}

// OpenStream sends a request and returns the transport's reader over the
// answer.
//
// A failure to send, a status other than 200, and a 200 that carries a JSON
// error are all failures of this call: the reader exists only for a stream.
func OpenStream[T Reader](parent context.Context, opts OpenOptions[T]) (T, error) {
	var zero T

	// The timeout bounds the whole request, streaming included, so the
	// context is created before the request goes out. The parent stays
	// apart, so a timeout the transport imposed is told from the run's own
	// cancellation.
	ctx := parent
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, opts.Timeout)
	}
	release := func() {
		if cancel != nil {
			cancel()
		}
	}

	req, err := opts.Request(ctx)
	if err != nil {
		release()
		// A request builder that already classified its failure keeps what
		// it said: a credential that could not be resolved, or a run that
		// stopped while the credential was being read. A plain construction
		// failure is a network failure, because no request went out.
		var classified *poo.ProviderError
		if errors.As(err, &classified) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return zero, err
		}
		return zero, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}

	// The request is timed from the moment it goes out, so the time to the
	// first token includes the endpoint's own latency.
	started := time.Now()

	resp, err := opts.Client.Do(req)
	if err != nil {
		// The failure is classified before the request's context is
		// released, because releasing it would replace the failure with
		// the context's own error.
		perr := classifyFailure(opts.Classify, parent, ctx, resp, err)
		if resp != nil {
			_ = resp.Body.Close()
		}
		release()
		return zero, perr
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		// The body is read under the request's timeout, so the context is
		// released after it.
		defer release()
		return zero, ClassifyStatus(resp)
	}

	// A failed request answered with 200 and a JSON body is not a stream. The
	// first byte tells the two apart, so the stream is not buffered and the
	// time to the first token measures the endpoint's latency rather than the
	// reader's.
	br := bufio.NewReader(resp.Body)
	first, err := PeekFirstByte(br)
	if err != nil {
		resp.Body.Close()
		release()
		return zero, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
	}
	if first == '{' {
		defer resp.Body.Close()
		defer release()
		limit := opts.Limit
		if limit <= 0 {
			limit = defaultErrorBodyLimit
		}
		errBody, err := io.ReadAll(io.LimitReader(br, limit))
		if err != nil {
			return zero, &poo.ProviderError{Kind: poo.ProviderKindNetwork, Message: err.Error()}
		}
		if perr, ok := ErrorInBody(errBody, resp.StatusCode, resp.Header); ok {
			return zero, perr
		}
		return zero, &poo.ProviderError{
			Kind:    poo.ProviderKindServer,
			Message: "the endpoint returned a body that is not a stream",
			Status:  resp.StatusCode,
		}
	}

	return opts.Build(Stream{
		Body:    resp.Body,
		Reader:  br,
		Context: ctx,
		Parent:  parent,
		Cancel:  cancel,
		TTFT:    NewTTFT(started),
	}), nil
}

// classifyFailure reads a send's failure with the transport's own rule, or the
// package's.
func classifyFailure(classify func(parent, request context.Context, resp *http.Response, err error) error,
	parent, request context.Context, resp *http.Response, err error) error {
	if classify != nil {
		return classify(parent, request, resp, err)
	}
	return ClassifyTransportError(parent, request, err)
}
