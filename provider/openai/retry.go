package openai

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/sdougbrown/poo"
)

// The retry policy.
//
// The plan: three attempts per request, the first try and two retries,
// with a backoff of one second and then two. A Retry-After overrides
// the backoff, capped at thirty seconds, and a rate limit that asks for
// longer than that is not retried at all: it settles the run. The wait
// is cancellable, and a request is only retried while the attempt has
// carried no delta.

// retryPolicy is the plan's retry policy.
type retryPolicy struct {
	// attempts is the number of attempts one request gets.
	attempts int
	// backoff holds the wait before the second and the third attempt.
	backoff []time.Duration
	// maxRetryAfter is the longest wait a rate limit may ask for and
	// still be retried.
	maxRetryAfter time.Duration
}

// defaultRetryPolicy is the plan's policy: three attempts, a backoff of
// one second and then two, and a wait of thirty seconds at the most.
func defaultRetryPolicy() retryPolicy {
	return retryPolicy{
		attempts:      3,
		backoff:       []time.Duration{time.Second, 2 * time.Second},
		maxRetryAfter: poo.MaxRetryAfter,
	}
}

// delay returns the wait before the next attempt, and whether there is
// one.
//
// The failed attempt is the attempt number, counting from one. A
// failure that is not worth another attempt has no wait: the request
// settles with it.
func (p retryPolicy) delay(attempt int, err error) (time.Duration, bool) {
	if attempt >= p.attempts {
		return 0, false
	}
	var pe *poo.ProviderError
	if !errors.As(err, &pe) {
		return 0, false
	}
	// The failure's own rule decides: a rate limit is worth another
	// attempt only when the endpoint asked for a wait of thirty seconds
	// or less, a server failure and a network failure always are, and a
	// rejection never is.
	if !pe.Retryable() {
		return 0, false
	}
	wait := p.backoffWait(attempt)
	if pe.RetryAfter > 0 {
		wait = pe.RetryAfter
	}
	if wait > p.maxRetryAfter {
		wait = p.maxRetryAfter
	}
	return wait, true
}

// backoffWait returns the wait before the attempt after the failed one.
func (p retryPolicy) backoffWait(attempt int) time.Duration {
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if len(p.backoff) == 0 {
		return 0
	}
	if idx >= len(p.backoff) {
		idx = len(p.backoff) - 1
	}
	return p.backoff[idx]
}

// waitFor waits for the given time, or until the context ends.
//
// A cancelled wait ends the request: the caller sees the context's
// error, and the run settles as cancelled.
func waitFor(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Attempt is one attempt of a request that failed.
//
// The transport reports every attempt, so a consumer can write the
// attempt to the log and tell how a request ended.
type Attempt struct {
	// Number is the attempt's number, counting from one.
	Number int
	// Err is the failure the attempt ended with.
	Err error
	// Delay is how long the transport waited before the next attempt.
	Delay time.Duration
}

// reportAttempt tells the consumer about an attempt that failed.
func (t *Transport) reportAttempt(attempt int, err error, delay time.Duration) {
	if t.cfg.OnAttempt == nil {
		return
	}
	t.cfg.OnAttempt(Attempt{Number: attempt, Err: err, Delay: delay})
}

// openWithRetry opens the request and returns the reader of the attempt
// that answered.
//
// A failure that is worth another attempt is retried within the
// budget. The wait between the attempts is cancellable.
func (t *Transport) openWithRetry(ctx context.Context, body []byte) (*streamReader, int, error) {
	attempt := 0
	for {
		attempt++
		reader, err := t.open(ctx, body)
		if err == nil {
			return reader, attempt, nil
		}
		wait, ok := t.policy.delay(attempt, err)
		if !ok {
			return nil, attempt, err
		}
		t.reportAttempt(attempt, err, wait)
		if werr := waitFor(ctx, wait); werr != nil {
			return nil, attempt, werr
		}
	}
}

// streamWithRetry reads a response whose attempt may be retried.
//
// A failure before the response carried a delta is retried within the
// budget. A failure after a delta is returned as it is: the model has
// already seen part of the answer, so the request cannot be repeated.
type streamWithRetry struct {
	// transport opens the attempts.
	transport *Transport
	// ctx is the request's context.
	ctx context.Context
	// body is the body every attempt sends.
	body []byte
	// attempt is the number of the attempt in flight.
	attempt int
	// current is the reader of the attempt in flight.
	current *streamReader
	// seen reports that the attempt in flight has carried something the
	// model has seen: a delta or a tool call. Such an attempt is not
	// repeated, because the core assembles the calls by their index and
	// a repeated call would join the discarded one.
	seen bool
	// usage holds the usage of the last attempt that failed, so a
	// request that ends with a failure still reports what it cost.
	usage *poo.UsageReport
}

// Next returns the next event of the response.
//
// A failure that arrives before the response carried a delta is retried,
// so the caller sees the events of the attempt that answered.
func (s *streamWithRetry) Next() (poo.StreamEvent, error) {
	for {
		if s.current == nil {
			reader, err := s.transport.open(s.ctx, s.body)
			if err != nil {
				wait, ok := s.transport.policy.delay(s.attempt, err)
				if !ok {
					return poo.StreamEvent{}, err
				}
				s.transport.reportAttempt(s.attempt, err, wait)
				if werr := waitFor(s.ctx, wait); werr != nil {
					return poo.StreamEvent{}, werr
				}
				s.attempt++
				continue
			}
			s.current = reader
		}

		ev, err := s.current.Next()
		if err == nil {
			if ev.Kind == "delta" || ev.Kind == "tool_call" {
				s.seen = true
			}
			return ev, nil
		}
		if errors.Is(err, io.EOF) {
			return poo.StreamEvent{}, io.EOF
		}
		// The request's own context ended: the run is stopping, and
		// the failure is the context's.
		if s.ctx.Err() != nil {
			return poo.StreamEvent{}, err
		}
		if !s.seen {
			if wait, ok := s.transport.policy.delay(s.attempt, err); ok {
				s.transport.reportAttempt(s.attempt, err, wait)
				// The attempt failed before it carried anything, but it
				// may have spent tokens: the usage is kept.
				s.usage = s.current.ReportUsage()
				s.current.Close()
				s.current = nil
				if werr := waitFor(s.ctx, wait); werr != nil {
					return poo.StreamEvent{}, werr
				}
				s.attempt++
				continue
			}
		}
		return poo.StreamEvent{}, err
	}
}

// Close releases the response.
func (s *streamWithRetry) Close() error {
	if s.current == nil {
		return nil
	}
	return s.current.Close()
}

// ReportUsage returns the usage of the attempt in flight.
//
// A request that failed before its usage arrived still produced tokens,
// so the transport reports what it knows.
func (s *streamWithRetry) ReportUsage() *poo.UsageReport {
	if s.current == nil {
		return s.usage
	}
	return s.current.ReportUsage()
}
